package youtubedispatch

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/iris-client-go/v4/iris"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendoutcome"
	ytlifecycle "github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
)

type claimGateTestSender struct {
	mu          sync.Mutex
	failRoom    map[string]bool
	unknownRoom map[string]bool
	messages    []string
}

func (s *claimGateTestSender) SendMessage(_ context.Context, roomID, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failRoom[roomID] {
		return fmt.Errorf("%s: %w", testSendFailedMessage, iris.ErrRateLimited)
	}

	if s.unknownRoom[roomID] {
		return fmt.Errorf("%s: %w", testSendFailedMessage, sendoutcome.ErrAdmissionOutcomeUnknown)
	}

	s.messages = append(s.messages, roomID+":"+message)

	return nil
}

func (s *claimGateTestSender) messageCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.messages)
}

func (s *claimGateTestSender) allMessages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	cloned := make([]string, len(s.messages))
	copy(cloned, s.messages)

	return cloned
}

func newClaimGateTestDispatcher(t *testing.T, sender *claimGateTestSender, config *Config) (*Dispatcher, *pgxpool.Pool) {
	t.Helper()

	if config.BatchSize <= 0 {
		config.BatchSize = 10
	}

	if config.LockTimeout <= 0 {
		config.LockTimeout = 5 * time.Minute
	}

	if config.PollInterval <= 0 {
		config.PollInterval = time.Second
	}

	if config.MaxRetries <= 0 {
		config.MaxRetries = 3
	}

	if config.RetryBackoff <= 0 {
		config.RetryBackoff = time.Minute
	}

	if config.DeliveryParallelism <= 0 {
		config.DeliveryParallelism = 2
	}

	db := newDeliveryPool(t)

	dispatcher := newDispatcherForTest(
		t,
		db,
		cachemocks.NewLenientClient(),
		sender,
		nil,
		slog.New(slog.DiscardHandler), config,
	)

	dispatcher.telemetry = nil
	dispatcher.send.transition = &lifecycleTransitionSpy{complete: store.ApplyResult{Outcome: store.ApplyApplied}}

	return dispatcher, db
}

func newCommunityClaimGateFixture(now time.Time, suffix string) (domain.YouTubeNotificationDelivery, domain.YouTubeNotificationOutbox, string) {
	contentID := "post-" + suffix
	postID := "community:" + contentID

	delivery := domain.YouTubeNotificationDelivery{
		ID:        100 + int64(len(suffix)),
		OutboxID:  200 + int64(len(suffix)),
		RoomID:    testRoomCommunity,
		CreatedAt: now.Add(15 * time.Second),
	}

	outbox := domain.YouTubeNotificationOutbox{
		ID:            200 + int64(len(suffix)),
		Kind:          domain.OutboxKindCommunityPost,
		ChannelID:     "UC_COMMUNITY",
		ContentID:     contentID,
		Payload:       fmt.Sprintf(`{"canonical_post_id":%q,"post_id":%q,"content_text":"body-%s"}`, postID, contentID, suffix),
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: now,
		CreatedAt:     now,
	}

	return delivery, outbox, postID
}

func newShortClaimGateFixture(now time.Time, suffix string) (domain.YouTubeNotificationDelivery, domain.YouTubeNotificationOutbox, string) {
	contentID := "short-" + suffix
	postID := "short:" + contentID

	delivery := domain.YouTubeNotificationDelivery{
		ID:        300 + int64(len(suffix)),
		OutboxID:  400 + int64(len(suffix)),
		RoomID:    testRoomShorts,
		CreatedAt: now.Add(15 * time.Second),
	}

	outbox := domain.YouTubeNotificationOutbox{
		ID:            400 + int64(len(suffix)),
		Kind:          domain.OutboxKindNewShort,
		ChannelID:     "UC_SHORTS",
		ContentID:     contentID,
		Payload:       fmt.Sprintf(`{"canonical_post_id":%q,"video_id":%q,"title":"title-%s"}`, postID, contentID, suffix),
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: now,
		CreatedAt:     now,
	}

	return delivery, outbox, postID
}

