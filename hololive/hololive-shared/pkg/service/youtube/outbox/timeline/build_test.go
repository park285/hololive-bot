package timeline

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var timelineBase = time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC)

func TestDeriveMetrics_NilRowDoesNotPanic(t *testing.T) {
	t.Parallel()

	require.NotPanics(t, func() { DeriveMetrics(nil) })
}

func TestClonePostLatencyInt64(t *testing.T) {
	t.Parallel()

	require.Nil(t, ClonePostLatencyInt64(nil))

	original := int64(42)
	cloned := ClonePostLatencyInt64(&original)
	require.NotNil(t, cloned)
	require.Equal(t, int64(42), *cloned)
	require.NotSame(t, &original, cloned)

	*cloned = 99
	require.Equal(t, int64(42), original)
}

func TestDurationMillisBetween(t *testing.T) {
	t.Parallel()

	start := timelineBase
	end := timelineBase.Add(90 * time.Second)

	require.Nil(t, durationMillisBetween(nil, &end))
	require.Nil(t, durationMillisBetween(&start, nil))
	require.Nil(t, durationMillisBetween(nil, nil))

	got := durationMillisBetween(&start, &end)
	require.NotNil(t, got)
	require.Equal(t, int64(90_000), *got)

	negative := durationMillisBetween(&end, &start)
	require.NotNil(t, negative)
	require.Equal(t, int64(-90_000), *negative)

	kst := time.FixedZone("KST", 9*3600)
	startKST := start.In(kst)
	sameDelta := durationMillisBetween(&startKST, &end)
	require.NotNil(t, sameDelta)
	require.Equal(t, int64(90_000), *sameDelta)
}

func TestSumDurationMillis(t *testing.T) {
	t.Parallel()

	require.Nil(t, sumDurationMillis())
	require.Nil(t, sumDurationMillis(nil, nil))

	a := int64(100)
	b := int64(250)
	neg := int64(-50)

	single := sumDurationMillis(nil, &a, nil)
	require.NotNil(t, single)
	require.Equal(t, int64(100), *single)

	total := sumDurationMillis(&a, &b, &neg)
	require.NotNil(t, total)
	require.Equal(t, int64(300), *total)
}

func TestDeriveMetrics_RetryAttemptCount(t *testing.T) {
	t.Parallel()

	cases := []struct {
		maxOrdinal int64
		want       int64
	}{
		{maxOrdinal: 0, want: 0},
		{maxOrdinal: 1, want: 0},
		{maxOrdinal: 2, want: 1},
		{maxOrdinal: 5, want: 4},
	}
	for _, tc := range cases {
		row := &PostDeliveryTimeline{MaxAttemptOrdinal: tc.maxOrdinal}
		DeriveMetrics(row)
		require.Equal(t, tc.want, row.RetryAttemptCount, "maxOrdinal=%d", tc.maxOrdinal)
	}
}

func TestDeriveMetrics_InternalLatencyExceededBoundary(t *testing.T) {
	t.Parallel()

	detected := timelineBase

	atThreshold := detected.Add(2 * time.Minute)
	rowAt := &PostDeliveryTimeline{DetectedAt: &detected, AlarmSentAt: &atThreshold}
	DeriveMetrics(rowAt)
	require.NotNil(t, rowAt.InternalLatencyMillis)
	require.Equal(t, PostLatencyExceededThresholdMillis, *rowAt.InternalLatencyMillis)
	require.NotNil(t, rowAt.InternalLatencyExceeded)
	require.False(t, *rowAt.InternalLatencyExceeded)

	overThreshold := detected.Add(2*time.Minute + time.Millisecond)
	rowOver := &PostDeliveryTimeline{DetectedAt: &detected, AlarmSentAt: &overThreshold}
	DeriveMetrics(rowOver)
	require.NotNil(t, rowOver.InternalLatencyExceeded)
	require.True(t, *rowOver.InternalLatencyExceeded)
}

