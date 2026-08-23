package youtubedispatch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/claim"
	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/store"
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
		return errors.New("send failed")
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

func newClaimGateTestDispatcher(t *testing.T, sender *claimGateTestSender, config *dispatchstate.Config) (result1 *Dispatcher, result2 *deliveryTestDB) {
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

	dispatcher := NewDispatcher(
		db,
		cachemocks.NewLenientClient(),
		sender,
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)), config,
	)
	dispatcher.telemetry = nil
	return dispatcher, db
}

func newClaimGateTestDispatcherWithDB(t *testing.T, db *deliveryTestDB, sender *claimGateTestSender, config *dispatchstate.Config) *Dispatcher {
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

	dispatcher := NewDispatcher(
		db,
		cachemocks.NewLenientClient(),
		sender,
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)), config,
	)
	dispatcher.telemetry = nil
	return dispatcher
}

func newSharedClaimGateTestDB(t *testing.T, maxOpenConns int) *deliveryTestDB {
	t.Helper()

	_ = maxOpenConns
	db := newDeliveryPool(t)

	return db
}

func newCommunityClaimGateFixture(now time.Time, suffix string) (domain.YouTubeNotificationDelivery, domain.YouTubeNotificationOutbox, string) {
	contentID := "post-" + suffix
	postID := "community:" + contentID
	return domain.YouTubeNotificationDelivery{
		ID:        100 + int64(len(suffix)),
		OutboxID:  200 + int64(len(suffix)),
		RoomID:    "room-community",
		CreatedAt: now.Add(15 * time.Second),
	}, domain.YouTubeNotificationOutbox{
		ID:            200 + int64(len(suffix)),
		Kind:          domain.OutboxKindCommunityPost,
		ChannelID:     "UC_COMMUNITY",
		ContentID:     contentID,
		Payload:       fmt.Sprintf(`{"canonical_post_id":%q,"post_id":%q,"content_text":"body-%s"}`, postID, contentID, suffix),
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: now,
		CreatedAt:     now,
	}, postID
}

func newShortClaimGateFixture(now time.Time, suffix string) (domain.YouTubeNotificationDelivery, domain.YouTubeNotificationOutbox, string) {
	contentID := "short-" + suffix
	postID := "short:" + contentID
	return domain.YouTubeNotificationDelivery{
		ID:        300 + int64(len(suffix)),
		OutboxID:  400 + int64(len(suffix)),
		RoomID:    "room-shorts",
		CreatedAt: now.Add(15 * time.Second),
	}, domain.YouTubeNotificationOutbox{
		ID:            400 + int64(len(suffix)),
		Kind:          domain.OutboxKindNewShort,
		ChannelID:     "UC_SHORTS",
		ContentID:     contentID,
		Payload:       fmt.Sprintf(`{"canonical_post_id":%q,"video_id":%q,"title":"title-%s"}`, postID, contentID, suffix),
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: now,
		CreatedAt:     now,
	}, postID
}

func insertSentSiblingDelivery(t *testing.T, db *deliveryTestDB, outbox *domain.YouTubeNotificationOutbox, roomID string, sentAt time.Time) {
	t.Helper()

	sibling := domain.YouTubeNotificationOutbox{
		Kind:          outbox.Kind,
		ChannelID:     outbox.ChannelID,
		ContentID:     outbox.ContentID,
		Payload:       outbox.Payload,
		Status:        domain.OutboxStatusSent,
		NextAttemptAt: sentAt,
		CreatedAt:     sentAt.Add(-time.Minute),
		SentAt:        &sentAt,
	}
	require.NoError(t, insertDeliveryTestRows(db, &sibling).Error)
	require.NoError(t, insertDeliveryTestRows(db, &domain.YouTubeNotificationDelivery{
		OutboxID:      sibling.ID,
		RoomID:        roomID,
		Status:        domain.OutboxStatusSent,
		NextAttemptAt: sentAt,
		CreatedAt:     sentAt.Add(-time.Minute),
		SentAt:        &sentAt,
	}).Error)
}

