package subscriptionclient_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/contracts/subscription"
	"github.com/kapu/hololive-shared/pkg/service/subscriptionclient"
	"github.com/park285/shared-go/pkg/httputil"
)

const testSubscriptionsPath = "/internal/subscriptions"

func newTestClient(t *testing.T, handler http.Handler) *subscriptionclient.Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &subscriptionclient.Client{
		HTTPClient:        httputil.NewJSONClient(server.URL, "test-api-key", 5*time.Second),
		SubscriptionsPath: testSubscriptionsPath,
	}
}

func assertStatusLookupRequest(t *testing.T, r *http.Request) {
	t.Helper()

	if r.Method != http.MethodGet {
		t.Errorf("method = %q, want %q", r.Method, http.MethodGet)
	}
	if want := testSubscriptionsPath + "/room-42"; r.URL.Path != want {
		t.Errorf("path = %q, want %q", r.URL.Path, want)
	}
	if got := r.Header.Get(httputil.HeaderAPIKey); got != "test-api-key" {
		t.Errorf("api key header = %q, want %q", got, "test-api-key")
	}
}

func TestIsSubscribedReturnsServerStatus(t *testing.T) {
	for _, tc := range []struct {
		name       string
		subscribed bool
	}{
		{name: "subscribed", subscribed: true},
		{name: "not subscribed", subscribed: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assertStatusLookupRequest(t, r)
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(subscription.SubscriptionStatusResponse{Subscribed: tc.subscribed}); err != nil {
					t.Errorf("encode response: %v", err)
				}
			}))

			got, err := client.IsSubscribed(context.Background(), " room-42 ")
			if err != nil {
				t.Fatalf("IsSubscribed() error = %v", err)
			}
			if got != tc.subscribed {
				t.Fatalf("IsSubscribed() = %v, want %v", got, tc.subscribed)
			}
		})
	}
}

func TestClientRejectsEmptyRoomIDWithoutRequest(t *testing.T) {
	var requests atomic.Int32
	client := newTestClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))

	for _, tc := range []struct {
		name string
		call func(ctx context.Context) error
	}{
		{name: "IsSubscribed", call: func(ctx context.Context) error {
			_, err := client.IsSubscribed(ctx, "   ")
			return err
		}},
		{name: "Subscribe", call: func(ctx context.Context) error {
			return client.Subscribe(ctx, "", "room name")
		}},
		{name: "Unsubscribe", call: func(ctx context.Context) error {
			return client.Unsubscribe(ctx, " ")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call(context.Background())
			if err == nil {
				t.Fatal("expected error for empty room id, got nil")
			}
			if !strings.Contains(err.Error(), "room id is required") {
				t.Fatalf("error = %q, want it to mention required room id", err)
			}
		})
	}

	if got := requests.Load(); got != 0 {
		t.Fatalf("server received %d requests, want 0", got)
	}
}

func TestIsSubscribedReturnsAPIErrorForNonSuccessStatus(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	_, err := client.IsSubscribed(context.Background(), "room-1")
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
	if !httputil.IsStatus(err, http.StatusInternalServerError) {
		t.Fatalf("error = %v, want APIError with status 500", err)
	}
	if !strings.Contains(err.Error(), "check status") {
		t.Fatalf("error = %q, want check status wrapping", err)
	}
}

func TestIsSubscribedFailsOnMalformedResponseBody(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte("not-json")); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))

	_, err := client.IsSubscribed(context.Background(), "room-1")
	if err == nil {
		t.Fatal("expected decode error, got nil")
	}
	if !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("error = %q, want decode response wrapping", err)
	}
}

func TestIsSubscribedHonorsContextDeadline(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.IsSubscribed(ctx, "room-1")
	if err == nil {
		t.Fatal("expected deadline error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded in chain", err)
	}
}

func TestSubscribeSendsTrimmedJSONPayload(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want %q", r.Method, http.MethodPost)
		}
		if r.URL.Path != testSubscriptionsPath {
			t.Errorf("path = %q, want %q", r.URL.Path, testSubscriptionsPath)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content type = %q, want %q", got, "application/json")
		}
		var req subscription.SubscribeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if req.RoomID != "room-7" {
			t.Errorf("room_id = %q, want %q", req.RoomID, "room-7")
		}
		if req.RoomName != "홀로 방" {
			t.Errorf("room_name = %q, want %q", req.RoomName, "홀로 방")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	if err := client.Subscribe(context.Background(), " room-7 ", " 홀로 방 "); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
}

func TestSubscribeReturnsAPIErrorForNonSuccessStatus(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))

	err := client.Subscribe(context.Background(), "room-7", "room name")
	if err == nil {
		t.Fatal("expected error for 503 response, got nil")
	}
	if !httputil.IsStatus(err, http.StatusServiceUnavailable) {
		t.Fatalf("error = %v, want APIError with status 503", err)
	}
}

func TestUnsubscribeSendsDeleteForTrimmedRoomID(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %q, want %q", r.Method, http.MethodDelete)
		}
		if want := testSubscriptionsPath + "/room-9"; r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}
		w.WriteHeader(http.StatusOK)
	}))

	if err := client.Unsubscribe(context.Background(), " room-9 "); err != nil {
		t.Fatalf("Unsubscribe() error = %v", err)
	}
}

func TestUnsubscribeReturnsAPIErrorForNonSuccessStatus(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	err := client.Unsubscribe(context.Background(), "room-9")
	if err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}
	if !httputil.IsStatus(err, http.StatusNotFound) {
		t.Fatalf("error = %v, want APIError with status 404", err)
	}
}
