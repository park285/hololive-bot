package twitch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	appErrors "github.com/kapu/hololive-kakao-bot-go/internal/errors"
	"github.com/kapu/hololive-shared/pkg/constants"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func httpResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestClient_EnsureValidToken_UsesCachedToken(t *testing.T) {
	c := newTestClient("id", "secret")
	c.token.Store("cached-token")
	c.tokenExpiry.Store(time.Now().Add(30 * time.Minute))

	called := 0
	c.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		called++
		return nil, fmt.Errorf("transport should not be called")
	})

	if err := c.ensureValidToken(context.Background()); err != nil {
		t.Fatalf("ensureValidToken error: %v", err)
	}
	if called != 0 {
		t.Fatalf("expected no HTTP call, got %d", called)
	}
}

func TestClient_RefreshToken_Success(t *testing.T) {
	c := newTestClient("id", "secret")

	c.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("method=%s want=POST", req.Method)
		}
		if req.URL.String() != constants.TwitchConfig.AuthURL {
			t.Fatalf("url=%s want=%s", req.URL.String(), constants.TwitchConfig.AuthURL)
		}
		return httpResponse(http.StatusOK, `{"access_token":"tok-1","expires_in":3600,"token_type":"bearer"}`), nil
	})

	if err := c.refreshToken(context.Background()); err != nil {
		t.Fatalf("refreshToken error: %v", err)
	}
	if token, _ := c.token.Load().(string); token != "tok-1" {
		t.Fatalf("token=%q want=%q", token, "tok-1")
	}
	expiry, _ := c.tokenExpiry.Load().(time.Time)
	if time.Until(expiry) < 59*time.Minute {
		t.Fatalf("expiry should be around 1 hour, got %s", expiry)
	}
}

func TestClient_RefreshToken_FailureStatus(t *testing.T) {
	c := newTestClient("id", "secret")
	c.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return httpResponse(http.StatusUnauthorized, `{"error":"bad_client"}`), nil
	})

	err := c.refreshToken(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "token request failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_GetStreams_CoreBranches(t *testing.T) {
	c := newTestClient("id", "secret")

	t.Run("not configured", func(t *testing.T) {
		notConfigured := newTestClient("", "")
		_, err := notConfigured.GetStreams(context.Background(), []string{"user1"})
		if err == nil || !strings.Contains(err.Error(), "not configured") {
			t.Fatalf("expected not configured error, got %v", err)
		}
	})

	t.Run("empty users", func(t *testing.T) {
		res, err := c.GetStreams(context.Background(), nil)
		if err != nil {
			t.Fatalf("empty users should not error: %v", err)
		}
		if res == nil || len(res.Data) != 0 {
			t.Fatalf("expected empty data, got %+v", res)
		}
	})

	t.Run("circuit open", func(t *testing.T) {
		circuitClient := newTestClient("id", "secret")
		circuitClient.circuitOpen.Store(true)
		circuitClient.circuitOpenedAt.Store(time.Now())

		_, err := circuitClient.GetStreams(context.Background(), []string{"user1"})
		var apiErr *appErrors.APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("expected APIError, got %v", err)
		}
		if apiErr.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("status=%d want=%d", apiErr.StatusCode, http.StatusServiceUnavailable)
		}
	})
}

func TestClient_GetStreams_SuccessAndHeaders(t *testing.T) {
	c := newTestClient("client-id", "secret")
	c.token.Store("valid-token")
	c.tokenExpiry.Store(time.Now().Add(1 * time.Hour))

	var gotAuthHeader, gotClientID string
	c.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotAuthHeader = req.Header.Get("Authorization")
		gotClientID = req.Header.Get("Client-Id")
		if req.Method != http.MethodGet {
			t.Fatalf("method=%s want=GET", req.Method)
		}
		if !strings.Contains(req.URL.String(), "user_login=tokoyami") {
			t.Fatalf("unexpected URL: %s", req.URL.String())
		}
		body := `{"data":[{"id":"1","user_login":"tokoyami","type":"live","title":"Live now"}],"pagination":{}}`
		return httpResponse(http.StatusOK, body), nil
	})

	res, err := c.GetStreams(context.Background(), []string{"tokoyami"})
	if err != nil {
		t.Fatalf("GetStreams error: %v", err)
	}
	if len(res.Data) != 1 || res.Data[0].UserLogin != "tokoyami" {
		t.Fatalf("unexpected stream data: %+v", res.Data)
	}
	if gotAuthHeader != "Bearer valid-token" {
		t.Fatalf("authorization header=%q", gotAuthHeader)
	}
	if gotClientID != "client-id" {
		t.Fatalf("client id header=%q", gotClientID)
	}
}