func TestDispatchDeliveryRowsClaimsCommunityPostBeforeSending(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 11, 1, 11, 12, 0, time.UTC)
	sender := &claimGateTestSender{failRoom: map[string]bool{}}
	dispatcher, db := newClaimGateTestDispatcher(t, sender, &dispatchstate.Config{})
	row, outbox, postID := newCommunityClaimGateFixture(now, "claim-win")

	result := dispatcher.send.dispatchDeliveryRows(context.Background(), []domain.YouTubeNotificationDelivery{row}, map[int64]domain.YouTubeNotificationOutbox{
		outbox.ID: outbox,
	})

	require.Equal(t, 1, sender.messageCount())
	require.Equal(t, []int64{row.ID}, result.SuccessDeliveryIDs)
	require.Zero(t, result.FailedDeliveries)

	var state domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, firstDeliveryTestRow(db, &state, "kind = ? AND post_id = ?", outbox.Kind, postID).Error)
	require.NotNil(t, state.AuthorizedAt)
	require.Nil(t, state.AlarmSentAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, state.DeliveryStatus)
}

func TestDispatchDeliveryRowsSkipsShortWhenAnotherExecutionOwnsRecentClaim(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 11, 1, 11, 12, 0, time.UTC)
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

	result := dispatcher.send.dispatchDeliveryRows(context.Background(), []domain.YouTubeNotificationDelivery{row}, map[int64]domain.YouTubeNotificationOutbox{
		outbox.ID: outbox,
	})

	require.Zero(t, sender.messageCount())
	require.Empty(t, result.SuccessDeliveryIDs)
	require.Equal(t, 1, result.FailedDeliveries)
	require.Equal(t, []int64{row.ID}, result.FailureBuckets[deliveryFailureReasonPreSendClaim])

	var state domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, firstDeliveryTestRow(db, &state, "kind = ? AND post_id = ?", outbox.Kind, postID).Error)
	require.NotNil(t, state.AuthorizedAt)
	require.Equal(t, authorizedAt, state.AuthorizedAt.UTC())
	require.Nil(t, state.AlarmSentAt)
}

func TestDispatchDeliveryRowsSkipsAlreadySentDuplicateWithoutSending(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 11, 1, 11, 12, 0, time.UTC)
	sender := &claimGateTestSender{failRoom: map[string]bool{}}
	dispatcher, db := newClaimGateTestDispatcher(t, sender, &dispatchstate.Config{})
	row, outbox, postID := newCommunityClaimGateFixture(now, "already-sent")
	authorizedAt := now.Add(-2 * time.Minute)
	alarmSentAt := now.Add(-90 * time.Second)
	detectedAt := now.Add(-3 * time.Minute)
	require.NoError(t, insertDeliveryTestRows(db, &domain.YouTubeCommunityShortsAlarmState{
		Kind:           outbox.Kind,
		PostID:         postID,
		ContentID:      outbox.ContentID,
		ChannelID:      outbox.ChannelID,
		DetectedAt:     detectedAt,
		AuthorizedAt:   &authorizedAt,
		AlarmSentAt:    &alarmSentAt,
		DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusSent,
	}).Error)
	insertSentSiblingDelivery(t, db, &outbox, row.RoomID, alarmSentAt)

	result := dispatcher.send.dispatchDeliveryRows(context.Background(), []domain.YouTubeNotificationDelivery{row}, map[int64]domain.YouTubeNotificationOutbox{
		outbox.ID: outbox,
	})

	require.Zero(t, sender.messageCount())
	require.Equal(t, []int64{row.ID}, result.SuccessDeliveryIDs)
	require.Zero(t, result.FailedDeliveries)
}

