package dispatch

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type timelineTestOutboxModel struct {
	ID            int64     `db:"id"`
	Kind          string    `db:"kind"`
	ChannelID     string    `db:"channel_id"`
	ContentID     string    `db:"content_id"`
	Payload       string    `db:"payload"`
	Status        string    `db:"status"`
	AttemptCount  int       `db:"attempt_count"`
	NextAttemptAt time.Time `db:"next_attempt_at"`
	CreatedAt     time.Time
	LockedAt      *time.Time
	SentAt        *time.Time
	Error         string `db:"error"`
}

func (timelineTestOutboxModel) TableName() string {
	return "youtube_notification_outbox"
}

type timelineTestBufferModel struct {
	ID                 int64  `db:"id"`
	DeliveryID         int64  `db:"delivery_id"`
	AttemptOrdinal     int    `db:"attempt_ordinal"`
	OutboxID           int64  `db:"outbox_id"`
	ChannelID          string `db:"channel_id"`
	ContentID          string `db:"content_id"`
	PostID             string `db:"post_id"`
	RoomID             string `db:"room_id"`
	AlarmType          string `db:"alarm_type"`
	ActualPublishedAt  *time.Time
	AlarmSentAt        *time.Time
	AlarmLatencyMillis *int64
	DedupeKey          string `db:"dedupe_key"`
	DeliveryPath       string `db:"delivery_path"`
	DeliveryMode       string `db:"delivery_mode"`
	SendResult         string `db:"send_result"`
	FailureReason      string `db:"failure_reason"`
	AttemptStartedAt   *time.Time
	AttemptFinishedAt  *time.Time
	EventAt            time.Time `db:"event_at"`
	NextAttemptAt      time.Time `db:"next_attempt_at"`
	CreatedAt          time.Time
	LockedAt           *time.Time
	LoggedAt           *time.Time
	Error              string `db:"error"`
}

func (timelineTestBufferModel) TableName() string {
	return "youtube_notification_delivery_telemetry"
}

type timelineTestTrackingModel struct {
	Kind                        string `db:"kind"`
	ContentID                   string `db:"content_id"`
	CanonicalContentID          string
	ChannelID                   string `db:"channel_id"`
	ActualPublishedAt           *time.Time
	DetectedAt                  time.Time `db:"detected_at"`
	AlarmSentAt                 *time.Time
	AlarmLatencyMillis          *int64
	AlarmLatencyExceeded        *bool
	DeliveryStatus              string `db:"delivery_status"`
	LatencyClassificationStatus string
	DelaySource                 string
	InternalDelayCause          string
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
}

func (timelineTestTrackingModel) TableName() string {
	return "youtube_content_alarm_tracking"
}

