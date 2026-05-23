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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	json "github.com/park285/hololive-bot/shared-go/pkg/json"
	sharedlogging "github.com/park285/hololive-bot/shared-go/pkg/logging"
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

	service, err := cache.NewCacheService(
		context.Background(),
		cache.Config{
			Host:              host,
			Port:              port,
			DisableCache:      true,
			ForceSingleClient: true,
		},
		newTestLogger(),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, service.Close())
		mini.Close()
	})

	return service, mini
}

func queueItemsOrEmpty(t *testing.T, mini *miniredis.Miniredis) []string {
	t.Helper()

	return queueItemsByKeyOrEmpty(t, mini, AlarmDispatchQueue)
}

func queueItemsByKeyOrEmpty(t *testing.T, mini *miniredis.Miniredis, key string) []string {
	t.Helper()

	items, err := mini.List(key)
	if err != nil {
		if strings.Contains(err.Error(), "no such key") {
			return nil
		}
		require.NoError(t, err)
	}
	return items
}

func mustMarshalEnvelope(t *testing.T, envelope domain.AlarmQueueEnvelope) string {
	t.Helper()

	raw, err := json.Marshal(envelope)
	require.NoError(t, err)
	return string(raw)
}

func readAlarmQueueFixture(t *testing.T, name string) []byte {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "contracts", "alarm", "testdata", name))
	require.NoError(t, err)
	return raw
}

func unwrapSingleRetryMember(t *testing.T, retrySet map[string]float64) (string, float64) {
	t.Helper()

	require.Len(t, retrySet, 1)
	for member, score := range retrySet {
		payload, err := unwrapRetryMember(member)
		require.NoError(t, err)
		return payload, score
	}
	t.Fatal("missing retry member")
	return "", 0
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

	_, err := publisher.Publish(context.Background(), notification, claimKeys)
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

	_, err := publisher.Publish(context.Background(), &domain.AlarmNotification{AlarmType: domain.AlarmTypeLive, RoomID: "room-1"}, nil)
	require.NoError(t, err)
	_, err = publisher.Publish(context.Background(), &domain.AlarmNotification{AlarmType: domain.AlarmTypeLive, RoomID: "room-2"}, nil)
	require.NoError(t, err)

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
			_, err := publisher.Publish(context.Background(), &domain.AlarmNotification{
				AlarmType: alarmType,
				RoomID:    "room-blocked",
			}, nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "youtube outbox path")

		})
	}
}

func TestPublisherPublishDispatchBatchAcceptsYouTubeOutboxContentEnvelope(t *testing.T) {
	t.Parallel()

	publisher := NewPublisher(cachemocks.NewStrictClient(), newTestLogger(), WithPublishMode(PublishModePGFirst), WithWakeupEnabled(false), WithOutbox(&fakeOutboxRepository{}))
	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeShorts,
			RoomID:    "room-canonical",
		},
		SourceKind: domain.AlarmDispatchSourceKindYouTubeOutbox,
		YouTubeOutbox: &domain.YouTubeOutboxDispatchPayload{
			OutboxIDs:         []int64{1},
			Kind:              domain.OutboxKindNewShort,
			AlarmType:         domain.AlarmTypeShorts,
			ChannelID:         "UC_test",
			RenderTemplateKey: domain.TemplateKeyOutboxShorts,
			Items: []domain.YouTubeOutboxItem{{
				OutboxID:  1,
				ContentID: "short:abc",
				Payload:   `{"video_id":"abc","title":"테스트 쇼츠"}`,
			}},
		},
		ClaimKeys: []string{"youtube-notification:NEW_SHORT:short:abc:room-canonical"},
		Version:   contractsalarm.QueueEnvelopeVersionV1,
	}

	result, err := publisher.PublishDispatchBatch(context.Background(), []domain.AlarmQueueEnvelope{envelope})
	require.NoError(t, err)
	assert.Equal(t, 1, result.RequestedDeliveries)
}

