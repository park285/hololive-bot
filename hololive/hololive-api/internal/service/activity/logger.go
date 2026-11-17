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

import "log/slog"

// Logger는 관리 작업 활동을 process logger(stdout)에 구조화 기록으로 남긴다.
type Logger struct {
	logger *slog.Logger
}

func NewActivityLogger(logger *slog.Logger) *Logger {
	return &Logger{logger: logger}
}

// Log는 호출자가 고른 스칼라·그룹 필드만 활동 상세로 기록한다.
func (l *Logger) Log(entryType, summary string, details []slog.Attr) {
	l.logger.Info("activity",
		slog.String("type", entryType),
		slog.String("summary", summary),
		slog.GroupAttrs("details", details...),
	)
}
