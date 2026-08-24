package youtubedispatch

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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

type recoveryInputFixtureNaming struct {
	channelID        string
	sentContentID    string
	sentBody         string
	pendingContentID string
	pendingBody      string
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

type communityShortsSentSnapshot struct {
	delivery deliveryTestDeliveryModel
	outbox   deliveryTestOutboxModel
	tracking deliveryTestTrackingModel
	state    domain.YouTubeCommunityShortsAlarmState
}

func newRecoveryInputFixtureSpec(
	kind domain.OutboxKind,
	roomID string,
	naming recoveryInputFixtureNaming,
	baseNow time.Time,
) recoveryInputFixtureSpec {
	return recoveryInputFixtureSpec{
		kind:               kind,
		channelID:          naming.channelID,
		roomID:             roomID,
		sentContentID:      naming.sentContentID,
		sentPayload:        recoveryInputFixturePayload(kind, naming.sentContentID, naming.sentBody, "2026-04-10T01:09:00Z"),
		pendingContentID:   naming.pendingContentID,
		pendingPayload:     recoveryInputFixturePayload(kind, naming.pendingContentID, naming.pendingBody, "2026-04-10T01:12:00Z"),
		sentDetectedAt:     time.Date(2026, time.April, 10, 1, 9, 30, 0, time.UTC),
		pendingDetectedAt:  time.Date(2026, time.April, 10, 1, 12, 30, 0, time.UTC),
		sentPublishedAt:    time.Date(2026, time.April, 10, 1, 9, 0, 0, time.UTC),
		pendingPublishedAt: time.Date(2026, time.April, 10, 1, 12, 0, 0, time.UTC),
		retryReadyAt:       baseNow.Add(-30 * time.Second),
		alreadySentAt:      baseNow.Add(-2 * time.Minute),
	}
}

func recoveryInputFixturePayload(kind domain.OutboxKind, contentID, body, publishedAt string) string {
	if kind == domain.OutboxKindNewShort {
		return fmt.Sprintf(
			`{"canonical_post_id":%q,"video_id":%q,"title":%q,"published_at":%q}`,
			"short:"+contentID, contentID, body, publishedAt,
		)
	}

	return fmt.Sprintf(
		`{"canonical_post_id":%q,"post_id":%q,"content_text":%q,"published_at":%q}`,
		"community:"+contentID, contentID, body, publishedAt,
	)
}

type recoverySelectiveSendNaming struct {
	communityChannelID string
	shortsChannelID    string
	sentSlug           string
	pendingSlug        string
	communitySentBody  string
	communityPendBody  string
	shortsSentBody     string
	shortsPendBody     string
}

func newRecoverySelectiveSendCases(baseNow time.Time, naming recoverySelectiveSendNaming) []recoverySelectiveSendCase {
	return []recoverySelectiveSendCase{
		{
			name: testCaseNameCommunity,
			spec: newRecoveryInputFixtureSpec(domain.OutboxKindCommunityPost, testRoomCommunity, recoveryInputFixtureNaming{
				channelID:        naming.communityChannelID,
				sentContentID:    "post-" + naming.sentSlug,
				sentBody:         naming.communitySentBody,
				pendingContentID: "post-" + naming.pendingSlug,
				pendingBody:      naming.communityPendBody,
			}, baseNow),
			sentMarker:    naming.communitySentBody,
			pendingMarker: naming.communityPendBody,
		},
		{
			name: testCaseNameShorts,
			spec: newRecoveryInputFixtureSpec(domain.OutboxKindNewShort, testRoomShorts, recoveryInputFixtureNaming{
				channelID:        naming.shortsChannelID,
				sentContentID:    "short-" + naming.sentSlug,
				sentBody:         naming.shortsSentBody,
				pendingContentID: "short-" + naming.pendingSlug,
				pendingBody:      naming.shortsPendBody,
			}, baseNow),
			sentMarker:    naming.shortsSentBody,
			pendingMarker: naming.shortsPendBody,
		},
	}
}

func TestSeedCommunityShortsRecoveryInputFixtureCreatesSentAndPendingPosts(t *testing.T) {
	t.Parallel()

	baseNow := time.Date(2026, time.April, 10, 1, 15, 0, 0, time.UTC)
	testCases := []struct {
		name string
		spec recoveryInputFixtureSpec
	}{
		{
			name: testCaseNameCommunity,
			spec: newRecoveryInputFixtureSpec(domain.OutboxKindCommunityPost, testRoomCommunity, recoveryInputFixtureNaming{
				channelID:        "UC_fixture_community",
				sentContentID:    "post-fixture-sent",
				sentBody:         "community fixture sent body",
				pendingContentID: "post-fixture-pending",
				pendingBody:      "community fixture pending body",
			}, baseNow),
		},
		{
			name: testCaseNameShorts,
			spec: newRecoveryInputFixtureSpec(domain.OutboxKindNewShort, testRoomShorts, recoveryInputFixtureNaming{
				channelID:        "UC_fixture_shorts",
				sentContentID:    "short-fixture-sent",
				sentBody:         "short fixture sent title",
				pendingContentID: "short-fixture-pending",
				pendingBody:      "short fixture pending title",
			}, baseNow),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db := newRecoveryInputFixtureDB(t, "recovery_input_fixture_"+tc.name)
			fixture := seedCommunityShortsRecoveryInputFixture(t, db, &tc.spec)

			assertRecoveryInputFixtureRows(t, db, fixture, tc.spec)
			assertRecoveryInputFixtureTracking(t, db, fixture, tc.spec)
		})
	}
}

