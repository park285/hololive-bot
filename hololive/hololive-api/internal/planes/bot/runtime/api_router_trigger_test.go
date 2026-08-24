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
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

func serveTriggerRouterRequest(t *testing.T, router http.Handler, method, path, apiKey string) int {
	t.Helper()

	request := httptest.NewRequestWithContext(t.Context(), method, path, http.NoBody)

	if apiKey != "" {
		request.Header.Set("X-Api-Key", apiKey)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	return recorder.Code
}

func TestProvideTriggerRouter_Branches(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	t.Run("nil trigger handler keeps health only", func(t *testing.T) {
		t.Parallel()

		router, err := sharedserver.NewTriggerRuntimeRouter(t.Context(), logger, nil, "api-key")
		if err != nil {
			t.Fatalf("NewTriggerRuntimeRouter() error = %v", err)
		}

		if code := serveTriggerRouterRequest(t, router, http.MethodGet, "/health", ""); code != http.StatusOK {
			t.Fatalf("/health status = %d, want %d", code, http.StatusOK)
		}

		if code := serveTriggerRouterRequest(t, router, http.MethodGet, "/ready", ""); code != http.StatusOK {
			t.Fatalf("/ready status = %d, want %d", code, http.StatusOK)
		}

		code := serveTriggerRouterRequest(t, router, http.MethodPost, triggercontracts.MajorEventWeeklyPath, "")
		if code != http.StatusNotFound {
			t.Fatalf("trigger status = %d, want %d", code, http.StatusNotFound)
		}
	})

	t.Run("trigger routes require api key and are registered", func(t *testing.T) {
		t.Parallel()

		triggerHandler := sharedserver.NewTriggerHandler(nil, nil, nil, logger)

		router, err := sharedserver.NewTriggerRuntimeRouter(t.Context(), logger, triggerHandler, "api-key")
		if err != nil {
			t.Fatalf("NewTriggerRuntimeRouter() error = %v", err)
		}

		noAuth := serveTriggerRouterRequest(t, router, http.MethodPost, triggercontracts.MajorEventWeeklyPath, "")
		if noAuth != http.StatusUnauthorized {
			t.Fatalf("trigger status without api key = %d, want %d", noAuth, http.StatusUnauthorized)
		}

		withAuth := serveTriggerRouterRequest(t, router, http.MethodPost, triggercontracts.MajorEventWeeklyPath, "api-key")
		if withAuth != http.StatusServiceUnavailable {
			t.Fatalf("trigger status with api key = %d, want %d", withAuth, http.StatusServiceUnavailable)
		}
	})

	t.Run("trigger routes fail closed when api key missing", func(t *testing.T) {
		t.Parallel()

		triggerHandler := sharedserver.NewTriggerHandler(nil, nil, nil, logger)

		router, err := sharedserver.NewTriggerRuntimeRouter(t.Context(), logger, triggerHandler, "")
		if err == nil {
			t.Fatal("NewTriggerRuntimeRouter() error = nil, want non-nil")
		}

		if router != nil {
			t.Fatal("NewTriggerRuntimeRouter() router = non-nil, want nil")
		}

		if err.Error() != "runtime router: register routes: API_SECRET_KEY required" {
			t.Fatalf("NewTriggerRuntimeRouter() error = %q, want %q", err.Error(), "runtime router: register routes: API_SECRET_KEY required")
		}
	})
}
