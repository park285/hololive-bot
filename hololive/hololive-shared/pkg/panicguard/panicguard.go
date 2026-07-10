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

package panicguard

import (
	"fmt"
	"log/slog"
	"runtime/debug"
)

type ErrorGroup interface {
	Go(func() error)
}

func Go(logger *slog.Logger, name string, fn func()) {
	go Run(logger, name, fn)
}

func GoE(group ErrorGroup, logger *slog.Logger, name string, fn func() error) {
	// crosscutting:allow 모든 errgroup 작업을 아래 RunE recovery 경계로 위임한다.
	group.Go(func() error {
		return RunE(logger, name, fn)
	})
}

func Run(logger *slog.Logger, name string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			logPanic(logger, name, r)
		}
	}()
	fn()
}

func RunE(logger *slog.Logger, name string, fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			logPanic(logger, name, r)
			if recovered, ok := r.(error); ok {
				err = fmt.Errorf("%s: recovered panic: %w", name, recovered)
				return
			}
			err = fmt.Errorf("%s: recovered panic: %v", name, r)
		}
	}()
	return fn()
}

func logPanic(logger *slog.Logger, name string, recovered any) {
	if logger == nil {
		logger = slog.Default()
	}
	logger.Error("background goroutine panic recovered",
		slog.String("guard", name),
		slog.Any("panic", fmt.Sprintf("%v", recovered)),
		slog.String("stack", string(debug.Stack())),
	)
}
