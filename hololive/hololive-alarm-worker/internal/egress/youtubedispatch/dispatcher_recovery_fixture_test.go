package youtubedispatch

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/store"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/telemetry"
)

type recoveryInputFixtureSpec struct {
	kind               domain.OutboxKind
	channelID          string
	roomID             string
	sentContentID      string
	sentPayload        string
	pendingContentID   string
	pendingPayload     string
	sentDetectedAt     time.Time
	pendingDetectedAt  time.Time
	sentPublishedAt    time.Time
	pendingPublishedAt time.Time
	retryReadyAt       time.Time
	alreadySentAt      time.Time
}

type recoveryInputFixture struct {
	sentOutbox      domain.YouTubeNotificationOutbox
	pendingOutbox   domain.YouTubeNotificationOutbox
	servedOutbox    domain.YouTubeNotificationOutbox
	sentDelivery    domain.YouTubeNotificationDelivery
	pendingDelivery domain.YouTubeNotificationDelivery
	servedDelivery  domain.YouTubeNotificationDelivery
	sentPostID      string
	pendingPostID   string
}

func TestSeedCommunityShortsRecoveryInputFixtureCreatesSentAndPendingPosts(t *testing.T) {
	t.Parallel()

	baseNow := time.Date(2026, 4, 10, 1, 15, 0, 0, time.UTC)
	testCases := []struct {
		name string
		spec recoveryInputFixtureSpec
	}{
		{
			name: "community",
			spec: recoveryInputFixtureSpec{
				kind:               domain.OutboxKindCommunityPost,
				channelID:          "UC_fixture_community",
				roomID:             "room-community",
				sentContentID:      "post-fixture-sent",
				sentPayload:        `{"canonical_post_id":"community:post-fixture-sent","post_id":"post-fixture-sent","content_text":"community fixture sent body","published_at":"2026-04-10T01:09:00Z"}`,
				pendingContentID:   "post-fixture-pending",
				pendingPayload:     `{"canonical_post_id":"community:post-fixture-pending","post_id":"post-fixture-pending","content_text":"community fixture pending body","published_at":"2026-04-10T01:12:00Z"}`,
				sentDetectedAt:     time.Date(2026, 4, 10, 1, 9, 30, 0, time.UTC),
				pendingDetectedAt:  time.Date(2026, 4, 10, 1, 12, 30, 0, time.UTC),
				sentPublishedAt:    time.Date(2026, 4, 10, 1, 9, 0, 0, time.UTC),
				pendingPublishedAt: time.Date(2026, 4, 10, 1, 12, 0, 0, time.UTC),
				retryReadyAt:       baseNow.Add(-30 * time.Second),
				alreadySentAt:      baseNow.Add(-2 * time.Minute),
			},
		},
		{
			name: "shorts",
			spec: recoveryInputFixtureSpec{
				kind:               domain.OutboxKindNewShort,
				channelID:          "UC_fixture_shorts",
				roomID:             "room-shorts",
				sentContentID:      "short-fixture-sent",
				sentPayload:        `{"canonical_post_id":"short:short-fixture-sent","video_id":"short-fixture-sent","title":"short fixture sent title","published_at":"2026-04-10T01:09:00Z"}`,
				pendingContentID:   "short-fixture-pending",
				pendingPayload:     `{"canonical_post_id":"short:short-fixture-pending","video_id":"short-fixture-pending","title":"short fixture pending title","published_at":"2026-04-10T01:12:00Z"}`,
				sentDetectedAt:     time.Date(2026, 4, 10, 1, 9, 30, 0, time.UTC),
				pendingDetectedAt:  time.Date(2026, 4, 10, 1, 12, 30, 0, time.UTC),
				sentPublishedAt:    time.Date(2026, 4, 10, 1, 9, 0, 0, time.UTC),
				pendingPublishedAt: time.Date(2026, 4, 10, 1, 12, 0, 0, time.UTC),
				retryReadyAt:       baseNow.Add(-30 * time.Second),
				alreadySentAt:      baseNow.Add(-2 * time.Minute),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db := newRecoveryInputFixtureDB(t, "recovery_input_fixture_"+tc.name)
			fixture := seedCommunityShortsRecoveryInputFixture(t, db, &tc.spec)

			var outboxes []deliveryTestOutboxModel
			require.NoError(t, findDeliveryTestRowsOrdered(db, &outboxes, "content_id ASC").Error)
			require.Len(t, outboxes, 3)
			require.Equal(t, tc.spec.sentContentID, fixture.sentOutbox.ContentID)
			require.Equal(t, tc.spec.pendingContentID, fixture.pendingOutbox.ContentID)
			require.NotEqual(t, fixture.sentOutbox.ID, fixture.servedOutbox.ID)
			require.NotEqual(t, fixture.sentOutbox.ContentID, fixture.servedOutbox.ContentID)
			require.Equal(t, fixture.sentPostID, telemetry.ResolveTelemetryPostID(fixture.servedOutbox.Kind, fixture.servedOutbox.ContentID, fixture.servedOutbox.Payload))

			var servedOutbox deliveryTestOutboxModel
			require.NoError(t, firstDeliveryTestRow(db, &servedOutbox, fixture.servedOutbox.ID).Error)
			require.Equal(t, string(domain.OutboxStatusSent), servedOutbox.Status)
			require.NotNil(t, servedOutbox.SentAt)
			require.Equal(t, tc.spec.alreadySentAt, servedOutbox.SentAt.UTC())

			var deliveries []deliveryTestDeliveryModel
			require.NoError(t, findDeliveryTestRowsOrdered(db, &deliveries, "id ASC").Error)
			require.Len(t, deliveries, 3)
			require.Equal(t, tc.spec.roomID, fixture.sentDelivery.RoomID)
			require.Equal(t, tc.spec.roomID, fixture.pendingDelivery.RoomID)
			require.Equal(t, tc.spec.roomID, fixture.servedDelivery.RoomID)

			var sentDelivery deliveryTestDeliveryModel
			require.NoError(t, firstDeliveryTestRow(db, &sentDelivery, fixture.sentDelivery.ID).Error)
			require.Equal(t, string(domain.OutboxStatusPending), sentDelivery.Status)
			require.Nil(t, sentDelivery.SentAt)

			var servedDelivery deliveryTestDeliveryModel
			require.NoError(t, firstDeliveryTestRow(db, &servedDelivery, fixture.servedDelivery.ID).Error)
			require.Equal(t, fixture.servedOutbox.ID, servedDelivery.OutboxID)
			require.Equal(t, string(domain.OutboxStatusSent), servedDelivery.Status)
			require.NotNil(t, servedDelivery.SentAt)
			require.Equal(t, tc.spec.alreadySentAt, servedDelivery.SentAt.UTC())

			var trackingRows []deliveryTestTrackingModel
			require.NoError(t, findDeliveryTestRows(db, &trackingRows).Error)
			require.Len(t, trackingRows, 2)

			var sentTracking deliveryTestTrackingModel
			require.NoError(t, firstDeliveryTestRowWhere(db, &sentTracking, "kind = ? AND content_id = ?", string(fixture.sentOutbox.Kind), fixture.sentOutbox.ContentID).Error)
			require.Equal(t, fixture.sentPostID, sentTracking.CanonicalContentID)
			require.NotNil(t, sentTracking.AlarmSentAt)
			require.Equal(t, tc.spec.alreadySentAt, sentTracking.AlarmSentAt.UTC())
			require.Equal(t, string(domain.YouTubeContentAlarmDeliveryStatusSent), sentTracking.DeliveryStatus)

			var pendingTracking deliveryTestTrackingModel
			require.NoError(t, firstDeliveryTestRowWhere(db, &pendingTracking, "kind = ? AND content_id = ?", string(fixture.pendingOutbox.Kind), fixture.pendingOutbox.ContentID).Error)
			require.Equal(t, fixture.pendingPostID, pendingTracking.CanonicalContentID)
			require.Nil(t, pendingTracking.AlarmSentAt)
			require.Equal(t, string(domain.YouTubeContentAlarmDeliveryStatusPending), pendingTracking.DeliveryStatus)

			var states []domain.YouTubeCommunityShortsAlarmState
			require.NoError(t, findDeliveryTestRows(db, &states).Error)
			require.Len(t, states, 2)

			var sentState domain.YouTubeCommunityShortsAlarmState
			require.NoError(t, firstDeliveryTestRow(db, &sentState, "kind = ? AND post_id = ?", fixture.sentOutbox.Kind, fixture.sentPostID).Error)
			require.NotNil(t, sentState.AlarmSentAt)
			require.Equal(t, tc.spec.alreadySentAt, sentState.AlarmSentAt.UTC())
			require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, sentState.DeliveryStatus)

			var pendingState domain.YouTubeCommunityShortsAlarmState
			require.NoError(t, firstDeliveryTestRow(db, &pendingState, "kind = ? AND post_id = ?", fixture.pendingOutbox.Kind, fixture.pendingPostID).Error)
			require.Nil(t, pendingState.AuthorizedAt)
			require.Nil(t, pendingState.AlarmSentAt)
			require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusDetected, pendingState.DeliveryStatus)
		})
	}
}