func assertRecoveryInputFixtureRows(
	t *testing.T,
	db *deliveryTestDB,
	fixture recoveryInputFixture,
	spec recoveryInputFixtureSpec,
) {
	t.Helper()

	var outboxes []deliveryTestOutboxModel

	require.NoError(t, findDeliveryTestRowsOrdered(db, &outboxes, "content_id ASC").Error)
	require.Len(t, outboxes, 3)
	require.Equal(t, spec.sentContentID, fixture.sentOutbox.ContentID)
	require.Equal(t, spec.pendingContentID, fixture.pendingOutbox.ContentID)
	require.NotEqual(t, fixture.sentOutbox.ID, fixture.servedOutbox.ID)
	require.NotEqual(t, fixture.sentOutbox.ContentID, fixture.servedOutbox.ContentID)
	require.Equal(t, fixture.sentPostID, telemetry.ResolveTelemetryPostID(fixture.servedOutbox.Kind, fixture.servedOutbox.ContentID, fixture.servedOutbox.Payload))

	var servedOutbox deliveryTestOutboxModel

	require.NoError(t, firstDeliveryTestRow(db, &servedOutbox, fixture.servedOutbox.ID).Error)
	require.Equal(t, string(domain.OutboxStatusSent), servedOutbox.Status)
	require.NotNil(t, servedOutbox.SentAt)
	require.Equal(t, spec.alreadySentAt, servedOutbox.SentAt.UTC())

	var deliveries []deliveryTestDeliveryModel

	require.NoError(t, findDeliveryTestRowsOrdered(db, &deliveries, "id ASC").Error)
	require.Len(t, deliveries, 3)
	require.Equal(t, spec.roomID, fixture.sentDelivery.RoomID)
	require.Equal(t, spec.roomID, fixture.pendingDelivery.RoomID)
	require.Equal(t, spec.roomID, fixture.servedDelivery.RoomID)

	var sentDelivery deliveryTestDeliveryModel

	require.NoError(t, firstDeliveryTestRow(db, &sentDelivery, fixture.sentDelivery.ID).Error)
	require.Equal(t, string(domain.OutboxStatusPending), sentDelivery.Status)
	require.Nil(t, sentDelivery.SentAt)

	var servedDelivery deliveryTestDeliveryModel

	require.NoError(t, firstDeliveryTestRow(db, &servedDelivery, fixture.servedDelivery.ID).Error)
	require.Equal(t, fixture.servedOutbox.ID, servedDelivery.OutboxID)
	require.Equal(t, string(domain.OutboxStatusSent), servedDelivery.Status)
	require.NotNil(t, servedDelivery.SentAt)
	require.Equal(t, spec.alreadySentAt, servedDelivery.SentAt.UTC())
}