func TestPublisherPublishValkeyBatch_ReturnsErrorOnResponseCountMismatch(t *testing.T) {
	realCache, _ := newTestCacheClient(t)
	client := &cachemocks.Client{
		BFunc: realCache.B,
		DoMultiFunc: func(_ context.Context, cmds ...valkey.Completed) []valkey.ValkeyResult {
			return nil
		},
	}
	publisher := NewPublisher(client, newTestLogger())

	result, err := publisher.PublishBatch(context.Background(), []*domain.AlarmNotification{
		{AlarmType: domain.AlarmTypeLive, RoomID: "room-mismatch"},
	}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected response count")
	assert.Equal(t, 0, result.ProcessedDeliveries)
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

func TestQueueConsumerRejectsUnsupportedEnvelopeVersion(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	consumer := NewConsumer(cacheClient, newTestLogger(), WithMaxBatch(5))
	raw := readAlarmQueueFixture(t, "envelope_unsupported_version.json")

	require.NoError(t, cacheClient.DoMulti(context.Background(),
		cacheClient.B().Lpush().Key(AlarmDispatchQueue).Element(string(raw)).Build(),
	)[0].Error())

	envelopes, err := consumer.DrainBatch(context.Background(), 1)
	require.NoError(t, err)
	assert.Empty(t, envelopes)

	dlqItems, err := mini.List(contractsalarm.DispatchDLQKey)
	require.NoError(t, err)
	require.Len(t, dlqItems, 1)
	assert.JSONEq(t, string(raw), dlqItems[0])
}

func TestParseEnvelopeSkipsInvalidJSON(t *testing.T) {
	_, ok := parseEnvelope("{invalid-json}", newTestLogger())
	assert.False(t, ok)
}

func TestQueueConsumerMovesInvalidJSONToDLQ(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	consumer := NewConsumer(cacheClient, newTestLogger(), WithMaxBatch(5))

	require.NoError(t, cacheClient.DoMulti(context.Background(),
		cacheClient.B().Lpush().Key(AlarmDispatchQueue).Element("{invalid-json}").Build(),
	)[0].Error())

	envelopes, err := consumer.DrainBatch(context.Background(), 1)
	require.NoError(t, err)
	assert.Empty(t, envelopes)

	dlqItems, err := mini.List(contractsalarm.DispatchDLQKey)
	require.NoError(t, err)
	require.Len(t, dlqItems, 1)
	assert.Equal(t, "{invalid-json}", dlqItems[0])
}

func TestConsumerDrainBatch_InvalidPayloadMovesRawPayloadToDLQ(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	consumer := NewConsumer(cacheClient, newTestLogger(), WithMaxBatch(5))

	require.NoError(t, cacheClient.DoMulti(context.Background(),
		cacheClient.B().Lpush().Key(AlarmDispatchQueue).Element("{invalid-json}").Build(),
	)[0].Error())

	envelopes, err := consumer.DrainBatch(context.Background(), 1)
	require.NoError(t, err)
	assert.Empty(t, envelopes)

	dlqItems, err := mini.List(contractsalarm.DispatchDLQKey)
	require.NoError(t, err)
	require.Len(t, dlqItems, 1)
	assert.Equal(t, "{invalid-json}", dlqItems[0])
}

func TestAlarmQueueDerivesRetryAndDLQKeys(t *testing.T) {
	cacheClient, _ := newTestCacheClient(t)
	consumer := NewConsumer(cacheClient, newTestLogger(), WithQueueKey("alarm:test:queue"))

	assert.Equal(t, "alarm:test:queue", consumer.queueKey)
	assert.Equal(t, "alarm:test:retry", consumer.retryQueueKey)
	assert.Equal(t, "alarm:test:dlq", consumer.dlqKey)
}

func TestQueueConsumerPreservesInvalidDelayedRetryMemberToDLQ(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	consumer := NewConsumer(cacheClient, newTestLogger(), WithMaxBatch(5))

	invalidMember := retryMemberPrefix + "broken-wrapper"
	results := cacheClient.DoMulti(context.Background(),
		cacheClient.B().Zadd().
			Key(contractsalarm.DispatchRetryQueueKey).
			ScoreMember().
			ScoreMember(float64(time.Now().UTC().UnixMilli()), invalidMember).
			Build(),
	)
	require.Len(t, results, 1)
	require.NoError(t, results[0].Error())

	envelopes, err := consumer.DrainBatch(context.Background(), 1)
	require.NoError(t, err)
	assert.Empty(t, envelopes)

	retryItems, err := mini.SortedSet(contractsalarm.DispatchRetryQueueKey)
	if err != nil {
		require.Contains(t, err.Error(), "no such key")
	} else {
		assert.Empty(t, retryItems)
	}

	dlqItems, err := mini.List(contractsalarm.DispatchDLQKey)
	require.NoError(t, err)
	require.Len(t, dlqItems, 1)
	assert.Equal(t, invalidMember, dlqItems[0])
}

func TestQueueConsumerAcceptsLegacyVersionZero(t *testing.T) {
	cacheClient, _ := newTestCacheClient(t)
	consumer := NewConsumer(cacheClient, newTestLogger(), WithMaxBatch(5))

	raw, err := json.Marshal(domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "legacy-room",
		},
		ClaimKeys:  []string{"notified:claim:legacy-room"},
		EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
		Version:    0,
	})
	require.NoError(t, err)

	require.NoError(t, cacheClient.DoMulti(context.Background(),
		cacheClient.B().Lpush().Key(AlarmDispatchQueue).Element(string(raw)).Build(),
	)[0].Error())

	envelopes, err := consumer.DrainBatch(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, envelopes, 1)
	assert.Equal(t, uint8(0), envelopes[0].Version)
	assert.Equal(t, "legacy-room", envelopes[0].Notification.RoomID)
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
		_, err := publisher.Publish(context.Background(), &domain.AlarmNotification{AlarmType: domain.AlarmTypeLive, RoomID: roomID}, nil)
		require.NoError(t, err)
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

	_, err := publisher.Publish(
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

func TestConsumerQueueKeyAlignmentUsesCustomNamespaceForRetryAndDLQ(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	customQueueKey := "alarm:test:queue"
	customRetryKey := "alarm:test:retry"
	customDLQKey := "alarm:test:dlq"
	consumer := NewConsumer(cacheClient, newTestLogger(), WithQueueKey(customQueueKey), WithMaxBatch(5))

	now := time.Now().UTC().Truncate(time.Millisecond)
	retryB := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "retry-b",
		},
		ClaimKeys:  []string{"notified:claim:retry-b"},
		EnqueuedAt: now.Format(time.RFC3339),
		Version:    contractsalarm.QueueEnvelopeVersionV1,
		Retry: &domain.AlarmQueueRetryMetadata{
			Attempt:       1,
			RetryAfterMS:  0,
			NextVisibleAt: now.Format(time.RFC3339Nano),
			LastError:     "temporary failure",
		},
	}
	retryA := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "retry-a",
		},
		ClaimKeys:  []string{"notified:claim:retry-a"},
		EnqueuedAt: now.Format(time.RFC3339),
		Version:    contractsalarm.QueueEnvelopeVersionV1,
		Retry: &domain.AlarmQueueRetryMetadata{
			Attempt:       1,
			RetryAfterMS:  0,
			NextVisibleAt: now.Format(time.RFC3339Nano),
			LastError:     "temporary failure",
		},
	}

	require.NoError(t, consumer.ScheduleRetry(context.Background(), []domain.AlarmQueueEnvelope{retryB, retryA}))
	require.Empty(t, queueItemsByKeyOrEmpty(t, mini, customQueueKey))
	_, err := mini.SortedSet(contractsalarm.DispatchRetryQueueKey)
	require.Error(t, err)

	activeRaw := mustMarshalEnvelope(t, domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "active-custom",
		},
		EnqueuedAt: now.Format(time.RFC3339),
		Version:    contractsalarm.QueueEnvelopeVersionV1,
	})
	cmd := cacheClient.B().Lpush().Key(customQueueKey).Element(activeRaw).Build()
	results := cacheClient.DoMulti(context.Background(), cmd)
	require.Len(t, results, 1)
	require.NoError(t, results[0].Error())

	envelopes, err := consumer.DrainBatch(context.Background(), 3)
	require.NoError(t, err)
	require.Len(t, envelopes, 3)
	assert.Equal(t, "retry-b", envelopes[0].Notification.RoomID)
	assert.Equal(t, "retry-a", envelopes[1].Notification.RoomID)
	assert.Equal(t, "active-custom", envelopes[2].Notification.RoomID)

	require.NoError(t, consumer.MoveToDLQ(context.Background(), envelopes[:1]))
	items, err := mini.List(customDLQKey)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Empty(t, queueItemsByKeyOrEmpty(t, mini, contractsalarm.DispatchDLQKey))
	assert.Empty(t, queueItemsByKeyOrEmpty(t, mini, customQueueKey))

	retryItems, err := mini.SortedSet(customRetryKey)
	if err != nil {
		require.Contains(t, err.Error(), "no such key")
	} else {
		assert.Empty(t, retryItems)
	}
}

