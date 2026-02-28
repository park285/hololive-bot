package majorevent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type fakeLinkCheckRepo struct {
	events              []*domain.MajorEvent
	updates             map[int]domain.MajorEventLinkStatus
	capturedStaleBefore time.Time
	capturedLimit       int
}

func (f *fakeLinkCheckRepo) ListEventsNeedingLinkCheck(_ context.Context, staleBefore time.Time, limit int) ([]*domain.MajorEvent, error) {
	f.capturedStaleBefore = staleBefore
	f.capturedLimit = limit
	return f.events, nil
}

func (f *fakeLinkCheckRepo) UpdateEventLinkStatus(_ context.Context, eventID int, status domain.MajorEventLinkStatus, _ time.Time) error {
	if f.updates == nil {
		f.updates = make(map[int]domain.MajorEventLinkStatus)
	}
	f.updates[eventID] = status
	return nil
}

type fakeHTTPResult struct {
	statusCode int
	err        error
}

type fakeHTTPClient struct {
	results map[string][]fakeHTTPResult
	calls   []string
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func (f *fakeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	key := req.Method + " " + req.URL.String()
	f.calls = append(f.calls, key)

	queue := f.results[key]
	if len(queue) == 0 {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(""))}, nil
	}

	result := queue[0]
	f.results[key] = queue[1:]
	if result.err != nil {
		return nil, result.err
	}

	return &http.Response{
		StatusCode: result.statusCode,
		Body:       io.NopCloser(strings.NewReader("")),
	}, nil
}

func newTestLinkChecker(repo *fakeLinkCheckRepo, client *fakeHTTPClient) *LinkChecker {
	checker := NewLinkChecker(client, repo, nil)
	checker.requestTimeout = 3 * time.Second
	checker.batchSize = 50
	checker.staleAfter = 72 * time.Hour
	checker.now = func() time.Time {
		return time.Date(2026, 2, 19, 8, 0, 0, 0, time.UTC)
	}
	checker.resolveHostIPs = func(_ context.Context, host string) ([]net.IP, error) {
		switch host {
		case "example.com":
			return []net.IP{net.ParseIP("93.184.216.34")}, nil
		case "private.example":
			return []net.IP{net.ParseIP("10.0.0.5")}, nil
		default:
			return nil, fmt.Errorf("unexpected host: %s", host)
		}
	}
	return checker
}

func TestCheckLink_HeadSuccess(t *testing.T) {
	repo := &fakeLinkCheckRepo{}
	client := &fakeHTTPClient{results: map[string][]fakeHTTPResult{
		"HEAD https://example.com/news/1": {{statusCode: http.StatusOK}},
	}}
	checker := newTestLinkChecker(repo, client)

	status, err := checker.checkLink(context.Background(), "https://example.com/news/1")
	if err != nil {
		t.Fatalf("checkLink returned error: %v", err)
	}
	if status != domain.MajorEventLinkStatusOK {
		t.Fatalf("expected status ok, got %s", status)
	}
	if len(client.calls) != 1 || client.calls[0] != "HEAD https://example.com/news/1" {
		t.Fatalf("expected only HEAD call, got %v", client.calls)
	}
}

func TestCheckLink_Head405FallbackGetSuccess(t *testing.T) {
	repo := &fakeLinkCheckRepo{}
	client := &fakeHTTPClient{results: map[string][]fakeHTTPResult{
		"HEAD https://example.com/news/2": {{statusCode: http.StatusMethodNotAllowed}},
		"GET https://example.com/news/2":  {{statusCode: http.StatusOK}},
	}}
	checker := newTestLinkChecker(repo, client)

	status, err := checker.checkLink(context.Background(), "https://example.com/news/2")
	if err != nil {
		t.Fatalf("checkLink returned error: %v", err)
	}
	if status != domain.MajorEventLinkStatusOK {
		t.Fatalf("expected status ok, got %s", status)
	}
	if len(client.calls) != 2 {
		t.Fatalf("expected HEAD+GET calls, got %v", client.calls)
	}
}