func TestDeliveryTelemetryRepository_ListPostDeliveryTimelinesSince_BuildsLatencyTimelineMetrics(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newDeliveryPool(t)

	publishedAt := time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC)
	detectedAt := publishedAt.Add(1 * time.Minute)
	queueEnqueuedAt := detectedAt.Add(20 * time.Second)
	firstAttemptStartedAt := queueEnqueuedAt.Add(10 * time.Second)
	firstAttemptFinishedAt := firstAttemptStartedAt.Add(15 * time.Second)
	retryReadyAt := firstAttemptFinishedAt.Add(1 * time.Minute)
	secondAttemptStartedAt := retryReadyAt.Add(5 * time.Second)
	secondAttemptFinishedAt := secondAttemptStartedAt.Add(15 * time.Second)
	alarmLatencyMillis := int64(secondAttemptFinishedAt.Sub(publishedAt) / time.Millisecond)
	alarmLatencyExceeded := true
	windowStart := publishedAt.Add(-30 * time.Minute)

	outboxRow := timelineTestOutboxModel{
		Kind:          string(domain.OutboxKindCommunityPost),
		ChannelID:     "UC_timeline",
		ContentID:     "post-timeline",
		Payload:       `{"post_id":"post-timeline","content_text":"timeline"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  1,
		NextAttemptAt: retryReadyAt,
		CreatedAt:     queueEnqueuedAt,
	}
	require.NoError(t, insertDeliveryTestRows(db, &outboxRow).Error)

	require.NoError(t, insertDeliveryTestRows(db, &timelineTestTrackingModel{
		Kind:                 string(domain.OutboxKindCommunityPost),
		ContentID:            outboxRow.ContentID,
		ChannelID:            outboxRow.ChannelID,
		ActualPublishedAt:    &publishedAt,
		DetectedAt:           detectedAt,
		AlarmSentAt:          &secondAttemptFinishedAt,
		AlarmLatencyMillis:   &alarmLatencyMillis,
		AlarmLatencyExceeded: &alarmLatencyExceeded,
		CreatedAt:            queueEnqueuedAt,
		UpdatedAt:            secondAttemptFinishedAt,
	}).Error)

	require.NoError(t, insertDeliveryTestRows(db, []timelineTestBufferModel{
		{
			DeliveryID:        101,
			AttemptOrdinal:    1,
			OutboxID:          outboxRow.ID,
			ChannelID:         outboxRow.ChannelID,
			ContentID:         outboxRow.ContentID,
			PostID:            outboxRow.ContentID,
			RoomID:            "room-a",
			AlarmType:         string(domain.AlarmTypeCommunity),
			DedupeKey:         "youtube-notification:COMMUNITY_POST:post-timeline",
			DeliveryPath:      "youtube_outbox_dispatcher",
			DeliveryMode:      "per_room",
			SendResult:        "failure",
			FailureReason:     "send message",
			AttemptStartedAt:  &firstAttemptStartedAt,
			AttemptFinishedAt: &firstAttemptFinishedAt,
			EventAt:           firstAttemptFinishedAt,
			NextAttemptAt:     retryReadyAt,
		},
		{
			DeliveryID:        101,
			AttemptOrdinal:    2,
			OutboxID:          outboxRow.ID,
			ChannelID:         outboxRow.ChannelID,
			ContentID:         outboxRow.ContentID,
			PostID:            outboxRow.ContentID,
			RoomID:            "room-a",
			AlarmType:         string(domain.AlarmTypeCommunity),
			DedupeKey:         "youtube-notification:COMMUNITY_POST:post-timeline",
			DeliveryPath:      "youtube_outbox_dispatcher",
			DeliveryMode:      "per_room",
			SendResult:        "success",
			AttemptStartedAt:  &secondAttemptStartedAt,
			AttemptFinishedAt: &secondAttemptFinishedAt,
			EventAt:           secondAttemptFinishedAt,
			NextAttemptAt:     secondAttemptFinishedAt,
		},
	}).Error)

	repository := NewDeliveryTelemetryRepository(db)
	rows, err := repository.ListPostDeliveryTimelinesSince(ctx, windowStart)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	row := rows[0]
	require.Equal(t, domain.OutboxKindCommunityPost, row.OutboxKind)
	require.Equal(t, domain.AlarmTypeCommunity, row.AlarmType)
	require.Equal(t, outboxRow.ContentID, row.ContentID)
	require.Equal(t, outboxRow.ContentID, row.PostID)
	require.NotNil(t, row.QueueEnqueuedAt)
	require.Equal(t, queueEnqueuedAt, row.QueueEnqueuedAt.UTC())
	require.NotNil(t, row.FirstAttemptStartedAt)
	require.Equal(t, firstAttemptStartedAt, row.FirstAttemptStartedAt.UTC())
	require.NotNil(t, row.LastAttemptStartedAt)
	require.Equal(t, secondAttemptStartedAt, row.LastAttemptStartedAt.UTC())
	require.NotNil(t, row.FirstAttemptFinishedAt)
	require.Equal(t, firstAttemptFinishedAt, row.FirstAttemptFinishedAt.UTC())
	require.NotNil(t, row.LastAttemptFinishedAt)
	require.Equal(t, secondAttemptFinishedAt, row.LastAttemptFinishedAt.UTC())
	require.NotNil(t, row.FirstSuccessAt)
	require.Equal(t, secondAttemptFinishedAt, row.FirstSuccessAt.UTC())
	require.NotNil(t, row.LastFailureAt)
	require.Equal(t, firstAttemptFinishedAt, row.LastFailureAt.UTC())
	require.NotNil(t, row.NextRetryAt)
	require.Equal(t, retryReadyAt, row.NextRetryAt.UTC())
	require.Equal(t, int64(1), row.SuccessSendCount)
	require.Equal(t, int64(1), row.FailedAttemptCount)
	require.Equal(t, int64(2), row.MaxAttemptOrdinal)
	require.Equal(t, int64(1), row.RetryAttemptCount)
	require.NotNil(t, row.PublishToDetectMillis)
	require.Equal(t, int64(60*time.Second/time.Millisecond), *row.PublishToDetectMillis)
	require.NotNil(t, row.DetectToQueueMillis)
	require.Equal(t, int64(20*time.Second/time.Millisecond), *row.DetectToQueueMillis)
	require.NotNil(t, row.QueueToFirstAttemptMillis)
	require.Equal(t, int64(10*time.Second/time.Millisecond), *row.QueueToFirstAttemptMillis)
	require.NotNil(t, row.FirstAttemptToFinishMillis)
	require.Equal(t, int64(15*time.Second/time.Millisecond), *row.FirstAttemptToFinishMillis)
	require.NotNil(t, row.FirstAttemptToSuccessMillis)
	require.Equal(t, int64(95*time.Second/time.Millisecond), *row.FirstAttemptToSuccessMillis)
	require.NotNil(t, row.InternalLatencyMillis)
	require.Equal(t, int64(125*time.Second/time.Millisecond), *row.InternalLatencyMillis)
	require.NotNil(t, row.InternalLatencyExceeded)
	require.True(t, *row.InternalLatencyExceeded)
	require.Equal(t, PostDelaySourceInternalDelivery, row.DelaySource)
	require.NotNil(t, row.AlarmLatencyMillis)
	require.Equal(t, alarmLatencyMillis, *row.AlarmLatencyMillis)
	require.NotNil(t, row.AlarmLatencyExceeded)
	require.True(t, *row.AlarmLatencyExceeded)
	require.NotNil(t, row.QueueWaitMillis)
	require.Equal(t, int64(30*time.Second/time.Millisecond), *row.QueueWaitMillis)
	require.NotNil(t, row.RetryAccumulationMillis)
	require.Equal(t, int64(80*time.Second/time.Millisecond), *row.RetryAccumulationMillis)
	require.False(t, row.JobFailureDetected)
	require.Equal(t, PostInternalDelayCauseRetryAccumulation, row.InternalDelayCause)
	require.Equal(t, PostLatencyClassificationStatusExceeded, row.LatencyClassification.Status)
	require.Equal(t, postLatencyExceededThresholdMillis, row.LatencyClassification.ThresholdMillis)
	require.Equal(t, PostDelaySourceInternalDelivery, row.LatencyClassification.DelaySource)
	require.Equal(t, PostInternalDelayCauseRetryAccumulation, row.LatencyClassification.InternalDelayCause)
	evidenceByKey := indexPostLatencyClassificationEvidence(row.LatencyClassification.Evidence)
	require.NotNil(t, evidenceByKey[PostLatencyClassificationEvidenceKeyAlarmLatency].Millis)
	require.Equal(t, alarmLatencyMillis, *evidenceByKey[PostLatencyClassificationEvidenceKeyAlarmLatency].Millis)
	require.True(t, evidenceByKey[PostLatencyClassificationEvidenceKeyAlarmLatency].Selected)
	require.NotNil(t, evidenceByKey[PostLatencyClassificationEvidenceKeyInternalLatency].Millis)
	require.Equal(t, int64(125*time.Second/time.Millisecond), *evidenceByKey[PostLatencyClassificationEvidenceKeyInternalLatency].Millis)
	require.True(t, evidenceByKey[PostLatencyClassificationEvidenceKeyInternalLatency].Selected)
	require.NotNil(t, evidenceByKey[PostLatencyClassificationEvidenceKeyRetryAccumulation].Millis)
	require.Equal(t, int64(80*time.Second/time.Millisecond), *evidenceByKey[PostLatencyClassificationEvidenceKeyRetryAccumulation].Millis)
	require.True(t, evidenceByKey[PostLatencyClassificationEvidenceKeyRetryAccumulation].Selected)
}

func TestDeliveryTelemetryRepository_PersistPostLatencyClassificationsByIdentities_StoresComputedValues(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newDeliveryPool(t)

	publishedAt := time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC)
	detectedAt := publishedAt.Add(30 * time.Second)
	queueEnqueuedAt := detectedAt.Add(30 * time.Second)
	firstAttemptStartedAt := queueEnqueuedAt.Add(100 * time.Second)
	firstAttemptFinishedAt := firstAttemptStartedAt.Add(10 * time.Second)
	alarmLatencyMillis := int64(firstAttemptFinishedAt.Sub(publishedAt) / time.Millisecond)
	alarmLatencyExceeded := true

	outboxRow := timelineTestOutboxModel{
		Kind:          string(domain.OutboxKindCommunityPost),
		ChannelID:     "UC_persisted_latency",
		ContentID:     "post-persisted-latency",
		Payload:       `{"post_id":"post-persisted-latency","content_text":"persisted"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: firstAttemptFinishedAt,
		CreatedAt:     queueEnqueuedAt,
	}
	require.NoError(t, insertDeliveryTestRows(db, &outboxRow).Error)

	require.NoError(t, insertDeliveryTestRows(db, &timelineTestTrackingModel{
		Kind:                 string(domain.OutboxKindCommunityPost),
		ContentID:            outboxRow.ContentID,
		ChannelID:            outboxRow.ChannelID,
		ActualPublishedAt:    &publishedAt,
		DetectedAt:           detectedAt,
		AlarmSentAt:          &firstAttemptFinishedAt,
		AlarmLatencyMillis:   &alarmLatencyMillis,
		AlarmLatencyExceeded: &alarmLatencyExceeded,
		CreatedAt:            queueEnqueuedAt,
		UpdatedAt:            firstAttemptFinishedAt,
	}).Error)

	require.NoError(t, insertDeliveryTestRows(db, &timelineTestBufferModel{
		DeliveryID:        9001,
		AttemptOrdinal:    1,
		OutboxID:          outboxRow.ID,
		ChannelID:         outboxRow.ChannelID,
		ContentID:         outboxRow.ContentID,
		PostID:            outboxRow.ContentID,
		RoomID:            "room-persisted",
		AlarmType:         string(domain.AlarmTypeCommunity),
		DedupeKey:         "youtube-notification:COMMUNITY_POST:post-persisted-latency",
		DeliveryPath:      "youtube_outbox_dispatcher",
		DeliveryMode:      "per_room",
		SendResult:        "success",
		AttemptStartedAt:  &firstAttemptStartedAt,
		AttemptFinishedAt: &firstAttemptFinishedAt,
		EventAt:           firstAttemptFinishedAt,
		NextAttemptAt:     firstAttemptFinishedAt,
	}).Error)

	repository := NewDeliveryTelemetryRepository(db)
	require.NoError(t, repository.PersistPostLatencyClassificationsByIdentities(ctx, []PostTrackingIdentity{{
		Kind:      domain.OutboxKindCommunityPost,
		ContentID: outboxRow.ContentID,
	}}))

	var stored timelineTestTrackingModel
	require.NoError(t, firstDeliveryTestRowWhere(db, &stored, "kind = ? AND content_id = ?", string(domain.OutboxKindCommunityPost), outboxRow.ContentID).Error)
	require.Equal(t, string(PostLatencyClassificationStatusExceeded), stored.LatencyClassificationStatus)
	require.Equal(t, string(PostDelaySourceInternalDelivery), stored.DelaySource)
	require.Equal(t, string(PostInternalDelayCauseQueueWait), stored.InternalDelayCause)
}