func TestConsumerScheduleRetryStoresDelayedEnvelope(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	consumer := NewConsumer(cacheClient, newTestLogger(), WithMaxBatch(5))

	nextVisibleAt := time.Now().UTC().Add(30 * time.Second).Truncate(time.Millisecond)
	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "retry-room",
		},
		ClaimKeys:  []string{"notified:claim:retry-room"},
		EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
		Version:    contractsalarm.QueueEnvelopeVersionV1,
		Retry: &domain.AlarmQueueRetryMetadata{
			Attempt:       2,
			RetryAfterMS:  30000,
			NextVisibleAt: nextVisibleAt.Format(time.RFC3339Nano),
			LastError:     "temporary upstream error",
		},
	}

	require.NoError(t, consumer.ScheduleRetry(context.Background(), []domain.AlarmQueueEnvelope{envelope}))

	retrySet, err := mini.SortedSet(contractsalarm.DispatchRetryQueueKey)
	require.NoError(t, err)
	payload, score := unwrapSingleRetryMember(t, retrySet)
	require.Equal(t, mustMarshalEnvelope(t, envelope), payload)
	assert.Equal(t, float64(nextVisibleAt.UnixMilli()), score)
	assert.Empty(t, queueItemsOrEmpty(t, mini))
}

func TestQueueConsumerRoundTripsRetryMetadata(t *testing.T) {
	cacheClient, _ := newTestCacheClient(t)
	consumer := NewConsumer(cacheClient, newTestLogger(), WithMaxBatch(5))

	nextVisibleAt := time.Now().UTC().Add(-1 * time.Second).Truncate(time.Millisecond)
	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "retry-round-trip",
		},
		ClaimKeys:  []string{"notified:claim:retry-round-trip"},
		EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
		Version:    contractsalarm.QueueEnvelopeVersionV1,
		Retry: &domain.AlarmQueueRetryMetadata{
			Attempt:       4,
			RetryAfterMS:  120000,
			NextVisibleAt: nextVisibleAt.Format(time.RFC3339Nano),
			LastError:     "temporary upstream error",
		},
	}

	require.NoError(t, consumer.ScheduleRetry(context.Background(), []domain.AlarmQueueEnvelope{envelope}))

	envelopes, err := consumer.DrainBatch(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, envelopes, 1)
	require.NotNil(t, envelopes[0].Retry)
	assert.Equal(t, 4, envelopes[0].Retry.Attempt)
	assert.Equal(t, int64(120000), envelopes[0].Retry.RetryAfterMS)
	assert.Equal(t, nextVisibleAt.Format(time.RFC3339Nano), envelopes[0].Retry.NextVisibleAt)
	assert.Equal(t, "temporary upstream error", envelopes[0].Retry.LastError)
}

