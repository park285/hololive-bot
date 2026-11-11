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

package apphttp

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/contracts/common"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
)

func TestProvideBotRouter_RejectsNilReadyProbe(t *testing.T) {
	t.Parallel()

	router, err := ProvideBotRouter(t.Context(), &apiconfig.BotPlaneConfig{}, slog.New(slog.DiscardHandler), nil, nil)
	if err == nil || router != nil {
		t.Fatalf("ProvideBotRouter(nil probe) router=%v error=%v, want nil router and error", router, err)
	}
}

func TestProvideBotRouter_InternalReadyReportsProbeWithoutDiagnostics(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		probeErr   error
		wantCode   int
		wantStatus string
	}{
		{name: "healthy", wantCode: http.StatusOK, wantStatus: "ready"},
		{name: "degraded", probeErr: errors.New("pool exhausted"), wantCode: http.StatusServiceUnavailable, wantStatus: "not_ready"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			probe := sharedreadiness.NewProbe("bot", sharedreadiness.Check{
				Name:  "postgres",
				Probe: func(context.Context) error { return tc.probeErr },
			})

			router, err := ProvideBotRouter(t.Context(), &apiconfig.BotPlaneConfig{
				Server: settings.ServerConfig{APIKey: "test-key"},
			}, slog.New(slog.DiscardHandler), nil, probe)
			if err != nil {
				t.Fatalf("ProvideBotRouter() error = %v", err)
			}

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/internal/ready", http.NoBody)
			req.Header.Set(common.APIKeyHeader, "test-key")

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tc.wantCode {
				t.Fatalf("/internal/ready status = %d, want %d", rec.Code, tc.wantCode)
			}

			var payload map[string]any

			if err := jsonv2.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("/internal/ready JSON 파싱 실패: %v, raw=%s", err, rec.Body.String())
			}

			if payload["status"] != tc.wantStatus || payload["plane"] != "bot" {
				t.Fatalf("payload = %v, want status %q and plane bot", payload, tc.wantStatus)
			}

			for _, key := range []string{"workerProfile", "irisWebhookReceive"} {
				if _, ok := payload[key]; ok {
					t.Fatalf("/internal/ready must omit %q: %v", key, payload)
				}
			}
		})
	}
}
