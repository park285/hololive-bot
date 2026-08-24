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

package activity

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sync"
	"time"
)

type LogEntry struct {
	Timestamp time.Time      `json:"timestamp"`
	Type      string         `json:"type"` // 예: "command", "auth", "system"
	Summary   string         `json:"summary"`
	Details   map[string]any `json:"details,omitempty"`
}

type Logger struct {
	filePath   string
	logger     *slog.Logger
	stdoutOnly bool
	mu         sync.RWMutex
	file       *os.File
}

var (
	activityLogRotateMaxBytes   int64 = 10 * 1024 * 1024 // 10MB
	activityLogReadMaxLineBytes int64 = 16 * 1024 * 1024 // 16MB
)

const activityLogTailChunkBytes int64 = 64 * 1024

const activityLogFilePerm = 0o600

func NewActivityLogger(filePath string, logger *slog.Logger) *Logger {
	return &Logger{
		filePath:   filePath,
		logger:     logger,
		stdoutOnly: filePath == "",
	}
}

func (l *Logger) Log(entryType, summary string, details map[string]any) {
	if l.stdoutOnly {
		l.logger.Info("activity",
			slog.String("type", entryType),
			slog.String("summary", summary),
			slog.Any("details", details),
		)

		return
	}

	entry := LogEntry{
		Timestamp: time.Now(),
		Type:      entryType,
		Summary:   summary,
		Details:   details,
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	file, err := l.activeFileLocked()
	if err != nil {
		l.logger.Error("Failed to open activity log file", slog.Any("error", err))

		return
	}

	if err := jsonv2.MarshalWrite(file, entry); err != nil {
		l.logger.Error("Failed to write activity log", slog.Any("error", err))

		return
	}

	if _, err := file.WriteString("\n"); err != nil {
		l.logger.Error("Failed to terminate activity log entry", slog.Any("error", err))
	}
}

func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	file := l.file

	l.file = nil

	if file == nil {
		return nil
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("close activity log: %w", err)
	}

	return nil
}

func (l *Logger) activeFileLocked() (*os.File, error) {
	if err := l.rotateIfNeededLocked(); err != nil {
		l.logger.Error("Failed to rotate activity log", slog.Any("error", err))
	}

	if l.file != nil {
		return l.file, nil
	}

	file, err := os.OpenFile(l.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, activityLogFilePerm)
	if err != nil {
		return nil, fmt.Errorf("open activity log: %w", err)
	}

	l.file = file

	return file, nil
}

func (l *Logger) closeFileLocked() {
	if l.file == nil {
		return
	}

	if err := l.file.Close(); err != nil {
		l.logger.Error("Failed to close activity log file", slog.Any("error", err))
	}

	l.file = nil
}

func (l *Logger) GetRecentLogs(limit int) ([]LogEntry, error) {
	if limit <= 0 {
		return []LogEntry{}, nil
	}

	if l.stdoutOnly {
		return []LogEntry{}, nil
	}

	l.mu.RLock()
	defer l.mu.RUnlock()

	f, err := os.Open(l.filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []LogEntry{}, nil
		}

		return nil, fmt.Errorf("failed to open activity log: %w", err)
	}

	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			l.logger.Error("Failed to close activity log file", slog.Any("error", closeErr))
		}
	}()

	out, err := tailRecentLogEntries(f, limit)
	if err != nil {
		return out, fmt.Errorf("tail recent log entries: %w", err)
	}

	return out, nil
}

func tailRecentLogEntries(f *os.File, limit int) ([]LogEntry, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat activity log: %w", err)
	}

	entries := make([]LogEntry, 0, limit)
	offset := info.Size()

	var (
		carry []byte
		read  int64
	)

	for offset > 0 && len(entries) < limit && read < activityLogReadMaxLineBytes {
		chunkSize := min(activityLogTailChunkBytes, offset)

		offset -= chunkSize

		read += chunkSize

		chunk := make([]byte, chunkSize, chunkSize+int64(len(carry)))
		if _, err := f.ReadAt(chunk, offset); err != nil {
			return nil, fmt.Errorf("failed to read activity log: %w", err)
		}

		var lines [][]byte

		lines, carry = splitTailChunk(append(chunk, carry...), offset > 0)
		entries = appendNewestFirstEntries(entries, lines, limit)
	}

	slices.Reverse(entries)

	return entries, nil
}

// hasEarlierBytes가 true면 첫 조각은 앞 chunk와 이어져야 완성되는 부분 줄이라 carry로 넘긴다.
func splitTailChunk(buf []byte, hasEarlierBytes bool) (lines [][]byte, carry []byte) {
	segments := bytes.Split(buf, []byte{'\n'})
	if hasEarlierBytes && len(segments) > 0 {
		carry = segments[0]
		segments = segments[1:]
	}

	lines = make([][]byte, 0, len(segments))
	for _, segment := range segments {
		if len(segment) > 0 {
			lines = append(lines, segment)
		}
	}

	return lines, carry
}

func appendNewestFirstEntries(entries []LogEntry, lines [][]byte, limit int) []LogEntry {
	for i := len(lines) - 1; i >= 0 && len(entries) < limit; i-- {
		var entry LogEntry

		if err := jsonv2.Unmarshal(lines[i], &entry); err != nil {
			continue // 잘못된 형식의 줄은 건너뜀
		}

		entries = append(entries, entry)
	}

	return entries
}

func (l *Logger) rotateIfNeededLocked() error {
	info, err := os.Stat(l.filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			l.closeFileLocked()

			return nil
		}

		return fmt.Errorf("stat activity log: %w", err)
	}

	if info.Size() < activityLogRotateMaxBytes {
		return nil
	}

	// rename 전에 핸들을 닫아야 다음 쓰기가 rotate된 파일이 아니라 새 파일로 간다.
	l.closeFileLocked()

	rotatedPath := l.filePath + ".1"
	if err := os.Remove(rotatedPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove rotated activity log: %w", err)
	}

	if err := os.Rename(l.filePath, rotatedPath); err != nil {
		return fmt.Errorf("rotate activity log: %w", err)
	}

	return nil
}