func TestConsumerDrainBatch_PreservesDeterministicSameTimestampRetryOrdering(t *testing.T) {
	cacheClient, _ := newTestCacheClient(t)
	consumer := NewConsumer(cacheClient, newTestLogger(), WithMaxBatch(5))

	nextVisibleAt := time.Now().UTC().Add(-1 * time.Minute).Truncate(time.Millisecond)
	retryB := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "retry-b",
		},
		ClaimKeys:  []string{"notified:claim:retry-b"},
		EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
		Version:    contractsalarm.QueueEnvelopeVersionV1,
		Retry: &domain.AlarmQueueRetryMetadata{
			Attempt:       1,
			RetryAfterMS:  0,
			NextVisibleAt: nextVisibleAt.Format(time.RFC3339Nano),
			LastError:     "temporary failure",
		},
	}
	retryA := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "retry-a",
		},
		ClaimKeys:  []string{"notified:claim:retry-a"},
		EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
		Version:    contractsalarm.QueueEnvelopeVersionV1,
		Retry: &domain.AlarmQueueRetryMetadata{
			Attempt:       1,
			RetryAfterMS:  0,
			NextVisibleAt: nextVisibleAt.Format(time.RFC3339Nano),
			LastError:     "temporary failure",
		},
	}

	require.NoError(t, consumer.ScheduleRetry(context.Background(), []domain.AlarmQueueEnvelope{retryB, retryA}))

	envelopes, err := consumer.DrainBatch(context.Background(), 2)
	require.NoError(t, err)
	require.Len(t, envelopes, 2)
	assert.Equal(t, "retry-b", envelopes[0].Notification.RoomID)
	assert.Equal(t, "retry-a", envelopes[1].Notification.RoomID)
}

