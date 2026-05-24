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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
	json "github.com/park285/shared-go/pkg/json"
)

func TestNewConsumerDefaultValues(t *testing.T) {
	cacheClient, _ := newTestCacheClient(t)
	consumer := NewConsumer(cacheClient, newTestLogger())

	assert.Equal(t, contractsalarm.DispatchQueueKey, consumer.queueKey)
	assert.Equal(t, contractsalarm.DispatchRetryQueueKey, consumer.retryQueueKey)
	assert.Equal(t, contractsalarm.DispatchDLQKey, consumer.dlqKey)
	assert.Equal(t, 50, consumer.maxBatch)
	assert.Equal(t, 1*time.Second, consumer.blockTimeout)
}

func TestNewConsumerAppliesOptions(t *testing.T) {
	cacheClient, _ := newTestCacheClient(t)
	consumer := NewConsumer(cacheClient, newTestLogger(),
		WithQueueKey("alarm:custom:queue"),
		WithMaxBatch(10),
	)

	assert.Equal(t, "alarm:custom:queue", consumer.queueKey)
	assert.Equal(t, "alarm:custom:retry", consumer.retryQueueKey)
	assert.Equal(t, "alarm:custom:dlq", consumer.dlqKey)
	assert.Equal(t, 10, consumer.maxBatch)
}

func TestDrainBatchReturnsEmptyOnTimeout(t *testing.T) {
	cacheClient, _ := newTestCacheClient(t)
	consumer := NewConsumer(cacheClient, newTestLogger(),
		WithMaxBatch(5),
	)
	// miniredis BRPOP with no items returns nil immediately
	consumer.blockTimeout = 50 * time.Millisecond

	envelopes, err := consumer.DrainBatch(context.Background(), 5)
	require.NoError(t, err)
	assert.Empty(t, envelopes)
}

func TestDrainBatchReturnsSingleItem(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	consumer := NewConsumer(cacheClient, newTestLogger(), WithMaxBatch(5))

	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "single-room",
		},
		ClaimKeys:  []string{"notified:claim:single-room"},
		EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
		Version:    contractsalarm.QueueEnvelopeVersionV1,
	}
	raw, err := json.Marshal(envelope)
	require.NoError(t, err)

	require.NoError(t, cacheClient.DoMulti(context.Background(),
		cacheClient.B().Lpush().Key(AlarmDispatchQueue).Element(string(raw)).Build(),
	)[0].Error())

	envelopes, err := consumer.DrainBatch(context.Background(), 5)
	require.NoError(t, err)
	require.Len(t, envelopes, 1)
	assert.Equal(t, "single-room", envelopes[0].Notification.RoomID)

	assert.Empty(t, queueItemsOrEmpty(t, mini))
}

func TestDrainBatchDrainsMultipleUpToLimit(t *testing.T) {
	cacheClient, _ := newTestCacheClient(t)
	consumer := NewConsumer(cacheClient, newTestLogger(), WithMaxBatch(3))

	for i := range 5 {
		roomID := "room-" + string(rune('a'+i))
		envelope := domain.AlarmQueueEnvelope{
			Notification: domain.AlarmNotification{
				AlarmType: domain.AlarmTypeLive,
				RoomID:    roomID,
			},
			ClaimKeys:  []string{"notified:claim:" + roomID},
			EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
			Version:    contractsalarm.QueueEnvelopeVersionV1,
		}
		raw, err := json.Marshal(envelope)
		require.NoError(t, err)
		require.NoError(t, cacheClient.DoMulti(context.Background(),
			cacheClient.B().Lpush().Key(AlarmDispatchQueue).Element(string(raw)).Build(),
		)[0].Error())
	}

	envelopes, err := consumer.DrainBatch(context.Background(), 3)
	require.NoError(t, err)
	assert.Len(t, envelopes, 3)
}
