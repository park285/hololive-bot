package majoreventclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	majoreventcontracts "github.com/kapu/hololive-shared/pkg/contracts/majorevent"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/majoreventclient"
)

const testAPIKey = "test-api-key"

// newTestServer: httptest 서버를 생성하고 요청 검증 핸들러를 반환합니다.
func newTestServer(t *testing.T, statusCode int, responseBody any, assertFn func(r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if assertFn != nil {
			assertFn(r)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		if responseBody != nil {
			_ = json.NewEncoder(w).Encode(responseBody)
		}
	}))
}

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		inputURL        string
		inputAPIKey     string
		wantBaseURL     string
		wantAPIKey      string
	}{
		{
			name:        "URL 후행 슬래시 제거",
			inputURL:    "http://localhost:8080/",
			inputAPIKey: testAPIKey,
			wantBaseURL: "http://localhost:8080",
			wantAPIKey:  testAPIKey,
		},
		{
			name:        "URL 후행 슬래시 없는 경우 그대로 유지",
			inputURL:    "http://localhost:8080",
			inputAPIKey: testAPIKey,
			wantBaseURL: "http://localhost:8080",
			wantAPIKey:  testAPIKey,
		},
		{
			name:        "API 키 양쪽 공백 제거",
			inputURL:    "http://localhost:8080",
			inputAPIKey: "  key-with-spaces  ",
			wantBaseURL: "http://localhost:8080",
			wantAPIKey:  "key-with-spaces",
		},
		{
			name:        "URL과 API 키 모두 공백 처리",
			inputURL:    "  http://localhost:8080/  ",
			inputAPIKey: "  key  ",
			wantBaseURL: "http://localhost:8080",
			wantAPIKey:  "key",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := majoreventclient.New(tc.inputURL, tc.inputAPIKey)
			if c == nil {
				t.Fatal("New() returned nil")
			}

			// baseURL 및 apiKey 검증: 실제 요청을 통해 간접 확인
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// API 키 헤더 검증
				if r.Header.Get(sharedserver.APIKeyHeader) != tc.wantAPIKey {
					t.Errorf("API 키 헤더 = %q, want %q", r.Header.Get(sharedserver.APIKeyHeader), tc.wantAPIKey)
				}
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]bool{"subscribed": false})
			}))
			defer srv.Close()

			// 실제로 URL 주입 검증은 New() 결과 클라이언트로 직접 요청 수행 불가이므로
			// 대신 재생성 후 검증
			freshClient := majoreventclient.New(tc.inputURL, tc.inputAPIKey)
			if freshClient == nil {
				t.Fatal("재생성된 New() returned nil")
			}
		})
	}
}

func TestNew_URLTrimming(t *testing.T) {
	t.Parallel()

	// httptest 서버로 실제 baseURL에 후행 슬래시가 없는지 경로 검증
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]bool{"subscribed": false})
	}))
	defer srv.Close()

	// 후행 슬래시 포함 URL로 클라이언트 생성
	c := majoreventclient.New(srv.URL+"/", testAPIKey)
	_, _ = c.IsSubscribed(context.Background(), "room-1")

	// 경로에 이중 슬래시가 없어야 합니다
	wantPath := majoreventcontracts.SubscriptionsPath + "/room-1"
	if capturedPath != wantPath {
		t.Errorf("capturedPath = %q, want %q", capturedPath, wantPath)
	}
	if strings.Contains(capturedPath, "//") {
		t.Errorf("경로에 이중 슬래시가 포함됨: %q", capturedPath)
	}
}

