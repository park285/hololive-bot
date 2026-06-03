package dispatch

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/store"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
	"github.com/park285/shared-go/pkg/json"
)

func TestEnqueueDeliveries_NoSubscribersMarksShortSentAtWithCanonicalTimestamp(t *testing.T) {
	fixedNow := time.Date(2026, 4, 10, 10, 11, 12, 123000000, time.FixedZone("KST", 9*60*60))
	withFixedSentAtNow(t, fixedNow)

	ctx := context.Background()
	db := newDeliveryTestDB(t)

	nextAttemptAt := time.Date(2026, 4, 10, 1, 0, 0, 0, time.UTC)
	item := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewShort,
		ChannelID:     "UC_short_no_subscribers",
		ContentID:     "short-no-subscribers",
		Payload:       `{"video_id":"short-no-subscribers","title":"short-title"}`,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: nextAttemptAt,
		LockedAt:      &nextAttemptAt,
	}
	require.NoError(t, db.Create(&item).Error)

	dispatcher := NewDispatcher(db.Pool, nil, &testSender{failRoom: map[string]bool{}}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
	})

	dispatcher.claim.enqueueDeliveries(ctx, []domain.YouTubeNotificationOutbox{item}, map[string]channelAlarmRoomTargets{
		item.ChannelID: {
			domain.AlarmTypeShorts: {},
		},
	})

	var updated deliveryTestOutboxModel
	require.NoError(t, db.First(&updated, item.ID).Error)
	require.Equal(t, string(domain.OutboxStatusSent), updated.Status)
	require.NotNil(t, updated.SentAt)
	require.Equal(t, yttimestamp.Canonical.Location, updated.SentAt.UTC().Location())
	require.Equal(t, "2026-04-10T01:11:12.123Z", updated.SentAt.UTC().Format(yttimestamp.Canonical.Layout))
}

func TestDeliveryRepositoryStoresShortPublishedAtAndSentAtWithCanonicalTimestamp(t *testing.T) {
	fixedNow := time.Date(2026, 4, 10, 10, 11, 12, 123000000, time.FixedZone("KST", 9*60*60))
	withFixedSentAtNow(t, fixedNow)

	ctx := context.Background()
	db := newDeliveryTestDB(t)

	nextAttemptAt := time.Date(2026, 4, 10, 1, 0, 0, 0, time.UTC)
	item := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewShort,
		ChannelID:     "UC_short_delivery",
		ContentID:     "short-delivery",
		Payload:       `{"video_id":"short-delivery","title":"short-title","published_at":"2026-04-10T01:10:00Z"}`,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: nextAttemptAt,
	}
	require.NoError(t, db.Create(&item).Error)
	require.NoError(t, db.Create(&deliveryTestTrackingModel{
		Kind:           string(item.Kind),
		ContentID:      item.ContentID,
		ChannelID:      item.ChannelID,
		DetectedAt:     nextAttemptAt,
		DeliveryStatus: string(domain.YouTubeContentAlarmDeliveryStatusPending),
	}).Error)

	delivery := domain.YouTubeNotificationDelivery{
		OutboxID:      item.ID,
		RoomID:        "room-short",
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: nextAttemptAt,
	}
	require.NoError(t, db.Create(&delivery).Error)

	repository := store.NewDeliveryRepository(db.Pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, repository.MarkSentBatch(ctx, []int64{delivery.ID}))
	require.NoError(t, repository.UpdateOutboxAggregateStatuses(ctx, []int64{item.ID}))

	var updatedDelivery deliveryTestDeliveryModel
	require.NoError(t, db.First(&updatedDelivery, delivery.ID).Error)
	require.Equal(t, string(domain.OutboxStatusSent), updatedDelivery.Status)
	require.NotNil(t, updatedDelivery.SentAt)
	require.Equal(t, yttimestamp.Canonical.Location, updatedDelivery.SentAt.UTC().Location())
	require.Equal(t, "2026-04-10T01:11:12.123Z", updatedDelivery.SentAt.UTC().Format(yttimestamp.Canonical.Layout))

	var updatedOutbox deliveryTestOutboxModel
	require.NoError(t, db.First(&updatedOutbox, item.ID).Error)
	require.Equal(t, string(domain.OutboxStatusSent), updatedOutbox.Status)
	require.NotNil(t, updatedOutbox.SentAt)
	require.Equal(t, yttimestamp.Canonical.Location, updatedOutbox.SentAt.UTC().Location())
	require.Equal(t, "2026-04-10T01:11:12.123Z", updatedOutbox.SentAt.UTC().Format(yttimestamp.Canonical.Layout))

	var payload struct {
		PublishedAt *time.Time `json:"published_at,omitempty"`
	}
	require.NoError(t, json.Unmarshal([]byte(updatedOutbox.Payload), &payload))
	require.NotNil(t, payload.PublishedAt)
	require.Equal(t, yttimestamp.Canonical.Location, payload.PublishedAt.UTC().Location())
	require.Equal(t, "2026-04-10T01:10:00Z", payload.PublishedAt.UTC().Format(yttimestamp.Canonical.Layout))
}

