package hostupdate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Registry operations are simulated; all container lifecycle, PostgreSQL dump /
// restore and data-volume operations use real Docker in a unique Compose project.
type dockerRollbackRunner struct {
	execRunner
	manager              *Manager
	fault                string
	calls                []string
	partialStartInjected bool
}

func (r *dockerRollbackRunner) Run(ctx context.Context, name string, args, env []string, stdout, stderr io.Writer) error {
	call := strings.Join(args, " ")
	r.calls = append(r.calls, call)
	if strings.Contains(call, " pull backend web yingce-agent") {
		if err := r.manager.compose(r.manager.composePath(), "v1.0.0", time.Minute, nil,
			"exec", "-T", "postgres", "psql", "-U", "canvas", "-d", "canvas", "-v", "ON_ERROR_STOP=1", "-c",
			"INSERT INTO evidence VALUES ('during-pull')"); err != nil {
			return err
		}
		return r.manager.compose(r.manager.composePath(), "v1.0.0", time.Minute, nil,
			"exec", "-T", "backend", "sh", "-c", "printf during-pull > /data/during-pull")
	}
	if strings.HasPrefix(call, "image inspect ghcr.io/") {
		repository := args[2][:strings.LastIndex(args[2], ":")]
		return json.NewEncoder(stdout).Encode([]string{repository + "@sha256:" + strings.Repeat("a", 64)})
	}
	if r.fault == "backup" || r.fault == "backup-start" {
		if strings.Contains(call, "tar backend -C /data -cf -") {
			return errors.New("injected backup failure")
		}
	}
	err := r.execRunner.Run(ctx, name, args, env, stdout, stderr)
	if err == nil && r.fault == "backup-start" && strings.Contains(call, " start backend yingce-agent web") {
		return errors.New("injected failure after old services restarted")
	}
	if err == nil && r.fault == "partial-start" && strings.Contains(call, " up -d --remove-orphans") {
		r.partialStartInjected = true
		return errors.New("injected failure after real rollback services started")
	}
	return err
}

func (r *dockerRollbackRunner) RunWithInput(ctx context.Context, name string, args, env []string, input io.Reader, stdout, stderr io.Writer) error {
	call := strings.Join(args, " ")
	r.calls = append(r.calls, call)
	if r.fault == "database" && strings.Contains(call, "pg_restore") {
		return errors.New("injected database restore failure")
	}
	if r.fault == "data" && strings.Contains(call, "tar backend -C /data -xf -") {
		return errors.New("injected data restore failure")
	}
	return r.execRunner.RunWithInput(ctx, name, args, env, input, stdout, stderr)
}

type dockerFixtureTransport struct{ compose string }

func (t dockerFixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == "raw.githubusercontent.com" {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(t.compose)), Request: req}, nil
	}
	return http.DefaultTransport.RoundTrip(req)
}

