package hostupdate

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// imageListingRunner 只应答 docker image ls / rm，用于验证镜像保留策略。
type imageListingRunner struct {
	listing string
	calls   [][]string
	inUse   map[string]bool
}

func (r *imageListingRunner) Run(_ context.Context, _ string, args, _ []string, stdout, _ io.Writer) error {
	r.calls = append(r.calls, append([]string(nil), args...))
	if len(args) >= 2 && args[0] == "image" && args[1] == "ls" {
		if stdout != nil {
			_, _ = io.WriteString(stdout, r.listing)
		}
		return nil
	}
	if len(args) >= 3 && args[0] == "image" && args[1] == "rm" && r.inUse[args[2]] {
		return errors.New("image is being used by a running container")
	}
	return nil
}

func TestSelectLatestReleaseSkipsDraftAndPrerelease(t *testing.T) {
	releases := []githubRelease{
		{TagName: "v1.7.3.13", Name: "稳定版"},
		{TagName: "v1.7.4.0-rc1", Prerelease: true},
		{TagName: "v1.7.3.14", Draft: true},
		{TagName: "nightly", Name: "非版本标签"},
		{TagName: "v1.7.3.12"},
	}
	latest, err := selectLatestRelease(releases)
	if err != nil {
		t.Fatal(err)
	}
	if latest.TagName != "v1.7.3.13" {
		t.Fatalf("latest = %q, want v1.7.3.13", latest.TagName)
	}
}

func TestSelectLatestReleaseRejectsOnlyPrerelease(t *testing.T) {
	_, err := selectLatestRelease([]githubRelease{{TagName: "v1.7.4.0-rc1", Prerelease: true}})
	if err == nil {
		t.Fatal("prerelease-only release list was accepted")
	}
}

func TestPruneBackupsKeepsRetentionAndProtectedBackup(t *testing.T) {
	backupDir := t.TempDir()
	names := []string{
		"backup-20261101-000000.zip",
		"backup-20261102-000000.zip",
		"backup-20261103-000000.zip",
		"backup-20261104-000000.zip",
		"backup-20261105-000000.zip",
	}
	base := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for index, name := range names {
		path := filepath.Join(backupDir, name)
		if err := os.WriteFile(path, []byte("zip"), 0o600); err != nil {
			t.Fatal(err)
		}
		stamp := base.Add(time.Duration(index) * 24 * time.Hour)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	manager := &Manager{
		config: Config{BackupDir: backupDir, BackupRetention: 3},
		// 最旧那份是当前回退点，即使超出保留份数也不能删除。
		state: persistedState{LastBackup: &Backup{ID: "backup-20261101-000000", Path: filepath.Join(backupDir, names[0])}},
	}
	removed, err := manager.pruneBackups(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != "backup-20261102-000000.zip" {
		t.Fatalf("removed = %v, want only the second-oldest backup", removed)
	}
	for _, name := range []string{names[0], names[2], names[3], names[4]} {
		if _, err := os.Stat(filepath.Join(backupDir, name)); err != nil {
			t.Fatalf("%s should be retained: %v", name, err)
		}
	}
}

func TestPruneImagesKeepsCurrentRollbackAndNonVersionTags(t *testing.T) {
	runner := &imageListingRunner{
		listing: strings.Join([]string{
			"ghcr.io/anlongvip/open-ai-canvas-backend:1.7.3.9",
			"ghcr.io/anlongvip/open-ai-canvas-web:1.7.3.9",
			"ghcr.io/anlongvip/open-ai-canvas-backend:1.7.3.8",
			"ghcr.io/anlongvip/open-ai-canvas-backend:1.7.3.7",
			"ghcr.io/anlongvip/open-ai-canvas-backend:sha-5509b17",
			"ghcr.io/anlongvip/open-ai-canvas-backend:latest",
			"ghcr.io/ddcat-ai/open-ai-canvas-backend:1.7.3.6",
		}, "\n"),
		inUse: map[string]bool{"ghcr.io/anlongvip/open-ai-canvas-backend:1.7.3.8": true},
	}
	manager := &Manager{
		config: Config{Repository: "anlongvip/zm-ai-canvas", PruneImages: true},
		runner: runner,
	}
	removed, err := manager.pruneImages([]string{"v1.7.3.9", "v1.7.3.8"})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != "ghcr.io/anlongvip/open-ai-canvas-backend:1.7.3.7" {
		t.Fatalf("removed = %v, want only the stale version tag", removed)
	}
	for _, call := range runner.calls {
		joined := strings.Join(call, " ")
		if strings.Contains(joined, "1.7.3.9") || strings.Contains(joined, "1.7.3.8") ||
			strings.Contains(joined, "sha-5509b17") || strings.Contains(joined, "latest") ||
			strings.Contains(joined, "ddcat-ai") {
			t.Fatalf("retained image was pruned: %v", call)
		}
	}
}

func TestPruneImagesDisabledByConfig(t *testing.T) {
	runner := &imageListingRunner{listing: "ghcr.io/anlongvip/open-ai-canvas-backend:1.7.3.7"}
	manager := &Manager{
		config: Config{Repository: "anlongvip/zm-ai-canvas"},
		runner: runner,
	}
	if _, err := manager.pruneImages([]string{"v1.7.3.9"}); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("prune ran while disabled: %v", runner.calls)
	}
}