// newPendingClaimGateDispatcher는 운영과 같은 TransitionStore로 claim·prepare·발송 전이를 실행한다.
func newPendingClaimGateDispatcher(t *testing.T, sender *claimGateTestSender) (*Dispatcher, *pgxpool.Pool) {
	t.Helper()

	dispatcher, db := newClaimGateTestDispatcher(t, sender, &Config{DeliveryParallelism: 1})

	dispatcher.send.transition = dispatcher.claim.transition

	return dispatcher, db
}

// seedPendingClaimGateDelivery는 fanout이 끝난 PENDING outbox와 testRoomCommunity의 delivery 행을 만든다.
func seedPendingClaimGateDelivery(
	t *testing.T,
	db *pgxpool.Pool,
	outbox *domain.YouTubeNotificationOutbox,
	at time.Time,
) domain.YouTubeNotificationDelivery {
	t.Helper()

	outbox.ID = 0
	outbox.Status = domain.OutboxStatusPending
	outbox.NextAttemptAt = at
	outbox.CreatedAt = at
	require.NoError(t, insertDeliveryTestRows(db, outbox).Error)

	delivery := domain.YouTubeNotificationDelivery{
		OutboxID:      outbox.ID,
		RoomID:        testRoomCommunity,
		Status:        domain.OutboxStatusPending,
		NextAttemptAt: at,
		CreatedAt:     at,
	}
	require.NoError(t, insertDeliveryTestRows(db, &delivery).Error)

	return delivery
}

// recordServedClaimGateDelivery는 같은 logical ID의 재등록 outbox가 roomID에 이미 발송되어
// SENT 행과 SENT 원장을 남긴 상태를 만든다. (kind, content_id) 유니크 때문에 content_id는 canonical 표기를 쓴다.
func recordServedClaimGateDelivery(
	t *testing.T,
	db *pgxpool.Pool,
	outbox *domain.YouTubeNotificationOutbox,
	postID, roomID string,
	sentAt time.Time,
) {
	t.Helper()

	served := domain.YouTubeNotificationOutbox{
		Kind:          outbox.Kind,
		ChannelID:     outbox.ChannelID,
		ContentID:     mustCanonicalDeliveryPostID(outbox.Kind, outbox.ContentID),
		Payload:       outbox.Payload,
		Status:        domain.OutboxStatusSent,
		NextAttemptAt: sentAt,
		CreatedAt:     sentAt.Add(-time.Minute),
		SentAt:        new(sentAt),
	}
	require.NotEqual(t, outbox.ContentID, served.ContentID)
	require.NoError(t, insertDeliveryTestRows(db, &served).Error)

	delivery := domain.YouTubeNotificationDelivery{
		OutboxID:      served.ID,
		RoomID:        roomID,
		Status:        domain.OutboxStatusSent,
		NextAttemptAt: sentAt,
		CreatedAt:     sentAt.Add(-time.Minute),
		SentAt:        new(sentAt),
	}
	require.NoError(t, insertDeliveryTestRows(db, &delivery).Error)
	recordClaimGateSentLedger(t, db, outbox.Kind, postID, roomID, sentAt, delivery.ID)
}

func recordClaimGateSentLedger(
	t *testing.T,
	db *pgxpool.Pool,
	kind domain.OutboxKind,
	postID, roomID string,
	sentAt time.Time,
	sourceDeliveryID int64,
) {
	t.Helper()

	require.NoError(t, store.RecordDeliveryLedgerWrites(t.Context(), db, store.LedgerStatusSent, []store.LedgerWrite{{
		Key:        ytcontentid.LogicalKey{Kind: kind, LogicalID: postID, RoomID: roomID},
		ObservedAt: sentAt, SourceDeliveryID: sourceDeliveryID,
	}}))
}

func countClaimGateSentLedger(t *testing.T, db *pgxpool.Pool, kind domain.OutboxKind, postID, roomID string) int {
	t.Helper()

	var count int

	require.NoError(t, db.QueryRow(t.Context(), `
		SELECT count(*)
		FROM youtube_notification_delivery_ledger
		WHERE kind = $1 AND logical_id = $2 AND room_id = $3 AND status = 'SENT'`,
		string(kind), postID, roomID).Scan(&count))

	return count
}

