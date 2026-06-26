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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	json "github.com/park285/shared-go/pkg/json"
)

func TestBotReadyResponder_OmitsWorkerAndWebhookDiagnostics(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.GET("/ready", botReadyResponder())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ready", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("/ready status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("/ready JSON 파싱 실패: %v, raw=%s", err, rec.Body.String())
	}

	if _, ok := payload["health"]; !ok {
		t.Fatalf("/ready payload missing \"health\": %v", payload)
	}
	if _, ok := payload["workerProfile"]; ok {
		t.Fatalf("unauthenticated /ready must omit \"workerProfile\": %v", payload)
	}
	if _, ok := payload["irisWebhookReceive"]; ok {
		t.Fatalf("unauthenticated /ready must omit \"irisWebhookReceive\": %v", payload)
	}
}