func TestDeliveryRepositoryMarkSentBatchRecordsCommunityAlarmSentAtWithCanonicalTimestamp(t *testing.T) {
	fixedNow := time.Date(2026, 4, 10, 10, 11, 12, 123000000, time.FixedZone("KST", 9*60*60))
	withFixedSentAtNow(t, fixedNow)

	ctx := context.Background()
	db := newDeliveryTestDB(t)

	detectedAt := time.Date(2026, 4, 10, 1, 10, 0, 0, time.UTC)
	item := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindCommunityPost,
		ChannelID:     "UC_community_tracking",
		ContentID:     "post-tracking",
		Payload:       `{"post_id":"post-tracking","content_text":"community-title","published_at":"2026-04-10T01:09:00Z"}`,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: detectedAt,
	}
	require.NoError(t, db.Create(&item).Error)
	require.NoError(t, db.Create(&deliveryTestTrackingModel{
		Kind:              string(item.Kind),
		ContentID:         item.ContentID,
		ChannelID:         item.ChannelID,
		ActualPublishedAt: new(time.Date(2026, 4, 10, 1, 9, 0, 0, time.UTC)),
		DetectedAt:        detectedAt,
		DeliveryStatus:    string(domain.YouTubeContentAlarmDeliveryStatusPending),
	}).Error)

	delivery := domain.YouTubeNotificationDelivery{
		OutboxID:      item.ID,
		RoomID:        "room-community",
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: detectedAt,
	}
	require.NoError(t, db.Create(&delivery).Error)

	repository := store.NewDeliveryRepository(db.Pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, repository.MarkSentBatch(ctx, []int64{delivery.ID}))

	var updatedTracking deliveryTestTrackingModel
	require.NoError(t, db.Where("kind = ? AND content_id = ?", string(item.Kind), item.ContentID).First(&updatedTracking).Error)
	require.NotNil(t, updatedTracking.AlarmSentAt)
	require.Equal(t, yttimestamp.Canonical.Location, updatedTracking.AlarmSentAt.UTC().Location())
	require.Equal(t, "2026-04-10T01:11:12.123Z", updatedTracking.AlarmSentAt.UTC().Format(yttimestamp.Canonical.Layout))
	require.NotNil(t, updatedTracking.AlarmLatencyMillis)
	require.Equal(t, int64((2*time.Minute+12*time.Second+123*time.Millisecond)/time.Millisecond), *updatedTracking.AlarmLatencyMillis)
	require.NotNil(t, updatedTracking.AlarmLatencyExceeded)
	require.True(t, *updatedTracking.AlarmLatencyExceeded)
	require.Equal(t, string(domain.YouTubeContentAlarmDeliveryStatusSent), updatedTracking.DeliveryStatus)
	require.Equal(t, string(PostLatencyClassificationStatusExceeded), updatedTracking.LatencyClassificationStatus)
	require.Equal(t, string(PostDelaySourceInternalDelivery), updatedTracking.DelaySource)
	require.Equal(t, string(PostInternalDelayCauseQueueWait), updatedTracking.InternalDelayCause)
}