func TestDispatchDeliveryRowsSkipsAlreadySentTrackingRowWithoutReclaim(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 11, 1, 11, 12, 0, time.UTC)
	sender := &claimGateTestSender{failRoom: map[string]bool{}}
	dispatcher, db := newClaimGateTestDispatcher(t, sender, &dispatchstate.Config{})
	row, outbox, postID := newShortClaimGateFixture(now, "tracking-already-sent")
	detectedAt := now.Add(-3 * time.Minute)
	alarmSentAt := now.Add(-90 * time.Second)
	require.NoError(t, insertDeliveryTestRows(db, &domain.YouTubeContentAlarmTracking{
		Kind:               outbox.Kind,
		ContentID:          outbox.ContentID,
		CanonicalContentID: postID,
		ChannelID:          outbox.ChannelID,
		DetectedAt:         detectedAt,
		AlarmSentAt:        &alarmSentAt,
		DeliveryStatus:     domain.YouTubeContentAlarmDeliveryStatusSent,
	}).Error)
	insertSentSiblingDelivery(t, db, &outbox, row.RoomID, alarmSentAt)

	result := dispatcher.send.dispatchDeliveryRows(context.Background(), []domain.YouTubeNotificationDelivery{row}, map[int64]domain.YouTubeNotificationOutbox{
		outbox.ID: outbox,
	})

	require.Zero(t, sender.messageCount())
	require.Equal(t, []int64{row.ID}, result.SuccessDeliveryIDs)
	require.Zero(t, result.FailedDeliveries)

	var stateCount int64
	require.NoError(t, countDeliveryTestRowsWhere(db, &domain.YouTubeCommunityShortsAlarmState{}, &stateCount, "kind = ? AND post_id = ?", outbox.Kind, postID).Error)
	require.Zero(t, stateCount)
}

func TestDispatchDeliveryRowsReleasesClaimAfterSendFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 11, 1, 11, 12, 0, time.UTC)
	sender := &claimGateTestSender{failRoom: map[string]bool{"room-community": true}}
	dispatcher, db := newClaimGateTestDispatcher(t, sender, &dispatchstate.Config{})
	row, outbox, postID := newCommunityClaimGateFixture(now, "release-on-fail")

	result := dispatcher.send.dispatchDeliveryRows(context.Background(), []domain.YouTubeNotificationDelivery{row}, map[int64]domain.YouTubeNotificationOutbox{
		outbox.ID: outbox,
	})

	require.Zero(t, sender.messageCount())
	require.Empty(t, result.SuccessDeliveryIDs)
	require.Equal(t, 1, result.FailedDeliveries)
	require.Equal(t, []int64{row.ID}, result.FailureBuckets["send message"])

	var state domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, firstDeliveryTestRow(db, &state, "kind = ? AND post_id = ?", outbox.Kind, postID).Error)
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

	result := dispatcher.send.dispatchDeliveryRows(context.Background(), []domain.YouTubeNotificationDelivery{row}, map[int64]domain.YouTubeNotificationOutbox{
		outbox.ID: outbox,
	})

	require.Equal(t, 1, sender.messageCount())
	require.Equal(t, []int64{row.ID}, result.SuccessDeliveryIDs)
	require.Zero(t, result.FailedDeliveries)

	var state domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, firstDeliveryTestRow(db, &state, "kind = ? AND post_id = ?", outbox.Kind, postID).Error)
	require.NotNil(t, state.AuthorizedAt)
	require.True(t, state.AuthorizedAt.UTC().After(staleAuthorizedAt))
	require.Nil(t, state.AlarmSentAt)
}

