package membernewsclient_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	commoncontracts "github.com/kapu/hololive-shared/pkg/contracts/common"
	membernewscontracts "github.com/kapu/hololive-shared/pkg/contracts/membernews"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/membernewsclient"
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

// sampleDigest: 테스트용 Digest 샘플 데이터.
func sampleDigest() membernewscontracts.Digest {
	return membernewscontracts.Digest{
		Period:   membernewscontracts.PeriodWeekly,
		Headline: "이번 주 멤버 소식 요약",
		TopItems: []membernewscontracts.SummaryItem{
			{
				Member:    "호카이마루",
				Category:  "방송",
				Title:     "새 노래 커버 공개",
				DateText:  "2026-03-01",
				Summary:   "새 노래를 커버했습니다.",
				SourceURL: "https://youtube.com/watch?v=abc",
			},
		},
		MoreSummary:  "추가 소식 2건",
		OmittedCount: 2,
		TotalCount:   3,
	}
}

func TestGenerateRoomDigest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		roomID       string
		period       membernewscontracts.Period
		statusCode   int
		responseBody any
		wantNilDigest bool
		wantErr      bool
		wantSentinel bool // membernewscontracts.ErrNoSubscribedMembers 여부
	}{
		{
			name:          "성공 (200 + 유효한 Digest JSON)",
			roomID:        "room-123",
			period:        membernewscontracts.PeriodWeekly,
			statusCode:    http.StatusOK,
			responseBody:  sampleDigest(),
			wantNilDigest: false,
			wantErr:       false,
		},
		{
			name:          "monthly 기간으로 성공",
			roomID:        "room-456",
			period:        membernewscontracts.PeriodMonthly,
			statusCode:    http.StatusOK,
			responseBody:  sampleDigest(),
			wantNilDigest: false,
			wantErr:       false,
		},
		{
			name:          "404 + no_subscribed_members → ErrNoSubscribedMembers",
			roomID:        "room-no-members",
			period:        membernewscontracts.PeriodWeekly,
			statusCode:    http.StatusNotFound,
			responseBody:  map[string]string{"error": "no_subscribed_members"},
			wantNilDigest: true,
			wantErr:       true,
			wantSentinel:  true,
		},
		{
			name:          "500 서버 에러 → 일반 에러",
			roomID:        "room-789",
			period:        membernewscontracts.PeriodWeekly,
			statusCode:    http.StatusInternalServerError,
			responseBody:  map[string]string{"error": "internal server error"},
			wantNilDigest: true,
			wantErr:       true,
			wantSentinel:  false,
		},
		{
			name:          "빈 roomID → 에러",
			roomID:        "",
			period:        membernewscontracts.PeriodWeekly,
			statusCode:    http.StatusOK,
			wantNilDigest: true,
			wantErr:       true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.roomID == "" {
				c := membernewsclient.New("http://localhost:0", testAPIKey)
				got, err := c.GenerateRoomDigest(context.Background(), tc.roomID, tc.period)
				if (err != nil) != tc.wantErr {
					t.Errorf("GenerateRoomDigest() err = %v, wantErr %v", err, tc.wantErr)
				}
				if (got == nil) != tc.wantNilDigest {
					t.Errorf("GenerateRoomDigest() digest nil = %v, want nil = %v", got == nil, tc.wantNilDigest)
				}
				return
			}

			srv := newTestServer(t, tc.statusCode, tc.responseBody, func(r *http.Request) {
				// HTTP 메서드 검증
				if r.Method != http.MethodPost {
					t.Errorf("method = %q, want POST", r.Method)
				}
				// 경로 검증
				if r.URL.Path != membernewscontracts.DigestPath {
					t.Errorf("path = %q, want %q", r.URL.Path, membernewscontracts.DigestPath)
				}
				// Content-Type 검증
				if ct := r.Header.Get("Content-Type"); ct != "application/json" {
					t.Errorf("Content-Type = %q, want application/json", ct)
				}
				// API 키 헤더 검증
				if r.Header.Get(commoncontracts.APIKeyHeader) != testAPIKey {
					t.Errorf("API 키 헤더 = %q, want %q", r.Header.Get(commoncontracts.APIKeyHeader), testAPIKey)
				}
			})
			defer srv.Close()

			c := membernewsclient.New(srv.URL, testAPIKey)
			got, err := c.GenerateRoomDigest(context.Background(), tc.roomID, tc.period)

			if (err != nil) != tc.wantErr {
				t.Errorf("GenerateRoomDigest() err = %v, wantErr %v", err, tc.wantErr)
			}
			if (got == nil) != tc.wantNilDigest {
				t.Errorf("GenerateRoomDigest() digest nil = %v, want nil = %v", got == nil, tc.wantNilDigest)
			}
			if tc.wantSentinel && !errors.Is(err, membernewscontracts.ErrNoSubscribedMembers) {
				t.Errorf("GenerateRoomDigest() err = %v, want ErrNoSubscribedMembers", err)
			}
		})
	}
}

