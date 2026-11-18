package testutil

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/internal/testredis"
)

func NewTestValkeyClient(t *testing.T) (valkey.Client, *miniredis.Miniredis) {
	t.Helper()

	host, port, mini := testredis.StartMiniRedis(t)

	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:       []string{net.JoinHostPort(host, strconv.Itoa(port))},
		DisableCache:      true,
		ForceSingleClient: true,
	})
	if err != nil {
		mini.Close()
		t.Fatalf("failed to create valkey client: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	if err := client.Do(ctx, client.B().Ping().Build()).Error(); err != nil {
		client.Close()
		mini.Close()
		t.Fatalf("failed to ping miniredis: %v", err)
	}

	t.Cleanup(func() { client.Close() })

	return client, mini
}
