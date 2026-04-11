package outbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
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

func newClaimGateTestDispatcher(t *testing.T, sender *claimGateTestSender, cfg Config) (*Dispatcher, *gorm.DB) {
	t.Helper()

	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 10
	}
	if cfg.LockTimeout <= 0 {
		cfg.LockTimeout = 5 * time.Minute
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = time.Second
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryBackoff <= 0 {
		cfg.RetryBackoff = time.Minute
	}
	if cfg.DeliveryParallelism <= 0 {
		cfg.DeliveryParallelism = 2
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&domain.YouTubeCommunityShortsAlarmState{},
		&domain.YouTubeContentAlarmTracking{},
	))

	dispatcher := NewDispatcher(
		db,
		cachemocks.NewLenientClient(),
		sender,
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		cfg,
	)
	dispatcher.telemetry = nil
	return dispatcher, db
}

func newClaimGateTestDispatcherWithDB(t *testing.T, db *gorm.DB, sender *claimGateTestSender, cfg Config) *Dispatcher {
	t.Helper()

	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 10
	}
	if cfg.LockTimeout <= 0 {
		cfg.LockTimeout = 5 * time.Minute
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = time.Second
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryBackoff <= 0 {
		cfg.RetryBackoff = time.Minute
	}
	if cfg.DeliveryParallelism <= 0 {
		cfg.DeliveryParallelism = 2
	}

	dispatcher := NewDispatcher(
		db,
		cachemocks.NewLenientClient(),
		sender,
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		cfg,
	)
	dispatcher.telemetry = nil
	return dispatcher
}

func newSharedClaimGateTestDB(t *testing.T, maxOpenConns int) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:claim_gate_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(maxOpenConns)
	sqlDB.SetMaxIdleConns(maxOpenConns)

	require.NoError(t, db.AutoMigrate(&domain.YouTubeCommunityShortsAlarmState{}, &domain.YouTubeContentAlarmTracking{}))
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

	result := dispatcher.dispatchDeliveryRows(context.Background(), []domain.YouTubeNotificationDelivery{row}, map[int64]domain.YouTubeNotificationOutbox{
		outbox.ID: outbox,
	})

	require.Equal(t, 1, sender.messageCount())
	require.Equal(t, []int64{row.ID}, result.successDeliveryIDs)
	require.Zero(t, result.failedDeliveries)

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

	result := dispatcher.dispatchDeliveryRows(context.Background(), []domain.YouTubeNotificationDelivery{row}, map[int64]domain.YouTubeNotificationOutbox{
		outbox.ID: outbox,
	})

	require.Zero(t, sender.messageCount())
	require.Empty(t, result.successDeliveryIDs)
	require.Equal(t, 1, result.failedDeliveries)
	require.Equal(t, []int64{row.ID}, result.failureBuckets[deliveryFailureReasonPreSendClaim])

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

	result := dispatcher.dispatchDeliveryRows(context.Background(), []domain.YouTubeNotificationDelivery{row}, map[int64]domain.YouTubeNotificationOutbox{
		outbox.ID: outbox,
	})

	require.Zero(t, sender.messageCount())
	require.Equal(t, []int64{row.ID}, result.successDeliveryIDs)
	require.Zero(t, result.failedDeliveries)
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

	result := dispatcher.dispatchDeliveryRows(context.Background(), []domain.YouTubeNotificationDelivery{row}, map[int64]domain.YouTubeNotificationOutbox{
		outbox.ID: outbox,
	})

	require.Zero(t, sender.messageCount())
	require.Equal(t, []int64{row.ID}, result.successDeliveryIDs)
	require.Zero(t, result.failedDeliveries)

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

	result := dispatcher.dispatchDeliveryRows(context.Background(), []domain.YouTubeNotificationDelivery{row}, map[int64]domain.YouTubeNotificationOutbox{
		outbox.ID: outbox,
	})

	require.Zero(t, sender.messageCount())
	require.Empty(t, result.successDeliveryIDs)
	require.Equal(t, 1, result.failedDeliveries)
	require.Equal(t, []int64{row.ID}, result.failureBuckets["send message"])

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

	result := dispatcher.dispatchDeliveryRows(context.Background(), []domain.YouTubeNotificationDelivery{row}, map[int64]domain.YouTubeNotificationOutbox{
		outbox.ID: outbox,
	})

	require.Equal(t, 1, sender.messageCount())
	require.Equal(t, []int64{row.ID}, result.successDeliveryIDs)
	require.Zero(t, result.failedDeliveries)

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

	result := dispatcher.dispatchDeliveryRows(context.Background(), []domain.YouTubeNotificationDelivery{firstRow, secondRow}, map[int64]domain.YouTubeNotificationOutbox{
		firstOutbox.ID:  firstOutbox,
		secondOutbox.ID: secondOutbox,
	})

	require.Equal(t, 1, sender.messageCount())
	require.ElementsMatch(t, []int64{firstRow.ID, secondRow.ID}, result.successDeliveryIDs)
	require.Zero(t, result.failedDeliveries)
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
			results := make([]deliveryDispatchResult, len(dispatchers))

			start := make(chan struct{})
			var wg sync.WaitGroup
			for i := range dispatchers {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					<-start
					results[idx] = dispatchers[idx].dispatchDeliveryRows(context.Background(), []domain.YouTubeNotificationDelivery{row}, map[int64]domain.YouTubeNotificationOutbox{
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
				totalSuccesses += len(results[i].successDeliveryIDs)
				totalFailures += results[i].failedDeliveries
				preSendClaimFailures += len(results[i].failureBuckets[deliveryFailureReasonPreSendClaim])
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