func TestDeriveMetrics_InternalLatencyExceededNilWhenAlarmSentMissing(t *testing.T) {
	t.Parallel()

	detected := timelineBase
	row := &PostDeliveryTimeline{DetectedAt: &detected}
	DeriveMetrics(row)
	require.Nil(t, row.InternalLatencyMillis)
	require.Nil(t, row.InternalLatencyExceeded)
}

func TestPositiveDurationMillis(t *testing.T) {
	t.Parallel()

	value, ok := positiveDurationMillis(nil)
	require.Equal(t, int64(0), value)
	require.False(t, ok)

	zero := int64(0)
	value, ok = positiveDurationMillis(&zero)
	require.Equal(t, int64(0), value)
	require.False(t, ok)

	neg := int64(-5)
	value, ok = positiveDurationMillis(&neg)
	require.Equal(t, int64(0), value)
	require.False(t, ok)

	pos := int64(5)
	value, ok = positiveDurationMillis(&pos)
	require.Equal(t, int64(5), value)
	require.True(t, ok)
}

func TestSelectDominantDelaySource(t *testing.T) {
	t.Parallel()

	require.Equal(t, PostDelaySourceNone, selectDominantDelaySource(0, false, 0, false))
	require.Equal(t, PostDelaySourceInternalDelivery, selectDominantDelaySource(0, false, 10, true))
	require.Equal(t, PostDelaySourceExternalCollection, selectDominantDelaySource(10, true, 0, false))
	require.Equal(t, PostDelaySourceMixed, selectDominantDelaySource(10, true, 10, true))
	require.Equal(t, PostDelaySourceExternalCollection, selectDominantDelaySource(20, true, 10, true))
	require.Equal(t, PostDelaySourceInternalDelivery, selectDominantDelaySource(10, true, 20, true))
}

func TestPostLatencyDelaySourceEligible(t *testing.T) {
	t.Parallel()

	overThreshold := PostLatencyExceededThresholdMillis + 1
	underThreshold := PostLatencyExceededThresholdMillis

	require.True(t, postLatencyDelaySourceEligible(&PostDeliveryTimeline{AlarmLatencyExceeded: new(true)}))
	require.False(t, postLatencyDelaySourceEligible(&PostDeliveryTimeline{AlarmLatencyExceeded: new(false)}))
	require.True(t, postLatencyDelaySourceEligible(&PostDeliveryTimeline{
		AlarmLatencyExceeded:    new(false),
		InternalLatencyExceeded: new(true),
	}))

	require.True(t, postLatencyDelaySourceEligible(&PostDeliveryTimeline{InternalLatencyExceeded: new(true)}))
	require.True(t, postLatencyDelaySourceEligible(&PostDeliveryTimeline{PublishToDetectMillis: &overThreshold}))
	require.False(t, postLatencyDelaySourceEligible(&PostDeliveryTimeline{PublishToDetectMillis: &underThreshold}))
	require.False(t, postLatencyDelaySourceEligible(&PostDeliveryTimeline{}))
}

func TestClassifyDelaySource_EligibleButNoPositiveDurationsIsNone(t *testing.T) {
	t.Parallel()

	row := &PostDeliveryTimeline{AlarmLatencyExceeded: new(true)}
	require.Equal(t, PostDelaySourceNone, classifyDelaySource(row))
}

func TestClassifyDelaySource_NotEligibleIsNone(t *testing.T) {
	t.Parallel()

	external := int64(1000)
	internal := int64(1000)
	row := &PostDeliveryTimeline{
		PublishToDetectMillis: &external,
		InternalLatencyMillis: &internal,
	}
	require.Equal(t, PostDelaySourceNone, classifyDelaySource(row))
}

func TestDeriveMetrics_ClassifiesExternalCollection(t *testing.T) {
	t.Parallel()

	published := timelineBase
	detected := published.Add(100 * time.Second)
	alarmSent := detected.Add(30 * time.Second)

	row := &PostDeliveryTimeline{
		ActualPublishedAt:    &published,
		DetectedAt:           &detected,
		AlarmSentAt:          &alarmSent,
		AlarmLatencyExceeded: new(true),
	}
	DeriveMetrics(row)

	require.Equal(t, int64(100_000), *row.PublishToDetectMillis)
	require.Equal(t, int64(30_000), *row.InternalLatencyMillis)
	require.Equal(t, PostDelaySourceExternalCollection, row.DelaySource)
}