func assertRecoveryInputFixtureTracking(
	t *testing.T,
	db *deliveryTestDB,
	fixture recoveryInputFixture,
	spec recoveryInputFixtureSpec,
) {
	t.Helper()

	var trackingRows []deliveryTestTrackingModel

	require.NoError(t, findDeliveryTestRows(db, &trackingRows).Error)
	require.Len(t, trackingRows, 2)

	var sentTracking deliveryTestTrackingModel

	require.NoError(t, firstDeliveryTestRowWhere(db, &sentTracking, "kind = ? AND content_id = ?", string(fixture.sentOutbox.Kind), fixture.sentOutbox.ContentID).Error)
	require.Equal(t, fixture.sentPostID, sentTracking.CanonicalContentID)
	require.NotNil(t, sentTracking.AlarmSentAt)
	require.Equal(t, spec.alreadySentAt, sentTracking.AlarmSentAt.UTC())
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
	require.Equal(t, spec.alreadySentAt, sentState.AlarmSentAt.UTC())
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, sentState.DeliveryStatus)

	var pendingState domain.YouTubeCommunityShortsAlarmState

	require.NoError(t, firstDeliveryTestRow(db, &pendingState, "kind = ? AND post_id = ?", fixture.pendingOutbox.Kind, fixture.pendingPostID).Error)
	require.Nil(t, pendingState.AuthorizedAt)
	require.Nil(t, pendingState.AlarmSentAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusDetected, pendingState.DeliveryStatus)
}

func assertCommunityShortsPostSent(
	t *testing.T,
	db *deliveryTestDB,
	item domain.YouTubeNotificationOutbox,
	deliveryID int64,
	postID string,
) communityShortsSentSnapshot {
	t.Helper()

	var snapshot communityShortsSentSnapshot

	require.NoError(t, firstDeliveryTestRow(db, &snapshot.delivery, deliveryID).Error)
	assert.Equal(t, string(domain.OutboxStatusSent), snapshot.delivery.Status)
	require.NotNil(t, snapshot.delivery.SentAt)

	require.NoError(t, firstDeliveryTestRow(db, &snapshot.outbox, item.ID).Error)
	assert.Equal(t, string(domain.OutboxStatusSent), snapshot.outbox.Status)
	require.NotNil(t, snapshot.outbox.SentAt)

	require.NoError(t, firstDeliveryTestRowWhere(db, &snapshot.tracking, "kind = ? AND content_id = ?", string(item.Kind), item.ContentID).Error)
	require.NotNil(t, snapshot.tracking.AlarmSentAt)
	assert.Equal(t, string(domain.YouTubeContentAlarmDeliveryStatusSent), snapshot.tracking.DeliveryStatus)

	require.NoError(t, firstDeliveryTestRow(db, &snapshot.state, "kind = ? AND post_id = ?", item.Kind, postID).Error)
	assert.Nil(t, snapshot.state.AuthorizedAt)
	require.NotNil(t, snapshot.state.AlarmSentAt)
	assert.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, snapshot.state.DeliveryStatus)

	return snapshot
}

func assertCommunityShortsSentAt(t *testing.T, snapshot communityShortsSentSnapshot, expectedSentAt time.Time) {
	t.Helper()

	assert.Equal(t, expectedSentAt, snapshot.delivery.SentAt.UTC())
	assert.Equal(t, expectedSentAt, snapshot.outbox.SentAt.UTC())
	assert.Equal(t, expectedSentAt, snapshot.tracking.AlarmSentAt.UTC())
	assert.Equal(t, expectedSentAt, snapshot.state.AlarmSentAt.UTC())
}

func newRecoveryInputFixtureDB(t *testing.T, _ string) *deliveryTestDB {
	t.Helper()

	db := newDeliveryPool(t)

	return db
}