func TestClient_GetStreams_401RefreshAndRetry(t *testing.T) {
	c := newTestClient("client-id", "secret")

	var mu sync.Mutex
	tokenCalls := 0
	streamCalls := 0

	c.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()

		switch {
		case req.URL.String() == constants.TwitchConfig.AuthURL:
			tokenCalls++
			if tokenCalls == 1 {
				return httpResponse(http.StatusOK, `{"access_token":"tok-1","expires_in":3600,"token_type":"bearer"}`), nil
			}
			return httpResponse(http.StatusOK, `{"access_token":"tok-2","expires_in":3600,"token_type":"bearer"}`), nil
		case strings.Contains(req.URL.String(), constants.TwitchConfig.BaseURL+"/streams"):
			streamCalls++
			auth := req.Header.Get("Authorization")
			if streamCalls == 1 {
				if auth != "Bearer tok-1" {
					t.Fatalf("first stream auth=%q want Bearer tok-1", auth)
				}
				return httpResponse(http.StatusUnauthorized, `{"error":"unauthorized"}`), nil
			}
			if auth != "Bearer tok-2" {
				t.Fatalf("second stream auth=%q want Bearer tok-2", auth)
			}
			return httpResponse(http.StatusOK, `{"data":[],"pagination":{}}`), nil
		default:
			return nil, fmt.Errorf("unexpected URL: %s", req.URL.String())
		}
	})

	res, err := c.GetStreams(context.Background(), []string{"user-a"})
	if err != nil {
		t.Fatalf("GetStreams error: %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil response")
	}
	if tokenCalls != 2 {
		t.Fatalf("tokenCalls=%d want=2", tokenCalls)
	}
	if streamCalls != 2 {
		t.Fatalf("streamCalls=%d want=2", streamCalls)
	}
	if token, _ := c.token.Load().(string); token != "tok-2" {
		t.Fatalf("token=%q want=tok-2", token)
	}
}

func TestClient_GetStreams_ErrorStatusBranches(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantStatus int
	}{
		{name: "rate limited", statusCode: http.StatusTooManyRequests, wantStatus: http.StatusTooManyRequests},
		{name: "server error", statusCode: http.StatusBadGateway, wantStatus: http.StatusBadGateway},
		{name: "unexpected status", statusCode: http.StatusTeapot, wantStatus: http.StatusTeapot},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient("id", "secret")
			c.token.Store("valid-token")
			c.tokenExpiry.Store(time.Now().Add(1 * time.Hour))

			c.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.String(), constants.TwitchConfig.BaseURL+"/streams") {
					return httpResponse(tt.statusCode, `{}`), nil
				}
				return nil, fmt.Errorf("unexpected URL: %s", req.URL.String())
			})

			_, err := c.GetStreams(context.Background(), []string{"user-a"})
			if err == nil {
				t.Fatal("expected error")
			}

			var apiErr *appErrors.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected APIError, got %T %v", err, err)
			}
			if apiErr.StatusCode != tt.wantStatus {
				t.Fatalf("status=%d want=%d", apiErr.StatusCode, tt.wantStatus)
			}
		})
	}
}

func TestClient_GetStreams_RequestError(t *testing.T) {
	c := newTestClient("id", "secret")
	c.token.Store("valid-token")
	c.tokenExpiry.Store(time.Now().Add(1 * time.Hour))

	c.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("network down")
	})

	_, err := c.GetStreams(context.Background(), []string{"user-a"})
	if err == nil || !strings.Contains(err.Error(), "do request") {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.failureCount.Load() != 1 {
		t.Fatalf("failureCount=%d want=1", c.failureCount.Load())
	}
}
