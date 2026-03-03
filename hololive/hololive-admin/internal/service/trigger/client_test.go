package trigger

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
)

func TestClient_SendWeeklyNotification_Success(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL+"/", "", nil)
	if err := client.SendWeeklyNotification(context.Background()); err != nil {
		t.Fatalf("SendWeeklyNotification() error = %v, want nil", err)
	}
	if gotPath != triggercontracts.MajorEventWeeklyPath {
		t.Fatalf("path = %q, want %q", gotPath, triggercontracts.MajorEventWeeklyPath)
	}
}

func TestClient_SendMonthlyNotification_Conflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL, "", nil)
	err := client.SendMonthlyNotification(context.Background())
	if !errors.Is(err, triggercontracts.ErrNotificationInProgress) {
		t.Fatalf("error = %v, want ErrNotificationInProgress", err)
	}
}

func TestClient_SendWeeklyNotification_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream failed"))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL, "", nil)
	err := client.SendWeeklyNotification(context.Background())
	if err == nil {
		t.Fatal("SendWeeklyNotification() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "status 502") {
		t.Fatalf("error = %q, expected to contain status 502", err.Error())
	}
	if !strings.Contains(err.Error(), "upstream failed") {
		t.Fatalf("error = %q, expected to contain response body", err.Error())
	}
}

func TestClient_SendMemberNewsWeekly_Success(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL, "", nil)
	if err := client.SendMemberNewsWeekly(context.Background()); err != nil {
		t.Fatalf("SendMemberNewsWeekly() error = %v, want nil", err)
	}
	if gotPath != triggercontracts.MemberNewsWeeklyPath {
		t.Fatalf("path = %q, want %q", gotPath, triggercontracts.MemberNewsWeeklyPath)
	}
}

func TestClient_SendWeeklyNotification_WithAPIKey(t *testing.T) {
	const apiKey = "test-key"
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get(sharedserver.APIKeyHeader)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL, apiKey, nil)
	if err := client.SendWeeklyNotification(context.Background()); err != nil {
		t.Fatalf("SendWeeklyNotification() error = %v, want nil", err)
	}
	if gotHeader != apiKey {
		t.Fatalf("header %s = %q, want %q", sharedserver.APIKeyHeader, gotHeader, apiKey)
	}
}
