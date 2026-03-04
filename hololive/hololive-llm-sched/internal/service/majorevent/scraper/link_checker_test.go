package scraper

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestLinkCheckerCheckLink_OKWithHead(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	checker := NewLinkChecker(server.Client(), LinkCheckerConfig{
		Timeout:     time.Second,
		Concurrency: 2,
	}, nil)

	status, err := checker.CheckLink(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("CheckLink() error = %v", err)
	}
	if status != domain.MajorEventLinkStatusOK {
		t.Fatalf("CheckLink() status = %s, want %s", status, domain.MajorEventLinkStatusOK)
	}
}

func TestLinkCheckerCheckLink_FallbackToGet(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(server.Close)

	checker := NewLinkChecker(server.Client(), DefaultLinkCheckerConfig(), nil)

	status, err := checker.CheckLink(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("CheckLink() error = %v", err)
	}
	if status != domain.MajorEventLinkStatusOK {
		t.Fatalf("CheckLink() status = %s, want %s", status, domain.MajorEventLinkStatusOK)
	}
}