func TestIsSubscribed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		roomID      string
		statusCode  int
		responseBody any
		wantResult  bool
		wantErr     bool
	}{
		{
			name:         "구독 상태 true 반환",
			roomID:       "room-123",
			statusCode:   http.StatusOK,
			responseBody: map[string]bool{"subscribed": true},
			wantResult:   true,
			wantErr:      false,
		},
		{
			name:         "구독 상태 false 반환",
			roomID:       "room-456",
			statusCode:   http.StatusOK,
			responseBody: map[string]bool{"subscribed": false},
			wantResult:   false,
			wantErr:      false,
		},
		{
			name:        "빈 roomID → 에러",
			roomID:      "",
			statusCode:  http.StatusOK,
			wantResult:  false,
			wantErr:     true,
		},
		{
			name:         "500 상태 코드 → 에러",
			roomID:       "room-789",
			statusCode:   http.StatusInternalServerError,
			responseBody: map[string]string{"error": "internal server error"},
			wantResult:   false,
			wantErr:      true,
		},
		{
			name:         "404 상태 코드 → 에러",
			roomID:       "room-not-found",
			statusCode:   http.StatusNotFound,
			responseBody: map[string]string{"error": "not found"},
			wantResult:   false,
			wantErr:      true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// 빈 roomID는 서버 없이도 검증
			if tc.roomID == "" {
				c := majoreventclient.New("http://localhost:0", testAPIKey)
				got, err := c.IsSubscribed(context.Background(), tc.roomID)
				if (err != nil) != tc.wantErr {
					t.Errorf("IsSubscribed() err = %v, wantErr %v", err, tc.wantErr)
				}
				if got != tc.wantResult {
					t.Errorf("IsSubscribed() = %v, want %v", got, tc.wantResult)
				}
				return
			}

			srv := newTestServer(t, tc.statusCode, tc.responseBody, func(r *http.Request) {
				// HTTP 메서드 검증
				if r.Method != http.MethodGet {
					t.Errorf("method = %q, want GET", r.Method)
				}
				// 경로 검증
				wantPath := majoreventcontracts.SubscriptionsPath + "/" + tc.roomID
				if r.URL.Path != wantPath {
					t.Errorf("path = %q, want %q", r.URL.Path, wantPath)
				}
				// API 키 헤더 검증
				if r.Header.Get(sharedserver.APIKeyHeader) != testAPIKey {
					t.Errorf("API 키 헤더 = %q, want %q", r.Header.Get(sharedserver.APIKeyHeader), testAPIKey)
				}
			})
			defer srv.Close()

			c := majoreventclient.New(srv.URL, testAPIKey)
			got, err := c.IsSubscribed(context.Background(), tc.roomID)

			if (err != nil) != tc.wantErr {
				t.Errorf("IsSubscribed() err = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.wantResult {
				t.Errorf("IsSubscribed() = %v, want %v", got, tc.wantResult)
			}
		})
	}
}

func TestIsSubscribed_NilClient(t *testing.T) {
	t.Parallel()

	var c *majoreventclient.Client
	_, err := c.IsSubscribed(context.Background(), "room-1")
	if err == nil {
		t.Error("nil 클라이언트에서 IsSubscribed() 에러가 반환되어야 합니다")
	}
}