func TestDerivePostDeliveryTimelineMetrics_ClassifiesExternalCollectionDelaySource(t *testing.T) {
	t.Parallel()

	publishedAt := time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC)
	detectedAt := publishedAt.Add(100 * time.Second)
	alarmSentAt := detectedAt.Add(30 * time.Second)
	alarmLatencyMillis := int64(alarmSentAt.Sub(publishedAt) / time.Millisecond)
	alarmLatencyExceeded := true

	row := PostDeliveryTimeline{
		ActualPublishedAt:    &publishedAt,
		DetectedAt:           &detectedAt,
		AlarmSentAt:          &alarmSentAt,
		AlarmLatencyMillis:   &alarmLatencyMillis,
		AlarmLatencyExceeded: new(alarmLatencyExceeded),
	}

	derivePostDeliveryTimelineMetrics(&row)

	require.NotNil(t, row.PublishToDetectMillis)
	require.Equal(t, int64(100*time.Second/time.Millisecond), *row.PublishToDetectMillis)
	require.NotNil(t, row.InternalLatencyMillis)
	require.Equal(t, int64(30*time.Second/time.Millisecond), *row.InternalLatencyMillis)
	require.NotNil(t, row.InternalLatencyExceeded)
	require.False(t, *row.InternalLatencyExceeded)
	require.Equal(t, PostDelaySourceExternalCollection, row.DelaySource)
	require.Equal(t, PostLatencyClassificationStatusExceeded, row.LatencyClassification.Status)
	require.Equal(t, PostDelaySourceExternalCollection, row.LatencyClassification.DelaySource)
	require.Equal(t, PostInternalDelayCauseNone, row.LatencyClassification.InternalDelayCause)
	evidenceByKey := indexPostLatencyClassificationEvidence(row.LatencyClassification.Evidence)
	require.True(t, evidenceByKey[PostLatencyClassificationEvidenceKeyPublishToDetect].Selected)
	require.NotNil(t, evidenceByKey[PostLatencyClassificationEvidenceKeyPublishToDetect].Millis)
	require.Equal(t, int64(100*time.Second/time.Millisecond), *evidenceByKey[PostLatencyClassificationEvidenceKeyPublishToDetect].Millis)
}