func newRecoveryInputFixtureDB(t *testing.T, _ string) *deliveryTestDB {
	t.Helper()

	db := newDeliveryPool(t)

	return db
}

func seedCommunityShortsRecoveryInputFixture(t *testing.T, db *deliveryTestDB, spec *recoveryInputFixtureSpec) recoveryInputFixture {
	t.Helper()

	sentItem := domain.YouTubeNotificationOutbox{
		Kind:          spec.kind,
		ChannelID:     spec.channelID,
		ContentID:     spec.sentContentID,
		Payload:       spec.sentPayload,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  1,
		NextAttemptAt: spec.retryReadyAt,
		CreatedAt:     spec.sentDetectedAt,
	}
	pendingItem := domain.YouTubeNotificationOutbox{
		Kind:          spec.kind,
		ChannelID:     spec.channelID,
		ContentID:     spec.pendingContentID,
		Payload:       spec.pendingPayload,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  1,
		NextAttemptAt: spec.retryReadyAt,
		CreatedAt:     spec.pendingDetectedAt,
	}
	// idx_yno_kind_content·idx_ynd_outbox_room 유니크 인덱스 때문에 같은 (kind, content_id)나
	// 같은 (outbox_id, room_id)로는 SENT 행을 둘 수 없어, canonical_post_id만 같은 재등록 outbox로 만든다.
	servedItem := domain.YouTubeNotificationOutbox{
		Kind:          spec.kind,
		ChannelID:     spec.channelID,
		ContentID:     spec.sentContentID + "-served",
		Payload:       spec.sentPayload,
		Status:        domain.OutboxStatusSent,
		AttemptCount:  1,
		NextAttemptAt: spec.alreadySentAt,
		CreatedAt:     spec.sentDetectedAt,
		SentAt:        new(spec.alreadySentAt),
	}
	require.NoError(t, insertDeliveryTestRows(db, &sentItem).Error)
	require.NoError(t, insertDeliveryTestRows(db, &pendingItem).Error)
	require.NoError(t, insertDeliveryTestRows(db, &servedItem).Error)

	sentPostID := store.CanonicalDeliveryPostID(spec.kind, sentItem.ContentID)
	pendingPostID := store.CanonicalDeliveryPostID(spec.kind, pendingItem.ContentID)

	require.NoError(t, insertDeliveryTestRows(db, &deliveryTestTrackingModel{
		Kind:               string(sentItem.Kind),
		ContentID:          sentItem.ContentID,
		CanonicalContentID: sentPostID,
		ChannelID:          sentItem.ChannelID,
		ActualPublishedAt:  new(spec.sentPublishedAt),
		DetectedAt:         spec.sentDetectedAt,
		AlarmSentAt:        new(spec.alreadySentAt),
		DeliveryStatus:     string(domain.YouTubeContentAlarmDeliveryStatusSent),
	}).Error)
	require.NoError(t, insertDeliveryTestRows(db, &deliveryTestTrackingModel{
		Kind:               string(pendingItem.Kind),
		ContentID:          pendingItem.ContentID,
		CanonicalContentID: pendingPostID,
		ChannelID:          pendingItem.ChannelID,
		ActualPublishedAt:  new(spec.pendingPublishedAt),
		DetectedAt:         spec.pendingDetectedAt,
		DeliveryStatus:     string(domain.YouTubeContentAlarmDeliveryStatusPending),
	}).Error)

	require.NoError(t, insertDeliveryTestRows(db, []domain.YouTubeCommunityShortsAlarmState{
		{
			Kind:              sentItem.Kind,
			PostID:            sentPostID,
			ContentID:         sentItem.ContentID,
			ChannelID:         sentItem.ChannelID,
			ActualPublishedAt: new(spec.sentPublishedAt),
			DetectedAt:        spec.sentDetectedAt,
			AlarmSentAt:       new(spec.alreadySentAt),
			DeliveryStatus:    domain.YouTubeCommunityShortsAlarmStateStatusSent,
		},
		{
			Kind:              pendingItem.Kind,
			PostID:            pendingPostID,
			ContentID:         pendingItem.ContentID,
			ChannelID:         pendingItem.ChannelID,
			ActualPublishedAt: new(spec.pendingPublishedAt),
			DetectedAt:        spec.pendingDetectedAt,
			DeliveryStatus:    domain.YouTubeCommunityShortsAlarmStateStatusDetected,
		},
	}).Error)

	sentDelivery := domain.YouTubeNotificationDelivery{
		OutboxID:      sentItem.ID,
		RoomID:        spec.roomID,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  1,
		NextAttemptAt: spec.retryReadyAt,
		CreatedAt:     spec.sentDetectedAt,
	}
	pendingDelivery := domain.YouTubeNotificationDelivery{
		OutboxID:      pendingItem.ID,
		RoomID:        spec.roomID,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  1,
		NextAttemptAt: spec.retryReadyAt,
		CreatedAt:     spec.pendingDetectedAt,
	}
	servedDelivery := domain.YouTubeNotificationDelivery{
		OutboxID:      servedItem.ID,
		RoomID:        spec.roomID,
		Status:        domain.OutboxStatusSent,
		AttemptCount:  1,
		NextAttemptAt: spec.alreadySentAt,
		CreatedAt:     spec.sentDetectedAt,
		SentAt:        new(spec.alreadySentAt),
	}
	require.NoError(t, insertDeliveryTestRows(db, &sentDelivery).Error)
	require.NoError(t, insertDeliveryTestRows(db, &pendingDelivery).Error)
	require.NoError(t, insertDeliveryTestRows(db, &servedDelivery).Error)

	return recoveryInputFixture{
		sentOutbox:      sentItem,
		pendingOutbox:   pendingItem,
		servedOutbox:    servedItem,
		sentDelivery:    sentDelivery,
		pendingDelivery: pendingDelivery,
		servedDelivery:  servedDelivery,
		sentPostID:      sentPostID,
		pendingPostID:   pendingPostID,
	}
}