func TestDeriveMetrics_ClassifiesMixedOnEqualDurations(t *testing.T) {
	t.Parallel()

	published := timelineBase
	detected := published.Add(70 * time.Second)
	alarmSent := detected.Add(70 * time.Second)

	row := &PostDeliveryTimeline{
		ActualPublishedAt:    &published,
		DetectedAt:           &detected,
		AlarmSentAt:          &alarmSent,
		AlarmLatencyExceeded: new(true),
	}
	DeriveMetrics(row)

	require.Equal(t, PostDelaySourceMixed, row.DelaySource)
}

func TestDeriveMetrics_ClassifiesInternalDelivery(t *testing.T) {
	t.Parallel()

	published := timelineBase
	detected := published.Add(30 * time.Second)
	alarmSent := detected.Add(100 * time.Second)

	row := &PostDeliveryTimeline{
		ActualPublishedAt:    &published,
		DetectedAt:           &detected,
		AlarmSentAt:          &alarmSent,
		AlarmLatencyExceeded: new(true),
	}
	DeriveMetrics(row)

	require.Equal(t, PostDelaySourceInternalDelivery, row.DelaySource)
}

func TestIsJobFailureDetected(t *testing.T) {
	t.Parallel()

	failure := timelineBase
	success := timelineBase.Add(time.Minute)

	require.False(t, isJobFailureDetected(&PostDeliveryTimeline{}))
	require.True(t, isJobFailureDetected(&PostDeliveryTimeline{LastFailureAt: &failure}))
	require.False(t, isJobFailureDetected(&PostDeliveryTimeline{LastFailureAt: &failure, AlarmSentAt: &success}))
	require.False(t, isJobFailureDetected(&PostDeliveryTimeline{LastFailureAt: &failure, FirstSuccessAt: &success}))
	require.False(t, isJobFailureDetected(&PostDeliveryTimeline{LastFailureAt: &failure, LastSuccessAt: &success}))
}

func TestDeriveRetryAccumulationMillis(t *testing.T) {
	t.Parallel()

	finished := timelineBase

	t.Run("no failed attempts returns nil", func(t *testing.T) {
		t.Parallel()
		row := &PostDeliveryTimeline{FailedAttemptCount: 0, FirstAttemptFinishedAt: &finished}
		require.Nil(t, deriveRetryAccumulationMillis(row))
	})

	t.Run("missing first attempt finished returns nil", func(t *testing.T) {
		t.Parallel()
		row := &PostDeliveryTimeline{FailedAttemptCount: 1}
		require.Nil(t, deriveRetryAccumulationMillis(row))
	})

	t.Run("prefers alarm sent over later candidates", func(t *testing.T) {
		t.Parallel()
		alarmSent := finished.Add(10 * time.Second)
		nextRetry := finished.Add(60 * time.Second)
		row := &PostDeliveryTimeline{
			FailedAttemptCount:     1,
			FirstAttemptFinishedAt: &finished,
			AlarmSentAt:            &alarmSent,
			NextRetryAt:            &nextRetry,
		}
		got := deriveRetryAccumulationMillis(row)
		require.NotNil(t, got)
		require.Equal(t, int64(10_000), *got)
	})

	t.Run("skips candidates not strictly after finished", func(t *testing.T) {
		t.Parallel()
		alarmBefore := finished.Add(-5 * time.Second)
		firstSuccess := finished.Add(20 * time.Second)
		row := &PostDeliveryTimeline{
			FailedAttemptCount:     1,
			FirstAttemptFinishedAt: &finished,
			AlarmSentAt:            &alarmBefore,
			FirstSuccessAt:         &firstSuccess,
		}
		got := deriveRetryAccumulationMillis(row)
		require.NotNil(t, got)
		require.Equal(t, int64(20_000), *got)
	})

	t.Run("no candidate after finished returns nil", func(t *testing.T) {
		t.Parallel()
		before := finished.Add(-time.Second)
		row := &PostDeliveryTimeline{
			FailedAttemptCount:     1,
			FirstAttemptFinishedAt: &finished,
			AlarmSentAt:            &before,
		}
		require.Nil(t, deriveRetryAccumulationMillis(row))
	})

	t.Run("sub-millisecond gap truncates to zero and returns nil", func(t *testing.T) {
		t.Parallel()
		justAfter := finished.Add(500 * time.Microsecond)
		row := &PostDeliveryTimeline{
			FailedAttemptCount:     1,
			FirstAttemptFinishedAt: &finished,
			AlarmSentAt:            &justAfter,
		}
		require.Nil(t, deriveRetryAccumulationMillis(row))
	})
}