func TestDerivePostDeliveryTimelineMetrics_ClassifiesMixedDelaySource(t *testing.T) {
	t.Parallel()

	publishedAt := time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC)
	detectedAt := publishedAt.Add(70 * time.Second)
	alarmSentAt := detectedAt.Add(70 * time.Second)
	alarmLatencyMillis := int64(alarmSentAt.Sub(publishedAt) / time.Millisecond)
	alarmLatencyExceeded := true

	row := PostDeliveryTimeline{
		ActualPublishedAt:    &publishedAt,
		DetectedAt:           &detectedAt,
		AlarmSentAt:          &alarmSentAt,
		AlarmLatencyMillis:   &alarmLatencyMillis,
		AlarmLatencyExceeded: new(alarmLatencyExceeded),
	}

	derivePostDeliveryTimelineMetrics(&row)

	require.NotNil(t, row.PublishToDetectMillis)
	require.Equal(t, int64(70*time.Second/time.Millisecond), *row.PublishToDetectMillis)
	require.NotNil(t, row.InternalLatencyMillis)
	require.Equal(t, int64(70*time.Second/time.Millisecond), *row.InternalLatencyMillis)
	require.NotNil(t, row.InternalLatencyExceeded)
	require.False(t, *row.InternalLatencyExceeded)
	require.Equal(t, PostDelaySourceMixed, row.DelaySource)
}

