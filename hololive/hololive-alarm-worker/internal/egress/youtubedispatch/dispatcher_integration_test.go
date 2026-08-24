// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package youtubedispatch_test

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"log/slog"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
)

var errSendFailed = errors.New("send failed")

type fakeSender struct {
	mu       sync.Mutex
	messages []sentMessage
	failNext bool
	failRoom map[string]bool
}

type sentMessage struct {
	Room    string
	Message string
}

func mustMarshalPayload(tb testing.TB, value any) []byte {
	tb.Helper()

	payload, err := jsonv2.Marshal(value)
	require.NoError(tb, err)

	return payload
}

func (f *fakeSender) SendMessage(_ context.Context, room, message string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failNext {
		f.failNext = false
		return errSendFailed
	}

	if len(f.failRoom) > 0 && f.failRoom[room] {
		delete(f.failRoom, room)

		return errSendFailed
	}

	f.messages = append(f.messages, sentMessage{Room: room, Message: message})

	return nil
}

func (f *fakeSender) getMessages() []sentMessage {
	f.mu.Lock()
	defer f.mu.Unlock()

	result := make([]sentMessage, len(f.messages))
	copy(result, f.messages)

	return result
}

func (f *fakeSender) setFailNext() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.failNext = true
}

func (f *fakeSender) setFailRoom(room string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failRoom == nil {
		f.failRoom = make(map[string]bool)
	}

	f.failRoom[room] = true
}

func TestDispatcher_ProcessOnce_Success(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") != integrationEnvEnabled {
		t.Skip("Skipping integration test (set INTEGRATION_TEST=true to run)")
	}

	ctx := t.Context()
	db := dbtest.NewPool(t)
	cleanupOutbox(t, db)

	sender := &fakeSender{}
	cacheService := setupCacheService(t)
	testLogger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	setupTestSubscribers(t, cacheService)

	config := dispatchstate.Config{
		BatchSize:    10,
		LockTimeout:  1 * time.Minute,
		PollInterval: 100 * time.Millisecond,
		MaxRetries:   3,
		RetryBackoff: 1 * time.Second,
	}

	dispatcher := youtubedispatch.NewDispatcher(db, cacheService, sender, nil, testLogger, &config)

	payload := mustMarshalPayload(t, map[string]string{
		payloadKeyVideoID: "test123",
		payloadKeyTitle:   "Test Video Title",
	})

	item := &domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewShort,
		ChannelID:     "UCtest123",
		ContentID:     "test_success_" + time.Now().Format("150405"),
		Payload:       string(payload),
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: time.Now(),
	}

	if err := insertDeliveryTestRows(db, item).Error; err != nil {
		t.Fatalf("Failed to create test outbox item: %v", err)
	}

	t.Cleanup(func() {
		deleteDeliveryTestRows(t, db, item)
	})

	dispatcher.ProcessOnceForTest(ctx)

	var updated domain.YouTubeNotificationOutbox

	if err := firstDeliveryTestRow(db, &updated, item.ID).Error; err != nil {
		t.Fatalf("Failed to fetch updated item: %v", err)
	}

	if updated.Status != domain.OutboxStatusSent {
		t.Errorf("Expected status SENT, got %s", updated.Status)
	}

	if updated.SentAt == nil {
		t.Error("Expected sent_at to be set")
	}

	msgs := sender.getMessages()
	if len(msgs) != 1 {
		t.Errorf("Expected 1 message sent, got %d", len(msgs))
	}

	if len(msgs) > 0 && msgs[0].Room != "testroom" {
		t.Errorf("Expected room 'testroom', got %s", msgs[0].Room)
	}
}