func TestClassifyPrimaryInternalDelayCause(t *testing.T) {
	t.Parallel()

	require.Equal(t, PostInternalDelayCauseNone, classifyPrimaryInternalDelayCause(nil))
	require.Equal(t, PostInternalDelayCauseJobFailure, classifyPrimaryInternalDelayCause(&PostDeliveryTimeline{JobFailureDetected: true}))

	high := int64(100_000)
	low := int64(50_000)
	equal := int64(70_000)
	zero := int64(0)

	require.Equal(t, PostInternalDelayCauseRetryAccumulation, classifyPrimaryInternalDelayCause(&PostDeliveryTimeline{
		RetryAccumulationMillis: &high,
		QueueWaitMillis:         &low,
	}))
	require.Equal(t, PostInternalDelayCauseQueueWait, classifyPrimaryInternalDelayCause(&PostDeliveryTimeline{
		RetryAccumulationMillis: &low,
		QueueWaitMillis:         &high,
	}))
	require.Equal(t, PostInternalDelayCauseRetryAccumulation, classifyPrimaryInternalDelayCause(&PostDeliveryTimeline{
		RetryAccumulationMillis: &equal,
		QueueWaitMillis:         &equal,
	}))
	require.Equal(t, PostInternalDelayCauseQueueWait, classifyPrimaryInternalDelayCause(&PostDeliveryTimeline{
		QueueWaitMillis: &high,
	}))
	require.Equal(t, PostInternalDelayCauseRetryAccumulation, classifyPrimaryInternalDelayCause(&PostDeliveryTimeline{
		RetryAccumulationMillis: &high,
	}))
	require.Equal(t, PostInternalDelayCauseNone, classifyPrimaryInternalDelayCause(&PostDeliveryTimeline{
		QueueWaitMillis:         &zero,
		RetryAccumulationMillis: &zero,
	}))
}

func TestClassifyPostLatencyClassificationStatus(t *testing.T) {
	t.Parallel()

	require.Equal(t, PostLatencyClassificationStatusInsufficientEvidence, classifyPostLatencyClassificationStatus(nil))
	require.Equal(t, PostLatencyClassificationStatusExceeded, classifyPostLatencyClassificationStatus(&PostDeliveryTimeline{AlarmLatencyExceeded: new(true)}))
	require.Equal(t, PostLatencyClassificationStatusWithinTarget, classifyPostLatencyClassificationStatus(&PostDeliveryTimeline{AlarmLatencyExceeded: new(false)}))

	over := PostLatencyExceededThresholdMillis + 1
	require.Equal(t, PostLatencyClassificationStatusWithinTarget, classifyPostLatencyClassificationStatus(&PostDeliveryTimeline{
		AlarmLatencyExceeded:  new(false),
		PublishToDetectMillis: &over,
	}))

	require.Equal(t, PostLatencyClassificationStatusExceeded, classifyPostLatencyClassificationStatus(&PostDeliveryTimeline{InternalLatencyExceeded: new(true)}))
	require.Equal(t, PostLatencyClassificationStatusExceeded, classifyPostLatencyClassificationStatus(&PostDeliveryTimeline{PublishToDetectMillis: &over}))
	require.Equal(t, PostLatencyClassificationStatusExceeded, classifyPostLatencyClassificationStatus(&PostDeliveryTimeline{QueueWaitMillis: &over}))
	require.Equal(t, PostLatencyClassificationStatusExceeded, classifyPostLatencyClassificationStatus(&PostDeliveryTimeline{RetryAccumulationMillis: &over}))

	under := PostLatencyExceededThresholdMillis
	require.Equal(t, PostLatencyClassificationStatusInsufficientEvidence, classifyPostLatencyClassificationStatus(&PostDeliveryTimeline{PublishToDetectMillis: &under}))
}

