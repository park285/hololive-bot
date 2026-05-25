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
	sharedlogging "github.com/park285/shared-go/pkg/logging"
)

func TestDrainDelayedRetriesReturnsReadyItems(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	consumer := NewConsumer(cacheClient, sharedlogging.NewTestLogger(), WithMaxBatch(5))

	pastAt := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Millisecond)
	ready := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "delayed-ready-room",
		},
		ClaimKeys:  []string{"notified:claim:delayed-ready-room"},
		EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
		Version:    contractsalarm.QueueEnvelopeVersionV1,
		Retry: &domain.AlarmQueueRetryMetadata{
			Attempt:       1,
			RetryAfterMS:  1000,
			NextVisibleAt: pastAt.Format(time.RFC3339Nano),
			LastError:     "transient",
		},
	}

	require.NoError(t, consumer.ScheduleRetry(context.Background(), []domain.AlarmQueueEnvelope{ready}))

	envelopes, err := consumer.DrainBatch(context.Background(), 5)
	require.NoError(t, err)
	require.Len(t, envelopes, 1)
	assert.Equal(t, "delayed-ready-room", envelopes[0].Notification.RoomID)

	retrySet, err := mini.SortedSet(contractsalarm.DispatchRetryQueueKey)
	if err == nil {
		assert.Empty(t, retrySet)
	}
}

func TestDrainDelayedRetriesSkipsFutureItems(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	consumer := NewConsumer(cacheClient, sharedlogging.NewTestLogger(), WithMaxBatch(5))

	futureAt := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Millisecond)
	notReady := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "delayed-future-room",
		},
		ClaimKeys:  []string{"notified:claim:delayed-future-room"},
		EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
		Version:    contractsalarm.QueueEnvelopeVersionV1,
		Retry: &domain.AlarmQueueRetryMetadata{
			Attempt:       1,
			RetryAfterMS:  300000,
			NextVisibleAt: futureAt.Format(time.RFC3339Nano),
			LastError:     "backing off",
		},
	}

	require.NoError(t, consumer.ScheduleRetry(context.Background(), []domain.AlarmQueueEnvelope{notReady}))
	consumer.blockTimeout = 50 * time.Millisecond

	envelopes, err := consumer.DrainBatch(context.Background(), 5)
	require.NoError(t, err)
	assert.Empty(t, envelopes)

	retrySet, err := mini.SortedSet(contractsalarm.DispatchRetryQueueKey)
	require.NoError(t, err)
	assert.Len(t, retrySet, 1)
}

func TestDrainBatchPrioritizesDelayedRetries(t *testing.T) {
	cacheClient, _ := newTestCacheClient(t)
	publisher := NewPublisher(cacheClient, sharedlogging.NewTestLogger())
	consumer := NewConsumer(cacheClient, sharedlogging.NewTestLogger(), WithMaxBatch(5))

	pastAt := time.Now().UTC().Add(-1 * time.Minute).Truncate(time.Millisecond)
	retryEnv := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "priority-retry-room",
		},
		ClaimKeys:  []string{"notified:claim:priority-retry-room"},
		EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
		Version:    contractsalarm.QueueEnvelopeVersionV1,
		Retry: &domain.AlarmQueueRetryMetadata{
			Attempt:       2,
			RetryAfterMS:  0,
			NextVisibleAt: pastAt.Format(time.RFC3339Nano),
			LastError:     "transient",
		},
	}

	_, err := publisher.Publish(context.Background(),
		&domain.AlarmNotification{AlarmType: domain.AlarmTypeLive, RoomID: "active-queue-room"},
		[]string{"notified:claim:active-queue-room"},
	)
	require.NoError(t, err)

	require.NoError(t, consumer.ScheduleRetry(context.Background(), []domain.AlarmQueueEnvelope{retryEnv}))

	envelopes, err := consumer.DrainBatch(context.Background(), 5)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(envelopes), 1)
	assert.Equal(t, "priority-retry-room", envelopes[0].Notification.RoomID)
}
