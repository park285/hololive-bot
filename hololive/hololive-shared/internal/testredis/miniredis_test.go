package testredis

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestStartMiniRedis(t *testing.T) {
	host, port, mini := StartMiniRedis(t)

	if host == "" {
		t.Fatal("host is empty")
	}

	if port <= 0 {
		t.Fatalf("invalid port: %d", port)
	}

	if mini == nil {
		t.Fatal("mini is nil")
	}

	if got := mini.Host(); got != host {
		t.Fatalf("host mismatch: got %q want %q", got, host)
	}

	if got := mini.Port(); got != strconv.Itoa(port) {
		t.Fatalf("port mismatch: got %q want %d", got, port)
	}

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)

	defer cancel()

	dialer := net.Dialer{}

	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		t.Fatalf("dial miniredis %s: %v", addr, err)
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("close miniredis connection: %v", err)
	}
}
