package trigger

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
)

func TestClientSendWeeklyNotificationSuccess(t *testing.T) {
	t.Parallel()

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL+"/", "", nil)
	if err := client.SendWeeklyNotification(context.Background()); err != nil {
		t.Fatalf("SendWeeklyNotification() error = %v", err)
	}

	if gotPath != triggercontracts.MajorEventWeeklyPath {
		t.Fatalf("path = %q, want %q", gotPath, triggercontracts.MajorEventWeeklyPath)
	}
}

func TestClientSendMonthlyNotificationConflict(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL, "", nil)
	err := client.SendMonthlyNotification(context.Background())
	if !errors.Is(err, triggercontracts.ErrNotificationInProgress) {
		t.Fatalf("SendMonthlyNotification() error = %v, want ErrNotificationInProgress", err)
	}
}

func TestClientSendMemberNewsWeeklyNon2xx(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream failed"))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL, "", nil)
	err := client.SendMemberNewsWeekly(context.Background())
	if err == nil {
		t.Fatal("SendMemberNewsWeekly() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "status 502") {
		t.Fatalf("error = %q, expected status 502", err.Error())
	}
	if !strings.Contains(err.Error(), "upstream failed") {
		t.Fatalf("error = %q, expected response body", err.Error())
	}
}

func TestClientSendMemberNewsWeeklySuccess(t *testing.T) {
	t.Parallel()

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL, "", nil)
	if err := client.SendMemberNewsWeekly(context.Background()); err != nil {
		t.Fatalf("SendMemberNewsWeekly() error = %v", err)
	}
	if gotPath != triggercontracts.MemberNewsWeeklyPath {
		t.Fatalf("path = %q, want %q", gotPath, triggercontracts.MemberNewsWeeklyPath)
	}
}

func TestClientSendWeeklyNotificationWithAPIKey(t *testing.T) {
	t.Parallel()

	const apiKey = "test-key"
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get(sharedserver.APIKeyHeader)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL, apiKey, nil)
	if err := client.SendWeeklyNotification(context.Background()); err != nil {
		t.Fatalf("SendWeeklyNotification() error = %v", err)
	}
	if gotHeader != apiKey {
		t.Fatalf("%s header = %q, want %q", sharedserver.APIKeyHeader, gotHeader, apiKey)
	}
}