func TestDispatcher_ProcessOnce_Retry(t *testing.T) {
	env := newDispatcherIntegrationEnv(t, newIntegrationDispatchConfig(1*time.Second))

	env.sender.setFailNext()
	setupTestSubscribers(t, env.cacheService)

	payload := mustMarshalPayload(t, map[string]string{
		payloadKeyVideoID: "retry123",
		payloadKeyTitle:   "Retry Test Video",
	})

	item := &domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewVideo,
		ChannelID:     "UCtest456",
		ContentID:     "test_retry_" + time.Now().Format("150405"),
		Payload:       string(payload),
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: time.Now(),
	}
	seedIntegrationOutboxItem(t, env.db, item)

	env.dispatcher.ProcessOnceForTest(t.Context())

	var updated domain.YouTubeNotificationOutbox

	if err := firstDeliveryTestRow(env.db, &updated, item.ID).Error; err != nil {
		t.Fatalf("Failed to fetch updated item: %v", err)
	}

	if updated.Status != domain.OutboxStatusPending {
		t.Errorf("Expected status PENDING (for retry), got %s", updated.Status)
	}

	if updated.LockedAt != nil {
		t.Error("Expected locked_at to be nil after failure")
	}

	deliveries := fetchDeliveryRows(t, env.db, item.ID)
	if len(deliveries) != 1 {
		t.Fatalf("Expected 1 delivery row, got %d", len(deliveries))
	}

	if deliveries[0].Status != domain.OutboxStatusPending {
		t.Errorf("Expected delivery status PENDING (for retry), got %s", deliveries[0].Status)
	}

	if deliveries[0].AttemptCount != 1 {
		t.Errorf("Expected delivery attempt_count 1, got %d", deliveries[0].AttemptCount)
	}

	if deliveries[0].NextAttemptAt.Before(time.Now()) {
		t.Error("Expected delivery next_attempt_at to be in the future")
	}

	if deliveries[0].LockedAt != nil {
		t.Error("Expected delivery locked_at to be nil after failure")
	}
}

func TestDispatcher_NoSubscribers_MarkedAsSent(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") != integrationEnvEnabled {
		t.Skip("Skipping integration test (set INTEGRATION_TEST=true to run)")
	}

	ctx := t.Context()
	db := dbtest.NewPool(t)
	cleanupOutbox(t, db)

	sender := &fakeSender{}
	cacheService := setupCacheService(t)
	testLogger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	config := dispatchstate.Config{
		BatchSize:    10,
		LockTimeout:  1 * time.Minute,
		PollInterval: 100 * time.Millisecond,
		MaxRetries:   3,
		RetryBackoff: 1 * time.Second,
	}

	dispatcher := youtubedispatch.NewDispatcher(db, cacheService, sender, nil, testLogger, &config)

	payload := mustMarshalPayload(t, map[string]string{
		payloadKeyVideoID: "nosub123",
		payloadKeyTitle:   "No Subscribers Test",
	})

	item := &domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewShort,
		ChannelID:     "UCnosubscribers",
		ContentID:     "test_nosub_" + time.Now().Format("150405"),
		Payload:       string(payload),
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: time.Now(),
	}

	if err := insertDeliveryTestRows(db, item).Error; err != nil {
		t.Fatalf("Failed to create test outbox item: %v", err)
	}

	t.Cleanup(func() {
		deleteDeliveryTestRows(t, db, item)
	})

	dispatcher.ProcessOnceForTest(ctx)

	var updated domain.YouTubeNotificationOutbox

	if err := firstDeliveryTestRow(db, &updated, item.ID).Error; err != nil {
		t.Fatalf("Failed to fetch updated item: %v", err)
	}

	if updated.Status != domain.OutboxStatusSent {
		t.Errorf("Expected status SENT (no subscribers = skip), got %s", updated.Status)
	}

	msgs := sender.getMessages()
	if len(msgs) != 0 {
		t.Errorf("Expected 0 messages sent (no subscribers), got %d", len(msgs))
	}
}

func TestDispatcher_PerRoomMode_Success(t *testing.T) {
	env := newDispatcherIntegrationEnv(t, newIntegrationDispatchConfig(50*time.Millisecond))

	setupChannelSubscribers(t, env.cacheService, "alarm:channel_subscribers:SHORTS:UCperroom_success", []string{"roomA", "roomB"})
	setupMemberName(t, env.cacheService, "UCperroom_success", "PerRoomMember")

	payload := mustMarshalPayload(t, map[string]string{
		payloadKeyVideoID: "perroom_success_video",
		payloadKeyTitle:   "PerRoom Success Video",
	})

	item := &domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewShort,
		ChannelID:     "UCperroom_success",
		ContentID:     "test_perroom_success_" + time.Now().Format("150405"),
		Payload:       string(payload),
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: time.Now(),
	}
	seedIntegrationOutboxItem(t, env.db, item)

	env.dispatcher.ProcessOnceForTest(t.Context())

	var updated domain.YouTubeNotificationOutbox

	if err := firstDeliveryTestRow(env.db, &updated, item.ID).Error; err != nil {
		t.Fatalf("Failed to fetch updated item: %v", err)
	}

	if updated.Status != domain.OutboxStatusSent {
		t.Fatalf("Expected status SENT, got %s", updated.Status)
	}

	deliveries := fetchDeliveryRows(t, env.db, item.ID)
	if len(deliveries) != 2 {
		t.Fatalf("Expected 2 delivery rows, got %d", len(deliveries))
	}

	for i := range deliveries {
		if deliveries[i].Status != domain.OutboxStatusSent {
			t.Fatalf("Expected delivery[%d] status SENT, got %s", i, deliveries[i].Status)
		}
	}

	msgs := env.sender.getMessages()
	if len(msgs) != 2 {
		t.Fatalf("Expected 2 messages sent, got %d", len(msgs))
	}
}

