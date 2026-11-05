package hostupdate

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// defaultBackupRetention 是默认保留的备份份数。每次更新都会生成一个包含数据库与
	// 数据目录的全量 ZIP，保留过多会耗尽服务器磁盘，保留过少则失去回退点。
	defaultBackupRetention = 3
	imagePruneTimeout      = 2 * time.Minute
)

// pruneRetainedArtifacts 在升级成功后执行保留策略：备份保留最近 N 份，业务镜像只保留
// 当前版本、上一版本，以及状态里记录的最后一次备份对应的版本。任何清理失败只写日志，
// 不能影响已经成功的升级结果。
func (m *Manager) pruneRetainedArtifacts(currentVersion, previousVersion string) {
	removedBackups, err := m.pruneBackups(m.config.BackupRetention)
	switch {
	case err != nil:
		m.recordRetentionLog("清理旧备份失败：" + safeOperationError(err))
	case len(removedBackups) > 0:
		m.recordRetentionLog("已清理旧备份：" + strings.Join(removedBackups, "、"))
	}
	if !m.config.PruneImages {
		return
	}
	keep := []string{currentVersion, previousVersion}
	if m.state.LastBackup != nil {
		keep = append(keep, m.state.LastBackup.Version)
	}
	removedImages, err := m.pruneImages(keep)
	switch {
	case err != nil:
		m.recordRetentionLog("清理旧镜像失败：" + safeOperationError(err))
	case len(removedImages) > 0:
		m.recordRetentionLog("已清理旧镜像：" + strings.Join(removedImages, "、"))
	}
}

// pruneBackups 按修改时间保留最近 keep 份备份，并始终保留状态里记录的最后一次备份。
func (m *Manager) pruneBackups(keep int) ([]string, error) {
	if keep < 1 {
		keep = 1
	}
	files, err := m.backupFiles()
	if err != nil {
		return nil, err
	}
	protected := ""
	if m.state.LastBackup != nil && m.state.LastBackup.Path != "" {
		protected = filepath.Base(m.state.LastBackup.Path)
	}
	removed := make([]string, 0, len(files))
	var failures []error
	for index, file := range files {
		if index < keep || file.Name() == protected {
			continue
		}
		path := filepath.Join(m.config.BackupDir, file.Name())
		if err := os.Remove(path); err != nil {
			failures = append(failures, err)
			continue
		}
		removed = append(removed, file.Name())
	}
	return removed, errors.Join(failures...)
}

func (m *Manager) backupFiles() ([]os.DirEntry, error) {
	entries, err := os.ReadDir(m.config.BackupDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	files := make([]os.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "backup-") || !strings.HasSuffix(name, ".zip") {
			continue
		}
		files = append(files, entry)
	}
	sort.SliceStable(files, func(i, j int) bool {
		left, leftErr := files[i].Info()
		right, rightErr := files[j].Info()
		if leftErr != nil || rightErr != nil {
			return files[i].Name() > files[j].Name()
		}
		return left.ModTime().After(right.ModTime())
	})
	return files, nil
}

// pruneImages 删除本仓库更早版本的业务镜像标签。仍被容器或回滚版本引用的镜像会删除
// 失败，这里直接跳过，交给 Docker 保护。
func (m *Manager) pruneImages(keepVersions []string) ([]string, error) {
	if !m.config.PruneImages {
		return nil, nil
	}
	repository := strings.TrimSpace(m.config.Repository)
	if repository == "" {
		return nil, nil
	}
	owner := strings.SplitN(repository, "/", 2)[0]
	if owner == "" {
		return nil, nil
	}
	prefix := "ghcr.io/" + owner + "/open-ai-canvas-"
	keep := make(map[string]bool, len(keepVersions))
	for _, version := range keepVersions {
		value := strings.TrimSpace(version)
		if value == "" {
			continue
		}
		keep[value] = true
		if !strings.HasPrefix(value, "v") {
			keep["v"+value] = true
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), imagePruneTimeout)
	defer cancel()
	var listing bytes.Buffer
	if err := m.runner.Run(ctx, "docker", []string{"image", "ls", "--format", "{{.Repository}}:{{.Tag}}"}, nil, &listing, io.Discard); err != nil {
		return nil, err
	}
	removed := make([]string, 0, 8)
	for _, line := range strings.Split(listing.String(), "\n") {
		reference := strings.TrimSpace(line)
		if !strings.HasPrefix(reference, prefix) {
			continue
		}
		index := strings.LastIndex(reference, ":")
		if index < 0 || index == len(reference)-1 {
			continue
		}
		tag := reference[index+1:]
		// 只处理正式版本标签，sha-<commit>、latest 等标签不在这里清理。
		if !IsReleaseVersion("v" + tag) {
			continue
		}
		if keep["v"+tag] || keep[tag] {
			continue
		}
		if err := m.runner.Run(ctx, "docker", []string{"image", "rm", reference}, nil, io.Discard, io.Discard); err != nil {
			continue
		}
		removed = append(removed, reference)
	}
	return removed, nil
}

func (m *Manager) recordRetentionLog(message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appendLogLocked(PhaseSucceeded, message)
	_ = m.saveStateLocked()
}