func TestBuildPostLatencyClassification_NilRow(t *testing.T) {
	t.Parallel()

	result := BuildPostLatencyClassification(nil)
	require.Equal(t, PostLatencyClassificationStatusInsufficientEvidence, result.Status)
	require.Equal(t, PostLatencyExceededThresholdMillis, result.ThresholdMillis)
	require.Equal(t, PostDelaySourceNone, result.DelaySource)
	require.Equal(t, PostInternalDelayCauseNone, result.InternalDelayCause)
	require.NotNil(t, result.Evidence)
	require.Len(t, result.Evidence, 0)
}

func TestBuildPostLatencyClassification_CarriesRowSourcesAndDefaultsEmptyStrings(t *testing.T) {
	t.Parallel()

	result := BuildPostLatencyClassification(&PostDeliveryTimeline{
		AlarmLatencyExceeded: new(true),
		DelaySource:          PostDelaySourceExternalCollection,
		InternalDelayCause:   PostInternalDelayCauseQueueWait,
	})
	require.Equal(t, PostLatencyClassificationStatusExceeded, result.Status)
	require.Equal(t, PostDelaySourceExternalCollection, result.DelaySource)
	require.Equal(t, PostInternalDelayCauseQueueWait, result.InternalDelayCause)

	defaults := BuildPostLatencyClassification(&PostDeliveryTimeline{})
	require.Equal(t, PostDelaySourceNone, defaults.DelaySource)
	require.Equal(t, PostInternalDelayCauseNone, defaults.InternalDelayCause)
}

func TestBuildPostLatencyClassificationEvidence(t *testing.T) {
	t.Parallel()

	require.Len(t, buildPostLatencyClassificationEvidence(nil), 0)

	alarm := int64(1)
	publish := int64(2)
	internal := int64(3)
	queue := int64(4)
	retry := int64(5)

	row := &PostDeliveryTimeline{
		AlarmLatencyMillis:      &alarm,
		AlarmLatencyExceeded:    new(true),
		PublishToDetectMillis:   &publish,
		InternalLatencyMillis:   &internal,
		QueueWaitMillis:         &queue,
		RetryAccumulationMillis: &retry,
		DelaySource:             PostDelaySourceMixed,
		InternalDelayCause:      PostInternalDelayCauseQueueWait,
		JobFailureDetected:      false,
	}
	evidence := buildPostLatencyClassificationEvidence(row)
	require.Len(t, evidence, 6)

	byKey := make(map[PostLatencyClassificationEvidenceKey]PostLatencyClassificationEvidence, len(evidence))
	for _, e := range evidence {
		byKey[e.Key] = e
	}

	require.True(t, byKey[PostLatencyClassificationEvidenceKeyAlarmLatency].Selected)
	require.True(t, byKey[PostLatencyClassificationEvidenceKeyPublishToDetect].Selected)
	require.True(t, byKey[PostLatencyClassificationEvidenceKeyInternalLatency].Selected)
	require.True(t, byKey[PostLatencyClassificationEvidenceKeyQueueWait].Selected)
	require.False(t, byKey[PostLatencyClassificationEvidenceKeyRetryAccumulation].Selected)

	jobFailure := byKey[PostLatencyClassificationEvidenceKeyJobFailure]
	require.False(t, jobFailure.Selected)
	require.NotNil(t, jobFailure.Bool)
	require.False(t, *jobFailure.Bool)

	require.NotSame(t, &publish, byKey[PostLatencyClassificationEvidenceKeyPublishToDetect].Millis)
	require.Equal(t, publish, *byKey[PostLatencyClassificationEvidenceKeyPublishToDetect].Millis)
}

