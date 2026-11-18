package testredis

import (
	"strconv"
	"testing"

	"github.com/alicebob/miniredis/v2"
)

func StartMiniRedis(t *testing.T) (host string, port int, mini *miniredis.Miniredis) {
	t.Helper()

	mini = miniredis.RunT(t)

	parsedPort, err := strconv.Atoi(mini.Port())
	if err != nil {
		mini.Close()
		t.Fatalf("parse miniredis port: %v", err)
	}

	host = mini.Host()

	return host, parsedPort, mini
}
