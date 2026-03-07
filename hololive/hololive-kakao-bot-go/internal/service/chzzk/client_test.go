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

package chzzk

import (
	"context"
	"fmt"
	"github.com/kapu/hololive-shared/pkg/constants"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	client := NewClient(http.DefaultClient, DefaultBaseURL, logger)

	if client == nil {
		t.Fatal("Expected non-nil client")
	}
	if client.baseURL != DefaultBaseURL {
		t.Errorf("Expected baseURL %q, got %q", DefaultBaseURL, client.baseURL)
	}
}

func TestGetLiveStatus_Success_Open(t *testing.T) {
	// OPEN 상태 테스트 데이터
	response := LiveStatusResponse{
		Code: 200,
		Content: &LiveStatusContent{
			LiveTitle:           "마인크래프트 생방송",
			Status:              "OPEN",
			ConcurrentUserCount: 1234,
			LiveCategoryValue:   "게임",
			ChatChannelId:       "chat123",
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/polling/v2/channels/test-channel/live-status" {
			t.Errorf("Unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	client := NewClient(http.DefaultClient, server.URL, logger)

	ctx := context.Background()
	content, err := client.GetLiveStatus(ctx, "test-channel")

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if content == nil {
		t.Fatal("Expected non-nil content")
	}
	if content.Status != "OPEN" {
		t.Errorf("Expected status OPEN, got: %s", content.Status)
	}
	if content.LiveTitle != "마인크래프트 생방송" {
		t.Errorf("Expected title '마인크래프트 생방송', got: %s", content.LiveTitle)
	}
	if content.ConcurrentUserCount != 1234 {
		t.Errorf("Expected 1234 viewers, got: %d", content.ConcurrentUserCount)
	}
}

func TestGetLiveStatus_Success_Close(t *testing.T) {
	// CLOSE 상태 (방송 없음)
	response := LiveStatusResponse{
		Code: 200,
		Content: &LiveStatusContent{
			Status: "CLOSE",
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	client := NewClient(http.DefaultClient, server.URL, logger)

	ctx := context.Background()
	content, err := client.GetLiveStatus(ctx, "test-channel")

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if content == nil {
		t.Fatal("Expected non-nil content")
	}
	if content.Status != "CLOSE" {
		t.Errorf("Expected status CLOSE, got: %s", content.Status)
	}
}

func TestGetLiveStatus_NotFound(t *testing.T) {
	// 404 Not Found 응답
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":404,"message":"Channel not found"}`))
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	client := NewClient(http.DefaultClient, server.URL, logger)

	ctx := context.Background()
	_, err := client.GetLiveStatus(ctx, "invalid-channel")

	if err == nil {
		t.Fatal("Expected error for 404 response")
	}
}

func TestGetLiveStatus_RateLimit_TriggersCircuitBreaker(t *testing.T) {
	// 429 Rate Limit 응답 - Circuit Breaker 트리거
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":429,"message":"Rate limit exceeded"}`))
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	client := NewClient(http.DefaultClient, server.URL, logger)

	ctx := context.Background()

	// 3번 연속 실패 시 Circuit Breaker OPEN
	for i := range 3 {
		_, err := client.GetLiveStatus(ctx, "test-channel")
		if err == nil {
			t.Errorf("Attempt %d: Expected error, got nil", i+1)
		}
	}

	if callCount != 3 {
		t.Errorf("Expected 3 API calls, got: %d", callCount)
	}

	// 4번째 호출은 Circuit Breaker가 막아야 함
	callCountBefore := callCount
	_, err := client.GetLiveStatus(ctx, "test-channel")
	if err == nil {
		t.Error("Expected circuit breaker error")
	}
	if callCount != callCountBefore {
		t.Errorf("Circuit breaker should prevent API call, but callCount increased from %d to %d", callCountBefore, callCount)
	}
}

func TestGetLiveStatus_ContextTimeout(t *testing.T) {
	// 타임아웃 테스트
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond) // 200ms 지연
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	client := NewClient(http.DefaultClient, server.URL, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.GetLiveStatus(ctx, "test-channel")

	if err == nil {
		t.Fatal("Expected timeout error")
	}
}

func TestGetScheduledLives_Success(t *testing.T) {
	// 예정 방송 3개
	response := ScheduledLivesResponse{
		Code: 200,
		Content: &ScheduledLivesContent{
			ScheduledLives: []ScheduledLive{
				{
					LiveId:           101,
					LiveTitle:        "오후 3시 잡담",
					ScheduledStartAt: "2026-01-27 15:00:00",
				},
				{
					LiveId:           102,
					LiveTitle:        "저녁 노래방",
					ScheduledStartAt: "2026-01-27 19:00:00",
				},
				{
					LiveId:           103,
					LiveTitle:        "심야 게임",
					ScheduledStartAt: "2026-01-27 23:00:00",
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/service/v1/channels/test-channel/scheduled-lives" {
			t.Errorf("Unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	client := NewClient(http.DefaultClient, server.URL, logger)

	ctx := context.Background()
	lives, err := client.GetScheduledLives(ctx, "test-channel")

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if len(lives) != 3 {
		t.Fatalf("Expected 3 scheduled lives, got: %d", len(lives))
	}
	if lives[0].LiveTitle != "오후 3시 잡담" {
		t.Errorf("Expected first live title '오후 3시 잡담', got: %s", lives[0].LiveTitle)
	}
	if lives[2].LiveId != 103 {
		t.Errorf("Expected last live ID 103, got: %d", lives[2].LiveId)
	}
}

func TestGetScheduledLives_EmptyArray(t *testing.T) {
	// 예정 방송 없음
	response := ScheduledLivesResponse{
		Code: 200,
		Content: &ScheduledLivesContent{
			ScheduledLives: []ScheduledLive{},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	client := NewClient(http.DefaultClient, server.URL, logger)

	ctx := context.Background()
	lives, err := client.GetScheduledLives(ctx, "test-channel")

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if len(lives) != 0 {
		t.Errorf("Expected empty array, got: %d items", len(lives))
	}
}

func TestCircuitBreaker_AutoReset(t *testing.T) {
	// Circuit Breaker 자동 리셋 테스트
	callCount := 0
	forceError := true

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if forceError {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// 성공 응답
		response := LiveStatusResponse{
			Code: 200,
			Content: &LiveStatusContent{
				Status: "CLOSE",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	client := NewClient(http.DefaultClient, server.URL, logger)

	ctx := context.Background()

	// Step 1: 3회 연속 실패로 Circuit OPEN
	for range 3 {
		_, _ = client.GetLiveStatus(ctx, "test-channel")
	}

	// Step 2: Circuit OPEN 상태 확인
	if !client.IsCircuitOpen() {
		t.Error("Circuit should be open after 3 failures")
	}

	// Step 3: 30초 대기를 시뮬레이션 (강제로 circuitOpenUntil 조작)
	// 프로덕션에서는 30초 후 자동 리셋되지만, 테스트에서는 시간 조작
	past := time.Now().Add(-1 * time.Second) // 과거 시점으로 설정
	client.circuitMu.Lock()
	client.circuitOpenUntil = &past
	client.circuitMu.Unlock()

	// Step 4: Circuit이 자동으로 닫혀야 함
	if client.IsCircuitOpen() {
		t.Error("Circuit should be closed after timeout")
	}

	// Step 5: 에러 해제 후 정상 요청
	forceError = false
	callCountBefore := callCount

	_, err := client.GetLiveStatus(ctx, "test-channel")
	if err != nil {
		t.Fatalf("Expected successful request after circuit reset, got: %v", err)
	}

	if callCount <= callCountBefore {
		t.Error("Expected API call after circuit reset")
	}
}

func TestGetLiveStatus_ServerError_IncreasesFailureCount(t *testing.T) {
	// 500 Internal Server Error - 실패 카운트 증가
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":500,"message":"Internal server error"}`))
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	client := NewClient(http.DefaultClient, server.URL, logger)

	ctx := context.Background()

	// 첫 번째 실패
	_, err := client.GetLiveStatus(ctx, "test-channel")
	if err == nil {
		t.Error("Expected error for 500 response")
	}

	// 실패 카운트 확인 (내부 필드는 직접 접근 불가하므로 행동으로 검증)
	// 2번 더 실패 시 Circuit OPEN
	_, _ = client.GetLiveStatus(ctx, "test-channel")
	_, _ = client.GetLiveStatus(ctx, "test-channel")

	if !client.IsCircuitOpen() {
		t.Error("Circuit should be open after 3 consecutive 500 errors")
	}
}

func TestGetScheduledLives_NilContent(t *testing.T) {
	// Content가 nil인 경우 (API 에러 응답)
	response := ScheduledLivesResponse{
		Code:    400,
		Content: nil,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	client := NewClient(http.DefaultClient, server.URL, logger)

	ctx := context.Background()
	lives, err := client.GetScheduledLives(ctx, "test-channel")

	// Content가 nil이면 빈 배열 반환 (에러 아님)
	if err != nil {
		t.Errorf("Expected no error for nil content, got: %v", err)
	}
	if len(lives) != 0 {
		t.Errorf("Expected empty array for nil content, got: %d items", len(lives))
	}
}

func TestGetLiveStatus_ContextCancellation(t *testing.T) {
	// Context 취소 테스트
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	client := NewClient(http.DefaultClient, server.URL, logger)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 즉시 취소

	_, err := client.GetLiveStatus(ctx, "test-channel")

	if err == nil {
		t.Fatal("Expected error for cancelled context")
	}
}

func TestIsCircuitOpen_InitiallyFalse(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	client := NewClient(http.DefaultClient, DefaultBaseURL, logger)

	if client.IsCircuitOpen() {
		t.Error("Circuit should be closed initially")
	}
}

func TestClient_UserAgent(t *testing.T) {
	// User-Agent 헤더 검증
	var receivedUserAgent string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUserAgent = r.Header.Get("User-Agent")

		response := LiveStatusResponse{
			Code: 200,
			Content: &LiveStatusContent{
				Status: "CLOSE",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	client := NewClient(http.DefaultClient, server.URL, logger)

	ctx := context.Background()
	_, err := client.GetLiveStatus(ctx, "test-channel")

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	expectedUA := "api.capu.blog/hololive-bot (Chzzk API client)"
	if receivedUserAgent != expectedUA {
		t.Errorf("Expected User-Agent %q, got %q", expectedUA, receivedUserAgent)
	}
}

func TestGetLiveStatus_MalformedJSON(t *testing.T) {
	// 잘못된 JSON 응답
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"content":`)) // Incomplete JSON
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	client := NewClient(http.DefaultClient, server.URL, logger)

	ctx := context.Background()
	_, err := client.GetLiveStatus(ctx, "test-channel")

	if err == nil {
		t.Fatal("Expected error for malformed JSON")
	}
}

func TestGetLivesByChannelIDs_PaginatesAndFilters(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.URL.Path != "/open/v1/lives" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		var response OpenAPIResponse[LivesResponse]
		switch callCount {
		case 1:
			response = OpenAPIResponse[LivesResponse]{
				Code: http.StatusOK,
				Content: LivesResponse{
					Data: []LiveData{
						{ChannelID: "other", LiveTitle: "other-live"},
						{ChannelID: "target-1", LiveTitle: "target-live-1"},
					},
					Page: PageInfo{Next: "page-2"},
				},
			}
		case 2:
			response = OpenAPIResponse[LivesResponse]{
				Code: http.StatusOK,
				Content: LivesResponse{
					Data: []LiveData{
						{ChannelID: "target-2", LiveTitle: "target-live-2"},
					},
				},
			}
		default:
			t.Fatalf("unexpected extra call: %d", callCount)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClientWithConfig(ClientConfig{
		HTTPClient:   http.DefaultClient,
		BaseURL:      server.URL,
		ClientID:     "id",
		ClientSecret: "secret",
		Logger:       slog.New(slog.NewTextHandler(os.Stderr, nil)),
	})
	client.openAPIBaseURL = server.URL

	lives, err := client.GetLivesByChannelIDs(context.Background(), []string{"target-1", "target-2", "target-3", "target-4", "target-5"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lives) != 2 {
		t.Fatalf("expected 2 lives, got %d", len(lives))
	}
	if lives[0].ChannelID != "target-1" || lives[1].ChannelID != "target-2" {
		t.Fatalf("unexpected lives: %#v", lives)
	}
}

func TestGetLivesByChannelIDs_UsesStatusChecksForSmallTargetSet(t *testing.T) {
	var (
		liveStatusCalls atomic.Int32
		pageCalls       atomic.Int32
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/polling/v2/channels/") && strings.HasSuffix(r.URL.Path, "/live-status"):
			liveStatusCalls.Add(1)
			channelID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/polling/v2/channels/"), "/live-status")
			response := LiveStatusResponse{
				Code: 200,
				Content: &LiveStatusContent{
					Status:    "CLOSE",
					LiveTitle: "closed",
				},
			}
			if channelID == "target-1" {
				response.Content.Status = "OPEN"
				response.Content.LiveTitle = "target-live-1"
				response.Content.ConcurrentUserCount = 123
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		case r.URL.Path == "/open/v1/lives":
			pageCalls.Add(1)
			t.Fatalf("unexpected page scan request for small target set")
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClientWithConfig(ClientConfig{
		HTTPClient:   http.DefaultClient,
		BaseURL:      server.URL,
		ClientID:     "id",
		ClientSecret: "secret",
		Logger:       slog.New(slog.NewTextHandler(os.Stderr, nil)),
	})
	client.openAPIBaseURL = server.URL

	lives, err := client.GetLivesByChannelIDs(context.Background(), []string{"target-1", "target-2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lives) != 1 {
		t.Fatalf("expected 1 live, got %d", len(lives))
	}
	if lives[0].ChannelID != "target-1" {
		t.Fatalf("unexpected live result: %#v", lives)
	}
	if got := liveStatusCalls.Load(); got != 2 {
		t.Fatalf("live status calls = %d, want 2", got)
	}
	if got := pageCalls.Load(); got != 0 {
		t.Fatalf("page scan calls = %d, want 0", got)
	}
}

func TestGetLivesByChannelIDs_UsesPageScanForLargeTargetSet(t *testing.T) {
	var (
		liveStatusCalls atomic.Int32
		pageCalls       atomic.Int32
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/polling/v2/channels/"):
			liveStatusCalls.Add(1)
			t.Fatalf("unexpected live status request for large target set")
		case r.URL.Path == "/open/v1/lives":
			pageCalls.Add(1)
			response := OpenAPIResponse[LivesResponse]{
				Code: http.StatusOK,
				Content: LivesResponse{
					Data: []LiveData{{ChannelID: "target-1", LiveTitle: "target-live-1"}},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClientWithConfig(ClientConfig{
		HTTPClient:   http.DefaultClient,
		BaseURL:      server.URL,
		ClientID:     "id",
		ClientSecret: "secret",
		Logger:       slog.New(slog.NewTextHandler(os.Stderr, nil)),
	})
	client.openAPIBaseURL = server.URL

	channelIDs := make([]string, 0, constants.ChzzkConfig.BatchLookupThreshold+1)
	for i := 0; i < constants.ChzzkConfig.BatchLookupThreshold+1; i++ {
		channelIDs = append(channelIDs, fmt.Sprintf("target-%d", i+1))
	}

	lives, err := client.GetLivesByChannelIDs(context.Background(), channelIDs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lives) != 1 {
		t.Fatalf("expected 1 live, got %d", len(lives))
	}
	if got := pageCalls.Load(); got != 1 {
		t.Fatalf("page scan calls = %d, want 1", got)
	}
	if got := liveStatusCalls.Load(); got != 0 {
		t.Fatalf("live status calls = %d, want 0", got)
	}
}

func TestGetLivesByChannelIDs_EmptyTargets(t *testing.T) {
	client := NewClientWithConfig(ClientConfig{
		HTTPClient:   http.DefaultClient,
		BaseURL:      DefaultBaseURL,
		ClientID:     "id",
		ClientSecret: "secret",
		Logger:       slog.New(slog.NewTextHandler(os.Stderr, nil)),
	})

	lives, err := client.GetLivesByChannelIDs(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lives) != 0 {
		t.Fatalf("expected empty result, got %#v", lives)
	}
}

func BenchmarkGetLiveStatus(b *testing.B) {
	response := LiveStatusResponse{
		Code: 200,
		Content: &LiveStatusContent{
			Status: "OPEN",
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	client := NewClient(http.DefaultClient, server.URL, logger)

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := client.GetLiveStatus(ctx, fmt.Sprintf("channel-%d", i))
		if err != nil {
			b.Fatalf("Unexpected error: %v", err)
		}
	}
}
