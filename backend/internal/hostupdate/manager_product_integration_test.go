package hostupdate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This opt-in suite uses the published backend/web/Agent and production Compose.
// It creates an empty, internal-network-only project; no production env or data
// is copied. Only the already-cached image pull and Compose download are skipped.
func TestProductContainerRollback(t *testing.T) {
	if os.Getenv("CANVAS_HOSTUPDATE_PRODUCT_TEST") != "1" {
		t.Skip("set CANVAS_HOSTUPDATE_PRODUCT_TEST=1 with cached release images and CANVAS_HOSTUPDATE_PRODUCT_COMPOSE")
	}
	composeSource := os.Getenv("CANVAS_HOSTUPDATE_PRODUCT_COMPOSE")
	if composeSource == "" {
		t.Fatal("CANVAS_HOSTUPDATE_PRODUCT_COMPOSE must name the release Compose file")
	}
	version := os.Getenv("CANVAS_HOSTUPDATE_PRODUCT_VERSION")
	if !IsReleaseVersion(version) {
		t.Fatal("CANVAS_HOSTUPDATE_PRODUCT_VERSION must be a fixed release version")
	}
	repository := os.Getenv("CANVAS_HOSTUPDATE_PRODUCT_REPOSITORY")
	if repository == "" {
		t.Fatal("CANVAS_HOSTUPDATE_PRODUCT_REPOSITORY is required")
	}
	for _, fault := range []string{"migration", "startup-health", "readiness"} {
		t.Run(fault, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "canvas-product-"+randomID())
			manager, body := productContainerFixture(t, dir, composeSource, repository, version)
			t.Cleanup(func() {
				manager.runner = execRunner{dir: dir}
				if err := manager.compose(manager.composePath(), version, 2*time.Minute, nil, "down", "--volumes", "--remove-orphans"); err != nil {
					t.Errorf("clean isolated product project: %v", err)
				}
			})
			productCompose(t, manager, version, "up", "-d", "--pull", "never", "--wait", "--wait-timeout", "120")
			if err := manager.checkHealthOnce(manager.healthURL(), version); err != nil {
				t.Fatal(err)
			}
			productSQL(t, manager, version, "CREATE TABLE updater_evidence (marker text PRIMARY KEY); INSERT INTO updater_evidence VALUES ('initial')")
			productCompose(t, manager, version, "exec", "-T", "backend", "sh", "-c", "printf initial > /data/updater-evidence; test -f /data/.settings-key || head -c 32 /dev/urandom > /data/.settings-key")
			keyBefore := productCompose(t, manager, version, "exec", "-T", "backend", "sha256sum", "/data/.settings-key")
			graceful := false
			if fault == "readiness" {
				// Exercise a real 503 and the same recovery entry used after the
				// verifier times out, without spending ten minutes in its retry loop.
				productCompose(t, manager, version, "stop", "web", "backend", "yingce-agent")
				graceful = strings.Contains(productCompose(t, manager, version, "logs", "--no-color", "backend"), "backend stopped gracefully")
				backup, err := manager.createBackup(version)
				if err != nil {
					t.Fatal(err)
				}
				if err := replaceFile(manager.composePath(), manager.previousComposePath(), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := replaceFile(manager.envPath(), manager.previousEnvPath(), 0o600); err != nil {
					t.Fatal(err)
				}
				productCompose(t, manager, version, "up", "-d", "--wait", "--wait-timeout", "120")
				productSQL(t, manager, version, "DELETE FROM schema_migrations WHERE version = (SELECT max(version) FROM schema_migrations); INSERT INTO updater_evidence VALUES ('fault-only')")
				err = manager.checkHealthOnce(manager.healthURL(), version)
				if err == nil || !strings.Contains(err.Error(), "HTTP 503") {
					t.Fatalf("real readiness did not reject broken schema: %v", err)
				}
				t.Logf("real product readiness: %v", err)
				manager.failWithRollback(err, version, backup)
			} else {
				runner := &productContainerRunner{execRunner: execRunner{dir: dir}, manager: manager, version: version, fault: fault}
				manager.runner = runner
				if fault == "startup-health" {
					var target map[string]any
					if err := json.Unmarshal([]byte(body), &target); err != nil {
						t.Fatal(err)
					}
					services := target["services"].(map[string]any)
					services["backend"].(map[string]any)["environment"].(map[string]any)["CANVAS_DATABASE_DRIVER"] = "product-test-invalid-driver"
					encoded, _ := json.Marshal(target)
					body = string(encoded)
				}
				manager.httpClient.Transport = productHealthTransport{manager: manager, version: version, compose: body}
				manager.runUpdate(version, version)
				if !runner.injected || !runner.observedFailure {
					t.Fatalf("real product failure not exercised: injected=%v failed=%v detail=%s", runner.injected, runner.observedFailure, runner.failureDetail)
				}
				t.Logf("real product %s failure: %s", fault, runner.failureDetail)
				graceful = runner.graceful
			}
			if manager.state.Operation.Phase != PhaseRolledBack {
				t.Fatalf("phase=%s error=%s rollback=%s", manager.state.Operation.Phase, manager.state.Operation.Error, manager.state.Operation.RollbackError)
			}
			if err := manager.checkHealthOnce(manager.healthURL(), version); err != nil {
				t.Fatal(err)
			}
			if got := productSQL(t, manager, version, "SELECT marker FROM updater_evidence ORDER BY marker"); got != "initial\n" {
				t.Fatalf("fault writes survived rollback: %q", got)
			}
			if got := productCompose(t, manager, version, "exec", "-T", "backend", "cat", "/data/updater-evidence"); got != "initial" {
				t.Fatalf("data backup not restored: %q", got)
			}
			if got := productCompose(t, manager, version, "exec", "-T", "backend", "sha256sum", "/data/.settings-key"); got != keyBefore {
				t.Fatal("settings key changed during rollback")
			}
			if !graceful {
				t.Fatal("real backend did not report graceful shutdown")
			}
			t.Logf("published product fault=%s phase=%s project=%s; database, data, key and real readiness restored", fault, manager.state.Operation.Phase, composeProjectName(dir))
		})
	}
}