func TestDeliveryRepositoryMarkSentBatchFinalizesClaimedAlarmStateWithClaimToken(t *testing.T) {
	fixedNow := time.Date(2026, 4, 10, 10, 11, 12, 123000000, time.FixedZone("KST", 9*60*60))
	withFixedSentAtNow(t, fixedNow)

	ctx := context.Background()
	db := newDeliveryTestDB(t)

	detectedAt := time.Date(2026, 4, 10, 1, 10, 0, 0, time.UTC)
	authorizedAt := time.Date(2026, 4, 10, 1, 10, 30, 0, time.UTC)
	item := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindCommunityPost,
		ChannelID:     "UC_community_claimed",
		ContentID:     "post-claimed",
		Payload:       `{"post_id":"post-claimed","canonical_post_id":"community:post-claimed","content_text":"community-title","published_at":"2026-04-10T01:09:00Z"}`,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: detectedAt,
	}
	require.NoError(t, db.Create(&item).Error)
	require.NoError(t, db.Create(&deliveryTestTrackingModel{
		Kind:               string(item.Kind),
		ContentID:          item.ContentID,
		CanonicalContentID: "community:post-claimed",
		ChannelID:          item.ChannelID,
		ActualPublishedAt:  new(time.Date(2026, 4, 10, 1, 9, 0, 0, time.UTC)),
		DetectedAt:         detectedAt,
		DeliveryStatus:     string(domain.YouTubeContentAlarmDeliveryStatusPending),
	}).Error)
	require.NoError(t, db.Create(&domain.YouTubeCommunityShortsAlarmState{
		Kind:              item.Kind,
		PostID:            "community:post-claimed",
		ContentID:         item.ContentID,
		ChannelID:         item.ChannelID,
		ActualPublishedAt: new(time.Date(2026, 4, 10, 1, 9, 0, 0, time.UTC)),
		DetectedAt:        detectedAt,
		AuthorizedAt:      &authorizedAt,
		DeliveryStatus:    domain.YouTubeCommunityShortsAlarmStateStatusEnqueued,
	}).Error)

	delivery := domain.YouTubeNotificationDelivery{
		OutboxID:      item.ID,
		RoomID:        "room-community",
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: detectedAt,
	}
	require.NoError(t, db.Create(&delivery).Error)

	repository := store.NewDeliveryRepository(db.Pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, repository.MarkSentBatch(ctx, []int64{delivery.ID}, dispatchstate.ClaimToken{Kind: item.Kind, PostID: "community:post-claimed", AuthorizedAt: authorizedAt}))

	var updatedState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&updatedState, "kind = ? AND post_id = ?", item.Kind, "community:post-claimed").Error)
	require.Nil(t, updatedState.AuthorizedAt)
	require.NotNil(t, updatedState.AlarmSentAt)
	require.Equal(t, "2026-04-10T01:11:12.123Z", updatedState.AlarmSentAt.UTC().Format(yttimestamp.Canonical.Layout))
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, updatedState.DeliveryStatus)
}

func TestDeliveryRepositoryMarkSentBatchRollsBackOnClaimMismatch(t *testing.T) {
	fixedNow := time.Date(2026, 4, 10, 10, 11, 12, 123000000, time.FixedZone("KST", 9*60*60))
	withFixedSentAtNow(t, fixedNow)

	ctx := context.Background()
	db := newDeliveryTestDB(t)

	detectedAt := time.Date(2026, 4, 10, 1, 10, 0, 0, time.UTC)
	authorizedAt := time.Date(2026, 4, 10, 1, 10, 30, 0, time.UTC)
	otherAuthorizedAt := authorizedAt.Add(30 * time.Second)
	item := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewShort,
		ChannelID:     "UC_short_claimed",
		ContentID:     "short-claimed",
		Payload:       `{"video_id":"short-claimed","canonical_post_id":"short:short-claimed","title":"short-title","published_at":"2026-04-10T01:09:00Z"}`,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: detectedAt,
	}
	require.NoError(t, db.Create(&item).Error)
	require.NoError(t, db.Create(&deliveryTestTrackingModel{
		Kind:               string(item.Kind),
		ContentID:          item.ContentID,
		CanonicalContentID: "short:short-claimed",
		ChannelID:          item.ChannelID,
		ActualPublishedAt:  new(time.Date(2026, 4, 10, 1, 9, 0, 0, time.UTC)),
		DetectedAt:         detectedAt,
		DeliveryStatus:     string(domain.YouTubeContentAlarmDeliveryStatusPending),
	}).Error)
	require.NoError(t, db.Create(&domain.YouTubeCommunityShortsAlarmState{
		Kind:              item.Kind,
		PostID:            "short:short-claimed",
		ContentID:         item.ContentID,
		ChannelID:         item.ChannelID,
		ActualPublishedAt: new(time.Date(2026, 4, 10, 1, 9, 0, 0, time.UTC)),
		DetectedAt:        detectedAt,
		AuthorizedAt:      &authorizedAt,
		DeliveryStatus:    domain.YouTubeCommunityShortsAlarmStateStatusEnqueued,
	}).Error)

	delivery := domain.YouTubeNotificationDelivery{
		OutboxID:      item.ID,
		RoomID:        "room-short",
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: detectedAt,
	}
	require.NoError(t, db.Create(&delivery).Error)

	repository := store.NewDeliveryRepository(db.Pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	err := repository.MarkSentBatch(ctx, []int64{delivery.ID}, dispatchstate.ClaimToken{Kind: item.Kind, PostID: "short:short-claimed", AuthorizedAt: otherAuthorizedAt})
	require.ErrorContains(t, err, "claim authorization mismatch")

	var updatedDelivery deliveryTestDeliveryModel
	require.NoError(t, db.First(&updatedDelivery, delivery.ID).Error)
	require.Equal(t, string(domain.OutboxStatusPending), updatedDelivery.Status)
	require.Nil(t, updatedDelivery.SentAt)

	var updatedTracking deliveryTestTrackingModel
	require.NoError(t, db.Where("kind = ? AND content_id = ?", string(item.Kind), item.ContentID).First(&updatedTracking).Error)
	require.Nil(t, updatedTracking.AlarmSentAt)
	require.Equal(t, string(domain.YouTubeContentAlarmDeliveryStatusPending), updatedTracking.DeliveryStatus)

	var updatedState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&updatedState, "kind = ? AND post_id = ?", item.Kind, "short:short-claimed").Error)
	require.NotNil(t, updatedState.AuthorizedAt)
	require.Equal(t, authorizedAt, updatedState.AuthorizedAt.UTC())
	require.Nil(t, updatedState.AlarmSentAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, updatedState.DeliveryStatus)
}