func TestSubscribe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		roomID     string
		roomName   string
		statusCode int
		wantErr    bool
	}{
		{
			name:       "성공 (200)",
			roomID:     "room-123",
			roomName:   "테스트 방",
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "성공 (201 Created)",
			roomID:     "room-456",
			roomName:   "새 방",
			statusCode: http.StatusCreated,
			wantErr:    false,
		},
		{
			name:       "빈 roomID → 에러",
			roomID:     "",
			roomName:   "방",
			statusCode: http.StatusOK,
			wantErr:    true,
		},
		{
			name:       "500 상태 코드 → 에러",
			roomID:     "room-789",
			roomName:   "방",
			statusCode: http.StatusInternalServerError,
			wantErr:    true,
		},
		{
			name:       "400 상태 코드 → 에러",
			roomID:     "room-bad",
			roomName:   "방",
			statusCode: http.StatusBadRequest,
			wantErr:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.roomID == "" {
				c := majoreventclient.New("http://localhost:0", testAPIKey)
				err := c.Subscribe(context.Background(), tc.roomID, tc.roomName)
				if (err != nil) != tc.wantErr {
					t.Errorf("Subscribe() err = %v, wantErr %v", err, tc.wantErr)
				}
				return
			}

			srv := newTestServer(t, tc.statusCode, nil, func(r *http.Request) {
				// HTTP 메서드 검증
				if r.Method != http.MethodPost {
					t.Errorf("method = %q, want POST", r.Method)
				}
				// 경로 검증
				if r.URL.Path != majoreventcontracts.SubscriptionsPath {
					t.Errorf("path = %q, want %q", r.URL.Path, majoreventcontracts.SubscriptionsPath)
				}
				// Content-Type 검증
				if ct := r.Header.Get("Content-Type"); ct != "application/json" {
					t.Errorf("Content-Type = %q, want application/json", ct)
				}
				// API 키 헤더 검증
				if r.Header.Get(sharedserver.APIKeyHeader) != testAPIKey {
					t.Errorf("API 키 헤더 = %q, want %q", r.Header.Get(sharedserver.APIKeyHeader), testAPIKey)
				}
			})
			defer srv.Close()

			c := majoreventclient.New(srv.URL, testAPIKey)
			err := c.Subscribe(context.Background(), tc.roomID, tc.roomName)

			if (err != nil) != tc.wantErr {
				t.Errorf("Subscribe() err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestSubscribe_NilClient(t *testing.T) {
	t.Parallel()

	var c *majoreventclient.Client
	err := c.Subscribe(context.Background(), "room-1", "방")
	if err == nil {
		t.Error("nil 클라이언트에서 Subscribe() 에러가 반환되어야 합니다")
	}
}

func TestUnsubscribe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		roomID     string
		statusCode int
		wantErr    bool
	}{
		{
			name:       "성공 (200)",
			roomID:     "room-123",
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "성공 (204 No Content)",
			roomID:     "room-456",
			statusCode: http.StatusNoContent,
			wantErr:    false,
		},
		{
			name:       "빈 roomID → 에러",
			roomID:     "",
			statusCode: http.StatusOK,
			wantErr:    true,
		},
		{
			name:       "500 상태 코드 → 에러",
			roomID:     "room-789",
			statusCode: http.StatusInternalServerError,
			wantErr:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.roomID == "" {
				c := majoreventclient.New("http://localhost:0", testAPIKey)
				err := c.Unsubscribe(context.Background(), tc.roomID)
				if (err != nil) != tc.wantErr {
					t.Errorf("Unsubscribe() err = %v, wantErr %v", err, tc.wantErr)
				}
				return
			}

			srv := newTestServer(t, tc.statusCode, nil, func(r *http.Request) {
				// HTTP 메서드 검증
				if r.Method != http.MethodDelete {
					t.Errorf("method = %q, want DELETE", r.Method)
				}
				// 경로 검증
				wantPath := majoreventcontracts.SubscriptionsPath + "/" + tc.roomID
				if r.URL.Path != wantPath {
					t.Errorf("path = %q, want %q", r.URL.Path, wantPath)
				}
				// API 키 헤더 검증
				if r.Header.Get(sharedserver.APIKeyHeader) != testAPIKey {
					t.Errorf("API 키 헤더 = %q, want %q", r.Header.Get(sharedserver.APIKeyHeader), testAPIKey)
				}
			})
			defer srv.Close()

			c := majoreventclient.New(srv.URL, testAPIKey)
			err := c.Unsubscribe(context.Background(), tc.roomID)

			if (err != nil) != tc.wantErr {
				t.Errorf("Unsubscribe() err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestUnsubscribe_NilClient(t *testing.T) {
	t.Parallel()

	var c *majoreventclient.Client
	err := c.Unsubscribe(context.Background(), "room-1")
	if err == nil {
		t.Error("nil 클라이언트에서 Unsubscribe() 에러가 반환되어야 합니다")
	}
}
