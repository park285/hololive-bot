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
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/testutil"
)

func newTestCacheClient(t *testing.T) (cache.Client, *miniredis.Miniredis) {
	t.Helper()

	return testutil.NewTestCacheServiceWithMini(t.Context(), t)
}

// retiredRedisDispatchQueueKey는 퇴역한 Redis dispatch queue 키다. 상수는 지웠고(stack-audit 2026-09-26 T11), 발행 경로가
// 그 키에 다시 쓰지 않는지만 확인한다.
const retiredRedisDispatchQueueKey = "alarm:dispatch:queue"

func queueItemsOrEmpty(t *testing.T, mini *miniredis.Miniredis) []string {
	t.Helper()

	return queueItemsByKeyOrEmpty(t, mini, retiredRedisDispatchQueueKey)
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

func TestPublisherPublishWritesPendingOutboxEnvelopeWithVersion(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	repository := &fakeOutboxRepository{}
	publisher := NewPublisher(cacheClient, sharedlogging.NewTestLogger(),
		WithOutbox(repository),
		WithWakeupEnabled(false),
	)

	notification := &domain.AlarmNotification{
		AlarmType:    domain.AlarmTypeLive,
		RoomID:       "room-1",
		MinutesUntil: 5,
		Users:        []string{"user-1"},
	}
	claimKeys := []string{"notified:claim:room-1"}

	_, err := publisher.Publish(t.Context(), notification, claimKeys)
	require.NoError(t, err)

	assert.Empty(t, queueItemsOrEmpty(t, mini))
	require.Equal(t, 1, repository.insertBatchCalls)
	require.Len(t, repository.lastBatchInput.Envelopes, 1)

	envelope := repository.lastBatchInput.Envelopes[0]
	assert.Equal(t, "room-1", envelope.Notification.RoomID)
	assert.Equal(t, contractsalarm.QueueEnvelopeVersionV1, envelope.Version)
	assert.Equal(t, claimKeys, envelope.ClaimKeys)

	_, err = time.Parse(time.RFC3339, envelope.EnqueuedAt)
	require.NoError(t, err)
}

func TestPublisherPublishRejectsContentAlarmTypes(t *testing.T) {
	t.Parallel()

	publisher := NewPublisher(cachemocks.NewStrictClient(), sharedlogging.NewTestLogger())

	for _, alarmType := range []domain.AlarmType{domain.AlarmTypeCommunity, domain.AlarmTypeShorts} {
		t.Run(string(alarmType), func(t *testing.T) {
			t.Parallel()

			_, err := publisher.Publish(t.Context(), &domain.AlarmNotification{
				AlarmType: alarmType,
				RoomID:    "room-blocked",
			}, nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "live alarm route does not support alarm type")
			assert.Contains(t, err.Error(), string(alarmType))
		})
	}
}

func TestPublisherPublishDispatchBatchAcceptsYouTubeOutboxContentEnvelope(t *testing.T) {
	t.Parallel()

	publisher := NewPublisher(cachemocks.NewStrictClient(), sharedlogging.NewTestLogger(), WithWakeupEnabled(false), WithOutbox(&fakeOutboxRepository{}))
	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeShorts,
			RoomID:    "room-canonical",
		},
		SourceKind: domain.AlarmDispatchSourceKindYouTubeOutbox,
		YouTubeOutbox: &domain.YouTubeOutboxDispatchPayload{
			OutboxIDs: []int64{1},
			Kind:      domain.OutboxKindNewShort,
			AlarmType: domain.AlarmTypeShorts,
			ChannelID: "UC_test",
			Items: []domain.YouTubeOutboxItem{{
				OutboxID:  1,
				ContentID: "short:abc",
				Payload:   `{"video_id":"abc","title":"테스트 쇼츠"}`,
			}},
		},
		ClaimKeys: []string{"youtube-notification:NEW_SHORT:short:abc:room-canonical"},
		Version:   contractsalarm.QueueEnvelopeVersionV1,
	}

	result, err := publisher.PublishDispatchBatch(t.Context(), []domain.AlarmQueueEnvelope{envelope})
	require.NoError(t, err)
	assert.Equal(t, 1, result.RequestedDeliveries)
}

// 발행자는 버전이 없는 envelope을 V1으로 채우지 않는다. 모든 생산자가 버전을 명시하므로 0은 검증 오류다
// (stack-audit 2026-09-26 T11 holo-alarm-publisher-version-zero-default).
func TestPublisherPublishDispatchBatchRejectsMissingEnvelopeVersion(t *testing.T) {
	t.Parallel()

	outbox := &fakeOutboxRepository{}
	publisher := NewPublisher(cachemocks.NewStrictClient(), sharedlogging.NewTestLogger(), WithWakeupEnabled(false), WithOutbox(outbox))
	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{AlarmType: domain.AlarmTypeShorts, RoomID: "room-versionless"},
		SourceKind:   domain.AlarmDispatchSourceKindYouTubeOutbox,
		YouTubeOutbox: &domain.YouTubeOutboxDispatchPayload{
			OutboxIDs: []int64{1},
			Kind:      domain.OutboxKindNewShort,
			AlarmType: domain.AlarmTypeShorts,
			ChannelID: "UC_test",
			Items: []domain.YouTubeOutboxItem{{
				OutboxID:  1,
				ContentID: "short:abc",
				Payload:   `{"video_id":"abc","title":"테스트 쇼츠"}`,
			}},
		},
		ClaimKeys: []string{"youtube-notification:NEW_SHORT:short:abc:room-versionless"},
	}

	_, err := publisher.PublishDispatchBatch(t.Context(), []domain.AlarmQueueEnvelope{envelope})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "envelope version 0 is unsupported")
}
