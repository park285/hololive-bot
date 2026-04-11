package outbox

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func TestProcessOnce_RetryAfterCommunityShortsSendFailureSendsExactlyOnce(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name                  string
		kind                  domain.OutboxKind
		channelID             string
		contentID             string
		roomID                string
		payload               string
		expectedMessageMarker string
	}{
		{
			name:                  "community",
			kind:                  domain.OutboxKindCommunityPost,
			channelID:             "UC_retry_exact_once_community",
			contentID:             "post-retry-exact-once",
			roomID:                "room-community",
			payload:               `{"post_id":"post-retry-exact-once","content_text":"community retry body","published_at":"2026-04-10T01:11:12Z"}`,
			expectedMessageMarker: "community retry body",
		},
		{
			name:                  "shorts",
			kind:                  domain.OutboxKindNewShort,
			channelID:             "UC_retry_exact_once_shorts",
			contentID:             "short-retry-exact-once",
			roomID:                "room-shorts",
			payload:               `{"video_id":"short-retry-exact-once","title":"short retry title","published_at":"2026-04-10T01:11:12Z"}`,
			expectedMessageMarker: "short retry title",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			dsn := fmt.Sprintf("file:post_send_finalize_retry_%d?mode=memory&cache=shared", time.Now().UnixNano())
			db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			sqlDB.SetMaxIdleConns(1)
			require.NoError(t, db.AutoMigrate(
				&sqliteOutboxModel{},
				&sqliteDeliveryModel{},
				&sqliteTrackingModel{},
				&sqliteTelemetryBufferModel{},
				&domain.YouTubeCommunityShortsAlarmState{},
			))

			now := time.Now().UTC()
			item := domain.YouTubeNotificationOutbox{
				Kind:          tc.kind,
				ChannelID:     tc.channelID,
				ContentID:     tc.contentID,
				Payload:       tc.payload,
				Status:        domain.OutboxStatusPending,
				AttemptCount:  0,
				NextAttemptAt: now,
			}
			require.NoError(t, db.Create(&item).Error)
			require.NoError(t, db.Create(&sqliteTrackingModel{
				Kind:       string(item.Kind),
				ContentID:  item.ContentID,
				ChannelID:  item.ChannelID,
				DetectedAt: now,
			}).Error)

			delivery := domain.YouTubeNotificationDelivery{
				OutboxID:      item.ID,
				RoomID:        tc.roomID,
				Status:        domain.OutboxStatusPending,
				AttemptCount:  0,
				NextAttemptAt: now,
			}
			require.NoError(t, db.Create(&delivery).Error)

			sender := &testSender{failRoom: map[string]bool{tc.roomID: true}}
			dispatcher := NewDispatcher(db, cachemocks.NewLenientClient(), sender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
				BatchSize:           10,
				LockTimeout:         time.Minute,
				PollInterval:        time.Second,
				MaxRetries:          3,
				RetryBackoff:        time.Minute,
				DeliveryParallelism: 1,
			})

			dispatcher.ProcessOnceForTest(ctx)

			var failedDelivery sqliteDeliveryModel
			require.NoError(t, db.First(&failedDelivery, delivery.ID).Error)
			assert.Equal(t, string(domain.OutboxStatusPending), failedDelivery.Status)
			assert.Equal(t, 1, failedDelivery.AttemptCount)
			assert.Nil(t, failedDelivery.LockedAt)
			assert.Nil(t, failedDelivery.SentAt)

			var failedOutbox sqliteOutboxModel
			require.NoError(t, db.First(&failedOutbox, item.ID).Error)
			assert.Equal(t, string(domain.OutboxStatusPending), failedOutbox.Status)
			assert.Nil(t, failedOutbox.SentAt)

			postID := canonicalDeliveryPostID(item.Kind, item.ContentID)
			var releasedState domain.YouTubeCommunityShortsAlarmState
			require.NoError(t, db.First(&releasedState, "kind = ? AND post_id = ?", item.Kind, postID).Error)
			assert.Nil(t, releasedState.AuthorizedAt)
			assert.Nil(t, releasedState.AlarmSentAt)
			assert.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusDetected, releasedState.DeliveryStatus)

			sender.mu.Lock()
			delete(sender.failRoom, tc.roomID)
			firstAttemptMessages := append([]string(nil), sender.messages...)
			sender.mu.Unlock()
			require.Len(t, firstAttemptMessages, 0)

			retryAt := time.Now().UTC().Add(-time.Second)
			require.NoError(t, db.Model(&domain.YouTubeNotificationDelivery{}).
				Where("id = ?", delivery.ID).
				Updates(map[string]any{"next_attempt_at": retryAt, "locked_at": nil}).Error)

			dispatcher.ProcessOnceForTest(ctx)
			dispatcher.ProcessOnceForTest(ctx)

			var sentDelivery sqliteDeliveryModel
			require.NoError(t, db.First(&sentDelivery, delivery.ID).Error)
			assert.Equal(t, string(domain.OutboxStatusSent), sentDelivery.Status)
			assert.Equal(t, 1, sentDelivery.AttemptCount)
			require.NotNil(t, sentDelivery.SentAt)

			var sentOutbox sqliteOutboxModel
			require.NoError(t, db.First(&sentOutbox, item.ID).Error)
			assert.Equal(t, string(domain.OutboxStatusSent), sentOutbox.Status)
			require.NotNil(t, sentOutbox.SentAt)

			var sentTracking sqliteTrackingModel
			require.NoError(t, db.Where("kind = ? AND content_id = ?", string(item.Kind), item.ContentID).First(&sentTracking).Error)
			require.NotNil(t, sentTracking.AlarmSentAt)
			assert.Equal(t, string(domain.YouTubeContentAlarmDeliveryStatusSent), sentTracking.DeliveryStatus)

			var sentState domain.YouTubeCommunityShortsAlarmState
			require.NoError(t, db.First(&sentState, "kind = ? AND post_id = ?", item.Kind, postID).Error)
			assert.Nil(t, sentState.AuthorizedAt)
			require.NotNil(t, sentState.AlarmSentAt)
			assert.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, sentState.DeliveryStatus)

			var deliveryRows []sqliteDeliveryModel
			require.NoError(t, db.Where("outbox_id = ?", item.ID).Order("id ASC").Find(&deliveryRows).Error)
			require.Len(t, deliveryRows, 1)

			sender.mu.Lock()
			messages := append([]string(nil), sender.messages...)
			sender.mu.Unlock()
			require.Len(t, messages, 1)
			assert.Contains(t, messages[0], tc.roomID+":")
			assert.Contains(t, messages[0], tc.expectedMessageMarker)
		})
	}
}