func seedCommunityShortsRecoveryInputFixture(t *testing.T, db *deliveryTestDB, spec *recoveryInputFixtureSpec) recoveryInputFixture {
	t.Helper()

	sentItem, pendingItem, servedItem := seedRecoveryInputFixtureOutboxes(t, db, spec)
	sentPostID := store.CanonicalDeliveryPostID(spec.kind, sentItem.ContentID)
	pendingPostID := store.CanonicalDeliveryPostID(spec.kind, pendingItem.ContentID)

	seedRecoveryInputFixtureTracking(t, db, spec, sentPostID, pendingPostID)

	sentDelivery, pendingDelivery, servedDelivery := seedRecoveryInputFixtureDeliveries(t, db, spec, sentItem.ID, pendingItem.ID, servedItem.ID)

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

func seedRecoveryInputFixtureOutboxes(
	t *testing.T,
	db *deliveryTestDB,
	spec *recoveryInputFixtureSpec,
) (sent, pending, served domain.YouTubeNotificationOutbox) {
	t.Helper()

	sent = domain.YouTubeNotificationOutbox{
		Kind:          spec.kind,
		ChannelID:     spec.channelID,
		ContentID:     spec.sentContentID,
		Payload:       spec.sentPayload,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  1,
		NextAttemptAt: spec.retryReadyAt,
		CreatedAt:     spec.sentDetectedAt,
	}
	pending = domain.YouTubeNotificationOutbox{
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
	served = domain.YouTubeNotificationOutbox{
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

	require.NoError(t, insertDeliveryTestRows(db, &sent).Error)
	require.NoError(t, insertDeliveryTestRows(db, &pending).Error)
	require.NoError(t, insertDeliveryTestRows(db, &served).Error)

	return sent, pending, served
}

func seedRecoveryInputFixtureTracking(
	t *testing.T,
	db *deliveryTestDB,
	spec *recoveryInputFixtureSpec,
	sentPostID, pendingPostID string,
) {
	t.Helper()

	require.NoError(t, insertDeliveryTestRows(db, &deliveryTestTrackingModel{
		Kind:               string(spec.kind),
		ContentID:          spec.sentContentID,
		CanonicalContentID: sentPostID,
		ChannelID:          spec.channelID,
		ActualPublishedAt:  new(spec.sentPublishedAt),
		DetectedAt:         spec.sentDetectedAt,
		AlarmSentAt:        new(spec.alreadySentAt),
		DeliveryStatus:     string(domain.YouTubeContentAlarmDeliveryStatusSent),
	}).Error)
	require.NoError(t, insertDeliveryTestRows(db, &deliveryTestTrackingModel{
		Kind:               string(spec.kind),
		ContentID:          spec.pendingContentID,
		CanonicalContentID: pendingPostID,
		ChannelID:          spec.channelID,
		ActualPublishedAt:  new(spec.pendingPublishedAt),
		DetectedAt:         spec.pendingDetectedAt,
		DeliveryStatus:     string(domain.YouTubeContentAlarmDeliveryStatusPending),
	}).Error)

	require.NoError(t, insertDeliveryTestRows(db, []domain.YouTubeCommunityShortsAlarmState{
		{
			Kind:              spec.kind,
			PostID:            sentPostID,
			ContentID:         spec.sentContentID,
			ChannelID:         spec.channelID,
			ActualPublishedAt: new(spec.sentPublishedAt),
			DetectedAt:        spec.sentDetectedAt,
			AlarmSentAt:       new(spec.alreadySentAt),
			DeliveryStatus:    domain.YouTubeCommunityShortsAlarmStateStatusSent,
		},
		{
			Kind:              spec.kind,
			PostID:            pendingPostID,
			ContentID:         spec.pendingContentID,
			ChannelID:         spec.channelID,
			ActualPublishedAt: new(spec.pendingPublishedAt),
			DetectedAt:        spec.pendingDetectedAt,
			DeliveryStatus:    domain.YouTubeCommunityShortsAlarmStateStatusDetected,
		},
	}).Error)
}

func seedRecoveryInputFixtureDeliveries(
	t *testing.T,
	db *deliveryTestDB,
	spec *recoveryInputFixtureSpec,
	sentOutboxID, pendingOutboxID, servedOutboxID int64,
) (sent, pending, served domain.YouTubeNotificationDelivery) {
	t.Helper()

	sent = domain.YouTubeNotificationDelivery{
		OutboxID:      sentOutboxID,
		RoomID:        spec.roomID,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  1,
		NextAttemptAt: spec.retryReadyAt,
		CreatedAt:     spec.sentDetectedAt,
	}
	pending = domain.YouTubeNotificationDelivery{
		OutboxID:      pendingOutboxID,
		RoomID:        spec.roomID,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  1,
		NextAttemptAt: spec.retryReadyAt,
		CreatedAt:     spec.pendingDetectedAt,
	}
	served = domain.YouTubeNotificationDelivery{
		OutboxID:      servedOutboxID,
		RoomID:        spec.roomID,
		Status:        domain.OutboxStatusSent,
		AttemptCount:  1,
		NextAttemptAt: spec.alreadySentAt,
		CreatedAt:     spec.sentDetectedAt,
		SentAt:        new(spec.alreadySentAt),
	}

	require.NoError(t, insertDeliveryTestRows(db, &sent).Error)
	require.NoError(t, insertDeliveryTestRows(db, &pending).Error)
	require.NoError(t, insertDeliveryTestRows(db, &served).Error)

	return sent, pending, served
}
