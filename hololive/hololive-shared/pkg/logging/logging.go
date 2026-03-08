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

package logging

import (
	"fmt"
	"io"
	"log/slog"

	internallogging "github.com/kapu/hololive-shared/internal/logging"
)

// Config: 로깅 설정 (로그 디렉토리, 로테이션 정책)
type Config = internallogging.Config

// NewLogger: 콘솔 출력용 기본 slog 로거를 생성합니다.
func NewLogger() *slog.Logger {
	return internallogging.NewLogger()
}

// NewLoggerWithLevel: 지정된 레벨로 콘솔 출력용 slog 로거를 생성합니다.
func NewLoggerWithLevel(level string) *slog.Logger {
	cfg := Config{Level: level}
	logger, err := internallogging.EnableFileLogging(cfg, "")
	if err != nil || logger == nil {
		return internallogging.NewLogger()
	}
	return logger
}

// NewTestLogger: 테스트용 로거를 생성합니다. 기본 출력은 버립니다.
func NewTestLogger() *slog.Logger {
	return internallogging.NewTestLogger()
}

// NewTestLoggerWithOutput: 테스트용 로거를 생성합니다. 제공된 Writer로 로그를 출력합니다.
func NewTestLoggerWithOutput(w io.Writer) *slog.Logger {
	return internallogging.NewTestLoggerWithOutput(w)
}

// EnableFileLogging: 파일 로깅을 활성화하고, 로그 로테이션이 적용된 로거를 반환합니다.
func EnableFileLogging(cfg Config, fileName string) (*slog.Logger, error) {
	logger, err := internallogging.EnableFileLogging(cfg, fileName)
	if err != nil {
		return nil, fmt.Errorf("enable file logging: %w", err)
	}
	return logger, nil
}

// EnableFileLoggingWithLevel: 지정된 레벨과 파일 로깅을 활성화합니다.
func EnableFileLoggingWithLevel(cfg Config, fileName, level string) (*slog.Logger, error) {
	cfg.Level = level
	logger, err := internallogging.EnableFileLogging(cfg, fileName)
	if err != nil {
		return nil, fmt.Errorf("enable file logging with level: %w", err)
	}
	return logger, nil
}
