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

// NewTestValkeyClient는 miniredis 기반 valkey 클라이언트와 miniredis 핸들을 반환합니다.
// 클라이언트는 테스트 종료 시 닫힙니다.
func NewTestValkeyClient(t *testing.T) (valkey.Client, *miniredis.Miniredis) {
	t.Helper()

	host, port, mini := testredis.StartMiniRedis(t)

	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:  []string{net.JoinHostPort(host, strconv.Itoa(port))},
		DisableCache: true,
		// 테스트는 임의 키 조합(MGET/MSET) 검증이 많아 단일 클라이언트 모드가 필요합니다.
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