func TestDispatcher_PerRoomMode_PartialFailureThenRetry(t *testing.T) {
	env := newDispatcherIntegrationEnv(t, newIntegrationDispatchConfig(30*time.Millisecond))

	env.sender.setFailRoom("roomB")
	setupChannelSubscribers(t, env.cacheService, "alarm:channel_subscribers:UCperroom_retry", []string{"roomA", "roomB"})
	setupMemberName(t, env.cacheService, "UCperroom_retry", "PerRoomRetryMember")

	payload := mustMarshalPayload(t, map[string]string{
		payloadKeyVideoID: "perroom_retry_video",
		payloadKeyTitle:   "PerRoom Retry Video",
	})

	item := &domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewVideo,
		ChannelID:     "UCperroom_retry",
		ContentID:     "test_perroom_retry_" + time.Now().Format("150405"),
		Payload:       string(payload),
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: time.Now(),
	}
	seedIntegrationOutboxItem(t, env.db, item)

	env.dispatcher.ProcessOnceForTest(t.Context())

	var first domain.YouTubeNotificationOutbox

	if err := firstDeliveryTestRow(env.db, &first, item.ID).Error; err != nil {
		t.Fatalf("Failed to fetch first state: %v", err)
	}

	if first.Status != domain.OutboxStatusPending {
		t.Fatalf("Expected outbox status PENDING after partial failure, got %s", first.Status)
	}

	deliveries := fetchDeliveryRows(t, env.db, item.ID)
	if len(deliveries) != 2 {
		t.Fatalf("Expected 2 delivery rows, got %d", len(deliveries))
	}

	time.Sleep(40 * time.Millisecond)
	env.dispatcher.ProcessOnceForTest(t.Context())

	var second domain.YouTubeNotificationOutbox

	if err := firstDeliveryTestRow(env.db, &second, item.ID).Error; err != nil {
		t.Fatalf("Failed to fetch second state: %v", err)
	}

	if second.Status != domain.OutboxStatusSent {
		t.Fatalf("Expected outbox status SENT after retry success, got %s", second.Status)
	}

	finalDeliveries := fetchDeliveryRows(t, env.db, item.ID)
	for i := range finalDeliveries {
		if finalDeliveries[i].Status != domain.OutboxStatusSent {
			t.Fatalf("Expected final delivery[%d] status SENT, got %s", i, finalDeliveries[i].Status)
		}
	}
}