func TestBuildPostLatencyClassificationEvidence_InternalLatencyExceededForcesSelection(t *testing.T) {
	t.Parallel()

	row := &PostDeliveryTimeline{
		DelaySource:             PostDelaySourceExternalCollection,
		InternalLatencyExceeded: new(true),
	}
	evidence := buildPostLatencyClassificationEvidence(row)
	byKey := make(map[PostLatencyClassificationEvidenceKey]PostLatencyClassificationEvidence, len(evidence))
	for _, e := range evidence {
		byKey[e.Key] = e
	}
	require.True(t, byKey[PostLatencyClassificationEvidenceKeyInternalLatency].Selected)
}

func TestClassifyPostLatencyReasonCode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input PostLatencyClassificationResult
		want  PostLatencyReasonCode
	}{
		{
			name:  "external collection delay source",
			input: PostLatencyClassificationResult{DelaySource: PostDelaySourceExternalCollection},
			want:  PostLatencyReasonCodeExternalCollection,
		},
		{
			name:  "mixed delay source",
			input: PostLatencyClassificationResult{DelaySource: PostDelaySourceMixed},
			want:  PostLatencyReasonCodeMixed,
		},
		{
			name:  "internal delivery with queue wait cause returns the cause",
			input: PostLatencyClassificationResult{DelaySource: PostDelaySourceInternalDelivery, InternalDelayCause: PostInternalDelayCauseQueueWait},
			want:  PostLatencyReasonCodeQueueWait,
		},
		{
			name:  "internal delivery with no internal cause",
			input: PostLatencyClassificationResult{DelaySource: PostDelaySourceInternalDelivery},
			want:  PostLatencyReasonCodeInternalDelivery,
		},
		{
			name:  "queue wait cause",
			input: PostLatencyClassificationResult{InternalDelayCause: PostInternalDelayCauseQueueWait},
			want:  PostLatencyReasonCodeQueueWait,
		},
		{
			name:  "retry accumulation cause",
			input: PostLatencyClassificationResult{InternalDelayCause: PostInternalDelayCauseRetryAccumulation},
			want:  PostLatencyReasonCodeRetryAccumulation,
		},
		{
			name:  "job failure cause",
			input: PostLatencyClassificationResult{InternalDelayCause: PostInternalDelayCauseJobFailure},
			want:  PostLatencyReasonCodeJobFailure,
		},
		{
			name:  "insufficient evidence status",
			input: PostLatencyClassificationResult{Status: PostLatencyClassificationStatusInsufficientEvidence},
			want:  PostLatencyReasonCodeInsufficientEvidence,
		},
		{
			name:  "within target with no signals",
			input: PostLatencyClassificationResult{Status: PostLatencyClassificationStatusWithinTarget},
			want:  PostLatencyReasonCodeNone,
		},
		{
			name:  "exceeded with no delay source or cause",
			input: PostLatencyClassificationResult{Status: PostLatencyClassificationStatusExceeded},
			want:  PostLatencyReasonCodeNone,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, ClassifyPostLatencyReasonCode(&tc.input))
		})
	}
}

func TestClassifyPostLatencyReasonCode_NilPanics(t *testing.T) {
	t.Parallel()

	require.Panics(t, func() { ClassifyPostLatencyReasonCode(nil) })
}

