package dispatch

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

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache/claim"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatchstate"
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

func newClaimGateTestDispatcher(t *testing.T, sender *claimGateTestSender, config Config) (*Dispatcher, *deliveryTestDB) {
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

	db := newDeliveryTestDB(t)

	dispatcher := NewDispatcher(
		db.Pool,
		cachemocks.NewLenientClient(),
		sender,
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		config,
	)
	dispatcher.telemetry = nil
	return dispatcher, db
}

func newClaimGateTestDispatcherWithDB(t *testing.T, db *deliveryTestDB, sender *claimGateTestSender, config Config) *Dispatcher {
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
		db.Pool,
		cachemocks.NewLenientClient(),
		sender,
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		config,
	)
	dispatcher.telemetry = nil
	return dispatcher
}

func newSharedClaimGateTestDB(t *testing.T, maxOpenConns int) *deliveryTestDB {
	t.Helper()

	_ = maxOpenConns
	db := newDeliveryTestDB(t)

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
			Payload:       fmt.Sprintf(`{"canonical_post_id":"%s","post_id":"%s","content_text":"body-%s"}`, postID, contentID, suffix),
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
			Payload:       fmt.Sprintf(`{"canonical_post_id":"%s","video_id":"%s","title":"title-%s"}`, postID, contentID, suffix),
			Status:        domain.OutboxStatusPending,
			AttemptCount:  0,
			NextAttemptAt: now,
			CreatedAt:     now,
		}, postID
}

func TestDispatchDeliveryRowsClaimsCommunityPostBeforeSending(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 11, 1, 11, 12, 0, time.UTC)
	sender := &claimGateTestSender{failRoom: map[string]bool{}}
	dispatcher, db := newClaimGateTestDispatcher(t, sender, Config{})
	row, outbox, postID := newCommunityClaimGateFixture(now, "claim-win")

	result := dispatcher.send.dispatchDeliveryRows(context.Background(), []domain.YouTubeNotificationDelivery{row}, map[int64]domain.YouTubeNotificationOutbox{
		outbox.ID: outbox,
	})

	require.Equal(t, 1, sender.messageCount())
	require.Equal(t, []int64{row.ID}, result.SuccessDeliveryIDs)
	require.Zero(t, result.FailedDeliveries)

	var state domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&state, "kind = ? AND post_id = ?", outbox.Kind, postID).Error)
	require.NotNil(t, state.AuthorizedAt)
	require.Nil(t, state.AlarmSentAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, state.DeliveryStatus)
}

func TestDispatchDeliveryRowsSkipsShortWhenAnotherExecutionOwnsRecentClaim(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 11, 1, 11, 12, 0, time.UTC)
	sender := &claimGateTestSender{failRoom: map[string]bool{}}
	dispatcher, db := newClaimGateTestDispatcher(t, sender, Config{LockTimeout: 5 * time.Minute})
	row, outbox, postID := newShortClaimGateFixture(now, "recent-claim")
	authorizedAt := now.Add(-30 * time.Second)
	detectedAt := now.Add(-2 * time.Minute)
	require.NoError(t, db.Create(&domain.YouTubeCommunityShortsAlarmState{
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
	require.NoError(t, db.First(&state, "kind = ? AND post_id = ?", outbox.Kind, postID).Error)
	require.NotNil(t, state.AuthorizedAt)
	require.Equal(t, authorizedAt, state.AuthorizedAt.UTC())
	require.Nil(t, state.AlarmSentAt)
}

func TestDispatchDeliveryRowsSkipsAlreadySentDuplicateWithoutSending(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 11, 1, 11, 12, 0, time.UTC)
	sender := &claimGateTestSender{failRoom: map[string]bool{}}
	dispatcher, db := newClaimGateTestDispatcher(t, sender, Config{})
	row, outbox, postID := newCommunityClaimGateFixture(now, "already-sent")
	authorizedAt := now.Add(-2 * time.Minute)
	alarmSentAt := now.Add(-90 * time.Second)
	detectedAt := now.Add(-3 * time.Minute)
	require.NoError(t, db.Create(&domain.YouTubeCommunityShortsAlarmState{
		Kind:           outbox.Kind,
		PostID:         postID,
		ContentID:      outbox.ContentID,
		ChannelID:      outbox.ChannelID,
		DetectedAt:     detectedAt,
		AuthorizedAt:   &authorizedAt,
		AlarmSentAt:    &alarmSentAt,
		DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusSent,
	}).Error)

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
	dispatcher, db := newClaimGateTestDispatcher(t, sender, Config{})
	row, outbox, postID := newShortClaimGateFixture(now, "tracking-already-sent")
	detectedAt := now.Add(-3 * time.Minute)
	alarmSentAt := now.Add(-90 * time.Second)
	require.NoError(t, db.Create(&domain.YouTubeContentAlarmTracking{
		Kind:               outbox.Kind,
		ContentID:          outbox.ContentID,
		CanonicalContentID: postID,
		ChannelID:          outbox.ChannelID,
		DetectedAt:         detectedAt,
		AlarmSentAt:        &alarmSentAt,
		DeliveryStatus:     domain.YouTubeContentAlarmDeliveryStatusSent,
	}).Error)

	result := dispatcher.send.dispatchDeliveryRows(context.Background(), []domain.YouTubeNotificationDelivery{row}, map[int64]domain.YouTubeNotificationOutbox{
		outbox.ID: outbox,
	})

	require.Zero(t, sender.messageCount())
	require.Equal(t, []int64{row.ID}, result.SuccessDeliveryIDs)
	require.Zero(t, result.FailedDeliveries)

	var stateCount int64
	require.NoError(t, db.Model(&domain.YouTubeCommunityShortsAlarmState{}).
		Where("kind = ? AND post_id = ?", outbox.Kind, postID).
		Count(&stateCount).Error)
	require.Zero(t, stateCount)
}

func TestDispatchDeliveryRowsReleasesClaimAfterSendFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 11, 1, 11, 12, 0, time.UTC)
	sender := &claimGateTestSender{failRoom: map[string]bool{"room-community": true}}
	dispatcher, db := newClaimGateTestDispatcher(t, sender, Config{})
	row, outbox, postID := newCommunityClaimGateFixture(now, "release-on-fail")

	result := dispatcher.send.dispatchDeliveryRows(context.Background(), []domain.YouTubeNotificationDelivery{row}, map[int64]domain.YouTubeNotificationOutbox{
		outbox.ID: outbox,
	})

	require.Zero(t, sender.messageCount())
	require.Empty(t, result.SuccessDeliveryIDs)
	require.Equal(t, 1, result.FailedDeliveries)
	require.Equal(t, []int64{row.ID}, result.FailureBuckets["send message"])

	var state domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&state, "kind = ? AND post_id = ?", outbox.Kind, postID).Error)
	require.Nil(t, state.AuthorizedAt)
	require.Nil(t, state.AlarmSentAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusDetected, state.DeliveryStatus)
}

