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

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
)

func TestConsumerDrainBatchDropsInvalidCanonicalEnvelopeAndReleasesClaims(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	consumer := NewConsumer(cacheClient, newTestLogger(), WithMaxBatch(5))
	claimKey := "notified:claim:canonical-invalid"
	require.NoError(t, mini.Set(claimKey, "1"))

	beforeRejected := testutil.ToFloat64(alarmQueueEnvelopeTotal.WithLabelValues("rejected_canonical_source"))
	raw, err := json.Marshal(domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeShorts,
			RoomID:    "room-canonical-invalid",
		},
		SourceKind: domain.AlarmDispatchSourceKindYouTubeOutbox,
		ClaimKeys:  []string{claimKey},
		EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
		Version:    contractsalarm.QueueEnvelopeVersionV1,
	})
	require.NoError(t, err)

	results := cacheClient.DoMulti(context.Background(),
		cacheClient.B().Lpush().Key(AlarmDispatchQueue).Element(string(raw)).Build(),
	)
	require.Len(t, results, 1)
	require.NoError(t, results[0].Error())

	envelopes, err := consumer.DrainBatch(context.Background(), 1)
	require.NoError(t, err)
	assert.Empty(t, envelopes)
	assert.False(t, mini.Exists(claimKey))
	assert.Empty(t, queueItemsOrEmpty(t, mini))
	assert.Equal(t, beforeRejected+1, testutil.ToFloat64(alarmQueueEnvelopeTotal.WithLabelValues("rejected_canonical_source")))
}

func TestResolveRetryVisibleAtUsesRetryAfterWhenNextVisibleAtBlank(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 123000000, time.UTC)

	got, err := resolveRetryVisibleAt(domain.AlarmQueueEnvelope{
		Retry: &domain.AlarmQueueRetryMetadata{
			RetryAfterMS:  1500,
			NextVisibleAt: " \t ",
		},
	}, now)
	require.NoError(t, err)

	assert.Equal(t, now.Add(1500*time.Millisecond), got)
}

func TestAlarmQueueDerivedKeysFallbackAndAppendSuffix(t *testing.T) {
	tests := []struct {
		name      string
		queueKey  string
		wantRetry string
		wantDLQ   string
	}{
		{
			name:      "blank key uses dispatch defaults",
			queueKey:  " \t ",
			wantRetry: contractsalarm.DispatchRetryQueueKey,
			wantDLQ:   contractsalarm.DispatchDLQKey,
		},
		{
			name:      "non queue suffix appends derived suffixes",
			queueKey:  "alarm:custom",
			wantRetry: "alarm:custom:retry",
			wantDLQ:   "alarm:custom:dlq",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wantRetry, deriveRetryQueueKey(tc.queueKey))
			assert.Equal(t, tc.wantDLQ, deriveDLQKey(tc.queueKey))
		})
	}
}

func TestUnwrapRetryMemberRejectsInvalidEncodedPayload(t *testing.T) {
	member := retryMemberPrefix + "0000000000000:00000000000000000001:000000:not_base64!!"

	_, err := unwrapRetryMember(member)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode retry member payload")
}

func TestDrainDelayedRetriesSkipsNonPositiveCountWithoutCacheAccess(t *testing.T) {
	consumer := NewConsumer(cachemocks.NewStrictClient(), newTestLogger())

	values, err := consumer.drainDelayedRetries(context.Background(), 0, time.Now().UTC())
	require.NoError(t, err)
	assert.Empty(t, values)
}
