package youtubedispatch

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/iris-client-go/v3/iris"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
)

type claimGateTestSender struct {
	mu       sync.Mutex
	failRoom map[string]bool
	messages []string
}

func (s *claimGateTestSender) SendMessage(_ context.Context, roomID, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failRoom[roomID] {
		return fmt.Errorf("%s: %w", testSendFailedMessage, iris.ErrRateLimited)
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

func newClaimGateTestDispatcher(t *testing.T, sender *claimGateTestSender, config *dispatchstate.Config) (*Dispatcher, *pgxpool.Pool) {
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

func newClaimGateTestDispatcherWithDB(t *testing.T, db *pgxpool.Pool, sender *claimGateTestSender, config *dispatchstate.Config) *Dispatcher {
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

	return dispatcher
}

func newSharedClaimGateTestDB(t *testing.T, maxOpenConns int) *pgxpool.Pool {
	t.Helper()

	_ = maxOpenConns

	db := newDeliveryPool(t)

	return db
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

	dispatcher, db := newClaimGateTestDispatcher(t, sender, &dispatchstate.Config{DeliveryParallelism: 1})

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

func TestDispatchDeliveryRowsClaimsCommunityPostBeforeSending(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 11, 1, 11, 12, 0, time.UTC)
	sender := &claimGateTestSender{failRoom: map[string]bool{}}
	dispatcher, db := newClaimGateTestDispatcher(t, sender, &dispatchstate.Config{})
	row, outbox, postID := newCommunityClaimGateFixture(now, "claim-win")

	result := dispatcher.send.dispatchDeliveryRows(t.Context(), []domain.YouTubeNotificationDelivery{row}, map[int64]domain.YouTubeNotificationOutbox{
		outbox.ID: outbox,
	})

	require.Equal(t, 1, sender.messageCount())
	require.Equal(t, []int64{row.ID}, result.SuccessDeliveryIDs)
	require.Zero(t, result.FailedDeliveries)

	var state domain.YouTubeCommunityShortsAlarmState

	require.NoError(t, firstDeliveryTestRow(db, &state, "kind = $1 AND post_id = $2", outbox.Kind, postID).Error)
	require.NotNil(t, state.AuthorizedAt)
	require.Nil(t, state.AlarmSentAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, state.DeliveryStatus)
}

func TestDispatchDeliveryRowsSkipsShortWhenAnotherExecutionOwnsRecentClaim(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	sender := &claimGateTestSender{failRoom: map[string]bool{}}
	dispatcher, db := newClaimGateTestDispatcher(t, sender, &dispatchstate.Config{LockTimeout: 5 * time.Minute})
	row, outbox, postID := newShortClaimGateFixture(now, "recent-claim")
	authorizedAt := now.Add(-30 * time.Second)
	detectedAt := now.Add(-2 * time.Minute)
	require.NoError(t, insertDeliveryTestRows(db, &domain.YouTubeCommunityShortsAlarmState{
		Kind:           outbox.Kind,
		PostID:         postID,
		ContentID:      outbox.ContentID,
		ChannelID:      outbox.ChannelID,
		DetectedAt:     detectedAt,
		AuthorizedAt:   &authorizedAt,
		DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusEnqueued,
	}).Error)

	result := dispatcher.send.dispatchDeliveryRows(t.Context(), []domain.YouTubeNotificationDelivery{row}, map[int64]domain.YouTubeNotificationOutbox{
		outbox.ID: outbox,
	})

	require.Zero(t, sender.messageCount())
	require.Empty(t, result.SuccessDeliveryIDs)
	require.Equal(t, 1, result.FailedDeliveries)
	require.Equal(t, []int64{row.ID}, result.FailureBuckets[deliveryFailureReasonPreSendClaim])

	var state domain.YouTubeCommunityShortsAlarmState

	require.NoError(t, firstDeliveryTestRow(db, &state, "kind = $1 AND post_id = $2", outbox.Kind, postID).Error)
	require.NotNil(t, state.AuthorizedAt)
	require.Equal(t, authorizedAt, state.AuthorizedAt.UTC())
	require.Nil(t, state.AlarmSentAt)
}

func TestDispatchDeliveryRowsReleasesClaimAfterSendFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 11, 1, 11, 12, 0, time.UTC)
	sender := &claimGateTestSender{failRoom: map[string]bool{testRoomCommunity: true}}
	dispatcher, db := newClaimGateTestDispatcher(t, sender, &dispatchstate.Config{})
	row, outbox, postID := newCommunityClaimGateFixture(now, "release-on-fail")

	result := dispatcher.send.dispatchDeliveryRows(t.Context(), []domain.YouTubeNotificationDelivery{row}, map[int64]domain.YouTubeNotificationOutbox{
		outbox.ID: outbox,
	})

	require.Zero(t, sender.messageCount())
	require.Empty(t, result.SuccessDeliveryIDs)
	require.Equal(t, 1, result.FailedDeliveries)
	require.Equal(t, []int64{row.ID}, result.FailureBuckets[deliveryReasonRateLimited])

	var state domain.YouTubeCommunityShortsAlarmState

	require.NoError(t, firstDeliveryTestRow(db, &state, "kind = $1 AND post_id = $2", outbox.Kind, postID).Error)
	require.Nil(t, state.AuthorizedAt)
	require.Nil(t, state.AlarmSentAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusDetected, state.DeliveryStatus)
}

func TestDispatchDeliveryRowsReclaimsStaleLegacyAuthorizationBeforeSending(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	sender := &claimGateTestSender{failRoom: map[string]bool{}}
	dispatcher, db := newClaimGateTestDispatcher(t, sender, &dispatchstate.Config{LockTimeout: 2 * time.Minute})
	row, outbox, postID := newCommunityClaimGateFixture(now, "stale-claim")
	staleAuthorizedAt := now.Add(-10 * time.Minute)
	detectedAt := now.Add(-11 * time.Minute)
	require.NoError(t, insertDeliveryTestRows(db, &domain.YouTubeCommunityShortsAlarmState{
		Kind:           outbox.Kind,
		PostID:         postID,
		ContentID:      outbox.ContentID,
		ChannelID:      outbox.ChannelID,
		DetectedAt:     detectedAt,
		AuthorizedAt:   &staleAuthorizedAt,
		DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusEnqueued,
	}).Error)

	result := dispatcher.send.dispatchDeliveryRows(t.Context(), []domain.YouTubeNotificationDelivery{row}, map[int64]domain.YouTubeNotificationOutbox{
		outbox.ID: outbox,
	})

	require.Equal(t, 1, sender.messageCount())
	require.Equal(t, []int64{row.ID}, result.SuccessDeliveryIDs)
	require.Zero(t, result.FailedDeliveries)

	var state domain.YouTubeCommunityShortsAlarmState

	require.NoError(t, firstDeliveryTestRow(db, &state, "kind = $1 AND post_id = $2", outbox.Kind, postID).Error)
	require.NotNil(t, state.AuthorizedAt)
	require.True(t, state.AuthorizedAt.UTC().After(staleAuthorizedAt))
	require.Nil(t, state.AlarmSentAt)
}

func TestDispatchDeliveryRowsConcurrentExecutionsStartCommunityShortsDeliveryOncePerPost(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		fixture func(now time.Time, suffix string) (domain.YouTubeNotificationDelivery, domain.YouTubeNotificationOutbox, string)
	}{
		{name: "community post", fixture: newCommunityClaimGateFixture},
		{name: "short", fixture: newShortClaimGateFixture},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			now := time.Date(2026, time.April, 11, 1, 11, 12, 0, time.UTC)
			sender := &claimGateTestSender{failRoom: map[string]bool{}}
			db := newSharedClaimGateTestDB(t, 8)
			dispatchers := []*Dispatcher{
				newClaimGateTestDispatcherWithDB(t, db, sender, &dispatchstate.Config{}),
				newClaimGateTestDispatcherWithDB(t, db, sender, &dispatchstate.Config{}),
			}
			row, outbox, postID := tc.fixture(now, "race")
			results := make([]dispatchstate.DispatchResult, len(dispatchers))

			start := make(chan struct{})

			var wg sync.WaitGroup

			for i := range dispatchers {
				wg.Go(func() {
					<-start

					results[i] = dispatchers[i].send.dispatchDeliveryRows(t.Context(), []domain.YouTubeNotificationDelivery{row}, map[int64]domain.YouTubeNotificationOutbox{
						outbox.ID: outbox,
					})
				})
			}

			close(start)
			wg.Wait()

			totalSuccesses := 0
			totalFailures := 0
			preSendClaimFailures := 0

			for i := range results {
				totalSuccesses += len(results[i].SuccessDeliveryIDs)
				totalFailures += results[i].FailedDeliveries
				preSendClaimFailures += len(results[i].FailureBuckets[deliveryFailureReasonPreSendClaim])
			}

			require.Equal(t, 1, sender.messageCount())
			require.Equal(t, 1, totalSuccesses)
			require.Equal(t, 1, totalFailures)
			require.Equal(t, 1, preSendClaimFailures)

			var state domain.YouTubeCommunityShortsAlarmState

			require.NoError(t, firstDeliveryTestRow(db, &state, "kind = $1 AND post_id = $2", outbox.Kind, postID).Error)
			require.Equal(t, postID, state.PostID)
			require.Equal(t, outbox.ContentID, state.ContentID)
			require.NotNil(t, state.AuthorizedAt)
			require.Nil(t, state.AlarmSentAt)
			require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, state.DeliveryStatus)
		})
	}
}

