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
)

func TestScheduleRetryAddsToSortedSet(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	consumer := NewConsumer(cacheClient, newTestLogger(), WithMaxBatch(5))

	nextVisibleAt := time.Now().UTC().Add(-1 * time.Second).Truncate(time.Millisecond)
	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "sched-retry-room",
		},
		ClaimKeys:  []string{"notified:claim:sched-retry-room"},
		EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
		Version:    contractsalarm.QueueEnvelopeVersionV1,
		Retry: &domain.AlarmQueueRetryMetadata{
			Attempt:       1,
			RetryAfterMS:  5000,
			NextVisibleAt: nextVisibleAt.Format(time.RFC3339Nano),
			LastError:     "transient error",
		},
	}

	require.NoError(t, consumer.ScheduleRetry(context.Background(), []domain.AlarmQueueEnvelope{envelope}))

	retrySet, err := mini.SortedSet(contractsalarm.DispatchRetryQueueKey)
	require.NoError(t, err)
	require.Len(t, retrySet, 1)

	payload, score := unwrapSingleRetryMember(t, retrySet)
	assert.Equal(t, mustMarshalEnvelope(t, envelope), payload)
	assert.Equal(t, float64(nextVisibleAt.UnixMilli()), score)
	assert.Empty(t, queueItemsOrEmpty(t, mini))
}

func TestScheduleRetryDLQOnMaxAttempts(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	consumer := NewConsumer(cacheClient, newTestLogger(), WithMaxBatch(5))

	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "dlq-overflow-room",
		},
		ClaimKeys:  []string{"notified:claim:dlq-overflow-room"},
		EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
		Version:    contractsalarm.QueueEnvelopeVersionV1,
		Retry: &domain.AlarmQueueRetryMetadata{
			Attempt:      10,
			RetryAfterMS: 5000,
			LastError:    "exhausted retries",
		},
	}

	require.NoError(t, consumer.MoveToDLQ(context.Background(), []domain.AlarmQueueEnvelope{envelope}))

	dlqItems, err := mini.List(contractsalarm.DispatchDLQKey)
	require.NoError(t, err)
	require.Len(t, dlqItems, 1)
	assert.JSONEq(t, mustMarshalEnvelope(t, envelope), dlqItems[0])

	retrySet, err := mini.SortedSet(contractsalarm.DispatchRetryQueueKey)
	if err == nil {
		assert.Empty(t, retrySet)
	}
}
