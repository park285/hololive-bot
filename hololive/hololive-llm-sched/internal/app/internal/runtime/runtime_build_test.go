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

package runtime

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config"
)

func TestBuildLLMSchedulerRuntime_FailFastOnNilInputs(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("nil config", func(t *testing.T) {
		runtime, err := BuildLLMSchedulerRuntime(context.Background(), nil, logger)
		if err == nil {
			t.Fatal("BuildLLMSchedulerRuntime() expected error for nil config")
		}
		if err.Error() != "llm scheduler config must not be nil" {
			t.Fatalf("BuildLLMSchedulerRuntime() error = %q, want %q", err.Error(), "llm scheduler config must not be nil")
		}
		if runtime != nil {
			t.Fatal("BuildLLMSchedulerRuntime() expected nil runtime on error")
		}
	})

	t.Run("nil logger", func(t *testing.T) {
		runtime, err := BuildLLMSchedulerRuntime(context.Background(), &config.LLMSchedulerConfig{}, nil)
		if err == nil {
			t.Fatal("BuildLLMSchedulerRuntime() expected error for nil logger")
		}
		if err.Error() != "logger must not be nil" {
			t.Fatalf("BuildLLMSchedulerRuntime() error = %q, want %q", err.Error(), "logger must not be nil")
		}
		if runtime != nil {
			t.Fatal("BuildLLMSchedulerRuntime() expected nil runtime on error")
		}
	})
}