func TestConsumerDrainBatch_PreservesSameTimestampOrderingAcrossSeparateScheduleRetryCalls(t *testing.T) {
	cacheClient, _ := newTestCacheClient(t)
	consumer := NewConsumer(cacheClient, newTestLogger(), WithMaxBatch(5))

	nextVisibleAt := time.Now().UTC().Add(-1 * time.Minute).Truncate(time.Millisecond)
	firstA := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "retry-first-call-a",
		},
		ClaimKeys:  []string{"notified:claim:retry-first-call-a"},
		EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
		Version:    contractsalarm.QueueEnvelopeVersionV1,
		Retry: &domain.AlarmQueueRetryMetadata{
			Attempt:       1,
			RetryAfterMS:  0,
			NextVisibleAt: nextVisibleAt.Format(time.RFC3339Nano),
			LastError:     "temporary failure",
		},
	}
	firstB := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "retry-first-call-b",
		},
		ClaimKeys:  []string{"notified:claim:retry-first-call-b"},
		EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
		Version:    contractsalarm.QueueEnvelopeVersionV1,
		Retry: &domain.AlarmQueueRetryMetadata{
			Attempt:       1,
			RetryAfterMS:  0,
			NextVisibleAt: nextVisibleAt.Format(time.RFC3339Nano),
			LastError:     "temporary failure",
		},
	}
	second := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "retry-second-call",
		},
		ClaimKeys:  []string{"notified:claim:retry-second-call"},
		EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
		Version:    contractsalarm.QueueEnvelopeVersionV1,
		Retry: &domain.AlarmQueueRetryMetadata{
			Attempt:       1,
			RetryAfterMS:  0,
			NextVisibleAt: nextVisibleAt.Format(time.RFC3339Nano),
			LastError:     "temporary failure",
		},
	}

	require.NoError(t, consumer.ScheduleRetry(context.Background(), []domain.AlarmQueueEnvelope{firstA, firstB}))
	require.NoError(t, consumer.ScheduleRetry(context.Background(), []domain.AlarmQueueEnvelope{second}))

	envelopes, err := consumer.DrainBatch(context.Background(), 3)
	require.NoError(t, err)
	require.Len(t, envelopes, 3)
	assert.Equal(t, "retry-first-call-a", envelopes[0].Notification.RoomID)
	assert.Equal(t, "retry-first-call-b", envelopes[1].Notification.RoomID)
	assert.Equal(t, "retry-second-call", envelopes[2].Notification.RoomID)
}

func TestConsumerScheduleRetry_PreservesDuplicateIdenticalEnvelopes(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	consumer := NewConsumer(cacheClient, newTestLogger(), WithMaxBatch(5))

	nextVisibleAt := time.Now().UTC().Add(-1 * time.Minute).Truncate(time.Millisecond)
	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "duplicate-retry",
		},
		ClaimKeys:  []string{"notified:claim:duplicate-retry"},
		EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
		Version:    contractsalarm.QueueEnvelopeVersionV1,
		Retry: &domain.AlarmQueueRetryMetadata{
			Attempt:       1,
			RetryAfterMS:  0,
			NextVisibleAt: nextVisibleAt.Format(time.RFC3339Nano),
			LastError:     "temporary failure",
		},
	}

	require.NoError(t, consumer.ScheduleRetry(context.Background(), []domain.AlarmQueueEnvelope{envelope, envelope}))

	retrySet, err := mini.SortedSet(contractsalarm.DispatchRetryQueueKey)
	require.NoError(t, err)
	require.Len(t, retrySet, 2)

	envelopes, err := consumer.DrainBatch(context.Background(), 2)
	require.NoError(t, err)
	require.Len(t, envelopes, 2)
	assert.Equal(t, "duplicate-retry", envelopes[0].Notification.RoomID)
	assert.Equal(t, "duplicate-retry", envelopes[1].Notification.RoomID)
}