func TestDispatchDeliveryRowsReclaimsStaleLegacyAuthorizationBeforeSending(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	sender := &claimGateTestSender{failRoom: map[string]bool{}}
	dispatcher, db := newClaimGateTestDispatcher(t, sender, Config{LockTimeout: 2 * time.Minute})
	row, outbox, postID := newCommunityClaimGateFixture(now, "stale-claim")
	staleAuthorizedAt := now.Add(-10 * time.Minute)
	detectedAt := now.Add(-11 * time.Minute)
	require.NoError(t, db.Create(&domain.YouTubeCommunityShortsAlarmState{
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
	require.NoError(t, db.First(&state, "kind = ? AND post_id = ?", outbox.Kind, postID).Error)
	require.NotNil(t, state.AuthorizedAt)
	require.True(t, state.AuthorizedAt.UTC().After(staleAuthorizedAt))
	require.Nil(t, state.AlarmSentAt)
}

func TestDispatchDeliveryRowsGroupedSendFiltersOutAlreadySentDuplicate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 11, 1, 11, 12, 0, time.UTC)
	sender := &claimGateTestSender{failRoom: map[string]bool{}}
	dispatcher, db := newClaimGateTestDispatcher(t, sender, Config{})
	firstRow, firstOutbox, firstPostID := newCommunityClaimGateFixture(now, "group-first")
	secondRow, secondOutbox, _ := newCommunityClaimGateFixture(now, "group-second")
	secondRow.ID = firstRow.ID + 1
	secondRow.OutboxID = firstOutbox.ID + 1
	secondOutbox.ID = secondRow.OutboxID
	secondOutbox.ChannelID = firstOutbox.ChannelID
	secondRow.RoomID = firstRow.RoomID
	firstAuthorizedAt := now.Add(-2 * time.Minute)
	firstAlarmSentAt := now.Add(-90 * time.Second)
	require.NoError(t, db.Create(&domain.YouTubeCommunityShortsAlarmState{
		Kind:           firstOutbox.Kind,
		PostID:         firstPostID,
		ContentID:      firstOutbox.ContentID,
		ChannelID:      firstOutbox.ChannelID,
		DetectedAt:     now.Add(-3 * time.Minute),
		AuthorizedAt:   &firstAuthorizedAt,
		AlarmSentAt:    &firstAlarmSentAt,
		DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusSent,
	}).Error)

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
				newClaimGateTestDispatcherWithDB(t, db, sender, Config{}),
				newClaimGateTestDispatcherWithDB(t, db, sender, Config{}),
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
			require.NoError(t, db.First(&state, "kind = ? AND post_id = ?", outbox.Kind, postID).Error)
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
	dispatcher, _ := newClaimGateTestDispatcher(t, sender, Config{})
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

func TestDispatchClaimedRowsIndividuallyReleasesOnlyOwnedClaimsOnFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 11, 1, 11, 12, 0, time.UTC)
	sender := &claimGateTestSender{failRoom: map[string]bool{"room-duplicate": true}}
	dispatcher, db := newClaimGateTestDispatcher(t, sender, Config{})
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
	require.NoError(t, db.First(&firstState, "kind = ? AND post_id = ?", firstOutbox.Kind, firstPostID).Error)
	require.NotNil(t, firstState.AuthorizedAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, firstState.DeliveryStatus)

	var secondState domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.First(&secondState, "kind = ? AND post_id = ?", secondOutbox.Kind, secondPostID).Error)
	require.NotNil(t, secondState.AuthorizedAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, secondState.DeliveryStatus)
}
