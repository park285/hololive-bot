package util

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestInstanceID(t *testing.T) {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown-host"
	}
	want := fmt.Sprintf("dispatcher:%s:%d", host, os.Getpid())

	if got := InstanceID("dispatcher"); got != want {
		t.Fatalf("InstanceID = %q, want %q", got, want)
	}
}

func TestInstanceIDIncludesPidForSameHostUniqueness(t *testing.T) {
	got := InstanceID("dispatcher")

	pidSuffix := fmt.Sprintf(":%d", os.Getpid())
	if !strings.HasSuffix(got, pidSuffix) {
		t.Fatalf("InstanceID = %q must end with pid suffix %q", got, pidSuffix)
	}
	if !strings.HasPrefix(got, "dispatcher:") {
		t.Fatalf("InstanceID = %q must start with prefix", got)
	}
}
