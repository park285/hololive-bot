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

package scraper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
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

func (r staticResolver) LookupIP(_ context.Context, _, host string) ([]net.IP, error) {
	ips, ok := r[host]
	if !ok {
		return nil, errors.New("host not found")
	}
	return ips, nil
}

type sequentialResolver struct {
	mu        sync.Mutex
	responses [][]net.IP
	calls     int
}

func (r *sequentialResolver) LookupIP(_ context.Context, _, host string) ([]net.IP, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if host == "" {
		return nil, errors.New("host not found")
	}
	if len(r.responses) == 0 {
		return nil, errors.New("host not found")
	}
	index := r.calls
	if index >= len(r.responses) {
		index = len(r.responses) - 1
	}
	r.calls++
	ips := r.responses[index]
	if len(ips) == 0 {
		return nil, errors.New("host not found")
	}
	copied := make([]net.IP, len(ips))
	copy(copied, ips)
	return copied, nil
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
	checker.client = withValidatedDialPolicy(checker.client, checker.resolver, checker.config.Timeout)

	status, err := checker.CheckLink(context.Background(), "https://example.com")
	if err == nil {
		t.Fatal("CheckLink() error = nil, want error")
	}
	if status != domain.MajorEventLinkStatusBlocked {
		t.Fatalf("CheckLink() status = %s, want %s", status, domain.MajorEventLinkStatusBlocked)
	}
}

func TestLinkCheckerCheckLink_BlockedDNSRebindingBetweenValidationAndDial(t *testing.T) {
	t.Parallel()

	var internalHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		internalHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	serverAddr, err := net.ResolveTCPAddr("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatalf("ResolveTCPAddr() error = %v", err)
	}

	resolver := &sequentialResolver{responses: [][]net.IP{
		{net.ParseIP("93.184.216.34")},
		{net.ParseIP("127.0.0.1")},
	}}

	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Fatalf("http.DefaultTransport type = %T, want *http.Transport", http.DefaultTransport)
	}
	transport := defaultTransport.Clone()
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := resolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, errors.New("host not found")
		}
		return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}
	client := &http.Client{Transport: transport}

	checker := NewLinkChecker(client, DefaultLinkCheckerConfig(), nil)
	checker.resolver = resolver
	checker.client = withBlockedRedirectPolicy(checker.client, checker.resolver, checker.config.Timeout)
	checker.client = withValidatedDialPolicy(checker.client, checker.resolver, checker.config.Timeout)

	status, err := checker.CheckLink(context.Background(), fmt.Sprintf("http://example.com:%d/secret", serverAddr.Port))
	if err == nil {
		t.Fatal("CheckLink() error = nil, want error")
	}
	if status != domain.MajorEventLinkStatusBlocked {
		t.Fatalf("CheckLink() status = %s, want %s", status, domain.MajorEventLinkStatusBlocked)
	}
	if got := internalHits.Load(); got != 0 {
		t.Fatalf("internal server hits = %d, want 0", got)
	}
}
