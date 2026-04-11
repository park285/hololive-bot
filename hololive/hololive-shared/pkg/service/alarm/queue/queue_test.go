// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package queue

import (
	"context"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	sharedlogging "github.com/park285/llm-kakao-bots/shared-go/pkg/logging"
)

func newTestLogger() *slog.Logger {
	return sharedlogging.NewTestLogger()
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

func queueItemsOrEmpty(t *testing.T, mini *miniredis.Miniredis) []string {
	t.Helper()

	items, err := mini.List(AlarmDispatchQueue)
	if err != nil {
		if strings.Contains(err.Error(), "no such key") {
			return nil
		}
		require.NoError(t, err)
	}
	return items
}

func TestPublisherPublishEnqueuesJSONEnvelopeWithVersion(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	publisher := NewPublisher(cacheClient, newTestLogger())

	notification := &domain.AlarmNotification{
		AlarmType:    domain.AlarmTypeLive,
		RoomID:       "room-1",
		MinutesUntil: 5,
		Users:        []string{"user-1"},
	}
	claimKeys := []string{"notified:claim:room-1"}

	err := publisher.Publish(context.Background(), notification, claimKeys)
	require.NoError(t, err)

	items := queueItemsOrEmpty(t, mini)
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

	require.NoError(t, publisher.Publish(context.Background(), &domain.AlarmNotification{AlarmType: domain.AlarmTypeLive, RoomID: "room-1"}, nil))
	require.NoError(t, publisher.Publish(context.Background(), &domain.AlarmNotification{AlarmType: domain.AlarmTypeLive, RoomID: "room-2"}, nil))

	items := queueItemsOrEmpty(t, mini)
	require.Len(t, items, 2)

	var first domain.AlarmQueueEnvelope
	var second domain.AlarmQueueEnvelope
	require.NoError(t, json.Unmarshal([]byte(items[0]), &first))
	require.NoError(t, json.Unmarshal([]byte(items[1]), &second))

	assert.Equal(t, "room-2", first.Notification.RoomID)
	assert.Equal(t, "room-1", second.Notification.RoomID)
}

func TestPublisherPublishRejectsContentAlarmTypes(t *testing.T) {
	t.Parallel()

	publisher := NewPublisher(cachemocks.NewStrictClient(), newTestLogger())

	for _, alarmType := range []domain.AlarmType{domain.AlarmTypeCommunity, domain.AlarmTypeShorts} {
		t.Run(string(alarmType), func(t *testing.T) {
			err := publisher.Publish(context.Background(), &domain.AlarmNotification{
				AlarmType: alarmType,
				RoomID:    "room-blocked",
			}, nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "youtube outbox path")

		})
	}
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
				Notification: domain.AlarmNotification{
					AlarmType: domain.AlarmTypeLive,
					RoomID:    "room",
				},
				Version: tc.version,
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
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "room",
		},
		Version: 2,
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

func TestConsumerDrainBatch_UsesBatchedDrain(t *testing.T) {
	cacheClient, _ := newTestCacheClient(t)
	publisher := NewPublisher(cacheClient, newTestLogger())
	consumer := NewConsumer(cacheClient, newTestLogger(), WithMaxBatch(5))

	for _, roomID := range []string{"room-1", "room-2", "room-3"} {
		require.NoError(t, publisher.Publish(context.Background(), &domain.AlarmNotification{AlarmType: domain.AlarmTypeLive, RoomID: roomID}, nil))
	}

	envelopes, err := consumer.DrainBatch(context.Background(), 3)
	require.NoError(t, err)
	require.Len(t, envelopes, 3)
	assert.Equal(t, "room-1", envelopes[0].Notification.RoomID)
	assert.Equal(t, "room-2", envelopes[1].Notification.RoomID)
	assert.Equal(t, "room-3", envelopes[2].Notification.RoomID)
}

func TestConsumerRequeue_PreservesEnvelopeOrderAfterExistingBacklog(t *testing.T) {
	cacheClient, _ := newTestCacheClient(t)
	publisher := NewPublisher(cacheClient, newTestLogger())
	consumer := NewConsumer(cacheClient, newTestLogger(), WithMaxBatch(5))

	retryA := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "retry-a",
		},
		ClaimKeys:  []string{"notified:claim:retry-a"},
		EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
		Version:    contractsalarm.QueueEnvelopeVersionV1,
	}
	retryB := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "retry-b",
		},
		ClaimKeys:  []string{"notified:claim:retry-b"},
		EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
		Version:    contractsalarm.QueueEnvelopeVersionV1,
	}

	err := publisher.Publish(
		context.Background(),
		&domain.AlarmNotification{AlarmType: domain.AlarmTypeLive, RoomID: "existing"},
		[]string{"notified:claim:existing"},
	)
	require.NoError(t, err)

	err = consumer.Requeue(context.Background(), []domain.AlarmQueueEnvelope{retryA, retryB})
	require.NoError(t, err)

	envelopes, err := consumer.DrainBatch(context.Background(), 3)
	require.NoError(t, err)
	require.Len(t, envelopes, 3)
	assert.Equal(t, "existing", envelopes[0].Notification.RoomID)
	assert.Equal(t, "retry-a", envelopes[1].Notification.RoomID)
	assert.Equal(t, "retry-b", envelopes[2].Notification.RoomID)
}