func TestDispatcher_PerRoomMode_NoSubscribers_MarkedAsSentWithoutDeliveryRows(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") != integrationEnvEnabled {
		t.Skip("Skipping integration test (set INTEGRATION_TEST=true to run)")
	}

	ctx := t.Context()
	db := dbtest.NewPool(t)
	cleanupOutbox(t, db)

	sender := &fakeSender{}
	cacheService := setupCacheService(t)
	testLogger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	config := dispatchstate.Config{
		BatchSize:    10,
		LockTimeout:  1 * time.Minute,
		PollInterval: 100 * time.Millisecond,
		MaxRetries:   3,
		RetryBackoff: 50 * time.Millisecond,
	}

	dispatcher := youtubedispatch.NewDispatcher(db, cacheService, sender, nil, testLogger, &config)

	payload := mustMarshalPayload(t, map[string]string{
		payloadKeyVideoID: "perroom_no_sub_video",
		payloadKeyTitle:   "PerRoom No Subscribers Video",
	})

	item := &domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewVideo,
		ChannelID:     "UCperroom_nosub",
		ContentID:     "test_perroom_nosub_" + time.Now().Format("150405"),
		Payload:       string(payload),
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: time.Now(),
	}
	if err := insertDeliveryTestRows(db, item).Error; err != nil {
		t.Fatalf("Failed to create test outbox item: %v", err)
	}

	t.Cleanup(func() { deleteDeliveryTestRows(t, db, item) })

	dispatcher.ProcessOnceForTest(ctx)

	var updated domain.YouTubeNotificationOutbox

	if err := firstDeliveryTestRow(db, &updated, item.ID).Error; err != nil {
		t.Fatalf("Failed to fetch updated item: %v", err)
	}

	if updated.Status != domain.OutboxStatusSent {
		t.Fatalf("Expected status SENT, got %s", updated.Status)
	}

	deliveries := fetchDeliveryRows(t, db, item.ID)
	if len(deliveries) != 0 {
		t.Fatalf("Expected 0 delivery rows, got %d", len(deliveries))
	}

	msgs := sender.getMessages()
	if len(msgs) != 0 {
		t.Fatalf("Expected 0 sent messages, got %d", len(msgs))
	}
}

func TestDispatcher_PerRoomMode_PartialTerminalFailure_MarksOutboxFailed(t *testing.T) {
	config := newIntegrationDispatchConfig(30 * time.Millisecond)

	config.MaxRetries = 1

	env := newDispatcherIntegrationEnv(t, config)

	env.sender.setFailRoom("roomB")
	setupChannelSubscribers(t, env.cacheService, "alarm:channel_subscribers:UCperroom_terminal_fail", []string{"roomA", "roomB"})
	setupMemberName(t, env.cacheService, "UCperroom_terminal_fail", "PerRoomTerminalFailMember")

	payload := mustMarshalPayload(t, map[string]string{
		payloadKeyVideoID: "perroom_terminal_fail_video",
		payloadKeyTitle:   "PerRoom Terminal Fail Video",
	})

	item := &domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewVideo,
		ChannelID:     "UCperroom_terminal_fail",
		ContentID:     "test_perroom_terminal_fail_" + time.Now().Format("150405"),
		Payload:       string(payload),
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: time.Now(),
	}
	seedIntegrationOutboxItem(t, env.db, item)

	env.dispatcher.ProcessOnceForTest(t.Context())

	var updated domain.YouTubeNotificationOutbox

	if err := firstDeliveryTestRow(env.db, &updated, item.ID).Error; err != nil {
		t.Fatalf("Failed to fetch updated outbox: %v", err)
	}

	if updated.Status != domain.OutboxStatusFailed {
		t.Fatalf("Expected outbox status FAILED, got %s", updated.Status)
	}

	deliveries := fetchDeliveryRows(t, env.db, item.ID)
	if len(deliveries) != 2 {
		t.Fatalf("Expected 2 delivery rows, got %d", len(deliveries))
	}

	failedCount := 0
	sentCount := 0

	for i := range deliveries {
		switch deliveries[i].Status {
		case domain.OutboxStatusPending:
		case domain.OutboxStatusFailed:
			failedCount++
		case domain.OutboxStatusSent:
			sentCount++
		}
	}

	if failedCount != 1 || sentCount != 1 {
		t.Fatalf("Expected 1 failed + 1 sent delivery, got failed=%d sent=%d", failedCount, sentCount)
	}
}

type concurrentAlarmCase struct {
	name            string
	kind            domain.OutboxKind
	channelID       string
	roomID          string
	memberName      string
	contentPrefix   string
	messageFragment string
	postID          func(contentID string) string
	payload         func(contentID string, publishedAt time.Time) string
}

