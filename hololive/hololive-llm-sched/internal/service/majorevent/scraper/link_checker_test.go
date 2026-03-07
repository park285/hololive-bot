package scraper

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestLinkCheckerCheckLink_OKWithHead(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodHead {
			return nil, errors.New("unexpected method")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}

	checker := NewLinkChecker(client, LinkCheckerConfig{
		Timeout:     time.Second,
		Concurrency: 2,
	}, nil)

	status, err := checker.CheckLink(context.Background(), "https://example.com")
	if err != nil {
		t.Fatalf("CheckLink() error = %v", err)
	}
	if status != domain.MajorEventLinkStatusOK {
		t.Fatalf("CheckLink() status = %s, want %s", status, domain.MajorEventLinkStatusOK)
	}
}

func TestLinkCheckerCheckLink_FallbackToGet(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.Method {
		case http.MethodHead:
			return &http.Response{StatusCode: http.StatusMethodNotAllowed, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
		case http.MethodGet:
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header)}, nil
		default:
			return nil, errors.New("unexpected method")
		}
	})}

	checker := NewLinkChecker(client, DefaultLinkCheckerConfig(), nil)

	status, err := checker.CheckLink(context.Background(), "https://example.com")
	if err != nil {
		t.Fatalf("CheckLink() error = %v", err)
	}
	if status != domain.MajorEventLinkStatusOK {
		t.Fatalf("CheckLink() status = %s, want %s", status, domain.MajorEventLinkStatusOK)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestLinkCheckerCheckLink_BlockedScheme(t *testing.T) {
	t.Parallel()

	checker := NewLinkChecker(nil, DefaultLinkCheckerConfig(), nil)

	status, err := checker.CheckLink(context.Background(), "ftp://example.com/file")
	if err == nil {
		t.Fatal("CheckLink() error = nil, want error")
	}
	if status != domain.MajorEventLinkStatusBlocked {
		t.Fatalf("CheckLink() status = %s, want %s", status, domain.MajorEventLinkStatusBlocked)
	}
}

func TestLinkCheckerCheckLink_BlockedInternalIPAddress(t *testing.T) {
	t.Parallel()

	checker := NewLinkChecker(nil, DefaultLinkCheckerConfig(), nil)

	status, err := checker.CheckLink(context.Background(), "http://127.0.0.1/internal")
	if err == nil {
		t.Fatal("CheckLink() error = nil, want error")
	}
	if status != domain.MajorEventLinkStatusBlocked {
		t.Fatalf("CheckLink() status = %s, want %s", status, domain.MajorEventLinkStatusBlocked)
	}
}

func TestLinkCheckerCheckLink_BlockedLocalhost(t *testing.T) {
	t.Parallel()

	checker := NewLinkChecker(nil, DefaultLinkCheckerConfig(), nil)

	status, err := checker.CheckLink(context.Background(), "https://localhost/admin")
	if err == nil {
		t.Fatal("CheckLink() error = nil, want error")
	}
	if status != domain.MajorEventLinkStatusBlocked {
		t.Fatalf("CheckLink() status = %s, want %s", status, domain.MajorEventLinkStatusBlocked)
	}
}