func TestCheckLink_HeadErrorFallbackGetSuccess(t *testing.T) {
	repo := &fakeLinkCheckRepo{}
	client := &fakeHTTPClient{results: map[string][]fakeHTTPResult{
		"HEAD https://example.com/news/3": {{err: errors.New("connection reset")}},
		"GET https://example.com/news/3":  {{statusCode: http.StatusOK}},
	}}
	checker := newTestLinkChecker(repo, client)

	status, err := checker.checkLink(context.Background(), "https://example.com/news/3")
	if err != nil {
		t.Fatalf("checkLink returned error: %v", err)
	}
	if status != domain.MajorEventLinkStatusOK {
		t.Fatalf("expected status ok, got %s", status)
	}
}

func TestCheckLink_Head404FailsWithoutFallback(t *testing.T) {
	repo := &fakeLinkCheckRepo{}
	client := &fakeHTTPClient{results: map[string][]fakeHTTPResult{
		"HEAD https://example.com/news/404": {{statusCode: http.StatusNotFound}},
	}}
	checker := newTestLinkChecker(repo, client)

	status, err := checker.checkLink(context.Background(), "https://example.com/news/404")
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if status != domain.MajorEventLinkStatusFailed {
		t.Fatalf("expected failed status, got %s", status)
	}
	if len(client.calls) != 1 {
		t.Fatalf("expected only HEAD call for 404, got %v", client.calls)
	}
}

func TestCheckLink_BlocksLocalhost(t *testing.T) {
	repo := &fakeLinkCheckRepo{}
	client := &fakeHTTPClient{results: map[string][]fakeHTTPResult{}}
	checker := newTestLinkChecker(repo, client)

	status, err := checker.checkLink(context.Background(), "http://localhost/internal")
	if err == nil {
		t.Fatal("expected block error")
	}
	if status != domain.MajorEventLinkStatusBlocked {
		t.Fatalf("expected blocked status, got %s", status)
	}
	if len(client.calls) != 0 {
		t.Fatalf("expected no HTTP request when blocked, got %v", client.calls)
	}
}

func TestCheckLink_BlocksPrivateResolvedIP(t *testing.T) {
	repo := &fakeLinkCheckRepo{}
	client := &fakeHTTPClient{results: map[string][]fakeHTTPResult{}}
	checker := newTestLinkChecker(repo, client)

	status, err := checker.checkLink(context.Background(), "https://private.example/news")
	if err == nil {
		t.Fatal("expected block error")
	}
	if status != domain.MajorEventLinkStatusBlocked {
		t.Fatalf("expected blocked status, got %s", status)
	}
}

func TestCheckLink_RedirectToBlockedHost_ReturnsBlocked(t *testing.T) {
	repo := &fakeLinkCheckRepo{}

	callCount := 0
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			callCount++
			if req.URL.String() != "https://example.com/news/redirect" {
				t.Fatalf("unexpected request URL: %s", req.URL.String())
			}
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": []string{"http://127.0.0.1/internal"}},
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    req,
			}, nil
		}),
	}

	checker := NewLinkChecker(client, repo, nil)
	checker.resolveHostIPs = func(_ context.Context, host string) ([]net.IP, error) {
		if host != "example.com" {
			return nil, fmt.Errorf("unexpected host: %s", host)
		}
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}

	status, err := checker.checkLink(context.Background(), "https://example.com/news/redirect")
	if err == nil {
		t.Fatal("expected block error")
	}
	if status != domain.MajorEventLinkStatusBlocked {
		t.Fatalf("expected blocked status, got %s", status)
	}
	if callCount != 1 {
		t.Fatalf("expected only initial request before blocked redirect, got %d", callCount)
	}
}

func TestShouldFallbackToGet(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		err        error
		want       bool
	}{
		{name: "request error", statusCode: 0, err: errors.New("boom"), want: true},
		{name: "405", statusCode: http.StatusMethodNotAllowed, want: true},
		{name: "403", statusCode: http.StatusForbidden, want: true},
		{name: "504", statusCode: http.StatusGatewayTimeout, want: true},
		{name: "404", statusCode: http.StatusNotFound, want: false},
		{name: "200", statusCode: http.StatusOK, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldFallbackToGet(tt.statusCode, tt.err)
			if got != tt.want {
				t.Fatalf("shouldFallbackToGet(%d, %v) = %v, want %v", tt.statusCode, tt.err, got, tt.want)
			}
		})
	}
}