func TestDerivePostDeliveryTimelineMetrics_ClassifiesQueueWaitPrimary(t *testing.T) {
	t.Parallel()

	detectedAt := time.Date(2026, 4, 10, 10, 1, 0, 0, time.UTC)
	queueEnqueuedAt := detectedAt.Add(80 * time.Second)
	firstAttemptStartedAt := queueEnqueuedAt.Add(20 * time.Second)
	firstAttemptFinishedAt := firstAttemptStartedAt.Add(10 * time.Second)
	alarmSentAt := firstAttemptFinishedAt

	row := PostDeliveryTimeline{
		DetectedAt:             &detectedAt,
		QueueEnqueuedAt:        &queueEnqueuedAt,
		FirstAttemptStartedAt:  &firstAttemptStartedAt,
		FirstAttemptFinishedAt: &firstAttemptFinishedAt,
		AlarmSentAt:            &alarmSentAt,
		FirstSuccessAt:         &alarmSentAt,
	}

	derivePostDeliveryTimelineMetrics(&row)

	require.NotNil(t, row.QueueWaitMillis)
	require.Equal(t, int64(100*time.Second/time.Millisecond), *row.QueueWaitMillis)
	require.Nil(t, row.RetryAccumulationMillis)
	require.False(t, row.JobFailureDetected)
	require.Equal(t, PostDelaySourceNone, row.DelaySource)
	require.Equal(t, PostInternalDelayCauseQueueWait, row.InternalDelayCause)
}

func TestDerivePostDeliveryTimelineMetrics_ClassifiesJobFailurePrimary(t *testing.T) {
	t.Parallel()

	detectedAt := time.Date(2026, 4, 10, 10, 1, 0, 0, time.UTC)
	queueEnqueuedAt := detectedAt.Add(20 * time.Second)
	firstAttemptStartedAt := queueEnqueuedAt.Add(10 * time.Second)
	firstAttemptFinishedAt := firstAttemptStartedAt.Add(15 * time.Second)
	nextRetryAt := firstAttemptFinishedAt.Add(2 * time.Minute)

	row := PostDeliveryTimeline{
		DetectedAt:             &detectedAt,
		QueueEnqueuedAt:        &queueEnqueuedAt,
		FirstAttemptStartedAt:  &firstAttemptStartedAt,
		FirstAttemptFinishedAt: &firstAttemptFinishedAt,
		LastAttemptFinishedAt:  &firstAttemptFinishedAt,
		LastFailureAt:          &firstAttemptFinishedAt,
		NextRetryAt:            &nextRetryAt,
		FailedAttemptCount:     1,
		MaxAttemptOrdinal:      1,
	}

	derivePostDeliveryTimelineMetrics(&row)

	require.NotNil(t, row.QueueWaitMillis)
	require.Equal(t, int64(30*time.Second/time.Millisecond), *row.QueueWaitMillis)
	require.NotNil(t, row.RetryAccumulationMillis)
	require.Equal(t, int64(120*time.Second/time.Millisecond), *row.RetryAccumulationMillis)
	require.True(t, row.JobFailureDetected)
	require.Equal(t, PostDelaySourceNone, row.DelaySource)
	require.Equal(t, PostInternalDelayCauseJobFailure, row.InternalDelayCause)
}

