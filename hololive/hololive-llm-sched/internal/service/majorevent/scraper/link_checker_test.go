package scraper

import (
	"context"
	"errors"
	"io"
	"net"
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
	checker.resolver = staticResolver{"example.com": {net.ParseIP("93.184.216.34")}}

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
	checker.resolver = staticResolver{"example.com": {net.ParseIP("93.184.216.34")}}

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

type staticResolver map[string][]net.IP

func (r staticResolver) LookupIP(_ context.Context, _ string, host string) ([]net.IP, error) {
	ips, ok := r[host]
	if !ok {
		return nil, errors.New("host not found")
	}
	return ips, nil
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

func TestLinkCheckerCheckLink_BlockedResolvedInternalIPAddress(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatalf("unexpected outbound request to %s", req.URL.String())
		return nil, nil
	})}

	checker := NewLinkChecker(client, DefaultLinkCheckerConfig(), nil)
	checker.resolver = staticResolver{"internal.test": {net.ParseIP("127.0.0.1")}}

	status, err := checker.CheckLink(context.Background(), "https://internal.test/resource")
	if err == nil {
		t.Fatal("CheckLink() error = nil, want error")
	}
	if status != domain.MajorEventLinkStatusBlocked {
		t.Fatalf("CheckLink() status = %s, want %s", status, domain.MajorEventLinkStatusBlocked)
	}
}

func TestLinkCheckerCheckLink_BlockedRedirectToInternalTarget(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Hostname() {
		case "example.com":
			return &http.Response{
				StatusCode: http.StatusFound,
				Header: http.Header{
					"Location": []string{"http://internal.test/secret"},
				},
				Body: io.NopCloser(strings.NewReader("")),
			}, nil
		case "internal.test":
			t.Fatalf("redirect target should have been blocked before request: %s", req.URL.String())
			return nil, nil
		default:
			return nil, errors.New("unexpected host")
		}
	})}

	checker := NewLinkChecker(client, DefaultLinkCheckerConfig(), nil)
	checker.resolver = staticResolver{
		"example.com":   {net.ParseIP("93.184.216.34")},
		"internal.test": {net.ParseIP("127.0.0.1")},
	}
	checker.client = withBlockedRedirectPolicy(checker.client, checker.resolver, checker.config.Timeout)

	status, err := checker.CheckLink(context.Background(), "https://example.com")
	if err == nil {
		t.Fatal("CheckLink() error = nil, want error")
	}
	if status != domain.MajorEventLinkStatusBlocked {
		t.Fatalf("CheckLink() status = %s, want %s", status, domain.MajorEventLinkStatusBlocked)
	}
}