func TestResolveRetryVisibleAt_Errors(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name     string
		envelope domain.AlarmQueueEnvelope
		wantErr  string
	}{
		{
			name:     "nil retry",
			envelope: domain.AlarmQueueEnvelope{},
			wantErr:  "retry metadata is required",
		},
		{
			name: "invalid next_visible_at",
			envelope: domain.AlarmQueueEnvelope{
				Retry: &domain.AlarmQueueRetryMetadata{
					NextVisibleAt: "not-a-time",
				},
			},
			wantErr: "parse retry next_visible_at",
		},
		{
			name: "negative retry_after_ms",
			envelope: domain.AlarmQueueEnvelope{
				Retry: &domain.AlarmQueueRetryMetadata{
					RetryAfterMS: -1,
				},
			},
			wantErr: "retry_after_ms must be zero or greater",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveRetryVisibleAt(tc.envelope, now)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestConsumerDrainBatch_ReturnsDueDelayedRetriesBeforeActiveQueueItems(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	publisher := NewPublisher(cacheClient, newTestLogger())
	consumer := NewConsumer(cacheClient, newTestLogger(), WithMaxBatch(5))

	dueAt := time.Now().UTC().Add(-1 * time.Minute).Truncate(time.Millisecond)
	futureAt := time.Now().UTC().Add(2 * time.Minute).Truncate(time.Millisecond)
	due := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "retry-due",
		},
		ClaimKeys:  []string{"notified:claim:retry-due"},
		EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
		Version:    contractsalarm.QueueEnvelopeVersionV1,
		Retry: &domain.AlarmQueueRetryMetadata{
			Attempt:       2,
			RetryAfterMS:  1000,
			NextVisibleAt: dueAt.Format(time.RFC3339Nano),
			LastError:     "transient error",
		},
	}
	future := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "retry-future",
		},
		ClaimKeys:  []string{"notified:claim:retry-future"},
		EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
		Version:    contractsalarm.QueueEnvelopeVersionV1,
		Retry: &domain.AlarmQueueRetryMetadata{
			Attempt:       3,
			RetryAfterMS:  120000,
			NextVisibleAt: futureAt.Format(time.RFC3339Nano),
			LastError:     "still backing off",
		},
	}

	require.NoError(t, consumer.ScheduleRetry(context.Background(), []domain.AlarmQueueEnvelope{due, future}))
	_, err := publisher.Publish(
		context.Background(),
		&domain.AlarmNotification{AlarmType: domain.AlarmTypeLive, RoomID: "active"},
		[]string{"notified:claim:active"},
	)
	require.NoError(t, err)

	envelopes, err := consumer.DrainBatch(context.Background(), 3)
	require.NoError(t, err)
	require.Len(t, envelopes, 2)
	assert.Equal(t, "retry-due", envelopes[0].Notification.RoomID)
	assert.Equal(t, "active", envelopes[1].Notification.RoomID)

	retrySet, err := mini.SortedSet(contractsalarm.DispatchRetryQueueKey)
	require.NoError(t, err)
	require.Len(t, retrySet, 1)
	payload, _ := unwrapSingleRetryMember(t, retrySet)
	assert.Equal(t, mustMarshalEnvelope(t, future), payload)
}