func TestSubscribeRoom(t *testing.T) {
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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.roomID == "" {
				c := membernewsclient.New("http://localhost:0", testAPIKey)
				err := c.SubscribeRoom(context.Background(), tc.roomID, tc.roomName)
				if (err != nil) != tc.wantErr {
					t.Errorf("SubscribeRoom() err = %v, wantErr %v", err, tc.wantErr)
				}
				return
			}

			srv := newTestServer(t, tc.statusCode, nil, func(r *http.Request) {
				// HTTP 메서드 검증
				if r.Method != http.MethodPost {
					t.Errorf("method = %q, want POST", r.Method)
				}
				// 경로 검증
				if r.URL.Path != membernewscontracts.SubscriptionsPath {
					t.Errorf("path = %q, want %q", r.URL.Path, membernewscontracts.SubscriptionsPath)
				}
				// API 키 헤더 검증
				if r.Header.Get(commoncontracts.APIKeyHeader) != testAPIKey {
					t.Errorf("API 키 헤더 = %q, want %q", r.Header.Get(commoncontracts.APIKeyHeader), testAPIKey)
				}
			})
			defer srv.Close()

			c := membernewsclient.New(srv.URL, testAPIKey)
			err := c.SubscribeRoom(context.Background(), tc.roomID, tc.roomName)

			if (err != nil) != tc.wantErr {
				t.Errorf("SubscribeRoom() err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestUnsubscribeRoom(t *testing.T) {
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
			name:       "404 상태 코드 → 에러",
			roomID:     "room-not-found",
			statusCode: http.StatusNotFound,
			wantErr:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.roomID == "" {
				c := membernewsclient.New("http://localhost:0", testAPIKey)
				err := c.UnsubscribeRoom(context.Background(), tc.roomID)
				if (err != nil) != tc.wantErr {
					t.Errorf("UnsubscribeRoom() err = %v, wantErr %v", err, tc.wantErr)
				}
				return
			}

			srv := newTestServer(t, tc.statusCode, nil, func(r *http.Request) {
				// HTTP 메서드 검증
				if r.Method != http.MethodDelete {
					t.Errorf("method = %q, want DELETE", r.Method)
				}
				// 경로 검증
				wantPath := membernewscontracts.SubscriptionsPath + "/" + tc.roomID
				if r.URL.Path != wantPath {
					t.Errorf("path = %q, want %q", r.URL.Path, wantPath)
				}
				// API 키 헤더 검증
				if r.Header.Get(commoncontracts.APIKeyHeader) != testAPIKey {
					t.Errorf("API 키 헤더 = %q, want %q", r.Header.Get(commoncontracts.APIKeyHeader), testAPIKey)
				}
			})
			defer srv.Close()

			c := membernewsclient.New(srv.URL, testAPIKey)
			err := c.UnsubscribeRoom(context.Background(), tc.roomID)

			if (err != nil) != tc.wantErr {
				t.Errorf("UnsubscribeRoom() err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestIsRoomSubscribed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		roomID       string
		statusCode   int
		responseBody any
		wantResult   bool
		wantErr      bool
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
			name:       "빈 roomID → 에러",
			roomID:     "",
			statusCode: http.StatusOK,
			wantResult: false,
			wantErr:    true,
		},
		{
			name:         "500 상태 코드 → 에러",
			roomID:       "room-789",
			statusCode:   http.StatusInternalServerError,
			responseBody: map[string]string{"error": "internal server error"},
			wantResult:   false,
			wantErr:      true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.roomID == "" {
				c := membernewsclient.New("http://localhost:0", testAPIKey)
				got, err := c.IsRoomSubscribed(context.Background(), tc.roomID)
				if (err != nil) != tc.wantErr {
					t.Errorf("IsRoomSubscribed() err = %v, wantErr %v", err, tc.wantErr)
				}
				if got != tc.wantResult {
					t.Errorf("IsRoomSubscribed() = %v, want %v", got, tc.wantResult)
				}
				return
			}

			srv := newTestServer(t, tc.statusCode, tc.responseBody, func(r *http.Request) {
				// HTTP 메서드 검증
				if r.Method != http.MethodGet {
					t.Errorf("method = %q, want GET", r.Method)
				}
				// 경로 검증
				wantPath := membernewscontracts.SubscriptionsPath + "/" + tc.roomID
				if r.URL.Path != wantPath {
					t.Errorf("path = %q, want %q", r.URL.Path, wantPath)
				}
				// API 키 헤더 검증
				if r.Header.Get(commoncontracts.APIKeyHeader) != testAPIKey {
					t.Errorf("API 키 헤더 = %q, want %q", r.Header.Get(commoncontracts.APIKeyHeader), testAPIKey)
				}
			})
			defer srv.Close()

			c := membernewsclient.New(srv.URL, testAPIKey)
			got, err := c.IsRoomSubscribed(context.Background(), tc.roomID)

			if (err != nil) != tc.wantErr {
				t.Errorf("IsRoomSubscribed() err = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.wantResult {
				t.Errorf("IsRoomSubscribed() = %v, want %v", got, tc.wantResult)
			}
		})
	}
}

func TestIsNoSubscribedMembers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     error
		want    bool
	}{
		{
			name: "ErrNoSubscribedMembers → true",
			err:  membernewscontracts.ErrNoSubscribedMembers,
			want: true,
		},
		{
			name: "래핑된 ErrNoSubscribedMembers → true",
			err:  errors.New("context: " + membernewscontracts.ErrNoSubscribedMembers.Error()),
			want: false, // fmt.Errorf로 래핑하지 않으면 Is() 체인에서 탐지 안 됨
		},
		{
			name: "다른 에러 → false",
			err:  errors.New("some other error"),
			want: false,
		},
		{
			name: "nil 에러 → false",
			err:  nil,
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := membernewsclient.IsNoSubscribedMembers(tc.err)
			if got != tc.want {
				t.Errorf("IsNoSubscribedMembers(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsNoSubscribedMembers_WrappedSentinel(t *testing.T) {
	t.Parallel()

	// GenerateRoomDigest가 반환하는 실제 sentinel 에러도 감지해야 합니다
	srv := newTestServer(t, http.StatusNotFound, map[string]string{"error": "no_subscribed_members"}, nil)
	defer srv.Close()

	c := membernewsclient.New(srv.URL, testAPIKey)
	_, err := c.GenerateRoomDigest(context.Background(), "room-1", membernewscontracts.PeriodWeekly)

	if !membernewsclient.IsNoSubscribedMembers(err) {
		t.Errorf("GenerateRoomDigest()가 반환한 에러에서 IsNoSubscribedMembers() = false, want true; err = %v", err)
	}
}