func TestDispatchDeliveryRowsGroupedSendFiltersOutAlreadySentDuplicate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 11, 1, 11, 12, 0, time.UTC)
	sender := &claimGateTestSender{failRoom: map[string]bool{}}
	dispatcher, db := newClaimGateTestDispatcher(t, sender, &dispatchstate.Config{})
	firstRow, firstOutbox, firstPostID := newCommunityClaimGateFixture(now, "group-first")
	secondRow, secondOutbox, _ := newCommunityClaimGateFixture(now, "group-second")
	secondRow.ID = firstRow.ID + 1
	secondRow.OutboxID = firstOutbox.ID + 1
	secondOutbox.ID = secondRow.OutboxID
	secondOutbox.ChannelID = firstOutbox.ChannelID
	secondRow.RoomID = firstRow.RoomID
	firstAuthorizedAt := now.Add(-2 * time.Minute)
	firstAlarmSentAt := now.Add(-90 * time.Second)
	require.NoError(t, insertDeliveryTestRows(db, &domain.YouTubeCommunityShortsAlarmState{
		Kind:           firstOutbox.Kind,
		PostID:         firstPostID,
		ContentID:      firstOutbox.ContentID,
		ChannelID:      firstOutbox.ChannelID,
		DetectedAt:     now.Add(-3 * time.Minute),
		AuthorizedAt:   &firstAuthorizedAt,
		AlarmSentAt:    &firstAlarmSentAt,
		DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusSent,
	}).Error)
	insertSentSiblingDelivery(t, db, &firstOutbox, firstRow.RoomID, firstAlarmSentAt)

	result := dispatcher.send.dispatchDeliveryRows(context.Background(), []domain.YouTubeNotificationDelivery{firstRow, secondRow}, map[int64]domain.YouTubeNotificationOutbox{
		firstOutbox.ID:  firstOutbox,
		secondOutbox.ID: secondOutbox,
	})

	require.Equal(t, 1, sender.messageCount())
	require.ElementsMatch(t, []int64{firstRow.ID, secondRow.ID}, result.SuccessDeliveryIDs)
	require.Zero(t, result.FailedDeliveries)
	messages := sender.allMessages()
	require.Len(t, messages, 1)
	require.Contains(t, messages[0], "body-group-second")
	require.NotContains(t, messages[0], "body-group-first")
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
			now := time.Date(2026, 4, 11, 1, 11, 12, 0, time.UTC)
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
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					<-start
					results[idx] = dispatchers[idx].send.dispatchDeliveryRows(context.Background(), []domain.YouTubeNotificationDelivery{row}, map[int64]domain.YouTubeNotificationOutbox{
						outbox.ID: outbox,
					})
				}(i)
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
			require.NoError(t, firstDeliveryTestRow(db, &state, "kind = ? AND post_id = ?", outbox.Kind, postID).Error)
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

	now := time.Date(2026, 4, 11, 1, 11, 12, 0, time.UTC)
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
		context.Background(),
		[]domain.YouTubeNotificationDelivery{firstRow, secondRow, duplicateRow},
		[]domain.YouTubeNotificationOutbox{firstOutbox, secondOutbox, duplicateOutbox},
		claim.NewMemoryDecisionCache(),
	)

	require.Len(t, selection.sendRows, 3)
	require.Len(t, selection.claimTokens, 2)
	require.Len(t, selection.rowClaimTokens, 3)
	require.Len(t, selection.rowClaimTokens[0], 1)
	require.Len(t, selection.rowClaimTokens[1], 1)
	require.Empty(t, selection.rowClaimTokens[2])
}

func TestSelectClaimedDeliveriesHandlesNilInputs(t *testing.T) {
	t.Parallel()

	dispatcher, _ := newClaimGateTestDispatcher(t, &claimGateTestSender{failRoom: map[string]bool{}}, &dispatchstate.Config{})

	selection := dispatcher.claim.selectClaimedDeliveries(
		context.Background(),
		nil,
		nil,
		claim.NewMemoryDecisionCache(),
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

	now := time.Date(2026, 4, 11, 1, 11, 12, 0, time.UTC)
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
		context.Background(),
		[]domain.YouTubeNotificationDelivery{firstRow, secondRow, duplicateRow},
		[]domain.YouTubeNotificationOutbox{firstOutbox, secondOutbox, duplicateOutbox},
		claim.NewMemoryDecisionCache(),
	)

	result := &dispatchstate.DispatchResult{FailureBuckets: make(map[string][]int64)}
	var mu sync.Mutex
	dispatcher.send.dispatchClaimedRowsIndividually(
		context.Background(),
		selection.sendRows,
		selection.sendOutboxes,
		map[int64]string{
			firstOutbox.ID:     "message-1",
			secondOutbox.ID:    "message-2",
			duplicateOutbox.ID: "message-3",
		},
		map[int64]bool{},
		selection.rowClaimTokens,
		result,
		&mu,
	)

	require.Equal(t, 2, sender.messageCount())
	require.ElementsMatch(t, []int64{firstRow.ID, secondRow.ID}, result.SuccessDeliveryIDs)
	require.Equal(t, []int64{duplicateRow.ID}, result.FailureBuckets["send message"])

	var firstState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, firstDeliveryTestRow(db, &firstState, "kind = ? AND post_id = ?", firstOutbox.Kind, firstPostID).Error)
	require.NotNil(t, firstState.AuthorizedAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, firstState.DeliveryStatus)

	var secondState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, firstDeliveryTestRow(db, &secondState, "kind = ? AND post_id = ?", secondOutbox.Kind, secondPostID).Error)
	require.NotNil(t, secondState.AuthorizedAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, secondState.DeliveryStatus)
}