func TestConsumerDrainBatch_DoesNotReturnDelayedRetryBeforeNextVisibleAt(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	publisher := NewPublisher(cacheClient, newTestLogger())
	consumer := NewConsumer(cacheClient, newTestLogger(), WithMaxBatch(5))

	futureAt := time.Now().UTC().Add(2 * time.Minute).Truncate(time.Millisecond)
	future := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "retry-future",
		},
		ClaimKeys:  []string{"notified:claim:retry-future"},
		EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
		Version:    contractsalarm.QueueEnvelopeVersionV1,
		Retry: &domain.AlarmQueueRetryMetadata{
			Attempt:       2,
			RetryAfterMS:  120000,
			NextVisibleAt: futureAt.Format(time.RFC3339Nano),
			LastError:     "still backing off",
		},
	}

	require.NoError(t, consumer.ScheduleRetry(context.Background(), []domain.AlarmQueueEnvelope{future}))
	_, err := publisher.Publish(
		context.Background(),
		&domain.AlarmNotification{AlarmType: domain.AlarmTypeLive, RoomID: "active-only"},
		[]string{"notified:claim:active-only"},
	)
	require.NoError(t, err)

	envelopes, err := consumer.DrainBatch(context.Background(), 2)
	require.NoError(t, err)
	require.Len(t, envelopes, 1)
	assert.Equal(t, "active-only", envelopes[0].Notification.RoomID)

	retrySet, err := mini.SortedSet(contractsalarm.DispatchRetryQueueKey)
	require.NoError(t, err)
	require.Len(t, retrySet, 1)
	payload, _ := unwrapSingleRetryMember(t, retrySet)
	assert.Equal(t, mustMarshalEnvelope(t, future), payload)
}

func TestConsumerMoveToDLQ_PreservesSerializedEnvelope(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	consumer := NewConsumer(cacheClient, newTestLogger(), WithMaxBatch(5))

	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "dlq-room",
		},
		ClaimKeys:  []string{"notified:claim:dlq-room"},
		EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
		Version:    contractsalarm.QueueEnvelopeVersionV1,
		Retry: &domain.AlarmQueueRetryMetadata{
			Attempt:       5,
			RetryAfterMS:  5000,
			NextVisibleAt: time.Now().UTC().Add(5 * time.Second).Format(time.RFC3339Nano),
			LastError:     "permanent delivery failure",
		},
	}

	require.NoError(t, consumer.MoveToDLQ(context.Background(), []domain.AlarmQueueEnvelope{envelope}))

	items, err := mini.List(contractsalarm.DispatchDLQKey)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, mustMarshalEnvelope(t, envelope), items[0])
}

func TestConsumerMoveToDLQ_PreservesOriginalLegacyRawPayload(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	consumer := NewConsumer(cacheClient, newTestLogger(), WithMaxBatch(5))

	legacyRaw := "{\n" +
		"  \"notification\": {\n" +
		"    \"room_id\": \"room-legacy\",\n" +
		"    \"channel\": null,\n" +
		"    \"stream\": null,\n" +
		"    \"minutes_until\": 7,\n" +
		"    \"users\": []\n" +
		"  },\n" +
		"  \"claim_keys\": [\"notified:claim:room-legacy\"],\n" +
		"  \"enqueued_at\": \"2026-02-25T13:00:00Z\",\n" +
		"  \"version\": 1\n" +
		"}"

	cmd := cacheClient.B().Lpush().Key(AlarmDispatchQueue).Element(legacyRaw).Build()
	results := cacheClient.DoMulti(context.Background(), cmd)
	require.Len(t, results, 1)
	require.NoError(t, results[0].Error())

	envelopes, err := consumer.DrainBatch(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, envelopes, 1)
	require.Equal(t, domain.AlarmTypeLive, envelopes[0].Notification.AlarmType)

	require.NoError(t, consumer.MoveToDLQ(context.Background(), envelopes))

	items, err := mini.List(contractsalarm.DispatchDLQKey)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, legacyRaw, items[0])
}

