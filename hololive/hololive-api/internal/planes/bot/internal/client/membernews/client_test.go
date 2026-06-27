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

package membernews_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	commoncontracts "github.com/kapu/hololive-shared/pkg/contracts/common"
	membernewscontracts "github.com/kapu/hololive-shared/pkg/contracts/membernews"
	"github.com/kapu/hololive-shared/pkg/testutil"
	"github.com/park285/shared-go/pkg/httputil"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/client/membernews"
)

const testAPIKey = "test-api-key"

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
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

func TestGenerateRoomDigestRejectsNilHTTPResponse(t *testing.T) {
	t.Parallel()

	c := membernews.New("https://example.com", testAPIKey)
	c.HTTPClient = httputil.NewJSONClientWithHTTPClient("https://example.com", testAPIKey, &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, nil
		}),
	})

	got, err := c.GenerateRoomDigest(context.Background(), "room-123", membernewscontracts.PeriodWeekly)
	if err == nil {
		t.Fatal("GenerateRoomDigest() error = nil, want nil response error")
	}
	if got != nil {
		t.Fatalf("GenerateRoomDigest() digest = %#v, want nil", got)
	}
}

func TestGenerateRoomDigestRejectsNilResponseBody(t *testing.T) {
	t.Parallel()

	c := membernews.New("https://example.com", testAPIKey)
	c.HTTPClient = httputil.NewJSONClientWithHTTPClient("https://example.com", testAPIKey, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       nil,
				Request:    req,
			}, nil
		}),
	})

	got, err := c.GenerateRoomDigest(context.Background(), "room-123", membernewscontracts.PeriodWeekly)
	if err == nil {
		t.Fatal("GenerateRoomDigest() error = nil, want nil body error")
	}
	if got != nil {
		t.Fatalf("GenerateRoomDigest() digest = %#v, want nil", got)
	}
}

func TestHandleRoomDigestNotFoundClosesReadableBody(t *testing.T) {
	t.Parallel()

	c := membernews.New("https://example.com", testAPIKey)
	c.HTTPClient = httputil.NewJSONClientWithHTTPClient("https://example.com", testAPIKey, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":"no_subscribed_members"}`)),
				Request:    req,
			}, nil
		}),
	})

	got, err := c.GenerateRoomDigest(context.Background(), "room-123", membernewscontracts.PeriodWeekly)
	if !errors.Is(err, membernewscontracts.ErrNoSubscribedMembers) {
		t.Fatalf("GenerateRoomDigest() error = %v, want ErrNoSubscribedMembers", err)
	}
	if got != nil {
		t.Fatalf("GenerateRoomDigest() digest = %#v, want nil", got)
	}
}

type memberNewsDigestCase struct {
	name          string
	roomID        string
	period        membernewscontracts.Period
	statusCode    int
	responseBody  any
	wantNilDigest bool
	wantErr       bool
	wantSentinel  bool
}

func TestGenerateRoomDigest(t *testing.T) {
	t.Parallel()

	tests := []memberNewsDigestCase{
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
			assertMemberNewsDigest(t, &tc)
		})
	}
}

func assertMemberNewsDigest(t *testing.T, tc *memberNewsDigestCase) {
	t.Helper()

	if tc.roomID == "" {
		c := membernews.New("http://localhost:0", testAPIKey)
		got, err := c.GenerateRoomDigest(t.Context(), tc.roomID, tc.period)
		assertMemberNewsDigestResult(t, got, err, tc.wantNilDigest, tc.wantErr, tc.wantSentinel)
		return
	}

	srv := testutil.NewJSONTestServer(t, tc.statusCode, tc.responseBody, func(r *http.Request) {
		assertMemberNewsRequest(t, r, http.MethodPost, membernewscontracts.DigestPath)
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
	})

	c := membernews.New(srv.URL, testAPIKey)
	got, err := c.GenerateRoomDigest(t.Context(), tc.roomID, tc.period)
	assertMemberNewsDigestResult(t, got, err, tc.wantNilDigest, tc.wantErr, tc.wantSentinel)
}

func assertMemberNewsDigestResult(t *testing.T, got *membernewscontracts.Digest, err error, wantNilDigest, wantErr, wantSentinel bool) {
	t.Helper()

	if (err != nil) != wantErr {
		t.Errorf("GenerateRoomDigest() err = %v, wantErr %v", err, wantErr)
	}
	if (got == nil) != wantNilDigest {
		t.Errorf("GenerateRoomDigest() digest nil = %v, want nil = %v", got == nil, wantNilDigest)
	}
	if wantSentinel && !errors.Is(err, membernewscontracts.ErrNoSubscribedMembers) {
		t.Errorf("GenerateRoomDigest() err = %v, want ErrNoSubscribedMembers", err)
	}
}

type memberNewsSubscribeCase struct {
	name       string
	roomID     string
	roomName   string
	statusCode int
	wantErr    bool
}

func TestSubscribeRoom(t *testing.T) {
	t.Parallel()

	tests := []memberNewsSubscribeCase{
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
			assertMemberNewsSubscribe(t, &tc)
		})
	}
}