func TestDispatcher_ProcessOnce_ConcurrentExecutionsSendCommunityShortsAlarmOnce(t *testing.T) {
	requireIntegrationEnv(t)

	testCases := []concurrentAlarmCase{
		{
			name:            "community post",
			kind:            domain.OutboxKindCommunityPost,
			channelID:       "UCintegration_race_community",
			roomID:          "room-community-race",
			memberName:      "ConcurrentCommunityMember",
			contentPrefix:   "community_race",
			messageFragment: "커뮤니티 글",
			postID: func(contentID string) string {
				return "community:" + contentID
			},
			payload: func(contentID string, publishedAt time.Time) string {
				payload := mustMarshalPayload(t, map[string]any{
					"canonical_post_id": "community:" + contentID,
					"post_id":           contentID,
					"content_text":      "Concurrent community delivery body",
					"published_at":      publishedAt,
				})

				return string(payload)
			},
		},
		{
			name:            "short",
			kind:            domain.OutboxKindNewShort,
			channelID:       "UCintegration_race_short",
			roomID:          "room-short-race",
			memberName:      "ConcurrentShortMember",
			contentPrefix:   "short_race",
			messageFragment: "새 쇼츠",
			postID: func(contentID string) string {
				return "short:" + contentID
			},
			payload: func(contentID string, publishedAt time.Time) string {
				payload := mustMarshalPayload(t, map[string]any{
					"canonical_post_id": "short:" + contentID,
					payloadKeyVideoID:   contentID,
					payloadKeyTitle:     "Concurrent short delivery title",
					"published_at":      publishedAt,
				})

				return string(payload)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runConcurrentAlarmCase(t, tc)
		})
	}
}

func runConcurrentAlarmCase(t *testing.T, tc concurrentAlarmCase) {
	t.Helper()

	ctx := t.Context()
	dbPrimary := dbtest.NewPool(t)
	dbSecondary := dbtest.NewPool(t)

	cleanupOutbox(t, dbPrimary)

	sender := &fakeSender{}
	cacheService := setupCacheService(t)

	setupMemberName(t, cacheService, tc.channelID, tc.memberName)

	config := newIntegrationDispatchConfig(30 * time.Millisecond)
	logger := newIntegrationTestLogger()
	dispatchers := []*youtubedispatch.Dispatcher{
		youtubedispatch.NewDispatcher(dbPrimary, cacheService, sender, nil, logger, &config),
		youtubedispatch.NewDispatcher(dbSecondary, cacheService, sender, nil, logger, &config),
	}

	contentID := "test_" + tc.contentPrefix + "_" + time.Now().UTC().Format("150405000000000")
	publishedAt := time.Date(2026, time.April, 10, 1, 2, 3, 0, time.UTC)
	postID := tc.postID(contentID)
	item := seedConcurrentAlarmRows(t, dbPrimary, tc, contentID, postID, publishedAt)
	start := make(chan struct{})

	var wg sync.WaitGroup

	for i := range dispatchers {
		wg.Go(func() {
			<-start
			dispatchers[i].ProcessOnceForTest(ctx)
		})
	}

	close(start)
	wg.Wait()

	msgs := sender.getMessages()
	require.Len(t, msgs, 1)
	require.Equal(t, tc.roomID, msgs[0].Room)
	require.Contains(t, msgs[0].Message, tc.messageFragment)

	assertConcurrentAlarmSentOnce(t, dbPrimary, tc, item, contentID, postID)
}

func seedConcurrentAlarmRows(
	t *testing.T,
	db *deliveryTestDB,
	tc concurrentAlarmCase,
	contentID, postID string,
	publishedAt time.Time,
) *domain.YouTubeNotificationOutbox {
	t.Helper()

	item := &domain.YouTubeNotificationOutbox{
		Kind:          tc.kind,
		ChannelID:     tc.channelID,
		ContentID:     contentID,
		Payload:       tc.payload(contentID, publishedAt),
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: time.Now(),
	}
	require.NoError(t, insertDeliveryTestRows(db, item).Error)

	delivery := &domain.YouTubeNotificationDelivery{
		OutboxID:      item.ID,
		RoomID:        tc.roomID,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: time.Now(),
	}
	require.NoError(t, insertDeliveryTestRows(db, delivery).Error)

	t.Cleanup(func() {
		deleteDeliveryTestRowsWhere(db, &domain.YouTubeCommunityShortsAlarmState{}, "kind = ? AND post_id = ?", tc.kind, postID)
		deleteDeliveryTestRows(t, db, delivery)
		deleteDeliveryTestRows(t, db, item)
	})

	return item
}

