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

package botruntime

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/park285/hololive-bot/shared-go/pkg/runtime/lifecycle"
)

func TestBotRuntimeClose_CallsCleanup(t *testing.T) {
	t.Parallel()

	calls := 0
	runtime := &BotRuntime{
		Managed: lifecycle.NewManaged(func() { calls++ }),
	}

	runtime.Close()

	if calls != 1 {
		t.Fatalf("cleanup calls = %d, want 1", calls)
	}
}

func TestBotRuntimeStartHTTPServer_Branches(t *testing.T) {
	t.Parallel()

	t.Run("nil runtime or nil server", func(t *testing.T) {
		var nilRuntime *BotRuntime
		nilRuntime.StartHTTPServer(make(chan error, 1))

		runtime := &BotRuntime{}
		runtime.StartHTTPServer(make(chan error, 1))
	})

	t.Run("listen error pushes err channel", func(t *testing.T) {
		runtime := &BotRuntime{
			HTTPServer: &http.Server{Addr: "invalid::addr"},
		}
		errCh := make(chan error, 1)

		runtime.StartHTTPServer(errCh)

		select {
		case err := <-errCh:
			if err == nil || !strings.Contains(err.Error(), "HTTP server error") {
				t.Fatalf("unexpected error: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for HTTP server error")
		}
	})
}

func TestBotRuntimeStartAndHelpers_NoPanicOnNilComponents(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer

	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	runtime := &BotRuntime{
		Logger: logger,
	}

	runtime.Start(t.Context(), nil)
	runtime.logError("expected test error", errors.New("boom"))

	if logBuf.Len() == 0 {
		t.Fatal("expected runtime helpers to write logs")
	}
}

func TestBotRuntimeRun_ExitsOnServerError(t *testing.T) {
	t.Parallel()

	runtime := &BotRuntime{
		Logger:     slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		ServerAddr: "invalid::addr",
		HTTPServer: &http.Server{
			Addr: "invalid::addr",
		},
	}

	done := make(chan struct{})

	go func() {
		runtime.Run()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run() did not exit on server error")
	}
}
