package httpx

import (
	"net/http/httptest"
	"testing"
)

func TestRateLimitIdentity_APIKeyPriority(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "198.51.100.10:44321"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")

	proxies, err := ParseTrustedProxies([]string{"198.51.100.10"})
	if err != nil {
		t.Fatalf("ParseTrustedProxies() error = %v", err)
	}

	got := RateLimitIdentity(req, "secret-key", proxies)
	if got[:4] != "key:" {
		t.Fatalf("identity = %q, want key hash prefix", got)
	}
}

func TestRateLimitIdentity_IgnoresForwardedForWhenUntrusted(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "198.51.100.10:44321"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")

	proxies, err := ParseTrustedProxies([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("ParseTrustedProxies() error = %v", err)
	}

	got := RateLimitIdentity(req, "", proxies)
	want := "ip:198.51.100.10"
	if got != want {
		t.Fatalf("identity = %q, want %q", got, want)
	}
}

func TestRateLimitIdentity_UsesForwardedForWhenTrusted(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "10.1.2.3:44321"
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 203.0.113.10")

	proxies, err := ParseTrustedProxies([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("ParseTrustedProxies() error = %v", err)
	}

	got := RateLimitIdentity(req, "", proxies)
	want := "ip:203.0.113.9"
	if got != want {
		t.Fatalf("identity = %q, want %q", got, want)
	}
}

func TestRateLimitIdentity_FallsBackToXRealIPWhenTrusted(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "10.1.2.3:44321"
	req.Header.Set("X-Real-IP", "203.0.113.11")

	proxies, err := ParseTrustedProxies([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("ParseTrustedProxies() error = %v", err)
	}

	got := RateLimitIdentity(req, "", proxies)
	want := "ip:203.0.113.11"
	if got != want {
		t.Fatalf("identity = %q, want %q", got, want)
	}
}

func TestParseTrustedProxies_InvalidValue(t *testing.T) {
	t.Parallel()

	if _, err := ParseTrustedProxies([]string{"not-an-ip"}); err == nil {
		t.Fatalf("ParseTrustedProxies() expected error, got nil")
	}
}