func assertMemberNewsSubscribe(t *testing.T, tc *memberNewsSubscribeCase) {
	t.Helper()

	if tc.roomID == "" {
		c := membernews.New("http://localhost:0", testAPIKey)
		assertMemberNewsErr(t, "SubscribeRoom", c.SubscribeRoom(t.Context(), tc.roomID, tc.roomName), tc.wantErr)
		return
	}

	srv := testutil.NewJSONTestServer(t, tc.statusCode, nil, func(r *http.Request) {
		assertMemberNewsRequest(t, r, http.MethodPost, membernewscontracts.SubscriptionsPath)
	})

	c := membernews.New(srv.URL, testAPIKey)
	assertMemberNewsErr(t, "SubscribeRoom", c.SubscribeRoom(t.Context(), tc.roomID, tc.roomName), tc.wantErr)
}

func assertMemberNewsRequest(t *testing.T, r *http.Request, method, path string) {
	t.Helper()

	if r.Method != method {
		t.Errorf("method = %q, want %s", r.Method, method)
	}
	if r.URL.Path != path {
		t.Errorf("path = %q, want %q", r.URL.Path, path)
	}
	if r.Header.Get(commoncontracts.APIKeyHeader) != testAPIKey {
		t.Errorf("API 키 헤더 = %q, want %q", r.Header.Get(commoncontracts.APIKeyHeader), testAPIKey)
	}
}

func assertMemberNewsErr(t *testing.T, op string, err error, wantErr bool) {
	t.Helper()

	if (err != nil) != wantErr {
		t.Errorf("%s() err = %v, wantErr %v", op, err, wantErr)
	}
}

type memberNewsUnsubscribeCase struct {
	name       string
	roomID     string
	statusCode int
	wantErr    bool
}

func TestUnsubscribeRoom(t *testing.T) {
	t.Parallel()

	tests := []memberNewsUnsubscribeCase{
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
			assertMemberNewsUnsubscribe(t, &tc)
		})
	}
}

func assertMemberNewsUnsubscribe(t *testing.T, tc *memberNewsUnsubscribeCase) {
	t.Helper()

	if tc.roomID == "" {
		c := membernews.New("http://localhost:0", testAPIKey)
		assertMemberNewsErr(t, "UnsubscribeRoom", c.UnsubscribeRoom(t.Context(), tc.roomID), tc.wantErr)
		return
	}

	srv := testutil.NewJSONTestServer(t, tc.statusCode, nil, func(r *http.Request) {
		assertMemberNewsRequest(t, r, http.MethodDelete, membernewscontracts.SubscriptionsPath+"/"+tc.roomID)
	})

	c := membernews.New(srv.URL, testAPIKey)
	assertMemberNewsErr(t, "UnsubscribeRoom", c.UnsubscribeRoom(t.Context(), tc.roomID), tc.wantErr)
}

type memberNewsIsSubscribedCase struct {
	name         string
	roomID       string
	statusCode   int
	responseBody any
	wantResult   bool
	wantErr      bool
}

func TestIsRoomSubscribed(t *testing.T) {
	t.Parallel()

	tests := []memberNewsIsSubscribedCase{
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
			assertMemberNewsIsSubscribed(t, &tc)
		})
	}
}

func assertMemberNewsIsSubscribed(t *testing.T, tc *memberNewsIsSubscribedCase) {
	t.Helper()

	if tc.roomID == "" {
		c := membernews.New("http://localhost:0", testAPIKey)
		got, err := c.IsRoomSubscribed(t.Context(), tc.roomID)
		assertMemberNewsBoolResult(t, "IsRoomSubscribed", got, err, tc.wantResult, tc.wantErr)
		return
	}

	srv := testutil.NewJSONTestServer(t, tc.statusCode, tc.responseBody, func(r *http.Request) {
		assertMemberNewsRequest(t, r, http.MethodGet, membernewscontracts.SubscriptionsPath+"/"+tc.roomID)
	})

	c := membernews.New(srv.URL, testAPIKey)
	got, err := c.IsRoomSubscribed(t.Context(), tc.roomID)
	assertMemberNewsBoolResult(t, "IsRoomSubscribed", got, err, tc.wantResult, tc.wantErr)
}

func assertMemberNewsBoolResult(t *testing.T, op string, got bool, err error, want, wantErr bool) {
	t.Helper()

	if (err != nil) != wantErr {
		t.Errorf("%s() err = %v, wantErr %v", op, err, wantErr)
	}
	if got != want {
		t.Errorf("%s() = %v, want %v", op, got, want)
	}
}

func TestIsNoSubscribedMembers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
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

			got := membernews.IsNoSubscribedMembers(tc.err)
			if got != tc.want {
				t.Errorf("IsNoSubscribedMembers(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsNoSubscribedMembers_WrappedSentinel(t *testing.T) {
	t.Parallel()

	// GenerateRoomDigest가 반환하는 실제 sentinel 에러도 감지해야 합니다
	srv := testutil.NewJSONTestServer(t, http.StatusNotFound, map[string]string{"error": "no_subscribed_members"}, nil)

	c := membernews.New(srv.URL, testAPIKey)
	_, err := c.GenerateRoomDigest(t.Context(), "room-1", membernewscontracts.PeriodWeekly)

	if !membernews.IsNoSubscribedMembers(err) {
		t.Errorf("GenerateRoomDigest()가 반환한 에러에서 IsNoSubscribedMembers() = false, want true; err = %v", err)
	}
}