func TestDeriveMetrics_QueueWaitPrimaryEndToEnd(t *testing.T) {
	t.Parallel()

	detected := timelineBase.Add(time.Minute)
	queueEnqueued := detected.Add(80 * time.Second)
	firstAttemptStarted := queueEnqueued.Add(20 * time.Second)
	firstAttemptFinished := firstAttemptStarted.Add(10 * time.Second)
	alarmSent := firstAttemptFinished

	row := &PostDeliveryTimeline{
		DetectedAt:             &detected,
		QueueEnqueuedAt:        &queueEnqueued,
		FirstAttemptStartedAt:  &firstAttemptStarted,
		FirstAttemptFinishedAt: &firstAttemptFinished,
		AlarmSentAt:            &alarmSent,
		FirstSuccessAt:         &alarmSent,
	}
	DeriveMetrics(row)

	require.NotNil(t, row.QueueWaitMillis)
	require.Equal(t, int64(100_000), *row.QueueWaitMillis)
	require.Nil(t, row.RetryAccumulationMillis)
	require.False(t, row.JobFailureDetected)
	require.Equal(t, PostDelaySourceNone, row.DelaySource)
	require.Equal(t, PostInternalDelayCauseQueueWait, row.InternalDelayCause)
}

func TestDeriveMetrics_JobFailurePrimaryEndToEnd(t *testing.T) {
	t.Parallel()

	detected := timelineBase.Add(time.Minute)
	queueEnqueued := detected.Add(20 * time.Second)
	firstAttemptStarted := queueEnqueued.Add(10 * time.Second)
	firstAttemptFinished := firstAttemptStarted.Add(15 * time.Second)
	nextRetry := firstAttemptFinished.Add(2 * time.Minute)

	row := &PostDeliveryTimeline{
		DetectedAt:             &detected,
		QueueEnqueuedAt:        &queueEnqueued,
		FirstAttemptStartedAt:  &firstAttemptStarted,
		FirstAttemptFinishedAt: &firstAttemptFinished,
		LastAttemptFinishedAt:  &firstAttemptFinished,
		LastFailureAt:          &firstAttemptFinished,
		NextRetryAt:            &nextRetry,
		FailedAttemptCount:     1,
		MaxAttemptOrdinal:      1,
	}
	DeriveMetrics(row)

	require.NotNil(t, row.QueueWaitMillis)
	require.Equal(t, int64(30_000), *row.QueueWaitMillis)
	require.NotNil(t, row.RetryAccumulationMillis)
	require.Equal(t, int64(120_000), *row.RetryAccumulationMillis)
	require.True(t, row.JobFailureDetected)
	require.Equal(t, PostDelaySourceNone, row.DelaySource)
	require.Equal(t, PostInternalDelayCauseJobFailure, row.InternalDelayCause)
}

func TestDeriveMetrics_ComputesAllDurationFields(t *testing.T) {
	t.Parallel()

	published := timelineBase
	detected := published.Add(60 * time.Second)
	queueEnqueued := detected.Add(20 * time.Second)
	firstAttemptStarted := queueEnqueued.Add(10 * time.Second)
	firstAttemptFinished := firstAttemptStarted.Add(15 * time.Second)
	firstSuccess := firstAttemptStarted.Add(95 * time.Second)
	alarmSent := detected.Add(125 * time.Second)

	row := &PostDeliveryTimeline{
		ActualPublishedAt:      &published,
		DetectedAt:             &detected,
		QueueEnqueuedAt:        &queueEnqueued,
		FirstAttemptStartedAt:  &firstAttemptStarted,
		FirstAttemptFinishedAt: &firstAttemptFinished,
		FirstSuccessAt:         &firstSuccess,
		AlarmSentAt:            &alarmSent,
	}
	DeriveMetrics(row)

	require.Equal(t, int64(60_000), *row.PublishToDetectMillis)
	require.Equal(t, int64(20_000), *row.DetectToQueueMillis)
	require.Equal(t, int64(10_000), *row.QueueToFirstAttemptMillis)
	require.Equal(t, int64(15_000), *row.FirstAttemptToFinishMillis)
	require.Equal(t, int64(95_000), *row.FirstAttemptToSuccessMillis)
	require.Equal(t, int64(125_000), *row.InternalLatencyMillis)
	require.Equal(t, int64(30_000), *row.QueueWaitMillis)
}
