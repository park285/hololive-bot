// Package logs: 시스템 로그 파일 읽기
package logs

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// LogDir: 로그 디렉토리 (Docker 마운트: /home/kapu/gemini/llm/logs → /app/logs)
	LogDir = "/app/logs"
	// MaxLogLines: 읽을 최대 라인 수
	MaxLogLines = 500
)

// AllowedLogFiles: 허용된 로그 파일 목록 (보안 화이트리스트)
var AllowedLogFiles = map[string]string{
	"combined":     "combined.log",
	"server":       "server.log",
	"bot":          "bot.log",
	"activity":     "activity.log",
	"admin":        "admin.log", // admin-backend 로그
	"admin-access": "admin-access.log",
}

// GetLogFileKeys: 허용된 로그 파일 키 목록
func GetLogFileKeys() []string {
	keys := make([]string, 0, len(AllowedLogFiles))
	for k := range AllowedLogFiles {
		keys = append(keys, k)
	}
	return keys
}

// GetLogFilePath: 로그 파일 경로 반환
func GetLogFilePath(fileKey string) (string, bool) {
	fileName, ok := AllowedLogFiles[fileKey]
	if !ok {
		return "", false
	}
	return filepath.Join(LogDir, fileName), true
}

// TailFile: 파일의 마지막 N줄 읽기
func TailFile(path string, n int) ([]string, error) {
	if n <= 0 {
		return []string{}, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	defer func() { _ = file.Close() }()

	lines := make([]string, 0, n)
	nextIdx := 0
	wrapped := false
	scanner := bufio.NewScanner(file)

	// 버퍼 크기 증가 (긴 줄 처리)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		if len(lines) < n {
			lines = append(lines, line)
			continue
		}

		lines[nextIdx] = line
		nextIdx = (nextIdx + 1) % n
		wrapped = true
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan log file: %w", err)
	}

	// ring buffer가 회전되지 않은 경우 append 순서 그대로 반환
	if !wrapped {
		return lines, nil
	}

	ordered := make([]string, 0, n)
	ordered = append(ordered, lines[nextIdx:]...)
	ordered = append(ordered, lines[:nextIdx]...)
	return ordered, nil
}

// LogFileInfo: 로그 파일 정보
type LogFileInfo struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Exists   bool   `json:"exists"`
	Size     int64  `json:"size,omitempty"`
	Modified string `json:"modified,omitempty"`
}

// ListLogFiles: 사용 가능한 로그 파일 목록
func ListLogFiles() []LogFileInfo {
	files := make([]LogFileInfo, 0, len(AllowedLogFiles))

	for key, fileName := range AllowedLogFiles {
		logPath := filepath.Join(LogDir, fileName)
		info, err := os.Stat(logPath)

		entry := LogFileInfo{
			Key:    key,
			Name:   fileName,
			Exists: err == nil,
		}

		if err == nil {
			entry.Size = info.Size()
			entry.Modified = info.ModTime().Format("2006-01-02T15:04:05Z07:00")
		}

		files = append(files, entry)
	}

	return files
}
