// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

// Package logging: 공통 로깅 유틸리티를 제공합니다.
// slog + tint + lumberjack 기반의 구조화된 로깅을 지원합니다.
package logging

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	logFilePerm         os.FileMode = 0o644
	logDirPerm          os.FileMode = 0o755
	compressSuffix                  = ".gz"
	backupTimeFormat                = "2006-01-02T15-04-05.000"
	archiveDirName                  = "archive"
	archiveScanInterval             = 5 * time.Second
)

// Config: 파일 로그 로테이션 설정입니다.
type Config struct {
	Level      string // 로그 레벨 (debug, info, warn, error)
	Dir        string // 로그 파일 디렉터리
	MaxSizeMB  int    // 단일 파일 최대 크기 (MB)
	MaxBackups int    // 보관할 백업 파일 수
	MaxAgeDays int    // 백업 파일 보관 일수
	Compress   bool   // 백업 파일 압축 여부
}

// ParseLevel: 문자열 로그 레벨을 slog.Level로 변환합니다.
func ParseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// LogError: 이벤트와 에러를 로그로 기록합니다. err이 nil이면 아무것도 하지 않습니다.
func LogError(logger *slog.Logger, event string, err error) {
	if logger == nil || err == nil {
		return
	}
	logger.Warn(event, "err", err)
}

// LogInfo: 이벤트와 추가 필드를 로그로 기록합니다.
func LogInfo(logger *slog.Logger, event string, fields ...any) {
	if logger == nil {
		return
	}
	logger.Info(event, fields...)
}

// NewLogger: 기본 slog 로거를 생성합니다. (stdout, tint 핸들러 사용)
func NewLogger() *slog.Logger {
	return slog.New(tint.NewHandler(os.Stdout, &tint.Options{
		Level:      slog.LevelInfo,
		TimeFormat: time.RFC3339,
		AddSource:  true,
		NoColor:    shouldDisableColor(os.Stdout),
	}))
}

// NewTestLogger: 테스트용 로거를 생성합니다. 모든 출력을 폐기하여 테스트 로그를 깔끔하게 유지합니다.
// 테스트에서 로거가 필요하지만 출력은 필요 없는 경우 사용하세요.
func NewTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// NewTestLoggerWithOutput: 테스트용 로거를 생성합니다. 제공된 Writer로 로그를 출력합니다.
// 테스트에서 로그 출력을 캡처하거나 검증해야 하는 경우 사용하세요.
func NewTestLoggerWithOutput(w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, nil))
}

// EnableFileLogging: 파일 로깅을 활성화하고, 파일과 stdout에 동시에 출력하는 로거를 반환합니다.
func EnableFileLogging(cfg Config, fileName string) (*slog.Logger, error) {
	level := ParseLevel(cfg.Level)
	logDir := strings.TrimSpace(cfg.Dir)
	if logDir == "" {
		var handler slog.Handler
		handler = tint.NewHandler(os.Stdout, &tint.Options{
			Level:      level,
			TimeFormat: time.RFC3339,
			AddSource:  true,
			NoColor:    shouldDisableColor(os.Stdout),
		})
		handler = NewSanitizeHandler(handler)
		logger := slog.New(handler)
		slog.SetDefault(logger)
		return logger, nil
	}
	if cfg.MaxSizeMB <= 0 || cfg.MaxBackups <= 0 || cfg.MaxAgeDays <= 0 {
		return nil, fmt.Errorf("invalid log config: size=%d backups=%d age_days=%d", cfg.MaxSizeMB, cfg.MaxBackups, cfg.MaxAgeDays)
	}

	if err := os.MkdirAll(logDir, logDirPerm); err != nil {
		return nil, fmt.Errorf("create log dir failed: %w", err)
	}

	logPath := filepath.Join(logDir, fileName)

	if err := ensureLogFilePerm(logPath); err != nil {
		return nil, fmt.Errorf("prepare log file failed: %w", err)
	}

	logArchiver := newCompressedLogArchiver(logPath, cfg.MaxBackups, cfg.MaxAgeDays, cfg.Compress)
	logFile := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    cfg.MaxSizeMB,
		MaxBackups: cfg.MaxBackups,
		MaxAge:     cfg.MaxAgeDays,
		Compress:   cfg.Compress,
	}

	w := io.MultiWriter(os.Stdout, &archiveAwareWriter{inner: logFile, archiver: logArchiver})

	var handler slog.Handler
	handler = tint.NewHandler(w, &tint.Options{
		Level:      level,
		TimeFormat: time.RFC3339,
		AddSource:  true,
		NoColor:    true,
	})
	handler = NewSanitizeHandler(handler)

	logger := slog.New(handler)
	slog.SetDefault(logger)
	logger.Info("file_logging_enabled",
		slog.String("path", logFile.Filename),
		slog.String("archive_dir", filepath.Join(logDir, archiveDirName)),
	)
	logArchiver.Trigger()
	return logger, nil
}

type archiveAwareWriter struct {
	inner    io.Writer
	archiver *compressedLogArchiver
}

func (w *archiveAwareWriter) Write(p []byte) (int, error) {
	n, err := w.inner.Write(p)
	if w.archiver != nil {
		w.archiver.Trigger()
	}
	return n, err
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
	if err := os.MkdirAll(archiveDir, logDirPerm); err != nil {
		return fmt.Errorf("create archive dir: %w", err)
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
		return nil, err
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
		return nil, err
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
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return time.Time{}, fmt.Errorf("unexpected backup name: %s", name)
	}

	timestamp := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
	return time.Parse(backupTimeFormat, timestamp)
}

func ensureLogFilePerm(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat log file failed: %w", err)
		}
		// #nosec G304 -- path is constructed from controlled log directory and filename.
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

func shouldDisableColor(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return true
	}

	if os.Getenv("NO_COLOR") != "" {
		return true
	}

	fd := file.Fd()
	return !isatty.IsTerminal(fd) && !isatty.IsCygwinTerminal(fd)
}
