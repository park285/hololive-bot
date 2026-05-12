// Package logging: 공통 로깅 유틸리티를 제공합니다.
// slog + tint 기반의 구조화된 로깅, archive-aware file rotation, OpenTelemetry 상관관계를 지원합니다.
package logging

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

type archiveAwareWriter struct {
	inner    io.Writer
	archiver *compressedLogArchiver
}

func (w *archiveAwareWriter) Write(p []byte) (int, error) {
	n, err := w.inner.Write(p)
	if w.archiver != nil {
		w.archiver.Trigger()
	}
	if err != nil {
		return n, fmt.Errorf("archive aware writer: write: %w", err)
	}
	return n, nil
}

type compressedLogArchiver struct {
	logPath    string
	maxBackups int
	maxAgeDays int

	mu      sync.Mutex
	running bool
	lastRun time.Time
}

func newCompressedLogArchiver(logPath string, maxBackups, maxAgeDays int, enabled bool) *compressedLogArchiver {
	if !enabled || strings.TrimSpace(logPath) == "" {
		return nil
	}

	return &compressedLogArchiver{
		logPath:    logPath,
		maxBackups: maxBackups,
		maxAgeDays: maxAgeDays,
	}
}

func (a *compressedLogArchiver) Trigger() {
	if a == nil {
		return
	}

	a.mu.Lock()
	if a.running || (!a.lastRun.IsZero() && time.Since(a.lastRun) < archiveScanInterval) {
		a.mu.Unlock()
		return
	}
	a.running = true
	a.lastRun = time.Now()
	a.mu.Unlock()

	err := archiveCompressedLogFiles(a.logPath, a.maxBackups, a.maxAgeDays)

	a.mu.Lock()
	a.running = false
	a.mu.Unlock()

	if err != nil {
		fmt.Fprintf(os.Stderr, "log archive warning: path=%s err=%v\n", a.logPath, err)
	}
}

type archivedLogFile struct {
	path      string
	timestamp time.Time
}

func archiveCompressedLogFiles(logPath string, maxBackups, maxAgeDays int) error {
	logDir := filepath.Dir(logPath)
	archiveDir := filepath.Join(logDir, archiveDirName)
	if err := ensureLogDirPerm(archiveDir); err != nil {
		return fmt.Errorf("prepare archive dir: %w", err)
	}

	names, err := matchingCompressedBackupNames(logDir, filepath.Base(logPath))
	if err != nil {
		return fmt.Errorf("list compressed backups: %w", err)
	}

	for _, name := range names {
		source := filepath.Join(logDir, name)
		target := filepath.Join(archiveDir, name)
		if err := os.Rename(source, target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("move compressed backup %s: %w", name, err)
		}
	}

	if err := pruneArchivedCompressedBackups(archiveDir, filepath.Base(logPath), maxBackups, maxAgeDays); err != nil {
		return fmt.Errorf("prune archived backups: %w", err)
	}

	return nil
}

func matchingCompressedBackupNames(dir, baseName string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("matching compressed backup names: read dir: %w", err)
	}

	prefix, ext := backupPrefixAndExt(baseName)
	suffix := ext + compressSuffix
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}
		names = append(names, name)
	}

	slices.Sort(names)
	return names, nil
}

func pruneArchivedCompressedBackups(archiveDir, baseName string, maxBackups, maxAgeDays int) error {
	files, err := archivedCompressedBackups(archiveDir, baseName)
	if err != nil {
		return err
	}

	removeByPath := make(map[string]struct{})
	if maxAgeDays > 0 {
		cutoff := time.Now().Add(-time.Duration(maxAgeDays) * 24 * time.Hour)
		for _, file := range files {
			if file.timestamp.Before(cutoff) {
				removeByPath[file.path] = struct{}{}
			}
		}
	}

	slices.SortFunc(files, func(a, b archivedLogFile) int {
		switch {
		case a.timestamp.After(b.timestamp):
			return -1
		case a.timestamp.Before(b.timestamp):
			return 1
		default:
			return 0
		}
	})
	if maxBackups > 0 && len(files) > maxBackups {
		for _, file := range files[maxBackups:] {
			removeByPath[file.path] = struct{}{}
		}
	}

	for path := range removeByPath {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove archived backup %s: %w", filepath.Base(path), err)
		}
	}

	return nil
}

func archivedCompressedBackups(archiveDir, baseName string) ([]archivedLogFile, error) {
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("archived compressed backups: read dir: %w", err)
	}

	prefix, ext := backupPrefixAndExt(baseName)
	suffix := ext + compressSuffix
	files := make([]archivedLogFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}

		timestamp, err := backupTimestampFromName(name, prefix, suffix)
		if err != nil {
			continue
		}

		files = append(files, archivedLogFile{
			path:      filepath.Join(archiveDir, name),
			timestamp: timestamp,
		})
	}

	return files, nil
}

func backupPrefixAndExt(baseName string) (string, string) {
	ext := filepath.Ext(baseName)
	return strings.TrimSuffix(baseName, ext) + "-", ext
}

func backupTimestampFromName(name, prefix, suffix string) (time.Time, error) {
	timestamp, ok := strings.CutPrefix(name, prefix)
	if !ok {
		return time.Time{}, fmt.Errorf("unexpected backup name: %s", name)
	}
	timestamp, ok = strings.CutSuffix(timestamp, suffix)
	if !ok {
		return time.Time{}, fmt.Errorf("unexpected backup name: %s", name)
	}

	parsed, err := time.Parse(backupTimeFormat, timestamp)
	if err != nil {
		return time.Time{}, fmt.Errorf("backup timestamp from name: parse %q: %w", timestamp, err)
	}
	return parsed, nil
}

func ensureLogFilePerm(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat log file failed: %w", err)
		}
		file, createErr := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, logFilePerm)
		if createErr != nil {
			return fmt.Errorf("create log file failed: %w", createErr)
		}
		if closeErr := file.Close(); closeErr != nil {
			return fmt.Errorf("close log file failed: %w", closeErr)
		}
		return nil
	}

	if info.IsDir() {
		return fmt.Errorf("log path is directory: %s", path)
	}

	if info.Mode().Perm() == logFilePerm {
		return nil
	}

	if chmodErr := os.Chmod(path, logFilePerm); chmodErr != nil {
		return fmt.Errorf("chmod log file failed: %w", chmodErr)
	}
	return nil
}

func ensureLogDirPerm(path string) error {
	if err := os.MkdirAll(path, logDirPerm); err != nil {
		return fmt.Errorf("create log dir failed: %w", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat log dir failed: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("log dir path is not directory: %s", path)
	}
	if info.Mode().Perm() == logDirPerm {
		return nil
	}

	if chmodErr := os.Chmod(path, logDirPerm); chmodErr != nil {
		return fmt.Errorf("chmod log dir failed: %w", chmodErr)
	}
	return nil
}
