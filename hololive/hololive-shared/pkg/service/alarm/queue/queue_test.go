package queue

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestCacheClient(t *testing.T) (cache.Client, *miniredis.Miniredis) {
	t.Helper()

	mini := miniredis.RunT(t)
	host, rawPort, err := net.SplitHostPort(mini.Addr())
	require.NoError(t, err)

	port, err := strconv.Atoi(rawPort)
	require.NoError(t, err)

	svc, err := cache.NewCacheService(
		context.Background(),
		cache.Config{
			Host:         host,
			Port:         port,
			DisableCache: true,
		},
		newTestLogger(),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, svc.Close())
		mini.Close()
	})

	return svc, mini
}

func TestPublisherPublishEnqueuesJSONEnvelopeWithVersion(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	publisher := NewPublisher(cacheClient, newTestLogger())

	notification := &domain.AlarmNotification{
		RoomID:       "room-1",
		MinutesUntil: 5,
		Users:        []string{"user-1"},
	}
	claimKeys := []string{"notified:claim:room-1"}

	err := publisher.Publish(context.Background(), notification, claimKeys)
	require.NoError(t, err)

	items, err := mini.List(AlarmDispatchQueue)
	require.NoError(t, err)
	require.Len(t, items, 1)

	var envelope domain.AlarmQueueEnvelope
	require.NoError(t, json.Unmarshal([]byte(items[0]), &envelope))
	assert.Equal(t, "room-1", envelope.Notification.RoomID)
	assert.Equal(t, contractsalarm.QueueEnvelopeVersionV1, envelope.Version)
	assert.Equal(t, claimKeys, envelope.ClaimKeys)
	_, err = time.Parse(time.RFC3339, envelope.EnqueuedAt)
	require.NoError(t, err)
}

func TestPublisherPublishLPushOrderNewestFirst(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	publisher := NewPublisher(cacheClient, newTestLogger())

	require.NoError(t, publisher.Publish(context.Background(), &domain.AlarmNotification{RoomID: "room-1"}, nil))
	require.NoError(t, publisher.Publish(context.Background(), &domain.AlarmNotification{RoomID: "room-2"}, nil))

	items, err := mini.List(AlarmDispatchQueue)
	require.NoError(t, err)
	require.Len(t, items, 2)

	var first domain.AlarmQueueEnvelope
	var second domain.AlarmQueueEnvelope
	require.NoError(t, json.Unmarshal([]byte(items[0]), &first))
	require.NoError(t, json.Unmarshal([]byte(items[1]), &second))

	assert.Equal(t, "room-2", first.Notification.RoomID)
	assert.Equal(t, "room-1", second.Notification.RoomID)
}

func TestParseEnvelopeSupportsV0AndV1(t *testing.T) {
	tests := []struct {
		name    string
		version uint8
	}{
		{name: "v0", version: 0},
		{name: "v1", version: contractsalarm.QueueEnvelopeVersionV1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(domain.AlarmQueueEnvelope{
				Notification: domain.AlarmNotification{RoomID: "room"},
				Version:      tc.version,
			})
			require.NoError(t, err)

			envelope, ok := parseEnvelope(string(raw), newTestLogger())
			assert.True(t, ok)
			assert.Equal(t, tc.version, envelope.Version)
		})
	}
}

func TestParseEnvelopeSkipsUnsupportedVersion(t *testing.T) {
	raw, err := json.Marshal(domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{RoomID: "room"},
		Version:      2,
	})
	require.NoError(t, err)

	_, ok := parseEnvelope(string(raw), newTestLogger())
	assert.False(t, ok)
}

func TestParseEnvelopeSkipsInvalidJSON(t *testing.T) {
	_, ok := parseEnvelope("{invalid-json}", newTestLogger())
	assert.False(t, ok)
}

func TestReleaseClaimKeysFiltersByPrefix(t *testing.T) {
	captured := make([]string, 0)
	client := &cachemocks.Client{
		DelManyFunc: func(_ context.Context, keys []string) (int64, error) {
			captured = append(captured, keys...)
			return int64(len(keys)), nil
		},
	}
	consumer := NewConsumer(client, newTestLogger())

	err := consumer.ReleaseClaimKeys(context.Background(), []string{
		" notified:claim:room-1 ",
		"notified:claim:event:room-1",
		"invalid:key",
		"",
		"   ",
	})
	require.NoError(t, err)

	assert.Equal(t, []string{
		"notified:claim:room-1",
		"notified:claim:event:room-1",
	}, captured)
}

func TestReleaseClaimKeysNoPrefixSkipsDel(t *testing.T) {
	called := false
	client := &cachemocks.Client{
		DelManyFunc: func(_ context.Context, _ []string) (int64, error) {
			called = true
			return 0, nil
		},
	}
	consumer := NewConsumer(client, newTestLogger())

	err := consumer.ReleaseClaimKeys(context.Background(), []string{"invalid:key", "  "})
	require.NoError(t, err)
	assert.False(t, called)
}