func TestConsumerDrainBatch_DropsContentAlarmTypesAndReleasesClaims(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	publisher := NewPublisher(cacheClient, newTestLogger())
	consumer := NewConsumer(cacheClient, newTestLogger(), WithMaxBatch(5))

	claimKey := "notified:claim:blocked-community"
	mini.Set(claimKey, "1")

	blockedRaw, err := json.Marshal(domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeCommunity,
			RoomID:    "room-blocked",
		},
		ClaimKeys:  []string{claimKey},
		EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
		Version:    contractsalarm.QueueEnvelopeVersionV1,
	})
	require.NoError(t, err)

	cmd := cacheClient.B().Lpush().Key(AlarmDispatchQueue).Element(string(blockedRaw)).Build()
	results := cacheClient.DoMulti(context.Background(), cmd)
	require.Len(t, results, 1)
	require.NoError(t, results[0].Error())

	require.NoError(t, publisher.Publish(
		context.Background(),
		&domain.AlarmNotification{AlarmType: domain.AlarmTypeLive, RoomID: "room-live"},
		[]string{"notified:claim:room-live"},
	))

	envelopes, err := consumer.DrainBatch(context.Background(), 2)
	require.NoError(t, err)
	require.Len(t, envelopes, 1)
	assert.Equal(t, domain.AlarmTypeLive, envelopes[0].Notification.AlarmType)
	assert.Equal(t, "room-live", envelopes[0].Notification.RoomID)
	assert.False(t, mini.Exists(claimKey))

	remaining := queueItemsOrEmpty(t, mini)
	assert.Len(t, remaining, 0)
}

func TestConsumerRequeue_DropsContentAlarmTypesAndReleasesClaims(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	consumer := NewConsumer(cacheClient, newTestLogger(), WithMaxBatch(5))

	claimKey := "notified:claim:blocked-shorts"
	mini.Set(claimKey, "1")

	valid := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "room-live",
		},
		ClaimKeys:  []string{"notified:claim:room-live"},
		EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
		Version:    contractsalarm.QueueEnvelopeVersionV1,
	}
	blocked := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeShorts,
			RoomID:    "room-blocked",
		},
		ClaimKeys:  []string{claimKey},
		EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
		Version:    contractsalarm.QueueEnvelopeVersionV1,
	}

	require.NoError(t, consumer.Requeue(context.Background(), []domain.AlarmQueueEnvelope{valid, blocked}))

	envelopes, err := consumer.DrainBatch(context.Background(), 2)
	require.NoError(t, err)
	require.Len(t, envelopes, 1)
	assert.Equal(t, domain.AlarmTypeLive, envelopes[0].Notification.AlarmType)
	assert.Equal(t, "room-live", envelopes[0].Notification.RoomID)
	assert.False(t, mini.Exists(claimKey))

	remaining := queueItemsOrEmpty(t, mini)
	assert.Len(t, remaining, 0)
}