func TestDockerRollback(t *testing.T) {
	if os.Getenv("CANVAS_HOSTUPDATE_DOCKER_TEST") != "1" {
		t.Skip("set CANVAS_HOSTUPDATE_DOCKER_TEST=1 to run isolated Docker/PostgreSQL fault injection")
	}
	image := "postgres:17-alpine"
	for _, fault := range []string{"migration", "backup", "backup-start", "database", "data", "partial-start"} {
		t.Run(fault, func(t *testing.T) {
			installDir := filepath.Join(t.TempDir(), "canvas-updater-test-"+randomID())
			for _, dir := range []string{installDir, filepath.Join(installDir, "state"), filepath.Join(installDir, "backups")} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			compose := dockerRollbackFixture(image)
			for name, body := range map[string]string{"compose.yml": compose, ".env": "CANVAS_IMAGE_TAG=1.0.0\nPOSTGRES_USER=canvas\nPOSTGRES_DB=canvas\n"} {
				if err := os.WriteFile(filepath.Join(installDir, name), []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			manager := &Manager{
				config: Config{Repository: "fixture/canvas", InstallDir: installDir, ComposeFile: "compose.yml", EnvFile: ".env", StateDir: filepath.Join(installDir, "state"), BackupDir: filepath.Join(installDir, "backups"), StepTimeout: time.Minute, StableWindow: time.Millisecond},
				runner: execRunner{dir: installDir},
				state:  persistedState{Operation: Operation{Phase: PhasePreflight, FromVersion: "v1.0.0", TargetVersion: "v1.0.1"}},
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				_, _ = io.WriteString(w, `{"code":0,"data":{"build":{"version":"v1.0.0"}}}`)
			}))
			defer server.Close()
			manager.config.HealthURL = server.URL
			manager.httpClient = &http.Client{Transport: dockerFixtureTransport{compose: compose}, Timeout: time.Second}
			t.Cleanup(func() {
				manager.runner = execRunner{dir: installDir}
				if err := manager.compose(manager.composePath(), "v1.0.0", time.Minute, nil, "down", "--volumes", "--remove-orphans"); err != nil {
					t.Errorf("clean isolated project %s: %v", composeProjectName(installDir), err)
				}
			})
			if err := manager.compose(manager.composePath(), "v1.0.0", time.Minute, nil, "up", "-d", "--pull", "never", "--wait", "postgres"); err != nil {
				t.Fatal(err)
			}
			if err := manager.compose(manager.composePath(), "v1.0.0", time.Minute, nil, "exec", "-T", "postgres", "psql", "-U", "canvas", "-d", "canvas", "-v", "ON_ERROR_STOP=1", "-c", "CREATE TABLE evidence (marker text PRIMARY KEY); INSERT INTO evidence VALUES ('initial')"); err != nil {
				t.Fatal(err)
			}
			if err := manager.compose(manager.composePath(), "v1.0.0", time.Minute, nil, "up", "-d", "--pull", "never", "--wait", "backend", "web", "yingce-agent"); err != nil {
				t.Fatal(err)
			}
			runner := &dockerRollbackRunner{execRunner: execRunner{dir: installDir}, manager: manager, fault: fault}
			manager.runner = runner
			manager.runUpdate("v1.0.0", "v1.0.1")
			wantPhase := PhaseManualIntervention
			if fault == "migration" {
				wantPhase = PhaseRolledBack
			} else if fault == "backup" {
				wantPhase = PhaseFailed
			}
			if manager.state.Operation.Phase != wantPhase {
				t.Fatalf("phase=%s want=%s error=%s rollback=%s", manager.state.Operation.Phase, wantPhase, manager.state.Operation.Error, manager.state.Operation.RollbackError)
			}
			if fault == "partial-start" && !runner.partialStartInjected {
				t.Fatal("rollback services did not reach the partial-start injection point")
			}
			var running bytes.Buffer
			if err := manager.compose(manager.composePath(), "v1.0.0", time.Minute, &running, "ps", "--status", "running", "--services"); err != nil {
				t.Fatal(err)
			}
			for _, service := range []string{"backend", "web", "yingce-agent"} {
				if strings.Contains("\n"+running.String(), "\n"+service+"\n") != (fault == "migration" || fault == "backup") {
					t.Fatalf("unexpected running services after %s: %s", fault, running.String())
				}
			}
			if fault == "migration" || fault == "backup" {
				var rows bytes.Buffer
				if err := manager.compose(manager.composePath(), "v1.0.0", time.Minute, &rows, "exec", "-T", "postgres", "psql", "-U", "canvas", "-d", "canvas", "-Atc", "SELECT marker FROM evidence ORDER BY marker"); err != nil {
					t.Fatal(err)
				}
				if rows.String() != "during-pull\ninitial\nshutdown\n" {
					t.Fatalf("committed database writes missing or failed migration leaked: %q", rows.String())
				}
				var data bytes.Buffer
				if err := manager.compose(manager.composePath(), "v1.0.0", time.Minute, &data, "exec", "-T", "backend", "sh", "-c", "test ! -e /data/migration-only && test -f /data/during-pull && test -f /data/shutdown && cat /data/.settings-key"); err != nil || data.String() != "original-key" {
					t.Fatalf("data snapshot not restored: output=%q err=%v", data.String(), err)
				}
			}
			if fault == "backup" || fault == "backup-start" {
				for _, call := range runner.calls {
					if strings.Contains(call, "pg_restore") || strings.Contains(call, " run --rm migrate") || strings.Contains(call, "dropdb") {
						t.Fatalf("backup failure performed migration or database restore: %s", call)
					}
				}
			}
			t.Logf("real Docker/PostgreSQL fault=%s phase=%s project=%s", fault, manager.state.Operation.Phase, composeProjectName(installDir))
		})
	}
}

func dockerRollbackFixture(image string) string {
	return fmt.Sprintf(`services:
  postgres:
    image: %[1]s
    environment: {POSTGRES_USER: canvas, POSTGRES_DB: canvas, POSTGRES_HOST_AUTH_METHOD: trust}
    volumes: ["postgres-data:/var/lib/postgresql/data"]
    mem_limit: 256m
    healthcheck:
      test: ["CMD", "pg_isready", "-U", "canvas", "-d", "canvas"]
      interval: 1s
      timeout: 3s
      retries: 30
  backend:
    image: %[1]s
    entrypoint: ["sh", "-c"]
    command:
      - |
        test -f /data/.settings-key || printf original-key > /data/.settings-key
        trap 'psql -h postgres -U canvas -d canvas -v ON_ERROR_STOP=1 -c "INSERT INTO evidence VALUES ('\''shutdown'\'') ON CONFLICT DO NOTHING"; printf shutdown > /data/shutdown; exit 0' TERM
        while :; do sleep 1 & wait $$!; done
    volumes: ["backend-data:/data"]
    depends_on:
      migrate: {condition: service_completed_successfully}
    stop_signal: SIGTERM
    stop_grace_period: 10s
    mem_limit: 64m
  web:
    image: %[1]s
    entrypoint: ["sleep", "infinity"]
    mem_limit: 32m
  yingce-agent:
    image: %[1]s
    entrypoint: ["sleep", "infinity"]
    mem_limit: 32m
  migrate:
    image: %[1]s
    environment: {FIXTURE_VERSION: "${CANVAS_IMAGE_TAG}"}
    entrypoint: ["sh", "-c"]
    command: ["test \"$$FIXTURE_VERSION\" = 1.0.0 && exit 0; psql -h postgres -U canvas -d canvas -v ON_ERROR_STOP=1 -c \"INSERT INTO evidence VALUES ('migration-only')\" && printf mutated > /data/.settings-key && touch /data/migration-only && exit 1"]
    volumes: ["backend-data:/data"]
    mem_limit: 64m
volumes:
  postgres-data:
  backend-data:
`, image)
}
