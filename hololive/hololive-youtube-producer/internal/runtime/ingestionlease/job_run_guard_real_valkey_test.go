package ingestionlease

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/stretchr/testify/require"
)

func TestJobRunGuardRealValkeyIntegration(t *testing.T) {
	addr := os.Getenv("TEST_VALKEY_ADDR")
	if addr == "" {
		t.Skip("TEST_VALKEY_ADDR is not set")
	}
	host, portText, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	port, err := strconv.Atoi(portText)
	require.NoError(t, err)

	ctx := context.Background()
	cacheClient, err := cache.NewCacheService(ctx, cache.Config{
		Host:              host,
		Port:              port,
		DisableCache:      true,
		ForceSingleClient: true,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, cacheClient.Close())
	})

	namespace := "real-valkey-test-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	winner := NewJobRunGuard(cacheClient, JobRunGuardConfig{Namespace: namespace, InstanceID: "ap-a"})
	peer := NewJobRunGuard(cacheClient, JobRunGuardConfig{Namespace: namespace, InstanceID: "ap-b"})
	identity := JobIdentity{PollerName: "videos", ChannelID: "UC_REAL", Interval: 250 * time.Millisecond}

	status, claim, err := winner.TryLease(ctx, identity, time.Second, identity.Interval)
	require.NoError(t, err)
	require.Equal(t, JobClaimAcquired, status.Result)
	require.NotNil(t, claim)

	status, peerClaim, err := peer.TryLease(ctx, identity, time.Second, identity.Interval)
	require.NoError(t, err)
	require.Equal(t, JobClaimPeerOwned, status.Result)
	require.Nil(t, peerClaim)

	completed, err := claim.MarkCompleted(ctx, identity.Interval)
	require.NoError(t, err)
	require.True(t, completed)

	status, peerClaim, err = peer.TryLease(ctx, identity, time.Second, identity.Interval)
	require.NoError(t, err)
	require.Equal(t, JobClaimAlreadyCompleted, status.Result)
	require.Nil(t, peerClaim)
}
