package hostupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type rollbackTestRunner struct {
	calls                []string
	marker               string
	backupFailure        bool
	oldStartFailure      bool
	rollbackStartFailure bool
	databaseFailure      bool
	dataFailure          bool
	databaseRestored     []byte
	dataRestored         []byte
	dataCleaned          bool
	servicesStarted      bool
	dataBeforeStart      bool
}

func (r *rollbackTestRunner) Run(_ context.Context, _ string, args, _ []string, stdout, _ io.Writer) error {
	joined := strings.Join(args, " ")
	r.calls = append(r.calls, joined)
	switch {
	case strings.Contains(joined, " pull backend web yingce-agent"):
		r.marker = "committed-during-image-download"
	case strings.Contains(joined, "image inspect"):
		image := args[2]
		repository := image[:strings.LastIndex(image, ":")]
		data, _ := json.Marshal([]string{repository + "@sha256:" + strings.Repeat("a", 64)})
		_, _ = stdout.Write(data)
	case strings.Contains(joined, "pg_dump"):
		_, _ = io.WriteString(stdout, "database:"+r.marker)
	case strings.Contains(joined, "tar backend -C /data -cf - ."):
		if r.backupFailure {
			return errors.New("injected backend-data backup failure")
		}
		writer := tar.NewWriter(stdout)
		content := []byte(r.marker)
		if err := writer.WriteHeader(&tar.Header{Name: "./committed.txt", Mode: 0o600, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		if _, err := writer.Write(content); err != nil {
			return err
		}
		if err := writer.Close(); err != nil {
			return err
		}
	case strings.Contains(joined, " run --rm migrate"):
		return errors.New("injected migration failure")
	case strings.Contains(joined, " stop web backend yingce-agent"):
		r.dataCleaned = false
		r.servicesStarted = false
	case strings.Contains(joined, " start backend yingce-agent web") && r.oldStartFailure:
		return errors.New("injected old service startup failure")
	case strings.Contains(joined, "entry in /data/"):
		r.dataCleaned = true
	case strings.Contains(joined, " up -d --remove-orphans"):
		r.servicesStarted = true
		r.dataBeforeStart = !r.dataFailure && !r.databaseFailure && len(r.databaseRestored) > 0 && len(r.dataRestored) > 0
		if r.rollbackStartFailure {
			return errors.New("injected partial rollback service startup failure")
		}
	}
	return nil
}

func (r *rollbackTestRunner) RunWithInput(_ context.Context, _ string, args, _ []string, input io.Reader, _, _ io.Writer) error {
	joined := strings.Join(args, " ")
	r.calls = append(r.calls, joined)
	data, err := io.ReadAll(input)
	if err != nil {
		return err
	}
	switch {
	case strings.Contains(joined, "pg_restore"):
		if r.databaseFailure {
			return errors.New("injected database restore failure")
		}
		r.databaseRestored = data
	case strings.Contains(joined, "tar backend -C /data -xf -"):
		if r.dataFailure {
			return errors.New("injected data restore failure")
		}
		r.dataRestored = data
	case strings.Contains(joined, "tar backend -C /data"):
		return errors.New("unexpected data restore command")
	}
	return nil
}

type releaseTestTransport struct{ base http.RoundTripper }

func (t releaseTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Host == "raw.githubusercontent.com" {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("services:\n  backend:\n    image: test\n")),
			Request:    request,
		}, nil
	}
	return t.base.RoundTrip(request)
}

