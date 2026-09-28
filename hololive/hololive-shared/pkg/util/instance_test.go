package util

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestInstanceID(t *testing.T) {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		t.Skipf("hostname unavailable: %v", err)
	}

	want := fmt.Sprintf("dispatcher:%s:%d", host, os.Getpid())

	got, err := InstanceID("dispatcher")
	if err != nil {
		t.Fatalf("InstanceID() error = %v", err)
	}

	if got != want {
		t.Fatalf("InstanceID = %q, want %q", got, want)
	}
}

func TestInstanceIDIncludesPidForSameHostUniqueness(t *testing.T) {
	got, err := instanceIDWithHostname("dispatcher", func() (string, error) { return "host-a", nil })
	if err != nil {
		t.Fatalf("instanceIDWithHostname() error = %v", err)
	}

	pidSuffix := fmt.Sprintf(":%d", os.Getpid())
	if !strings.HasSuffix(got, pidSuffix) {
		t.Fatalf("InstanceID = %q must end with pid suffix %q", got, pidSuffix)
	}

	if !strings.HasPrefix(got, "dispatcher:host-a:") {
		t.Fatalf("InstanceID = %q must start with prefix and hostname", got)
	}
}

// hostname을 얻지 못하면 "unknown-host"로 바꾸지 않고 오류다(DEC-20260926-hololive-legacy-env-config-retirement).
// 여러 host의 worker가 같은 lease owner를 쓰면 claim 소유 판정이 어긋나기 때문이다.
func TestInstanceIDRejectsHostnameFailure(t *testing.T) {
	cases := map[string]func() (string, error){
		"error": func() (string, error) { return "", errors.New("uname failed") },
		"empty": func() (string, error) { return " ", nil },
	}

	for name, hostname := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := instanceIDWithHostname("dispatcher", hostname)
			if err == nil {
				t.Fatalf("instanceIDWithHostname() = %q, want error", got)
			}
		})
	}
}