func requireClaimGateDeliverySent(t *testing.T, db *pgxpool.Pool, id int64) {
	t.Helper()

	row := loadClaimGateDeliveryRow(t, db, id)
	require.Equal(t, string(domain.OutboxStatusSent), row.Status, row.RoomID)
	require.NotNil(t, row.SentAt, row.RoomID)
	require.Nil(t, row.LockedAt, row.RoomID)
}

// 같은 room에 SENT 원장이 있으면 post 단위 상태와 무관하게 prepare 단계에서 발송 없이 SENT로 수렴한다.
func TestProcessPendingDeliveriesSkipsRoomWithSentLedger(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		fixture func(now time.Time, suffix string) (domain.YouTubeNotificationDelivery, domain.YouTubeNotificationOutbox, string)
		seed    func(t *testing.T, db *pgxpool.Pool, outbox *domain.YouTubeNotificationOutbox, postID string, sentAt time.Time)
	}{
		{
			name:    "community alarm state sent",
			fixture: newCommunityClaimGateFixture,
			seed: func(t *testing.T, db *pgxpool.Pool, outbox *domain.YouTubeNotificationOutbox, postID string, sentAt time.Time) {
				t.Helper()

				require.NoError(t, insertDeliveryTestRows(db, &domain.YouTubeCommunityShortsAlarmState{
					Kind:           outbox.Kind,
					PostID:         postID,
					ContentID:      outbox.ContentID,
					ChannelID:      outbox.ChannelID,
					DetectedAt:     sentAt.Add(-time.Minute),
					AlarmSentAt:    &sentAt,
					DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusSent,
				}).Error)
			},
		},
		{
			name:    "short tracking row sent",
			fixture: newShortClaimGateFixture,
			seed: func(t *testing.T, db *pgxpool.Pool, outbox *domain.YouTubeNotificationOutbox, postID string, sentAt time.Time) {
				t.Helper()

				require.NoError(t, insertDeliveryTestRows(db, &domain.YouTubeContentAlarmTracking{
					Kind:               outbox.Kind,
					ContentID:          outbox.ContentID,
					CanonicalContentID: postID,
					ChannelID:          outbox.ChannelID,
					DetectedAt:         sentAt.Add(-time.Minute),
					AlarmSentAt:        &sentAt,
					DeliveryStatus:     domain.YouTubeContentAlarmDeliveryStatusSent,
				}).Error)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			now := time.Now().UTC().Truncate(time.Microsecond)
			sender := &claimGateTestSender{failRoom: map[string]bool{}}
			dispatcher, db := newPendingClaimGateDispatcher(t, sender)
			_, outbox, postID := tc.fixture(now, "ledger-sent")
			sentAt := now.Add(-90 * time.Second)

			row := seedPendingClaimGateDelivery(t, db, &outbox, now.Add(-time.Minute))
			tc.seed(t, db, &outbox, postID, sentAt)
			recordServedClaimGateDelivery(t, db, &outbox, postID, row.RoomID, sentAt)

			require.Equal(t, 1, dispatcher.claim.processPendingDeliveries(t.Context()))
			require.Zero(t, sender.messageCount())
			requireClaimGateDeliverySent(t, db, row.ID)

			var stateCount int64

			require.NoError(t, countDeliveryTestRowsWhere(db, &domain.YouTubeCommunityShortsAlarmState{}, &stateCount,
				"kind = $1 AND post_id = $2 AND alarm_sent_at IS NULL", outbox.Kind, postID).Error)
			require.Zero(t, stateCount, "fulfilled room must not leave an unsent post-level alarm state")
		})
	}
}