func indexPostLatencyClassificationEvidence(items []PostLatencyClassificationEvidence) map[PostLatencyClassificationEvidenceKey]PostLatencyClassificationEvidence {
	indexed := make(map[PostLatencyClassificationEvidenceKey]PostLatencyClassificationEvidence, len(items))
	for i := range items {
		indexed[items[i].Key] = items[i]
	}
	return indexed
}

func TestDeliveryTelemetryRepository_ListPostDeliveryTimelinesWithinPublishedWindow_AppliesUpperBound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newDeliveryPool(t)

	publishedAt := time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC)
	detectedAt := publishedAt.Add(2 * time.Minute)
	eventAt := detectedAt.Add(1 * time.Minute)

	outboxRow := timelineTestOutboxModel{
		Kind:          string(domain.OutboxKindCommunityPost),
		ChannelID:     "UC_window",
		ContentID:     "post-window",
		Payload:       `{"post_id":"post-window"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: eventAt,
		CreatedAt:     detectedAt,
	}
	require.NoError(t, insertDeliveryTestRows(db, &outboxRow).Error)
	require.NoError(t, insertDeliveryTestRows(db, &timelineTestTrackingModel{
		Kind:              string(domain.OutboxKindCommunityPost),
		ContentID:         outboxRow.ContentID,
		ChannelID:         outboxRow.ChannelID,
		ActualPublishedAt: &publishedAt,
		DetectedAt:        detectedAt,
		CreatedAt:         detectedAt,
		UpdatedAt:         eventAt,
	}).Error)
	require.NoError(t, insertDeliveryTestRows(db, &timelineTestBufferModel{
		DeliveryID:     7001,
		AttemptOrdinal: 1,
		OutboxID:       outboxRow.ID,
		ChannelID:      outboxRow.ChannelID,
		ContentID:      outboxRow.ContentID,
		PostID:         outboxRow.ContentID,
		RoomID:         "room-window",
		AlarmType:      string(domain.AlarmTypeCommunity),
		DedupeKey:      "youtube-notification:COMMUNITY_POST:post-window",
		DeliveryPath:   "youtube_outbox_dispatcher",
		DeliveryMode:   "grouped",
		SendResult:     "success",
		EventAt:        eventAt,
		NextAttemptAt:  eventAt,
	}).Error)

	repository := NewDeliveryTelemetryRepository(db)
	insideRows, err := repository.ListPostDeliveryTimelinesWithinPublishedWindow(ctx, publishedAt.Add(-time.Minute), publishedAt.Add(time.Minute))
	require.NoError(t, err)
	require.Len(t, insideRows, 1)
	require.Equal(t, outboxRow.ContentID, insideRows[0].ContentID)

	outsideRows, err := repository.ListPostDeliveryTimelinesWithinPublishedWindow(ctx, publishedAt.Add(10*time.Minute), publishedAt.Add(20*time.Minute))
	require.NoError(t, err)
	require.Empty(t, outsideRows)
}

func TestDeliveryTelemetryRepository_ListPostDeliveryTimelinesWithinObservationWindow_ExcludesLateDetections(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newDeliveryPool(t)
	windowStart := time.Date(2026, 4, 10, 1, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(24 * time.Hour)

	timelyPublishedAt := windowStart.Add(2 * time.Minute)
	timelyDetectedAt := timelyPublishedAt.Add(30 * time.Second)
	timelyEventAt := timelyDetectedAt.Add(time.Minute)
	latePublishedAt := windowStart.Add(5 * time.Minute)
	lateDetectedAt := windowEnd.Add(time.Minute)
	lateEventAt := lateDetectedAt.Add(time.Minute)

	timelyOutbox := timelineTestOutboxModel{
		ID:            7101,
		Kind:          string(domain.OutboxKindCommunityPost),
		ChannelID:     "UC-timely",
		ContentID:     "post-timely",
		Payload:       `{"post_id":"post-timely"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: timelyEventAt,
		CreatedAt:     timelyDetectedAt,
	}
	lateOutbox := timelineTestOutboxModel{
		ID:            7102,
		Kind:          string(domain.OutboxKindNewShort),
		ChannelID:     "UC-late",
		ContentID:     "short-late",
		Payload:       `{"post_id":"short-late"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: lateEventAt,
		CreatedAt:     lateDetectedAt,
	}
	require.NoError(t, insertDeliveryTestRows(db, []timelineTestOutboxModel{timelyOutbox, lateOutbox}).Error)
	require.NoError(t, insertDeliveryTestRows(db, []timelineTestTrackingModel{
		{
			Kind:              string(domain.OutboxKindCommunityPost),
			ContentID:         timelyOutbox.ContentID,
			ChannelID:         timelyOutbox.ChannelID,
			ActualPublishedAt: &timelyPublishedAt,
			DetectedAt:        timelyDetectedAt,
			CreatedAt:         timelyDetectedAt,
			UpdatedAt:         timelyEventAt,
		},
		{
			Kind:              string(domain.OutboxKindNewShort),
			ContentID:         lateOutbox.ContentID,
			ChannelID:         lateOutbox.ChannelID,
			ActualPublishedAt: &latePublishedAt,
			DetectedAt:        lateDetectedAt,
			CreatedAt:         lateDetectedAt,
			UpdatedAt:         lateEventAt,
		},
	}).Error)
	require.NoError(t, insertDeliveryTestRows(db, []timelineTestBufferModel{
		{
			DeliveryID:     7101,
			AttemptOrdinal: 1,
			OutboxID:       timelyOutbox.ID,
			ChannelID:      timelyOutbox.ChannelID,
			ContentID:      timelyOutbox.ContentID,
			PostID:         timelyOutbox.ContentID,
			RoomID:         "room-timely",
			AlarmType:      string(domain.AlarmTypeCommunity),
			DedupeKey:      "youtube-notification:COMMUNITY_POST:post-timely",
			DeliveryPath:   "youtube_outbox_dispatcher",
			DeliveryMode:   "grouped",
			SendResult:     "success",
			EventAt:        timelyEventAt,
			NextAttemptAt:  timelyEventAt,
		},
		{
			DeliveryID:     7102,
			AttemptOrdinal: 1,
			OutboxID:       lateOutbox.ID,
			ChannelID:      lateOutbox.ChannelID,
			ContentID:      lateOutbox.ContentID,
			PostID:         lateOutbox.ContentID,
			RoomID:         "room-late",
			AlarmType:      string(domain.AlarmTypeShorts),
			DedupeKey:      "youtube-notification:NEW_SHORT:short-late",
			DeliveryPath:   "youtube_outbox_dispatcher",
			DeliveryMode:   "grouped",
			SendResult:     "success",
			EventAt:        lateEventAt,
			NextAttemptAt:  lateEventAt,
		},
	}).Error)

	repository := NewDeliveryTelemetryRepository(db)
	rows, err := repository.ListPostDeliveryTimelinesWithinObservationWindow(ctx, windowStart, windowEnd, windowEnd)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, timelyOutbox.ContentID, rows[0].ContentID)
}

func TestDeliveryTelemetryRepository_ListPostDeliveryTimelinesByFinalizedObservationWindow_UsesFrozenBaseline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newDeliveryPool(t)

	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	finalizedAt := time.Date(2026, 4, 11, 1, 0, 0, 0, time.UTC)
	timelyPublishedAt := time.Date(2026, 4, 10, 1, 5, 0, 0, time.UTC)
	timelyDetectedAt := timelyPublishedAt.Add(20 * time.Second)
	timelyQueueAt := timelyDetectedAt.Add(10 * time.Second)
	timelyAttemptStartedAt := timelyQueueAt.Add(5 * time.Second)
	timelyAttemptFinishedAt := timelyAttemptStartedAt.Add(15 * time.Second)
	latePublishedAt := time.Date(2026, 4, 11, 2, 0, 0, 0, time.UTC)
	lateDetectedAt := latePublishedAt.Add(30 * time.Second)
	lateAttemptFinishedAt := lateDetectedAt.Add(time.Minute)

	timelyOutbox := timelineTestOutboxModel{
		ID:            9301,
		Kind:          string(domain.OutboxKindCommunityPost),
		ChannelID:     "UC_COMMUNITY",
		ContentID:     "community:post-timely",
		Payload:       `{"post_id":"community:post-timely"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: timelyAttemptFinishedAt,
		CreatedAt:     timelyQueueAt,
	}
	lateOutbox := timelineTestOutboxModel{
		ID:            9302,
		Kind:          string(domain.OutboxKindNewShort),
		ChannelID:     "UC_LATE",
		ContentID:     "short:late-after-freeze",
		Payload:       `{"post_id":"short:late-after-freeze"}`,
		Status:        string(domain.OutboxStatusSent),
		AttemptCount:  0,
		NextAttemptAt: lateAttemptFinishedAt,
		CreatedAt:     lateDetectedAt,
	}
	require.NoError(t, insertDeliveryTestRows(db, []timelineTestOutboxModel{timelyOutbox, lateOutbox}).Error)

	require.NoError(t, insertDeliveryTestRows(db, []timelineTestTrackingModel{
		{
			Kind:               string(domain.OutboxKindCommunityPost),
			ContentID:          timelyOutbox.ContentID,
			CanonicalContentID: timelyOutbox.ContentID,
			ChannelID:          timelyOutbox.ChannelID,
			ActualPublishedAt:  &timelyPublishedAt,
			DetectedAt:         timelyDetectedAt,
			CreatedAt:          timelyDetectedAt,
			UpdatedAt:          timelyAttemptFinishedAt,
		},
		{
			Kind:               string(domain.OutboxKindNewShort),
			ContentID:          lateOutbox.ContentID,
			CanonicalContentID: lateOutbox.ContentID,
			ChannelID:          lateOutbox.ChannelID,
			ActualPublishedAt:  &latePublishedAt,
			DetectedAt:         lateDetectedAt,
			CreatedAt:          lateDetectedAt,
			UpdatedAt:          lateAttemptFinishedAt,
		},
	}).Error)
	require.NoError(t, insertDeliveryTestRows(db, []deliveryTelemetryTestObservationBaselineModel{{
		RuntimeName:       "youtube-producer",
		BigBangCutoverAt:  cutoverAt,
		Kind:              string(domain.OutboxKindCommunityPost),
		PostID:            timelyOutbox.ContentID,
		ChannelID:         timelyOutbox.ChannelID,
		ActualPublishedAt: &timelyPublishedAt,
		DetectedAt:        timelyDetectedAt,
		FinalizedAt:       finalizedAt,
	}}).Error)

	require.NoError(t, insertDeliveryTestRows(db, []timelineTestBufferModel{
		{
			DeliveryID:        9301,
			AttemptOrdinal:    1,
			OutboxID:          timelyOutbox.ID,
			ChannelID:         timelyOutbox.ChannelID,
			ContentID:         timelyOutbox.ContentID,
			PostID:            timelyOutbox.ContentID,
			RoomID:            "room-timely",
			AlarmType:         string(domain.AlarmTypeCommunity),
			DedupeKey:         "youtube-notification:COMMUNITY_POST:community:post-timely",
			DeliveryPath:      "youtube_outbox_dispatcher",
			DeliveryMode:      "grouped",
			SendResult:        "success",
			AttemptStartedAt:  &timelyAttemptStartedAt,
			AttemptFinishedAt: &timelyAttemptFinishedAt,
			EventAt:           timelyAttemptFinishedAt,
			NextAttemptAt:     timelyAttemptFinishedAt,
		},
		{
			DeliveryID:        9302,
			AttemptOrdinal:    1,
			OutboxID:          lateOutbox.ID,
			ChannelID:         lateOutbox.ChannelID,
			ContentID:         lateOutbox.ContentID,
			PostID:            lateOutbox.ContentID,
			RoomID:            "room-late",
			AlarmType:         string(domain.AlarmTypeShorts),
			DedupeKey:         "youtube-notification:NEW_SHORT:short:late-after-freeze",
			DeliveryPath:      "youtube_outbox_dispatcher",
			DeliveryMode:      "grouped",
			SendResult:        "success",
			AttemptStartedAt:  &lateDetectedAt,
			AttemptFinishedAt: &lateAttemptFinishedAt,
			EventAt:           lateAttemptFinishedAt,
			NextAttemptAt:     lateAttemptFinishedAt,
		},
	}).Error)

	repository := NewDeliveryTelemetryRepository(db)
	rows, err := repository.ListPostDeliveryTimelinesByFinalizedObservationWindow(ctx, "youtube-producer", cutoverAt)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, timelyOutbox.ContentID, rows[0].ContentID)
	require.Equal(t, timelyOutbox.ChannelID, rows[0].ChannelID)
	require.NotNil(t, rows[0].QueueEnqueuedAt)
	require.Equal(t, timelyQueueAt, rows[0].QueueEnqueuedAt.UTC())
}