func TestDeliveryRepositoryMarkSentBatchKeepsEarliestAlarmSentAtAcrossDuplicateExecution(t *testing.T) {
	ctx := context.Background()
	db := newDeliveryTestDB(t)

	currentNow := time.Date(2026, 4, 10, 10, 11, 12, 123000000, time.FixedZone("KST", 9*60*60))
	original := dispatchstate.SentAtNow
	dispatchstate.SentAtNow = func() time.Time {
		return currentNow
	}
	t.Cleanup(func() {
		dispatchstate.SentAtNow = original
	})

	detectedAt := time.Date(2026, 4, 10, 1, 10, 0, 0, time.UTC)
	item := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewShort,
		ChannelID:     "UC_short_tracking",
		ContentID:     "short-tracking",
		Payload:       `{"video_id":"short-tracking","title":"short-title","published_at":"2026-04-10T01:09:00Z"}`,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: detectedAt,
	}
	require.NoError(t, db.Create(&item).Error)
	require.NoError(t, db.Create(&deliveryTestTrackingModel{
		Kind:              string(item.Kind),
		ContentID:         item.ContentID,
		ChannelID:         item.ChannelID,
		ActualPublishedAt: new(time.Date(2026, 4, 10, 1, 9, 0, 0, time.UTC)),
		DetectedAt:        detectedAt,
	}).Error)

	firstDelivery := domain.YouTubeNotificationDelivery{
		OutboxID:      item.ID,
		RoomID:        "room-1",
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: detectedAt,
	}
	secondDelivery := domain.YouTubeNotificationDelivery{
		OutboxID:      item.ID,
		RoomID:        "room-2",
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: detectedAt,
	}
	require.NoError(t, db.Create(&firstDelivery).Error)
	require.NoError(t, db.Create(&secondDelivery).Error)

	repository := store.NewDeliveryRepository(db.Pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, repository.MarkSentBatch(ctx, []int64{firstDelivery.ID}))

	firstExpected := yttimestamp.Normalize(currentNow)
	currentNow = currentNow.Add(40 * time.Second)
	require.NoError(t, repository.MarkSentBatch(ctx, []int64{firstDelivery.ID}))

	currentNow = currentNow.Add(40 * time.Second)
	require.NoError(t, repository.MarkSentBatch(ctx, []int64{secondDelivery.ID}))

	var updatedTracking deliveryTestTrackingModel
	require.NoError(t, db.Where("kind = ? AND content_id = ?", string(item.Kind), item.ContentID).First(&updatedTracking).Error)
	require.NotNil(t, updatedTracking.AlarmSentAt)
	require.Equal(t, firstExpected, updatedTracking.AlarmSentAt.UTC())
	require.Equal(t, string(domain.YouTubeContentAlarmDeliveryStatusSent), updatedTracking.DeliveryStatus)

	var updatedFirstDelivery deliveryTestDeliveryModel
	require.NoError(t, db.First(&updatedFirstDelivery, firstDelivery.ID).Error)
	require.NotNil(t, updatedFirstDelivery.SentAt)
	require.Equal(t, firstExpected, updatedFirstDelivery.SentAt.UTC())

	var updatedSecondDelivery deliveryTestDeliveryModel
	require.NoError(t, db.First(&updatedSecondDelivery, secondDelivery.ID).Error)
	require.NotNil(t, updatedSecondDelivery.SentAt)
	require.True(t, updatedSecondDelivery.SentAt.UTC().After(firstExpected))
}

func withFixedSentAtNow(t *testing.T, fixed time.Time) {
	t.Helper()

	original := dispatchstate.SentAtNow
	dispatchstate.SentAtNow = func() time.Time {
		return fixed
	}
	t.Cleanup(func() {
		dispatchstate.SentAtNow = original
	})
}

type deliveryTestTrackingModel struct {
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

func (deliveryTestTrackingModel) TableName() string {
	return "youtube_content_alarm_tracking"
}