// 묶음 발송 후보 중 이 room에 SENT 원장이 있는 post만 빠지고 나머지 post는 발송된다.
func TestProcessPendingDeliveriesFiltersSentLedgerPostOutOfGroup(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Microsecond)
	sender := &claimGateTestSender{failRoom: map[string]bool{}}
	dispatcher, db := newPendingClaimGateDispatcher(t, sender)
	_, firstOutbox, firstPostID := newCommunityClaimGateFixture(now, "group-first")
	_, secondOutbox, _ := newCommunityClaimGateFixture(now, "group-second")

	secondOutbox.ChannelID = firstOutbox.ChannelID

	firstRow := seedPendingClaimGateDelivery(t, db, &firstOutbox, now.Add(-time.Minute))
	secondRow := seedPendingClaimGateDelivery(t, db, &secondOutbox, now.Add(-time.Minute))
	recordServedClaimGateDelivery(t, db, &firstOutbox, firstPostID, testRoomCommunity, now.Add(-90*time.Second))

	require.Equal(t, 2, dispatcher.claim.processPendingDeliveries(t.Context()))

	messages := sender.allMessages()
	require.Len(t, messages, 1)
	require.Contains(t, messages[0], "body-group-second")
	require.NotContains(t, messages[0], "body-group-first")
	requireClaimGateDeliverySent(t, db, firstRow.ID)
	requireClaimGateDeliverySent(t, db, secondRow.ID)
}

// 이미 발송된 post라도 다른 room만 받았다면 이 room에도 발송하고 원장을 남긴다. 이때 post 단위 발송 시각은 가장 이른 값을 유지한다.
func TestProcessPendingDeliveriesSendsSentPostToRoomWithoutLedger(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Microsecond)
	sender := &claimGateTestSender{failRoom: map[string]bool{}}
	dispatcher, db := newPendingClaimGateDispatcher(t, sender)
	_, outbox, postID := newCommunityClaimGateFixture(now, "room-scoped")
	alarmSentAt := now.Add(-90 * time.Second)

	row := seedPendingClaimGateDelivery(t, db, &outbox, now.Add(-time.Minute))
	require.NoError(t, insertDeliveryTestRows(db, &domain.YouTubeCommunityShortsAlarmState{
		Kind:           outbox.Kind,
		PostID:         postID,
		ContentID:      outbox.ContentID,
		ChannelID:      outbox.ChannelID,
		DetectedAt:     now.Add(-3 * time.Minute),
		AlarmSentAt:    &alarmSentAt,
		DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusSent,
	}).Error)
	recordServedClaimGateDelivery(t, db, &outbox, postID, "room-other", alarmSentAt)

	require.Equal(t, 1, dispatcher.claim.processPendingDeliveries(t.Context()))
	require.Equal(t, 1, sender.messageCount())
	require.Contains(t, sender.allMessages()[0], row.RoomID+":")
	requireClaimGateDeliverySent(t, db, row.ID)
	require.Equal(t, 1, countClaimGateSentLedger(t, db, outbox.Kind, postID, row.RoomID))

	var state domain.YouTubeCommunityShortsAlarmState

	require.NoError(t, firstDeliveryTestRow(db, &state, "kind = $1 AND post_id = $2", outbox.Kind, postID).Error)
	require.NotNil(t, state.AlarmSentAt)
	require.Equal(t, alarmSentAt, state.AlarmSentAt.UTC())
}

