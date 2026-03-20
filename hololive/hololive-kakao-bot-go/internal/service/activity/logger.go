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
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
)

// LogEntry: 활동 로그의 한 항목을 나타내는 구조체.
type LogEntry struct {
	Timestamp time.Time      `json:"timestamp"`
	Type      string         `json:"type"` // e.g., "command", "auth", "system"
	Summary   string         `json:"summary"`
	Details   map[string]any `json:"details,omitempty"`
}

// Logger: 파일 기반의 간단한 활동 로그 기록기.
type Logger struct {
	filePath   string
	logger     *slog.Logger
	stdoutOnly bool
	mu         sync.RWMutex
}

var (
	activityLogRotateMaxBytes   int64 = 10 * 1024 * 1024 // 10MB
	activityLogReadMaxLineBytes       = 16 * 1024 * 1024 // 16MB
)

const activityLogFilePerm = 0o600

// NewActivityLogger: 새로운 활동 로그 기록기를 생성합니다.
func NewActivityLogger(filePath string, logger *slog.Logger) *Logger {
	return &Logger{
		filePath:   filePath,
		logger:     logger,
		stdoutOnly: filePath == "",
	}
}

// Log: 새로운 활동 로그를 파일에 추가한다. (Thread-safe).
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

	if err := l.rotateIfNeeded(); err != nil {
		l.logger.Error("Failed to rotate activity log", slog.Any("error", err))
	}

	f, err := os.OpenFile(l.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, activityLogFilePerm)
	if err != nil {
		l.logger.Error("Failed to open activity log file", slog.Any("error", err))
		return
	}
	defer f.Close()

	if err := json.NewEncoder(f).Encode(entry); err != nil {
		l.logger.Error("Failed to write activity log", slog.Any("error", err))
	}
}

// GetRecentLogs: 최근 활동 로그를 조회합니다.
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
	defer f.Close()

	ring := make([]LogEntry, limit)
	count := 0
	writePos := 0

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), activityLogReadMaxLineBytes)

	for scanner.Scan() {
		var entry LogEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue // 잘못된 형식의 줄은 건너뜀
		}

		ring[writePos] = entry

		writePos = (writePos + 1) % limit
		if count < limit {
			count++
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read activity log: %w", err)
	}

	if count == 0 {
		return []LogEntry{}, nil
	}

	if count < limit {
		logs := make([]LogEntry, count)
		copy(logs, ring[:count])

		return logs, nil
	}

	logs := make([]LogEntry, count)
	for i := range count {
		logs[i] = ring[(writePos+i)%limit]
	}

	return logs, nil
}

func (l *Logger) rotateIfNeeded() error {
	info, err := os.Stat(l.filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}

		return fmt.Errorf("stat activity log: %w", err)
	}

	if info.Size() < activityLogRotateMaxBytes {
		return nil
	}

	rotatedPath := l.filePath + ".1"
	if err := os.Remove(rotatedPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove rotated activity log: %w", err)
	}

	if err := os.Rename(l.filePath, rotatedPath); err != nil {
		return fmt.Errorf("rotate activity log: %w", err)
	}

	return nil
}
