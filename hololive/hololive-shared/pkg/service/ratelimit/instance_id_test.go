package ratelimit

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// hostname을 얻지 못하면 random이나 "local"로 바꾸지 않고 오류다(DEC-20260926-hololive-legacy-env-config-retirement).
func TestResolveInstanceIDRejectsHostnameFailure(t *testing.T) {
	cases := map[string]func() (string, error){
		"error": func() (string, error) { return "", errors.New("uname failed") },
		"empty": func() (string, error) { return "  ", nil },
	}

	for name, hostname := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := resolveInstanceID(hostname)
			if err == nil {
				t.Fatalf("resolveInstanceID() = %q, want error", got)
			}
		})
	}
}

func TestResolveInstanceIDUsesSanitizedHostname(t *testing.T) {
	got, err := resolveInstanceID(func() (string, error) { return " api host:1 ", nil })
	if err != nil {
		t.Fatalf("resolveInstanceID() error = %v", err)
	}

	if got != "api_host_1" {
		t.Fatalf("resolveInstanceID() = %q, want %q", got, "api_host_1")
	}
}

func TestMemberIDIncludesHostnameInstanceID(t *testing.T) {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		t.Skipf("hostname unavailable: %v", err)
	}

	limiter := newTestLimiter(t)

	wantPrefix := "123:" + sanitizeInstanceID(hostname) + ":"
	if member := limiter.memberID(123); !strings.HasPrefix(member, wantPrefix) || !strings.HasSuffix(member, ":1") {
		t.Fatalf("memberID() = %q, want hostname prefix %q and sequence suffix :1", member, wantPrefix)
	}
}