func TestCheckStaleLinks_UpdatesStatusesAndCapturesStaleWindow(t *testing.T) {
	repo := &fakeLinkCheckRepo{
		events: []*domain.MajorEvent{
			{ID: 1, Link: "https://example.com/news/ok"},
			{ID: 2, Link: "http://localhost/internal"},
		},
	}
	client := &fakeHTTPClient{results: map[string][]fakeHTTPResult{
		"HEAD https://example.com/news/ok": {{statusCode: http.StatusOK}},
	}}
	checker := newTestLinkChecker(repo, client)

	result, err := checker.CheckStaleLinks(context.Background())
	if err != nil {
		t.Fatalf("CheckStaleLinks returned error: %v", err)
	}

	expectedStaleBefore := checker.now().UTC().Add(-checker.staleAfter)
	if !repo.capturedStaleBefore.Equal(expectedStaleBefore) {
		t.Fatalf("staleBefore mismatch: got %v want %v", repo.capturedStaleBefore, expectedStaleBefore)
	}
	if repo.capturedLimit != checker.batchSize {
		t.Fatalf("limit mismatch: got %d want %d", repo.capturedLimit, checker.batchSize)
	}

	if result.Checked != 2 || result.OK != 1 || result.Blocked != 1 || result.Failed != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got := repo.updates[1]; got != domain.MajorEventLinkStatusOK {
		t.Fatalf("event 1 status mismatch: %s", got)
	}
	if got := repo.updates[2]; got != domain.MajorEventLinkStatusBlocked {
		t.Fatalf("event 2 status mismatch: %s", got)
	}
}

func TestCheckStaleLinks_NoEvents_LogsDebug(t *testing.T) {
	repo := &fakeLinkCheckRepo{}
	client := &fakeHTTPClient{results: map[string][]fakeHTTPResult{}}

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	checker := NewLinkChecker(client, repo, logger)
	checker.batchSize = 20
	checker.staleAfter = 48 * time.Hour
	checker.now = func() time.Time {
		return time.Date(2026, 2, 19, 8, 0, 0, 0, time.UTC)
	}

	result, err := checker.CheckStaleLinks(context.Background())
	if err != nil {
		t.Fatalf("CheckStaleLinks returned error: %v", err)
	}
	if result.Checked != 0 || result.OK != 0 || result.Failed != 0 || result.Blocked != 0 {
		t.Fatalf("expected zero result, got %+v", result)
	}

	logs := logBuf.String()
	if !strings.Contains(logs, "Major event link check skipped: no stale targets") {
		t.Fatalf("expected debug log for no stale targets, got %q", logs)
	}
}

func TestCheckStaleLinks_RedirectBlockedCountedAsBlocked(t *testing.T) {
	repo := &fakeLinkCheckRepo{
		events: []*domain.MajorEvent{
			{ID: 101, Link: "https://example.com/news/redirect"},
		},
	}

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != "https://example.com/news/redirect" {
				t.Fatalf("unexpected request URL: %s", req.URL.String())
			}
			return &http.Response{
				StatusCode: http.StatusMovedPermanently,
				Header:     http.Header{"Location": []string{"http://localhost/private"}},
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    req,
			}, nil
		}),
	}

	checker := NewLinkChecker(client, repo, nil)
	checker.now = func() time.Time {
		return time.Date(2026, 2, 19, 8, 0, 0, 0, time.UTC)
	}
	checker.resolveHostIPs = func(_ context.Context, host string) ([]net.IP, error) {
		if host != "example.com" {
			return nil, fmt.Errorf("unexpected host: %s", host)
		}
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}

	result, err := checker.CheckStaleLinks(context.Background())
	if err != nil {
		t.Fatalf("CheckStaleLinks returned error: %v", err)
	}

	if result.Checked != 1 || result.Blocked != 1 || result.Failed != 0 || result.OK != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got := repo.updates[101]; got != domain.MajorEventLinkStatusBlocked {
		t.Fatalf("event status mismatch: %s", got)
	}
}