func TestDispatchDeliveryRowsSendsAlreadySentPostToRoomWithoutSentRow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 11, 1, 11, 12, 0, time.UTC)
	sender := &claimGateTestSender{failRoom: map[string]bool{}}
	dispatcher, db := newClaimGateTestDispatcher(t, sender, &dispatchstate.Config{})
	row, outbox, postID := newCommunityClaimGateFixture(now, "room-scoped")
	authorizedAt := now.Add(-2 * time.Minute)
	alarmSentAt := now.Add(-90 * time.Second)
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
	insertSentSiblingDelivery(t, db, &outbox, "room-other", alarmSentAt)

	result := dispatcher.send.dispatchDeliveryRows(context.Background(), []domain.YouTubeNotificationDelivery{row}, map[int64]domain.YouTubeNotificationOutbox{
		outbox.ID: outbox,
	})

	require.Equal(t, 1, sender.messageCount())
	require.Contains(t, sender.allMessages()[0], row.RoomID+":")
	require.Equal(t, []int64{row.ID}, result.SuccessDeliveryIDs)
	require.Empty(t, result.SuccessClaimTokens)
	require.Zero(t, result.FailedDeliveries)

	var state domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, firstDeliveryTestRow(db, &state, "kind = ? AND post_id = ?", outbox.Kind, postID).Error)
	require.NotNil(t, state.AlarmSentAt)
	require.Equal(t, alarmSentAt, state.AlarmSentAt.UTC())
}