type productContainerRunner struct {
	execRunner
	manager         *Manager
	version         string
	fault           string
	injected        bool
	observedFailure bool
	failureDetail   string
	graceful        bool
}

// Keep every test container on the internal network. Probe the actual Web HTTP
// endpoint from the Agent container instead of publishing a host port, which
// Docker does not forward for an internal-only bridge on all engine versions.
type productHealthTransport struct {
	manager *Manager
	version string
	compose string
}

func (p productHealthTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Host != "product-health.invalid" {
		return (dockerFixtureTransport{compose: p.compose}).RoundTrip(request)
	}
	var output bytes.Buffer
	script := `fetch('http://web:3000/api/health/ready').then(async r => console.log(JSON.stringify({status:r.status,body:await r.text()}))).catch(e=>{console.error(e.message);process.exit(1)})`
	if err := p.manager.compose(p.manager.composePath(), p.version, 15*time.Second, &output, "exec", "-T", "yingce-agent", "node", "-e", script); err != nil {
		return nil, err
	}
	var result struct {
		Status int    `json:"status"`
		Body   string `json:"body"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: result.Status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(result.Body)), Request: request}, nil
}

func (r *productContainerRunner) Run(ctx context.Context, name string, args, env []string, stdout, stderr io.Writer) error {
	call := strings.Join(args, " ")
	if strings.HasSuffix(call, " pull backend web yingce-agent") {
		// verifyImages has already checked that all three cached images have
		// their real registry digests. The isolated network needs no registry.
		return nil
	}
	if r.fault == "migration" && strings.HasSuffix(call, " run --rm migrate") {
		r.injected = true
		if err := r.manager.compose(r.manager.composePath(), r.version, time.Minute, nil, "exec", "-T", "postgres", "psql", "-U", "canvas", "-d", "canvas", "-v", "ON_ERROR_STOP=1", "-c", "UPDATE schema_migrations SET checksum = 'product-test-corruption' WHERE version = (SELECT max(version) FROM schema_migrations); INSERT INTO updater_evidence VALUES ('fault-only')"); err != nil {
			return err
		}
		if err := r.manager.compose(r.manager.composePath(), r.version, time.Minute, nil, "run", "--rm", "--no-deps", "--pull", "never", "-T", "--user", "root", "--entrypoint", "sh", "backend", "-c", "printf fault > /data/updater-evidence"); err != nil {
			return err
		}
	}
	if r.fault == "startup-health" && strings.Contains(call, " up -d --remove-orphans") {
		r.injected = true
	}
	var diagnostic bytes.Buffer
	err := r.execRunner.Run(ctx, name, args, env, stdout, io.MultiWriter(stderr, &diagnostic))
	if err == nil && strings.HasSuffix(call, " stop web backend yingce-agent") {
		var logs bytes.Buffer
		if logErr := r.manager.compose(r.manager.composePath(), r.version, time.Minute, &logs, "logs", "--no-color", "backend"); logErr != nil {
			return logErr
		}
		r.graceful = r.graceful || strings.Contains(logs.String(), "backend stopped gracefully")
	}
	if err != nil && r.fault == "migration" && strings.HasSuffix(call, " run --rm migrate") {
		r.failureDetail = strings.TrimSpace(diagnostic.String())
		r.observedFailure = strings.Contains(r.failureDetail, "校验和不一致")
	}
	if err != nil && r.fault == "startup-health" && strings.Contains(call, " up -d --remove-orphans") {
		var logs bytes.Buffer
		if logErr := r.manager.compose(r.manager.composePath(), r.version, time.Minute, &logs, "logs", "--no-color", "backend"); logErr != nil {
			return logErr
		}
		r.failureDetail = strings.TrimSpace(logs.String())
		r.observedFailure = strings.Contains(r.failureDetail, "不支持的数据库驱动：product-test-invalid-driver")
	}
	return err
}

func productContainerFixture(t *testing.T, dir, source, repository, version string) (*Manager, string) {
	t.Helper()
	for _, path := range []string{dir, filepath.Join(dir, "state"), filepath.Join(dir, "backups"), filepath.Join(dir, "socket")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	images := immutableImageRefs(repository, version)
	env := fmt.Sprintf("CANVAS_IMAGE_TAG=%s\nCANVAS_BACKEND_IMAGE=%s\nCANVAS_WEB_IMAGE=%s\nCANVAS_YINGCE_AGENT_IMAGE=%s\nPOSTGRES_USER=canvas\nPOSTGRES_DB=canvas\nPOSTGRES_PASSWORD=%s\nDATABASE_URL=postgresql://canvas:%s@postgres:5432/canvas?sslmode=disable\nYINGCE_AGENT_TOKEN=%s\nCANVAS_UPDATER_TOKEN=%s\nCANVAS_UPDATER_SOCKET_DIR=%s\nCANVAS_SHUTDOWN_TIMEOUT=30s\n", strings.TrimPrefix(version, "v"), images.backend, images.web, images.agent, "isolated-"+filepath.Base(dir), "isolated-"+filepath.Base(dir), strings.Repeat("a", 64), strings.Repeat("b", 64), filepath.Join(dir, "socket"))
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(env), 0o600); err != nil {
		t.Fatal(err)
	}
	m := &Manager{config: Config{Repository: repository, InstallDir: dir, ComposeFile: "compose.json", EnvFile: ".env", StateDir: filepath.Join(dir, "state"), BackupDir: filepath.Join(dir, "backups"), HealthURL: "http://product-health.invalid/api/health/ready", StepTimeout: 3 * time.Minute, StableWindow: time.Millisecond}, runner: execRunner{dir: dir}, httpClient: &http.Client{Timeout: 20 * time.Second}, state: persistedState{Operation: Operation{Phase: PhasePreflight, FromVersion: version, TargetVersion: version}}}
	// Resolve the public Compose with fresh fixture-only env, then constrain it.
	var output, stderr bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := m.runner.Run(ctx, "docker", []string{"compose", "--project-name", composeProjectName(dir), "--env-file", m.envPath(), "-f", source, "config", "--format", "json"}, nil, &output, &stderr); err != nil {
		t.Fatalf("render release Compose: %v: %s", err, stderr.String())
	}
	var config map[string]any
	if err := json.Unmarshal(output.Bytes(), &config); err != nil {
		t.Fatal(err)
	}
	services := config["services"].(map[string]any)
	if len(services) != 6 {
		t.Fatal("expected the six-service managed release Compose")
	}
	for name, raw := range services {
		service := raw.(map[string]any)
		service["restart"] = "no"
		service["mem_limit"] = "512m"
		if name == "web" {
			delete(service, "ports")
		} else if _, exists := service["ports"]; exists {
			t.Fatal("only Web may publish a port")
		}
		volumes, _ := service["volumes"].([]any)
		for _, rawMount := range volumes {
			mount := rawMount.(map[string]any)
			if mount["type"] == "bind" && mount["source"] != filepath.Join(dir, "socket") {
				t.Fatal("refuse release Compose with a non-fixture host bind")
			}
		}
		if health, ok := service["healthcheck"].(map[string]any); ok {
			health["interval"] = "1s"
			health["start_period"] = "1s"
		}
	}
	for _, raw := range config["volumes"].(map[string]any) {
		volume := raw.(map[string]any)
		if !strings.HasPrefix(fmt.Sprint(volume["name"]), composeProjectName(dir)+"_") || volume["external"] == true {
			t.Fatal("refuse a volume outside the fixture project")
		}
	}
	for _, raw := range config["networks"].(map[string]any) {
		network := raw.(map[string]any)
		if network["external"] == true || !strings.HasPrefix(fmt.Sprint(network["name"]), composeProjectName(dir)+"_") {
			t.Fatal("refuse a network outside the fixture project")
		}
		network["internal"] = true
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.composePath(), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.verifyImages(version); err != nil {
		t.Fatalf("cached release image digest verification: %v", err)
	}
	m.httpClient.Transport = productHealthTransport{manager: m, version: version, compose: string(encoded)}
	return m, string(encoded)
}

func productCompose(t *testing.T, m *Manager, version string, args ...string) string {
	t.Helper()
	var output bytes.Buffer
	if err := m.compose(m.composePath(), version, 3*time.Minute, &output, args...); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func productSQL(t *testing.T, m *Manager, version, sql string) string {
	t.Helper()
	return productCompose(t, m, version, "exec", "-T", "postgres", "psql", "-U", "canvas", "-d", "canvas", "-v", "ON_ERROR_STOP=1", "-Atc", sql)
}