func assertConcurrentAlarmSentOnce(
	t *testing.T,
	db *deliveryTestDB,
	tc concurrentAlarmCase,
	item *domain.YouTubeNotificationOutbox,
	contentID, postID string,
) {
	t.Helper()

	var updated domain.YouTubeNotificationOutbox

	require.NoError(t, firstDeliveryTestRow(db, &updated, item.ID).Error)
	require.Equal(t, domain.OutboxStatusSent, updated.Status)
	require.NotNil(t, updated.SentAt)

	deliveries := fetchDeliveryRows(t, db, item.ID)
	require.Len(t, deliveries, 1)
	require.Equal(t, domain.OutboxStatusSent, deliveries[0].Status)
	require.NotNil(t, deliveries[0].SentAt)

	var state domain.YouTubeCommunityShortsAlarmState

	require.NoError(t, firstDeliveryTestRow(db, &state, "kind = ? AND post_id = ?", tc.kind, postID).Error)
	require.Equal(t, postID, state.PostID)
	require.Equal(t, contentID, state.ContentID)
	require.NotNil(t, state.AlarmSentAt)
	require.Nil(t, state.AuthorizedAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, state.DeliveryStatus)
}

func TestDispatcher_Cleanup_RemovesOldFailedRows(t *testing.T) {
	config := newIntegrationDispatchConfig(1 * time.Second)

	config.CleanupAfter = 1 * time.Hour
	config.CleanupEnabled = false

	env := newDispatcherIntegrationEnv(t, config)

	oldFailed := &domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewVideo,
		ChannelID:     "UCcleanup_old_failed",
		ContentID:     "test_cleanup_old_failed_" + time.Now().Format("150405"),
		Payload:       `{"video_id":"cleanup_old_failed","title":"cleanup old failed"}`,
		Status:        domain.OutboxStatusFailed,
		AttemptCount:  3,
		NextAttemptAt: time.Now().Add(-24 * time.Hour),
		CreatedAt:     time.Now().Add(-48 * time.Hour),
		Error:         "old failed",
	}
	recentFailed := &domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewVideo,
		ChannelID:     "UCcleanup_recent_failed",
		ContentID:     "test_cleanup_recent_failed_" + time.Now().Format("150405"),
		Payload:       `{"video_id":"cleanup_recent_failed","title":"cleanup recent failed"}`,
		Status:        domain.OutboxStatusFailed,
		AttemptCount:  1,
		NextAttemptAt: time.Now(),
		CreatedAt:     time.Now(),
		Error:         "recent failed",
	}

	if err := insertDeliveryTestRows(env.db, oldFailed).Error; err != nil {
		t.Fatalf("Failed to create old failed outbox item: %v", err)
	}

	if err := insertDeliveryTestRows(env.db, recentFailed).Error; err != nil {
		t.Fatalf("Failed to create recent failed outbox item: %v", err)
	}

	env.dispatcher.CleanupForTest(t.Context())

	var oldCount int64

	if err := countDeliveryTestRowsWhere(env.db, &domain.YouTubeNotificationOutbox{}, &oldCount, "id = ?", oldFailed.ID).Error; err != nil {
		t.Fatalf("Failed to count old failed item: %v", err)
	}

	if oldCount != 0 {
		t.Fatal("Expected old failed item to be deleted, still exists")
	}

	var recentCount int64

	if err := countDeliveryTestRowsWhere(env.db, &domain.YouTubeNotificationOutbox{}, &recentCount, "id = ?", recentFailed.ID).Error; err != nil {
		t.Fatalf("Failed to count recent failed item: %v", err)
	}

	if recentCount != 1 {
		t.Fatalf("Expected recent failed item to remain, count=%d", recentCount)
	}
}

func cleanupOutbox(t *testing.T, db *deliveryTestDB) {
	t.Helper()
	execDeliveryTestSQL(t, db, `
		DELETE FROM youtube_notification_delivery
		WHERE outbox_id IN (
			SELECT id FROM youtube_notification_outbox WHERE content_id LIKE 'test%'
		)
	`)
	execDeliveryTestSQL(t, db, "DELETE FROM youtube_notification_outbox WHERE content_id LIKE 'test%'")
}

