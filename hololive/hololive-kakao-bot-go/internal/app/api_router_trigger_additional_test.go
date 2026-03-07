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

package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
)

func TestProvideTriggerRouter_Branches(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("nil trigger handler keeps health only", func(t *testing.T) {
		router, err := ProvideTriggerRouter(context.Background(), logger, nil, "api-key")
		if err != nil {
			t.Fatalf("ProvideTriggerRouter() error = %v", err)
		}

		healthReq := httptest.NewRequest(http.MethodGet, "/health", nil)
		healthRes := httptest.NewRecorder()
		router.ServeHTTP(healthRes, healthReq)
		if healthRes.Code != http.StatusOK {
			t.Fatalf("/health status = %d, want %d", healthRes.Code, http.StatusOK)
		}

		triggerReq := httptest.NewRequest(http.MethodPost, triggercontracts.MajorEventWeeklyPath, nil)
		triggerRes := httptest.NewRecorder()
		router.ServeHTTP(triggerRes, triggerReq)
		if triggerRes.Code != http.StatusNotFound {
			t.Fatalf("trigger status = %d, want %d", triggerRes.Code, http.StatusNotFound)
		}
	})

	t.Run("trigger routes require api key and are registered", func(t *testing.T) {
		triggerHandler := sharedserver.NewTriggerHandler(nil, nil, nil, logger)
		router, err := ProvideTriggerRouter(context.Background(), logger, triggerHandler, "api-key")
		if err != nil {
			t.Fatalf("ProvideTriggerRouter() error = %v", err)
		}

		noAuthReq := httptest.NewRequest(http.MethodPost, triggercontracts.MajorEventWeeklyPath, nil)
		noAuthRes := httptest.NewRecorder()
		router.ServeHTTP(noAuthRes, noAuthReq)
		if noAuthRes.Code != http.StatusUnauthorized {
			t.Fatalf("trigger status without api key = %d, want %d", noAuthRes.Code, http.StatusUnauthorized)
		}

		withAuthReq := httptest.NewRequest(http.MethodPost, triggercontracts.MajorEventWeeklyPath, nil)
		withAuthReq.Header.Set("X-API-Key", "api-key")
		withAuthRes := httptest.NewRecorder()
		router.ServeHTTP(withAuthRes, withAuthReq)
		if withAuthRes.Code != http.StatusServiceUnavailable {
			t.Fatalf("trigger status with api key = %d, want %d", withAuthRes.Code, http.StatusServiceUnavailable)
		}
	})
}