func newTwoRoomClaimGateOutbox(t *testing.T, db *deliveryTestDB, suffix string, now time.Time) (outbox domain.YouTubeNotificationOutbox, postID string) {
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

func loadClaimGateDeliveryRow(t *testing.T, db *deliveryTestDB, id int64) deliveryTestDeliveryModel {
	t.Helper()

	var row deliveryTestDeliveryModel
	require.NoError(t, firstDeliveryTestRow(db, &row, id).Error)
	return row
}

func TestProcessPendingDeliveriesSendsPostToEveryRoomAcrossBatches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	sender := &claimGateTestSender{failRoom: map[string]bool{}}
	dispatcher, db := newClaimGateTestDispatcher(t, sender, &dispatchstate.Config{BatchSize: 1, DeliveryParallelism: 1})
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
	require.NoError(t, firstDeliveryTestRow(db, &state, "kind = ? AND post_id = ?", outbox.Kind, postID).Error)
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

func TestDispatchDeliveryRowsContendingWorkerDefersRoomWithoutConsumingAttempt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	sender := &claimGateTestSender{failRoom: map[string]bool{}}
	db := newSharedClaimGateTestDB(t, 8)
	config := func() *dispatchstate.Config {
		return &dispatchstate.Config{BatchSize: 1, DeliveryParallelism: 1, RetryBackoff: time.Minute}
	}
	dispatchers := []*Dispatcher{
		newClaimGateTestDispatcherWithDB(t, db, sender, config()),
		newClaimGateTestDispatcherWithDB(t, db, sender, config()),
	}
	outbox, postID := newTwoRoomClaimGateOutbox(t, db, "contend", now.Add(-time.Minute))

	lockedAt := now
	rooms := []string{"room-contend-a", "room-contend-b"}
	rows := make([]domain.YouTubeNotificationDelivery, 0, len(rooms))
	for _, roomID := range rooms {
		delivery := domain.YouTubeNotificationDelivery{
			OutboxID:      outbox.ID,
			RoomID:        roomID,
			Status:        store.DeliveryStatusSending,
			NextAttemptAt: now.Add(-time.Minute),
			CreatedAt:     now.Add(-time.Minute),
			LockedAt:      &lockedAt,
		}
		require.NoError(t, insertDeliveryTestRows(db, &delivery).Error)
		rows = append(rows, delivery)
	}
	outboxByID := map[int64]domain.YouTubeNotificationOutbox{outbox.ID: outbox}

	results := make([]dispatchstate.DispatchResult, len(dispatchers))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range dispatchers {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			results[idx] = dispatchers[idx].send.dispatchDeliveryRows(ctx, []domain.YouTubeNotificationDelivery{rows[idx]}, outboxByID)
		}(i)
	}
	close(start)
	wg.Wait()

	require.Equal(t, 1, sender.messageCount())
	winner, loser := -1, -1
	for i := range results {
		require.Zero(t, results[i].FailedDeliveries, "contention must not be accounted as a delivery failure")
		require.Empty(t, results[i].FailureBuckets[deliveryFailureReasonPreSendClaim])
		if len(results[i].SuccessDeliveryIDs) == 1 {
			winner = i
		} else {
			loser = i
		}
	}
	require.NotEqual(t, -1, winner)
	require.NotEqual(t, -1, loser)
	require.Equal(t, []int64{rows[winner].ID}, results[winner].SuccessDeliveryIDs)
	require.Len(t, results[winner].SuccessClaimTokens, 1)

	deferred := loadClaimGateDeliveryRow(t, db, rows[loser].ID)
	require.Equal(t, string(domain.OutboxStatusPending), deferred.Status)
	require.Zero(t, deferred.AttemptCount, "pre-send claim contention must not consume an attempt")
	require.Nil(t, deferred.LockedAt)
	require.True(t, deferred.NextAttemptAt.After(now), "deferred row must wait for the claim owner before retrying")

	inFlight := loadClaimGateDeliveryRow(t, db, rows[winner].ID)
	require.Equal(t, string(store.DeliveryStatusSending), inFlight.Status)

	dispatchers[winner].claim.markDispatchResult(ctx, []domain.YouTubeNotificationDelivery{rows[winner]}, &results[winner])
	require.Equal(t, string(domain.OutboxStatusSent), loadClaimGateDeliveryRow(t, db, rows[winner].ID).Status)

	var state domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, firstDeliveryTestRow(db, &state, "kind = ? AND post_id = ?", outbox.Kind, postID).Error)
	require.NotNil(t, state.AlarmSentAt)

	retryAt := time.Now().UTC().Add(-time.Second)
	require.NoError(t, updateDeliveryTestRowsWhere(db, &domain.YouTubeNotificationDelivery{}, map[string]any{"next_attempt_at": retryAt}, "id = ?", rows[loser].ID).Error)
	require.Equal(t, 1, dispatchers[loser].claim.processPendingDeliveries(ctx))

	require.Equal(t, 2, sender.messageCount())
	sentRooms := make([]string, 0, len(rooms))
	for _, message := range sender.allMessages() {
		sentRooms = append(sentRooms, message[:len("room-contend-x")])
	}
	require.ElementsMatch(t, rooms, sentRooms)

	delivered := loadClaimGateDeliveryRow(t, db, rows[loser].ID)
	require.Equal(t, string(domain.OutboxStatusSent), delivered.Status)
	require.NotNil(t, delivered.SentAt)
	require.Zero(t, delivered.AttemptCount)
}

func TestDispatchDeliveryRowsSkipsShortWhenAnotherExecutionOwnsRecentClaimDefersPersistedRow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
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
	row.Status = store.DeliveryStatusSending
	row.LockedAt = &lockedAt
	row.NextAttemptAt = now.Add(-time.Minute)
	row.AttemptCount = 1
	require.NoError(t, insertDeliveryTestRows(db, &row).Error)

	selection := dispatcher.claim.selectClaimedDeliveries(ctx, []domain.YouTubeNotificationDelivery{row}, []domain.YouTubeNotificationOutbox{outbox}, claim.NewMemoryDecisionCache())

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