func newRollbackTestManager(t *testing.T, runner *rollbackTestRunner) (*Manager, func()) {
	t.Helper()
	installDir := t.TempDir()
	stateDir := filepath.Join(installDir, "state")
	backupDir := filepath.Join(installDir, "backups")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	composePath := filepath.Join(installDir, "docker-compose.deploy.yml")
	envPath := filepath.Join(installDir, ".env")
	if err := os.WriteFile(composePath, []byte("services:\n  backend:\n    image: old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envPath, []byte("CANVAS_IMAGE_TAG=1.0.0\nPOSTGRES_USER=canvas\nPOSTGRES_DB=canvas\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/health" {
			http.NotFound(w, request)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"data":{"build":{"version":"v1.0.0"}}}`)
	}))
	manager := &Manager{
		config: Config{
			Repository:   "example/canvas",
			InstallDir:   installDir,
			ComposeFile:  "docker-compose.deploy.yml",
			EnvFile:      ".env",
			StateDir:     stateDir,
			BackupDir:    backupDir,
			HealthURL:    server.URL + "/health",
			StableWindow: time.Millisecond,
			StepTimeout:  time.Minute,
		},
		runner: runner,
		httpClient: &http.Client{
			Transport: releaseTestTransport{base: http.DefaultTransport},
			Timeout:   time.Second,
		},
		state: persistedState{Operation: Operation{Phase: PhasePreflight, FromVersion: "v1.0.0", TargetVersion: "v1.0.1"}},
	}
	return manager, server.Close
}

func TestUpdateMigrationFailureRestoresFinalDatabaseAndBackendDataSnapshot(t *testing.T) {
	runner := &rollbackTestRunner{marker: "before-image-download"}
	manager, closeServer := newRollbackTestManager(t, runner)
	defer closeServer()

	manager.runUpdate("v1.0.0", "v1.0.1")

	if manager.state.Operation.Phase != PhaseRolledBack {
		t.Fatalf("operation phase = %q, want rolled_back; operation=%#v", manager.state.Operation.Phase, manager.state.Operation)
	}
	if manager.state.LastBackup == nil {
		t.Fatal("final backup was not retained")
	}
	if !bytes.Contains(runner.databaseRestored, []byte("database:committed-during-image-download")) {
		t.Fatalf("restored database missed committed write during image download: %q", runner.databaseRestored)
	}
	archive := tar.NewReader(bytes.NewReader(runner.dataRestored))
	header, err := archive.Next()
	if err != nil {
		t.Fatalf("read restored backend data archive: %v", err)
	}
	content, err := io.ReadAll(archive)
	if err != nil {
		t.Fatal(err)
	}
	if header.Name != "./committed.txt" || string(content) != "committed-during-image-download" {
		t.Fatalf("restored backend data = %q:%q", header.Name, content)
	}
	if !runner.dataBeforeStart {
		t.Fatal("old services started before database and backend data restoration completed")
	}
	assertCallOrder(t, runner.calls,
		"stop web backend yingce-agent",
		"pg_dump",
		"run --rm migrate",
		"pg_restore",
		"entry in /data/",
		"tar backend -C /data -xf -",
		"up -d --remove-orphans",
	)
}

func TestBackupFailureRestartsOldServicesWithoutDatabaseOrDataRestore(t *testing.T) {
	runner := &rollbackTestRunner{marker: "committed-during-image-download", backupFailure: true}
	manager, closeServer := newRollbackTestManager(t, runner)
	defer closeServer()

	manager.runUpdate("v1.0.0", "v1.0.1")

	if manager.state.Operation.Phase != PhaseFailed {
		t.Fatalf("operation phase = %q, want failed; operation=%#v", manager.state.Operation.Phase, manager.state.Operation)
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "dropdb") || strings.Contains(call, "pg_restore") || strings.Contains(call, " -xf -") || strings.Contains(call, "run --rm migrate") {
			t.Fatalf("backup failure must not restore or migrate: %s", call)
		}
	}
	if runner.dataCleaned || runner.servicesStarted {
		t.Fatalf("backup failure changed backend data or used target up path: cleaned=%t started=%t", runner.dataCleaned, runner.servicesStarted)
	}
	if !hasCall(runner.calls, "start backend yingce-agent web") {
		t.Fatal("old services were not restarted after backup failure")
	}
}

func TestBackupFailureAndOldServiceStartFailureRequireManualIntervention(t *testing.T) {
	runner := &rollbackTestRunner{marker: "committed-during-image-download", backupFailure: true, oldStartFailure: true}
	manager, closeServer := newRollbackTestManager(t, runner)
	defer closeServer()

	manager.runUpdate("v1.0.0", "v1.0.1")

	if manager.state.Operation.Phase != PhaseManualIntervention {
		t.Fatalf("operation phase = %q, want manual_intervention; operation=%#v", manager.state.Operation.Phase, manager.state.Operation)
	}
	if !strings.Contains(manager.state.Operation.Error, "恢复旧服务") {
		t.Fatalf("manual intervention error does not identify old service recovery: %q", manager.state.Operation.Error)
	}
	if runner.servicesStarted || !hasCall(runner.calls, "start backend yingce-agent web") {
		t.Fatalf("unexpected service start state: started=%t calls=%#v", runner.servicesStarted, runner.calls)
	}
	if !strings.Contains(runner.calls[len(runner.calls)-1], "stop web backend yingce-agent") {
		t.Fatalf("failed old service restart did not stop partial services: %#v", runner.calls)
	}
}

func TestDatabaseRestoreFailureKeepsBusinessServicesStopped(t *testing.T) {
	runner := &rollbackTestRunner{marker: "committed-during-image-download", databaseFailure: true}
	manager, closeServer := newRollbackTestManager(t, runner)
	defer closeServer()

	manager.runUpdate("v1.0.0", "v1.0.1")

	if manager.state.Operation.Phase != PhaseManualIntervention {
		t.Fatalf("operation phase = %q, want manual_intervention; operation=%#v", manager.state.Operation.Phase, manager.state.Operation)
	}
	if runner.dataCleaned || runner.servicesStarted || hasCall(runner.calls, "-xf -") || hasCall(runner.calls, " up -d --remove-orphans") {
		t.Fatalf("database restore failure proceeded to data restore or service start: %#v", runner)
	}
}

func TestBackendDataRestoreFailureKeepsBusinessServicesStopped(t *testing.T) {
	runner := &rollbackTestRunner{marker: "committed-during-image-download", dataFailure: true}
	manager, closeServer := newRollbackTestManager(t, runner)
	defer closeServer()

	manager.runUpdate("v1.0.0", "v1.0.1")

	if manager.state.Operation.Phase != PhaseManualIntervention {
		t.Fatalf("operation phase = %q, want manual_intervention; operation=%#v", manager.state.Operation.Phase, manager.state.Operation)
	}
	if !runner.dataCleaned || runner.servicesStarted || !hasCall(runner.calls, "tar backend -C /data -xf -") || hasCall(runner.calls, " up -d --remove-orphans") {
		t.Fatalf("data restore failure proceeded to service start: %#v", runner)
	}
}

func TestRollbackStartupFailureStopsPartiallyStartedBusinessServices(t *testing.T) {
	runner := &rollbackTestRunner{marker: "committed-during-image-download", rollbackStartFailure: true}
	manager, closeServer := newRollbackTestManager(t, runner)
	defer closeServer()

	manager.runUpdate("v1.0.0", "v1.0.1")

	if manager.state.Operation.Phase != PhaseManualIntervention {
		t.Fatalf("operation phase = %q, want manual_intervention; operation=%#v", manager.state.Operation.Phase, manager.state.Operation)
	}
	if runner.servicesStarted || len(runner.calls) == 0 || !strings.Contains(runner.calls[len(runner.calls)-1], "stop web backend yingce-agent") {
		t.Fatalf("partially started services were not stopped after rollback startup failure: started=%t calls=%#v", runner.servicesStarted, runner.calls)
	}
}

func TestValidateBackendDataTarRejectsPathTraversal(t *testing.T) {
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	content := []byte("outside")
	if err := writer.WriteHeader(&tar.Header{Name: "../../outside.txt", Mode: 0o600, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := validateBackendDataTar(bytes.NewReader(buffer.Bytes())); err == nil {
		t.Fatal("path traversal entry was accepted")
	}
}

func TestValidateBackendDataBackupRejectsPathTraversal(t *testing.T) {
	var tarBuffer bytes.Buffer
	writer := tar.NewWriter(&tarBuffer)
	if err := writer.WriteHeader(&tar.Header{Name: "../../outside.txt", Mode: 0o600, Size: 1, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	backup := writeTestBackup(t, t.TempDir(), tarBuffer.Bytes())
	if err := validateBackendDataBackup(backup); err == nil {
		t.Fatal("backup containing path traversal was accepted")
	}
}

func TestValidateBackendDataTarLinks(t *testing.T) {
	for _, test := range []struct {
		name    string
		headers []tar.Header
		valid   bool
	}{
		{"contained links", []tar.Header{{Name: "file", Typeflag: tar.TypeReg}, {Name: "alias", Typeflag: tar.TypeSymlink, Linkname: "file"}, {Name: "hard", Typeflag: tar.TypeLink, Linkname: "file"}}, true},
		{"link chain parent traversal", []tar.Header{{Name: "a", Typeflag: tar.TypeSymlink, Linkname: "."}, {Name: "b", Typeflag: tar.TypeSymlink, Linkname: "a/../outside"}}, false},
		{"link before child", []tar.Header{{Name: "alias", Typeflag: tar.TypeSymlink, Linkname: "dir"}, {Name: "alias/file", Typeflag: tar.TypeReg}}, false},
		{"link after child", []tar.Header{{Name: "alias/file", Typeflag: tar.TypeReg}, {Name: "alias", Typeflag: tar.TypeSymlink, Linkname: "dir"}}, false},
		{"hard link to symlink", []tar.Header{{Name: "alias", Typeflag: tar.TypeSymlink, Linkname: "file"}, {Name: "hard", Typeflag: tar.TypeLink, Linkname: "alias"}}, false},
		{"duplicate path", []tar.Header{{Name: "file", Typeflag: tar.TypeReg}, {Name: "./file", Typeflag: tar.TypeSymlink, Linkname: "target"}}, false},
		{"absolute link", []tar.Header{{Name: "alias", Typeflag: tar.TypeSymlink, Linkname: "/outside"}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var buffer bytes.Buffer
			writer := tar.NewWriter(&buffer)
			for _, header := range test.headers {
				if err := writer.WriteHeader(&header); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			err := validateBackendDataTar(bytes.NewReader(buffer.Bytes()))
			if (err == nil) != test.valid {
				t.Fatalf("valid=%t error=%v", test.valid, err)
			}
		})
	}
}

func hasCall(calls []string, fragment string) bool {
	for _, call := range calls {
		if strings.Contains(call, fragment) {
			return true
		}
	}
	return false
}

func assertCallOrder(t *testing.T, calls []string, fragments ...string) {
	t.Helper()
	start := 0
	for _, fragment := range fragments {
		found := -1
		for index := start; index < len(calls); index++ {
			if strings.Contains(calls[index], fragment) {
				found = index
				break
			}
		}
		if found < 0 {
			t.Fatalf("call %q missing after index %d; calls=%#v", fragment, start, calls)
		}
		start = found + 1
	}
}

func writeTestBackup(t *testing.T, directory string, dataTar []byte) Backup {
	t.Helper()
	path := filepath.Join(directory, "rollback-fixture.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, content := range map[string][]byte{
		"metadata.json":    []byte(`{"version":"v1.0.0"}`),
		"database.dump":    []byte("database snapshot"),
		"backend-data.tar": dataTar,
	} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	return Backup{ID: "rollback-fixture", Path: path, Checksum: "sha256:" + hex.EncodeToString(hash[:]), Version: "v1.0.0"}
}