// prepare 뒤 다른 실행이 같은 키를 SENT로 기록하면 BeginSending이 원장을 잠가 발송을 막고,
// 잠금이 만료된 뒤 다시 claim·prepare할 때 행이 SENT로 수렴한다.
func TestProcessPendingDeliveriesSentLedgerAfterPrepareBlocksSend(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Microsecond)
	sender := &claimGateTestSender{failRoom: map[string]bool{}}
	dispatcher, db := newPendingClaimGateDispatcher(t, sender)
	_, outbox, postID := newCommunityClaimGateFixture(now, "ledger-race")

	row := seedPendingClaimGateDelivery(t, db, &outbox, now.Add(-time.Minute))

	claimed, err := dispatcher.claim.transition.ClaimPending(ctx, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	outboxByID := map[int64]domain.YouTubeNotificationOutbox{outbox.ID: outbox}
	prepared, err := dispatcher.claim.transition.PrepareClaimed(ctx, claimed, outboxByID)
	require.NoError(t, err)
	require.Len(t, prepared.ActiveRows, 1)

	recordServedClaimGateDelivery(t, db, &outbox, postID, row.RoomID, now.Add(-time.Second))

	result := dispatcher.claim.dispatchDeliveryRows(ctx, prepared.ActiveRows, outboxByID)

	require.Zero(t, sender.messageCount())
	require.Empty(t, result.SuccessDeliveryIDs)

	pending := loadClaimGateDeliveryRow(t, db, row.ID)
	require.Equal(t, string(domain.OutboxStatusPending), pending.Status)
	require.NotNil(t, pending.LockedAt)

	// 잠금 만료를 기다리지 않도록 claim 시각만 과거로 옮긴다.
	_, err = db.Exec(ctx, "UPDATE youtube_notification_delivery SET locked_at = $2 WHERE id = $1", row.ID, now.Add(-time.Hour))
	require.NoError(t, err)

	require.Equal(t, 1, dispatcher.claim.processPendingDeliveries(ctx))
	require.Zero(t, sender.messageCount())
	requireClaimGateDeliverySent(t, db, row.ID)
}

func newTwoRoomClaimGateOutbox(t *testing.T, db *pgxpool.Pool, suffix string, now time.Time) (outbox domain.YouTubeNotificationOutbox, postID string) {
	t.Helper()

	contentID := "post-" + suffix

	postID = "community:" + contentID
	outbox = domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindCommunityPost,
		ChannelID:     "UC_COMMUNITY_" + suffix,
		ContentID:     contentID,
		Payload:       fmt.Sprintf(`{"canonical_post_id":%q,"post_id":%q,"content_text":"body-%s"}`, postID, contentID, suffix),
		Status:        domain.OutboxStatusPending,
		NextAttemptAt: now,
		CreatedAt:     now,
	}
	require.NoError(t, insertDeliveryTestRows(db, &outbox).Error)
	require.NoError(t, insertDeliveryTestRows(db, &deliveryTestTrackingModel{
		Kind:               string(outbox.Kind),
		ContentID:          outbox.ContentID,
		CanonicalContentID: postID,
		ChannelID:          outbox.ChannelID,
		DetectedAt:         now,
	}).Error)

	return outbox, postID
}

func loadClaimGateDeliveryRow(t *testing.T, db *pgxpool.Pool, id int64) deliveryTestDeliveryModel {
	t.Helper()

	var row deliveryTestDeliveryModel

	require.NoError(t, firstDeliveryTestRow(db, &row, id).Error)

	return row
}

func TestProcessPendingDeliveriesSendsPostToEveryRoomAcrossBatches(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Microsecond)
	sender := &claimGateTestSender{failRoom: map[string]bool{}}
	dispatcher, db := newClaimGateTestDispatcher(t, sender, &Config{BatchSize: 1, DeliveryParallelism: 1})

	dispatcher.send.transition = dispatcher.claim.transition

	outbox, postID := newTwoRoomClaimGateOutbox(t, db, "cross-batch", now.Add(-time.Minute))

	rooms := []string{"room-batch-a", "room-batch-b"}
	deliveries := make([]domain.YouTubeNotificationDelivery, 0, len(rooms))

	for _, roomID := range rooms {
		delivery := domain.YouTubeNotificationDelivery{
			OutboxID:      outbox.ID,
			RoomID:        roomID,
			Status:        domain.OutboxStatusPending,
			NextAttemptAt: now.Add(-time.Minute),
			CreatedAt:     now.Add(-time.Minute),
		}
		require.NoError(t, insertDeliveryTestRows(db, &delivery).Error)

		deliveries = append(deliveries, delivery)
	}

	require.Equal(t, 1, dispatcher.claim.processPendingDeliveries(ctx))
	require.Equal(t, 1, sender.messageCount())

	var state domain.YouTubeCommunityShortsAlarmState

	require.NoError(t, firstDeliveryTestRow(db, &state, "kind = $1 AND post_id = $2", outbox.Kind, postID).Error)
	require.NotNil(t, state.AlarmSentAt, "first batch must complete the post-level alarm-once state")

	require.Equal(t, 1, dispatcher.claim.processPendingDeliveries(ctx))
	require.Equal(t, 2, sender.messageCount())

	sentRooms := make([]string, 0, len(rooms))

	for _, message := range sender.allMessages() {
		sentRooms = append(sentRooms, message[:len("room-batch-x")])
	}

	require.ElementsMatch(t, rooms, sentRooms)

	for i := range deliveries {
		row := loadClaimGateDeliveryRow(t, db, deliveries[i].ID)
		require.Equal(t, string(domain.OutboxStatusSent), row.Status, row.RoomID)
		require.NotNil(t, row.SentAt, row.RoomID)
		require.Nil(t, row.LockedAt, row.RoomID)
	}

	require.Zero(t, dispatcher.claim.processPendingDeliveries(ctx))
	require.Equal(t, 2, sender.messageCount())
}

