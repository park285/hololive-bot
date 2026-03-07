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

package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
)

// --- 인라인 스케줄러 스텁 ---

type stubMajorEvent struct {
	err error
}

func (s *stubMajorEvent) SendWeeklyNotification(_ context.Context) error { return s.err }

type stubMajorEventMonthly struct {
	err error
}

func (s *stubMajorEventMonthly) SendMonthlyNotification(_ context.Context) error { return s.err }

type stubMemberNewsWeekly struct {
	err error
}

func (s *stubMemberNewsWeekly) SendWeeklyDigest(_ context.Context) error { return s.err }

// --- 헬퍼 ---

func newDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTriggerRouter(h *TriggerHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h.RegisterInternalRoutes(r.Group("/"))
	return r
}

// postTrigger: 트리거 엔드포인트에 POST 요청을 보내고 응답을 반환합니다.
func postTrigger(t *testing.T, r *gin.Engine, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// unmarshalBody: 응답 본문을 map으로 파싱합니다.
func unmarshalBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("응답 본문 파싱 실패: %v", err)
	}
	return m
}

// --- TriggerWeeklyNotification 테스트 ---

func TestTriggerWeeklyNotification(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		scheduler   MajorEventScheduler // nil이면 nil 포인터로 처리
		wantStatus  int
		wantBodyKey string
		wantBodyVal string
	}{
		{
			name:        "스케줄러 nil → 503",
			scheduler:   nil,
			wantStatus:  http.StatusServiceUnavailable,
			wantBodyKey: "error",
			wantBodyVal: "major event scheduler not initialized",
		},
		{
			name:        "정상 실행 → 200",
			scheduler:   &stubMajorEvent{err: nil},
			wantStatus:  http.StatusOK,
			wantBodyKey: "status",
			wantBodyVal: "weekly notification sent",
		},
		{
			name:        "이미 진행 중 → 409",
			scheduler:   &stubMajorEvent{err: triggercontracts.ErrNotificationInProgress},
			wantStatus:  http.StatusConflict,
			wantBodyKey: "error",
			wantBodyVal: "notification already in progress",
		},
		{
			name:        "일반 오류 → 500",
			scheduler:   &stubMajorEvent{err: errors.New("db timeout")},
			wantStatus:  http.StatusInternalServerError,
			wantBodyKey: "error",
			wantBodyVal: "internal server error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := NewTriggerHandler(tt.scheduler, nil, nil, newDiscardLogger())
			r := newTriggerRouter(h)

			rec := postTrigger(t, r, triggercontracts.MajorEventWeeklyPath)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			body := unmarshalBody(t, rec)
			if got := body[tt.wantBodyKey]; got != tt.wantBodyVal {
				t.Fatalf("body[%q] = %v, want %q", tt.wantBodyKey, got, tt.wantBodyVal)
			}
		})
	}
}

// --- TriggerMonthlyNotification 테스트 ---

func TestTriggerMonthlyNotification(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		scheduler   MajorEventMonthlyScheduler
		wantStatus  int
		wantBodyKey string
		wantBodyVal string
	}{
		{
			name:        "스케줄러 nil → 503",
			scheduler:   nil,
			wantStatus:  http.StatusServiceUnavailable,
			wantBodyKey: "error",
			wantBodyVal: "major event monthly scheduler not initialized",
		},
		{
			name:        "정상 실행 → 200",
			scheduler:   &stubMajorEventMonthly{err: nil},
			wantStatus:  http.StatusOK,
			wantBodyKey: "status",
			wantBodyVal: "monthly notification sent",
		},
		{
			name:        "이미 진행 중 → 409",
			scheduler:   &stubMajorEventMonthly{err: triggercontracts.ErrNotificationInProgress},
			wantStatus:  http.StatusConflict,
			wantBodyKey: "error",
			wantBodyVal: "notification already in progress",
		},
		{
			name:        "일반 오류 → 500",
			scheduler:   &stubMajorEventMonthly{err: errors.New("timeout")},
			wantStatus:  http.StatusInternalServerError,
			wantBodyKey: "error",
			wantBodyVal: "internal server error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := NewTriggerHandler(nil, tt.scheduler, nil, newDiscardLogger())
			r := newTriggerRouter(h)

			rec := postTrigger(t, r, triggercontracts.MajorEventMonthlyPath)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			body := unmarshalBody(t, rec)
			if got := body[tt.wantBodyKey]; got != tt.wantBodyVal {
				t.Fatalf("body[%q] = %v, want %q", tt.wantBodyKey, got, tt.wantBodyVal)
			}
		})
	}
}

// --- TriggerMemberNewsWeekly 테스트 ---

func TestTriggerMemberNewsWeekly(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		scheduler   MemberNewsWeeklyScheduler
		wantStatus  int
		wantBodyKey string
		wantBodyVal string
	}{
		{
			name:        "스케줄러 nil → 503",
			scheduler:   nil,
			wantStatus:  http.StatusServiceUnavailable,
			wantBodyKey: "error",
			wantBodyVal: "member news weekly scheduler not initialized",
		},
		{
			name:        "정상 실행 → 200",
			scheduler:   &stubMemberNewsWeekly{err: nil},
			wantStatus:  http.StatusOK,
			wantBodyKey: "status",
			wantBodyVal: "member news weekly digest sent",
		},
		{
			name:        "오류 발생 → 500",
			scheduler:   &stubMemberNewsWeekly{err: errors.New("fetch failed")},
			wantStatus:  http.StatusInternalServerError,
			wantBodyKey: "error",
			wantBodyVal: "internal server error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := NewTriggerHandler(nil, nil, tt.scheduler, newDiscardLogger())
			r := newTriggerRouter(h)

			rec := postTrigger(t, r, triggercontracts.MemberNewsWeeklyPath)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			body := unmarshalBody(t, rec)
			if got := body[tt.wantBodyKey]; got != tt.wantBodyVal {
				t.Fatalf("body[%q] = %v, want %q", tt.wantBodyKey, got, tt.wantBodyVal)
			}
		})
	}
}

// --- RegisterInternalRoutesWithAuth 테스트 ---

func TestRegisterInternalRoutesWithAuth(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	const testKey = "secret-key"

	tests := []struct {
		name       string
		apiKey     string // 미들웨어에 설정된 키
		headerVal  string // 요청에 보내는 키 ("" = 미전송)
		wantStatus int
	}{
		{
			name:       "빈 API 키(개발 모드): 인증 없이 통과",
			apiKey:     "",
			headerVal:  "",
			wantStatus: http.StatusOK,
		},
		{
			name:       "API 키 설정 시 헤더 미전송 → 401",
			apiKey:     testKey,
			headerVal:  "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "잘못된 키 전송 → 403",
			apiKey:     testKey,
			headerVal:  "wrong",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "올바른 키 전송 → 200",
			apiKey:     testKey,
			headerVal:  testKey,
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := NewTriggerHandler(
				&stubMajorEvent{err: nil},
				nil,
				nil,
				newDiscardLogger(),
			)

			r := gin.New()
			h.RegisterInternalRoutesWithAuth(r.Group("/"), tt.apiKey)

			req := httptest.NewRequest(http.MethodPost, triggercontracts.MajorEventWeeklyPath, nil)
			if tt.headerVal != "" {
				req.Header.Set(middleware.APIKeyHeader, tt.headerVal)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}
