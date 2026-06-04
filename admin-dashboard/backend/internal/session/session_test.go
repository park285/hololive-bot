package session

import (
	"testing"
	"time"
)

func TestTTLSecondsMinimumOne(t *testing.T) {
	now := time.Now()
	if ttlSeconds(now, now) != 1 {
		t.Fatal("ttl must be clamped to one second")
	}
}

func TestCappedExpiresAt(t *testing.T) {
	now := time.Now()
	absolute := now.Add(5 * time.Second)
	if got := cappedExpiresAt(now, time.Minute, absolute); !got.Equal(absolute) {
		t.Fatalf("expected cap at absolute timeout, got %s", got)
	}
}

func TestValkeyAddressParser(t *testing.T) {
	addr, password, err := parseValkeyAddress(":pw@valkey-cache:6379")
	if err != nil {
		t.Fatal(err)
	}
	if addr != "valkey-cache:6379" || password != "pw" {
		t.Fatalf("unexpected parse result %q %q", addr, password)
	}
}