type postSendFinalizeFailureSender struct {
	mu           sync.Mutex
	messages     []string
	afterSend    func(roomID, message string) error
	afterSendErr error
}

func (s *postSendFinalizeFailureSender) SendMessage(_ context.Context, roomID, message string) error {
	s.mu.Lock()
	s.messages = append(s.messages, roomID+":"+message)
	hook := s.afterSend
	s.mu.Unlock()

	if hook != nil {
		if err := hook(roomID, message); err != nil {
			s.mu.Lock()
			if s.afterSendErr == nil {
				s.afterSendErr = err
			}
			s.mu.Unlock()
		}
	}

	return nil
}

func (s *postSendFinalizeFailureSender) sentMessages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	cloned := make([]string, len(s.messages))
	copy(cloned, s.messages)
	return cloned
}

func (s *postSendFinalizeFailureSender) hookError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.afterSendErr
}

func TestProcessOnce_RetryAfterCommunityShortsPostSendFinalizeFailureKeepsSingleDeliveredAlarm(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name                  string
		kind                  domain.OutboxKind
		channelID             string
		contentID             string
		roomID                string
		payload               string
		expectedMessageMarker string
	}{
		{
			name:                  "community",
			kind:                  domain.OutboxKindCommunityPost,
			channelID:             "UC_retry_finalize_community",
			contentID:             "post-retry-finalize-once",
			roomID:                "room-community",
			payload:               `{"canonical_post_id":"community:post-retry-finalize-once","post_id":"post-retry-finalize-once","content_text":"community finalize body","published_at":"2026-04-10T01:11:12Z"}`,
			expectedMessageMarker: "community finalize body",
		},
		{
			name:                  "shorts",
			kind:                  domain.OutboxKindNewShort,
			channelID:             "UC_retry_finalize_shorts",
			contentID:             "short-retry-finalize-once",
			roomID:                "room-shorts",
			payload:               `{"canonical_post_id":"short:short-retry-finalize-once","video_id":"short-retry-finalize-once","title":"short finalize title","published_at":"2026-04-10T01:11:12Z"}`,
			expectedMessageMarker: "short finalize title",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			require.NoError(t, db.AutoMigrate(
				&sqliteOutboxModel{},
				&sqliteDeliveryModel{},
				&sqliteTrackingModel{},
				&sqliteTelemetryBufferModel{},
				&domain.YouTubeCommunityShortsAlarmState{},
			))

			now := time.Now().UTC()
			item := domain.YouTubeNotificationOutbox{
				Kind:          tc.kind,
				ChannelID:     tc.channelID,
				ContentID:     tc.contentID,
				Payload:       tc.payload,
				Status:        domain.OutboxStatusPending,
				AttemptCount:  0,
				NextAttemptAt: now,
			}
			require.NoError(t, db.Create(&item).Error)
			require.NoError(t, db.Create(&sqliteTrackingModel{
				Kind:       string(item.Kind),
				ContentID:  item.ContentID,
				ChannelID:  item.ChannelID,
				DetectedAt: now,
			}).Error)

			delivery := domain.YouTubeNotificationDelivery{
				OutboxID:      item.ID,
				RoomID:        tc.roomID,
				Status:        domain.OutboxStatusPending,
				AttemptCount:  0,
				NextAttemptAt: now,
			}
			require.NoError(t, db.Create(&delivery).Error)

			postID := canonicalDeliveryPostID(item.Kind, item.ContentID)
			staleAuthorizedAt := now.Add(-10 * time.Minute)
			var mutateOnce sync.Once
			sender := &postSendFinalizeFailureSender{
				afterSend: func(_ string, _ string) error {
					var hookErr error
					mutateOnce.Do(func() {
						deadline := time.Now().Add(2 * time.Second)
						for {
							result := db.Model(&domain.YouTubeCommunityShortsAlarmState{}).
								Where("kind = ? AND post_id = ?", item.Kind, postID).
								Updates(map[string]any{
									"authorized_at": staleAuthorizedAt,
									"updated_at":    time.Now().UTC(),
								})
							if result.Error != nil {
								hookErr = result.Error
								return
							}
							if result.RowsAffected > 0 {
								return
							}
							if time.Now().After(deadline) {
								hookErr = fmt.Errorf("post-send finalize hook: claim row not found for %s", postID)
								return
							}
							time.Sleep(10 * time.Millisecond)
						}
					})
					return hookErr
				},
			}
			dispatcher := NewDispatcher(db, cachemocks.NewLenientClient(), sender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
				BatchSize:           10,
				LockTimeout:         time.Minute,
				PollInterval:        time.Second,
				MaxRetries:          3,
				RetryBackoff:        time.Minute,
				DeliveryParallelism: 1,
			})

			dispatcher.ProcessOnceForTest(ctx)
			require.NoError(t, sender.hookError())
			require.Len(t, sender.sentMessages(), 1)

			var pendingDelivery sqliteDeliveryModel
			require.NoError(t, db.First(&pendingDelivery, delivery.ID).Error)
			assert.Equal(t, string(domain.OutboxStatusPending), pendingDelivery.Status)
			assert.Nil(t, pendingDelivery.SentAt)

			var pendingOutbox sqliteOutboxModel
			require.NoError(t, db.First(&pendingOutbox, item.ID).Error)
			assert.Equal(t, string(domain.OutboxStatusPending), pendingOutbox.Status)
			assert.Nil(t, pendingOutbox.SentAt)

			retryAt := time.Now().UTC().Add(-time.Second)
			require.NoError(t, db.Model(&domain.YouTubeNotificationDelivery{}).
				Where("id = ?", delivery.ID).
				Updates(map[string]any{"next_attempt_at": retryAt, "locked_at": nil}).Error)

			dispatcher.ProcessOnceForTest(ctx)

			var sentDelivery sqliteDeliveryModel
			require.NoError(t, db.First(&sentDelivery, delivery.ID).Error)
			assert.Equal(t, string(domain.OutboxStatusSent), sentDelivery.Status)
			require.NotNil(t, sentDelivery.SentAt)

			var sentOutbox sqliteOutboxModel
			require.NoError(t, db.First(&sentOutbox, item.ID).Error)
			assert.Equal(t, string(domain.OutboxStatusSent), sentOutbox.Status)
			require.NotNil(t, sentOutbox.SentAt)

			var sentTracking sqliteTrackingModel
			require.NoError(t, db.Where("kind = ? AND content_id = ?", string(item.Kind), item.ContentID).First(&sentTracking).Error)
			require.NotNil(t, sentTracking.AlarmSentAt)
			assert.Equal(t, string(domain.YouTubeContentAlarmDeliveryStatusSent), sentTracking.DeliveryStatus)

			var sentState domain.YouTubeCommunityShortsAlarmState
			require.NoError(t, db.First(&sentState, "kind = ? AND post_id = ?", item.Kind, postID).Error)
			assert.Nil(t, sentState.AuthorizedAt)
			require.NotNil(t, sentState.AlarmSentAt)
			assert.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, sentState.DeliveryStatus)

			var deliveryRows []sqliteDeliveryModel
			require.NoError(t, db.Where("outbox_id = ?", item.ID).Order("id ASC").Find(&deliveryRows).Error)
			require.Len(t, deliveryRows, 1)

			messages := sender.sentMessages()
			require.Len(t, messages, 1)
			assert.Contains(t, messages[0], tc.roomID+":")
			assert.Contains(t, messages[0], tc.expectedMessageMarker)
		})
	}
}
