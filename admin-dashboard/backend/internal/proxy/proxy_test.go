package proxy

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

type closeNotifyingRecorder struct {
	*httptest.ResponseRecorder
	closedCh chan bool
}

func (r *closeNotifyingRecorder) CloseNotify() <-chan bool {
	return r.closedCh
}

func TestNormalizeProxyTargetURL_StripsPathQueryFragment(t *testing.T) {
	t.Parallel()

	u, normalized, err := normalizeProxyTargetURL("http://example.com/api/holo?x=1#frag")
	if err != nil {
		t.Fatalf("normalizeProxyTargetURL error: %v", err)
	}
	if !normalized {
		t.Fatalf("expected normalized=true, got false")
	}
	if got := u.String(); got != "http://example.com" {
		t.Fatalf("normalized url mismatch: got %q, want %q", got, "http://example.com")
	}
}

func TestProxyHolo_RewritesPathToAPIDomain(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	gotPathCh := make(chan string, 1)
	upstream := httptest.NewServer(h2c.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPathCh <- r.URL.Path
		w.WriteHeader(http.StatusOK)
	}), &http2.Server{}))
	t.Cleanup(upstream.Close)

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
	holoProxy, err := createProxy(upstream.URL, logger, "holo")
	if err != nil {
		t.Fatalf("createProxy error: %v", err)
	}

	bp := &BotProxies{
		Holo:   holoProxy,
		logger: logger,
	}

	router := gin.New()
	router.Any("/admin/api/holo/*path", bp.ProxyHolo)

	w := &closeNotifyingRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		closedCh:         make(chan bool, 1),
	}
	req := httptest.NewRequest(http.MethodGet, "http://example.com/admin/api/holo/stats", nil)
	router.ServeHTTP(w, req)

	select {
	case got := <-gotPathCh:
		if got != "/api/holo/stats" {
			t.Fatalf("upstream path mismatch: got %q, want %q", got, "/api/holo/stats")
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("upstream was not called")
	}
}