// 한 방의 발송 결과가 불명이어도 같은 post의 다른 방은 다음 라운드에서 미뤄지지 않고 바로 발송된다.
func TestOutcomeUnknownInOneRoomDoesNotDeferOtherRoomOfSamePost(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Microsecond)
	unknownRoom, nextRoom := "room-unknown-a", "room-unknown-b"
	sender := &claimGateTestSender{unknownRoom: map[string]bool{unknownRoom: true}}
	dispatcher, db := newClaimGateTestDispatcher(t, sender, &Config{BatchSize: 1, DeliveryParallelism: 1})

	dispatcher.send.transition = dispatcher.claim.transition

	outbox, postID := newTwoRoomClaimGateOutbox(t, db, "unknown-split", now.Add(-2*time.Minute))

	// BatchSize 1에서 결과 불명 방이 먼저 claim되도록 due 시각을 앞에 둔다.
	deliveries := make([]domain.YouTubeNotificationDelivery, 0, 2)

	for i, roomID := range []string{unknownRoom, nextRoom} {
		at := now.Add(-time.Duration(2-i) * time.Minute)
		delivery := domain.YouTubeNotificationDelivery{
			OutboxID: outbox.ID, RoomID: roomID, Status: domain.OutboxStatusPending, NextAttemptAt: at, CreatedAt: at,
		}
		require.NoError(t, insertDeliveryTestRows(db, &delivery).Error)

		deliveries = append(deliveries, delivery)
	}

	require.Equal(t, 1, dispatcher.claim.processPendingDeliveries(ctx))
	require.Zero(t, sender.messageCount())

	unknown := loadClaimGateDeliveryRow(t, db, deliveries[0].ID)
	require.Equal(t, string(ytlifecycle.StatusSending), unknown.Status, "outcome unknown must stay SENDING for the stale sweep")

	require.Equal(t, 1, dispatcher.claim.processPendingDeliveries(ctx))

	messages := sender.allMessages()
	require.Len(t, messages, 1)
	require.True(t, strings.HasPrefix(messages[0], nextRoom+":"), messages[0])
	requireClaimGateDeliverySent(t, db, deliveries[1].ID)
	require.Equal(t, 1, countClaimGateSentLedger(t, db, outbox.Kind, postID, nextRoom))
	require.Zero(t, countClaimGateSentLedger(t, db, outbox.Kind, postID, unknownRoom))

	var state domain.YouTubeCommunityShortsAlarmState

	require.NoError(t, firstDeliveryTestRow(db, &state, "kind = $1 AND post_id = $2", outbox.Kind, postID).Error)
	require.NotNil(t, state.AlarmSentAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, state.DeliveryStatus)
}

func (s *claimGateTestSender) PrepareMessageRequest(_ context.Context, _, body string) (string, string, error) {
	return body, testPreparedTextRoute, nil
}

func (s *claimGateTestSender) SendPreparedMessage(ctx context.Context, room, body, _, id string) (string, error) {
	return testAdmission(id, s.SendMessage(ctx, room, body))
}