func TestSelectClaimedDeliveriesTracksRowClaimOwnership(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 11, 1, 11, 12, 0, time.UTC)
	sender := &claimGateTestSender{failRoom: map[string]bool{}}
	dispatcher, _ := newClaimGateTestDispatcher(t, sender, &dispatchstate.Config{})
	firstRow, firstOutbox, _ := newCommunityClaimGateFixture(now, "owned")
	secondRow, secondOutbox, _ := newCommunityClaimGateFixture(now, "other")
	duplicateRow, duplicateOutbox, _ := newCommunityClaimGateFixture(now, "owned")

	secondRow.ID = firstRow.ID + 1
	secondRow.OutboxID = firstOutbox.ID + 1
	secondOutbox.ID = secondRow.OutboxID
	secondRow.RoomID = "room-other"
	duplicateRow.ID = secondRow.ID + 1
	duplicateRow.OutboxID = secondRow.OutboxID + 1
	duplicateOutbox.ID = duplicateRow.OutboxID
	duplicateRow.RoomID = "room-duplicate"

	selection := dispatcher.claim.selectClaimedDeliveries(
		t.Context(),
		[]domain.YouTubeNotificationDelivery{firstRow, secondRow, duplicateRow},
		[]domain.YouTubeNotificationOutbox{firstOutbox, secondOutbox, duplicateOutbox},
		newClaimDecisionCache(),
	)

	require.Len(t, selection.sendRows, 3)
	require.Len(t, selection.claimTokens, 3)
	require.Len(t, selection.rowClaimTokens, 3)
	require.Len(t, selection.rowClaimTokens[0], 1)
	require.Len(t, selection.rowClaimTokens[1], 1)
	require.Len(t, selection.rowClaimTokens[2], 1)
	require.False(t, selection.rowClaimTokens[0][0].Reused)
	require.True(t, selection.rowClaimTokens[2][0].Reused)
	require.Equal(t, selection.rowClaimTokens[0][0].AuthorizedAt, selection.rowClaimTokens[2][0].AuthorizedAt)
}

