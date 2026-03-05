package testredis

import (
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
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatalf("dial miniredis %s: %v", addr, err)
	}
	_ = conn.Close()
}