func TestConsumerMoveToDLQ_UsesCurrentStateWhenDirectEnvelopeWasMutated(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	consumer := NewConsumer(cacheClient, newTestLogger(), WithMaxBatch(5))

	legacyRaw := "{\n" +
		"  \"notification\": {\n" +
		"    \"room_id\": \"room-direct-mutate\",\n" +
		"    \"channel\": null,\n" +
		"    \"stream\": null,\n" +
		"    \"minutes_until\": 5,\n" +
		"    \"users\": []\n" +
		"  },\n" +
		"  \"claim_keys\": [\"notified:claim:room-direct-mutate\"],\n" +
		"  \"enqueued_at\": \"2026-02-25T13:00:00Z\",\n" +
		"  \"version\": 1\n" +
		"}"

	cmd := cacheClient.B().Lpush().Key(AlarmDispatchQueue).Element(legacyRaw).Build()
	results := cacheClient.DoMulti(context.Background(), cmd)
	require.Len(t, results, 1)
	require.NoError(t, results[0].Error())

	envelopes, err := consumer.DrainBatch(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, envelopes, 1)

	envelopes[0].Retry = &domain.AlarmQueueRetryMetadata{
		Attempt:       2,
		RetryAfterMS:  1000,
		NextVisibleAt: time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano),
		LastError:     "latest failure",
	}

	require.NoError(t, consumer.MoveToDLQ(context.Background(), envelopes))

	items, err := mini.List(contractsalarm.DispatchDLQKey)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.NotEqual(t, legacyRaw, items[0])

	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(items[0]), &raw))
	_, hasSourcePayload := raw["source_payload"]
	assert.False(t, hasSourcePayload)
	_, hasRetry := raw["retry"]
	assert.True(t, hasRetry)
}

func TestConsumerMoveToDLQ_PreservesLegacyRawPayloadAcrossRetryRoundTrip(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	consumer := NewConsumer(cacheClient, newTestLogger(), WithMaxBatch(5))

	legacyRaw := "{\n" +
		"  \"notification\": {\n" +
		"    \"room_id\": \"room-legacy-retry\",\n" +
		"    \"channel\": null,\n" +
		"    \"stream\": null,\n" +
		"    \"minutes_until\": 3,\n" +
		"    \"users\": []\n" +
		"  },\n" +
		"  \"claim_keys\": [\"notified:claim:room-legacy-retry\"],\n" +
		"  \"enqueued_at\": \"2026-02-25T13:00:00Z\",\n" +
		"  \"version\": 1\n" +
		"}"

	cmd := cacheClient.B().Lpush().Key(AlarmDispatchQueue).Element(legacyRaw).Build()
	results := cacheClient.DoMulti(context.Background(), cmd)
	require.Len(t, results, 1)
	require.NoError(t, results[0].Error())

	drained, err := consumer.DrainBatch(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, drained, 1)

	drained[0].Retry = &domain.AlarmQueueRetryMetadata{
		Attempt:       2,
		RetryAfterMS:  0,
		NextVisibleAt: time.Now().UTC().Add(-1 * time.Second).Format(time.RFC3339Nano),
		LastError:     "temporary failure",
	}

	require.NoError(t, consumer.ScheduleRetry(context.Background(), drained))

	retrySet, err := mini.SortedSet(contractsalarm.DispatchRetryQueueKey)
	require.NoError(t, err)
	require.Len(t, retrySet, 1)

	var retriedPayload string
	for member := range retrySet {
		payload, unwrapErr := unwrapRetryMember(member)
		require.NoError(t, unwrapErr)
		retriedPayload = payload
	}
	require.NotEmpty(t, retriedPayload)
	require.Contains(t, retriedPayload, "\"source_payload\":")

	retried, err := consumer.DrainBatch(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, retried, 1)

	require.NoError(t, consumer.MoveToDLQ(context.Background(), retried))

	items, err := mini.List(contractsalarm.DispatchDLQKey)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, retriedPayload, items[0])

	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(items[0]), &raw))
	sourcePayload, ok := raw["source_payload"].(string)
	require.True(t, ok)
	assert.Equal(t, legacyRaw, sourcePayload)
}

func TestConsumerDrainBatch_DropsContentAlarmTypesAndReleasesClaims(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	publisher := NewPublisher(cacheClient, newTestLogger())
	consumer := NewConsumer(cacheClient, newTestLogger(), WithMaxBatch(5))

	claimKey := "notified:claim:blocked-community"
	require.NoError(t, mini.Set(claimKey, "1"))

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

	_, err = publisher.Publish(
		context.Background(),
		&domain.AlarmNotification{AlarmType: domain.AlarmTypeLive, RoomID: "room-live"},
		[]string{"notified:claim:room-live"},
	)
	require.NoError(t, err)

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
	require.NoError(t, mini.Set(claimKey, "1"))

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