func TestSelectClaimedDeliveriesHandlesNilInputs(t *testing.T) {
	t.Parallel()

	dispatcher, _ := newClaimGateTestDispatcher(t, &claimGateTestSender{failRoom: map[string]bool{}}, &dispatchstate.Config{})

	selection := dispatcher.claim.selectClaimedDeliveries(
		t.Context(),
		nil,
		nil,
		newClaimDecisionCache(),
	)

	require.Empty(t, selection.sendRows)
	require.Empty(t, selection.sendOutboxes)
	require.Empty(t, selection.claimTokens)
	require.Empty(t, selection.rowClaimTokens)
	require.Empty(t, selection.retryDeliveryIDs)
	require.Empty(t, selection.retryOutboxIDs)
}

func TestDispatchClaimedRowsIndividuallyReleasesOnlyOwnedClaimsOnFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 11, 1, 11, 12, 0, time.UTC)
	sender := &claimGateTestSender{failRoom: map[string]bool{"room-duplicate": true}}
	dispatcher, db := newClaimGateTestDispatcher(t, sender, &dispatchstate.Config{})
	firstRow, firstOutbox, firstPostID := newCommunityClaimGateFixture(now, "owned")
	secondRow, secondOutbox, secondPostID := newCommunityClaimGateFixture(now, "other")
	duplicateRow, duplicateOutbox, _ := newCommunityClaimGateFixture(now, "owned")

	secondRow.ID = firstRow.ID + 1
	secondRow.OutboxID = firstOutbox.ID + 1
	secondOutbox.ID = secondRow.OutboxID
	secondRow.RoomID = "room-other"
	duplicateRow.ID = secondRow.ID + 1
	duplicateRow.OutboxID = secondRow.OutboxID + 1
	duplicateOutbox.ID = duplicateRow.OutboxID
	duplicateRow.RoomID = "room-duplicate"

	selection := dispatcher.claim.selectClaimedDeliveries(
		t.Context(),
		[]domain.YouTubeNotificationDelivery{firstRow, secondRow, duplicateRow},
		[]domain.YouTubeNotificationOutbox{firstOutbox, secondOutbox, duplicateOutbox},
		newClaimDecisionCache(),
	)

	result := &dispatchstate.DispatchResult{FailureBuckets: make(map[string][]int64)}

	var mu sync.Mutex

	messages := map[int64]string{
		firstOutbox.ID:     "message-1",
		secondOutbox.ID:    "message-2",
		duplicateOutbox.ID: "message-3",
	}

	for i := range selection.sendRows {
		dispatcher.send.dispatchClaimedDeliveryRow(t.Context(), &selection.sendRows[i], &selection.sendOutboxes[i], messages, nil, selection.rowClaimTokens[i], result, &mu)
	}

	require.Equal(t, 2, sender.messageCount())
	require.ElementsMatch(t, []int64{firstRow.ID, secondRow.ID}, result.SuccessDeliveryIDs)
	require.Equal(t, []int64{duplicateRow.ID}, result.FailureBuckets[deliveryReasonRateLimited])

	var firstState domain.YouTubeCommunityShortsAlarmState

	require.NoError(t, firstDeliveryTestRow(db, &firstState, "kind = $1 AND post_id = $2", firstOutbox.Kind, firstPostID).Error)
	require.NotNil(t, firstState.AuthorizedAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, firstState.DeliveryStatus)

	var secondState domain.YouTubeCommunityShortsAlarmState

	require.NoError(t, firstDeliveryTestRow(db, &secondState, "kind = $1 AND post_id = $2", secondOutbox.Kind, secondPostID).Error)
	require.NotNil(t, secondState.AuthorizedAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, secondState.DeliveryStatus)
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

				authorizedAt := sentAt.Add(-30 * time.Second)
				require.NoError(t, insertDeliveryTestRows(db, &domain.YouTubeCommunityShortsAlarmState{
					Kind:           outbox.Kind,
					PostID:         postID,
					ContentID:      outbox.ContentID,
					ChannelID:      outbox.ChannelID,
					DetectedAt:     sentAt.Add(-time.Minute),
					AuthorizedAt:   &authorizedAt,
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
			require.Zero(t, stateCount, "fulfilled room must not reclaim the post-level alarm state")
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

// post 단위로는 이미 발송됐어도 다른 room만 받았다면 이 room에는 claim token 없이 발송하고 원장을 남긴다.
func TestProcessPendingDeliveriesSendsSentPostToRoomWithoutLedger(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Microsecond)
	sender := &claimGateTestSender{failRoom: map[string]bool{}}
	dispatcher, db := newPendingClaimGateDispatcher(t, sender)
	_, outbox, postID := newCommunityClaimGateFixture(now, "room-scoped")
	authorizedAt := now.Add(-2 * time.Minute)
	alarmSentAt := now.Add(-90 * time.Second)

	row := seedPendingClaimGateDelivery(t, db, &outbox, now.Add(-time.Minute))
	require.NoError(t, insertDeliveryTestRows(db, &domain.YouTubeCommunityShortsAlarmState{
		Kind:           outbox.Kind,
		PostID:         postID,
		ContentID:      outbox.ContentID,
		ChannelID:      outbox.ChannelID,
		DetectedAt:     now.Add(-3 * time.Minute),
		AuthorizedAt:   &authorizedAt,
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
	dispatcher, db := newClaimGateTestDispatcher(t, sender, &dispatchstate.Config{BatchSize: 1, DeliveryParallelism: 1})

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

func TestDispatchDeliveryRowsSkipsShortWhenAnotherExecutionOwnsRecentClaimDefersPersistedRow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Microsecond)
	sender := &claimGateTestSender{failRoom: map[string]bool{}}
	dispatcher, db := newClaimGateTestDispatcher(t, sender, &dispatchstate.Config{LockTimeout: 5 * time.Minute, RetryBackoff: time.Minute})
	row, outbox, postID := newShortClaimGateFixture(now, "recent-claim-persisted")
	authorizedAt := now.Add(-30 * time.Second)
	require.NoError(t, insertDeliveryTestRows(db, &domain.YouTubeCommunityShortsAlarmState{
		Kind:           outbox.Kind,
		PostID:         postID,
		ContentID:      outbox.ContentID,
		ChannelID:      outbox.ChannelID,
		DetectedAt:     now.Add(-2 * time.Minute),
		AuthorizedAt:   &authorizedAt,
		DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusEnqueued,
	}).Error)

	lockedAt := now

	row.Status = domain.OutboxStatusPending
	row.LockedAt = &lockedAt
	row.NextAttemptAt = now.Add(-time.Minute)
	row.AttemptCount = 1
	row.RowVersion = 1

	require.NoError(t, insertDeliveryTestRows(db, &outbox).Error)
	require.NoError(t, insertDeliveryTestRows(db, &row).Error)
	require.NoError(t, updateDeliveryTestRowsWhere(db, &domain.YouTubeNotificationDelivery{}, map[string]any{
		"row_version": 1,
	}, "id = $1", row.ID).Error)

	selection := dispatcher.claim.selectClaimedDeliveries(ctx, []domain.YouTubeNotificationDelivery{row}, []domain.YouTubeNotificationOutbox{outbox}, newClaimDecisionCache())

	require.Empty(t, selection.sendRows)
	require.Empty(t, selection.retryDeliveryIDs)
	require.Equal(t, []int64{row.ID}, selection.deferredDeliveryIDs)

	persisted := loadClaimGateDeliveryRow(t, db, row.ID)
	require.Equal(t, string(domain.OutboxStatusPending), persisted.Status)
	require.Equal(t, 1, persisted.AttemptCount)
	require.Nil(t, persisted.LockedAt)
	require.True(t, persisted.NextAttemptAt.After(now))
	require.Zero(t, sender.messageCount())
}

// 알람 상태는 정본 논리 ID로 조회한다. ID가 없거나 잘못되거나 불일치하면 실패한다.
func TestDeliveryClaimIdentityForOutboxRequiresCanonicalIdentity(t *testing.T) {
	t.Parallel()

	valid := []struct {
		name   string
		outbox domain.YouTubeNotificationOutbox
		postID string
	}{
		{
			name: "short",
			outbox: domain.YouTubeNotificationOutbox{
				Kind: domain.OutboxKindNewShort, ContentID: "short-a",
				Payload: `{"canonical_post_id":"short:short-a","video_id":"short-a"}`,
			},
			postID: "short:short-a",
		},
		{
			name: "community content id alias",
			outbox: domain.YouTubeNotificationOutbox{
				Kind: domain.OutboxKindCommunityPost, ContentID: "community:post-a",
				Payload: `{"canonical_post_id":"community:post-a","post_id":"post-a"}`,
			},
			postID: "community:post-a",
		},
	}
	for _, tc := range valid {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			identity, err := deliveryClaimIdentityForOutbox(&tc.outbox)
			require.NoError(t, err)
			require.Equal(t, store.DeliveryClaimIdentityKey(tc.outbox.Kind, tc.postID), identity)
		})
	}

	invalid := []struct {
		name   string
		outbox domain.YouTubeNotificationOutbox
	}{
		{
			name:   "short missing canonical",
			outbox: domain.YouTubeNotificationOutbox{Kind: domain.OutboxKindNewShort, ContentID: "short-a", Payload: `{"video_id":"short-a"}`},
		},
		{
			name:   "community missing canonical",
			outbox: domain.YouTubeNotificationOutbox{Kind: domain.OutboxKindCommunityPost, ContentID: "post-a", Payload: `{"post_id":"post-a"}`},
		},
		{
			name:   "malformed payload",
			outbox: domain.YouTubeNotificationOutbox{Kind: domain.OutboxKindCommunityPost, ContentID: "post-a", Payload: `{broken`},
		},
		{
			name: "canonical mismatch",
			outbox: domain.YouTubeNotificationOutbox{
				Kind: domain.OutboxKindNewShort, ContentID: "short-a", Payload: `{"canonical_post_id":"short:short-b"}`,
			},
		},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			identity, err := deliveryClaimIdentityForOutbox(&tc.outbox)
			require.Error(t, err)
			require.Empty(t, identity)
		})
	}
}

func (s *claimGateTestSender) PrepareMessageRequest(_ context.Context, _, body string) (string, string, error) {
	return body, testPreparedTextRoute, nil
}

func (s *claimGateTestSender) SendPreparedMessage(ctx context.Context, room, body, _, _ string) error {
	return s.SendMessage(ctx, room, body)
}