func setupCacheService(t *testing.T) *cache.Service {
	t.Helper()

	valkeyHost := os.Getenv("TEST_VALKEY_HOST")
	valkeyPort := 6379

	if valkeyAddr := os.Getenv("TEST_VALKEY_ADDR"); valkeyAddr != "" {
		host, port, err := net.SplitHostPort(valkeyAddr)
		require.NoError(t, err)

		valkeyHost = host
		valkeyPort, err = strconv.Atoi(port)
		require.NoError(t, err)
	}

	if valkeyHost == "" {
		valkeyHost = "localhost"
	}

	config := cache.Config{
		Host:              valkeyHost,
		Port:              valkeyPort,
		DisableCache:      true,
		ForceSingleClient: true,
	}

	testLogger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

	cacheService, err := cache.NewCacheService(t.Context(), config, testLogger)
	if err != nil {
		t.Fatalf("Failed to create cache service: %v", err)
	}

	t.Cleanup(func() {
		require.NoError(t, cacheService.Close())
	})

	return cacheService
}

func setupTestSubscribers(t *testing.T, cacheService *cache.Service) {
	t.Helper()

	ctx := t.Context()

	_, err := cacheService.SAdd(ctx, "alarm:channel_subscribers:SHORTS:UCtest123", []string{"testroom"})
	require.NoError(t, err)

	_, err = cacheService.SAdd(ctx, "alarm:channel_subscribers:UCtest456", []string{"testroom"})
	require.NoError(t, err)
	require.NoError(t, cacheService.HSet(ctx, "alarm:member_names", "UCtest123", "TestMember"))
	require.NoError(t, cacheService.HSet(ctx, "alarm:member_names", "UCtest456", "TestMember2"))

	t.Cleanup(func() {
		require.NoError(t, cacheService.Del(ctx, "alarm:channel_subscribers:SHORTS:UCtest123"))
		require.NoError(t, cacheService.Del(ctx, "alarm:channel_subscribers:UCtest456"))
	})
}

func setupChannelSubscribers(t *testing.T, cacheService *cache.Service, key string, subscribers []string) {
	t.Helper()

	ctx := t.Context()
	_, err := cacheService.SAdd(ctx, key, subscribers)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cacheService.Del(ctx, key)) })
}

func setupMemberName(t *testing.T, cacheService *cache.Service, channelID, name string) {
	t.Helper()

	ctx := t.Context()
	require.NoError(t, cacheService.HSet(ctx, "alarm:member_names", channelID, name))
}

func fetchDeliveryRows(t *testing.T, db *deliveryTestDB, outboxID int64) []domain.YouTubeNotificationDelivery {
	t.Helper()

	var rows []domain.YouTubeNotificationDelivery

	if err := findDeliveryTestRowsOrderedWhere(db, &rows, "room_id ASC", "outbox_id = ?", outboxID).Error; err != nil {
		t.Fatalf("Failed to fetch delivery rows: %v", err)
	}

	return rows
}

type dispatcherIntegrationEnv struct {
	db           *deliveryTestDB
	sender       *fakeSender
	cacheService *cache.Service
	dispatcher   *youtubedispatch.Dispatcher
}

func requireIntegrationEnv(t *testing.T) {
	t.Helper()

	if os.Getenv("INTEGRATION_TEST") != integrationEnvEnabled {
		t.Skip("Skipping integration test (set INTEGRATION_TEST=true to run)")
	}
}

func newIntegrationTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func newIntegrationDispatchConfig(retryBackoff time.Duration) dispatchstate.Config {
	return dispatchstate.Config{
		BatchSize:    10,
		LockTimeout:  1 * time.Minute,
		PollInterval: 100 * time.Millisecond,
		MaxRetries:   3,
		RetryBackoff: retryBackoff,
	}
}

func newDispatcherIntegrationEnv(t *testing.T, config dispatchstate.Config) dispatcherIntegrationEnv {
	t.Helper()
	requireIntegrationEnv(t)

	db := dbtest.NewPool(t)

	cleanupOutbox(t, db)

	sender := &fakeSender{}
	cacheService := setupCacheService(t)

	return dispatcherIntegrationEnv{
		db:           db,
		sender:       sender,
		cacheService: cacheService,
		dispatcher:   youtubedispatch.NewDispatcher(db, cacheService, sender, nil, newIntegrationTestLogger(), &config),
	}
}

func seedIntegrationOutboxItem(t *testing.T, db *deliveryTestDB, item *domain.YouTubeNotificationOutbox) {
	t.Helper()

	if err := insertDeliveryTestRows(db, item).Error; err != nil {
		t.Fatalf("Failed to create test outbox item: %v", err)
	}

	t.Cleanup(func() { deleteDeliveryTestRows(t, db, item) })
}
