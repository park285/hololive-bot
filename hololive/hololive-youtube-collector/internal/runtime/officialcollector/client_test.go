package officialcollector

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/providerhttp"
)

func TestClientRespectsRetryAfter(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "12")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	client := officialTestClient(t, server, 1024)
	_, err := client.Fetch(t.Context())

	if collecterr.CodeOf(err) != collecterr.Cooldown {
		t.Fatalf("error = %v", err)
	}

	hint := collecterr.RetryOf(err)
	if hint.Kind() != collecterr.RetryAfter || hint.After() != 12*time.Second {
		t.Fatalf("retry hint = %#v", hint)
	}
}

func TestHTTP003OfficialRootAndEmptyPath(t *testing.T) {
	t.Parallel()

	wrapped, err := providerhttp.WrapProviderHTTPDoer(&http.Client{})
	if err != nil {
		t.Fatal(err)
	}

	for _, raw := range []string{"https://schedule.hololive.tv", "https://schedule.hololive.tv/"} {
		client, err := NewClient(wrapped, raw, 1024)
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}

		if got := client.base.JoinPath("api", "list", "2").String(); got != "https://schedule.hololive.tv/api/list/2" {
			t.Fatalf("endpoint(%s) = %s", raw, got)
		}
	}
}

func TestHTTP004OfficialRejectsNonRootPath(t *testing.T) {
	t.Parallel()

	wrapped, err := providerhttp.WrapProviderHTTPDoer(&http.Client{})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := NewClient(wrapped, "https://schedule.hololive.tv/api", 1024); err == nil {
		t.Fatal("accepted official path prefix")
	}
}

func TestHTTP005OfficialUserinfoIsRedacted(t *testing.T) {
	t.Parallel()

	const secret = "official-userinfo-secret"

	wrapped, err := providerhttp.WrapProviderHTTPDoer(&http.Client{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = NewClient(wrapped, "https://user:"+secret+"@schedule.hololive.tv", 1024)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("error = %v", err)
	}
}

func TestHTTP007OfficialForbiddenIsConfiguration(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)

		if _, err := w.Write([]byte(`{"error":"no"}`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	_, err := officialTestClient(t, server, 1024).Fetch(t.Context())
	if collecterr.CodeOf(err) != collecterr.Configuration || collecterr.ClassOf(err) != collecterr.ClassConfiguration {
		t.Fatalf("403 = %v", err)
	}
}

func TestHTTP008OfficialRetryAfterCooldown(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	_, err := officialTestClient(t, server, 1024).Fetch(t.Context())
	if collecterr.CodeOf(err) != collecterr.Cooldown || collecterr.RetryOf(err).After() != 5*time.Second {
		t.Fatalf("503 = %v hint=%v", err, collecterr.RetryOf(err))
	}
}

func officialTestClient(t *testing.T, server *httptest.Server, maxBody int64) *Client {
	t.Helper()

	wrapped, err := providerhttp.WrapProviderHTTPDoer(server.Client())
	if err != nil {
		t.Fatal(err)
	}

	client, err := NewClient(wrapped, server.URL, maxBody)
	if err != nil {
		t.Fatal(err)
	}

	return client
}
