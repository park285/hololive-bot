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

package outbox_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	"github.com/park285/iris-client-go/iris"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
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

func (f *fakeSender) SendMessage(_ context.Context, room, message string, _ ...iris.SendOption) error {
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

func (f *fakeSender) SendImage(_ context.Context, _, _ string) error { return nil }
func (f *fakeSender) Ping(_ context.Context) bool                    { return true }
func (f *fakeSender) GetConfig(_ context.Context) (*iris.Config, error) {
	return &iris.Config{}, nil
}
func (f *fakeSender) Decrypt(_ context.Context, data string) (string, error) { return data, nil }

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
	if os.Getenv("INTEGRATION_TEST") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TEST=true to run)")
	}

	ctx := context.Background()
	db := setupTestDB(t)
	cleanupOutbox(t, db)

	sender := &fakeSender{}
	cacheService := setupCacheService(t)
	testLogger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	setupTestSubscribers(t, cacheService)

	cfg := outbox.Config{
		BatchSize:    10,
		LockTimeout:  1 * time.Minute,
		PollInterval: 100 * time.Millisecond,
		MaxRetries:   3,
		RetryBackoff: 1 * time.Second,
	}

	dispatcher := outbox.NewDispatcher(db, cacheService, sender, nil, testLogger, cfg)

	payload, _ := json.Marshal(map[string]string{
		"video_id": "test123",
		"title":    "Test Video Title",
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

	if err := db.Create(item).Error; err != nil {
		t.Fatalf("Failed to create test outbox item: %v", err)
	}
	t.Cleanup(func() {
		db.Delete(item)
	})

	dispatcher.ProcessOnceForTest(ctx)

	var updated domain.YouTubeNotificationOutbox
	if err := db.First(&updated, item.ID).Error; err != nil {
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
	if os.Getenv("INTEGRATION_TEST") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TEST=true to run)")
	}

	ctx := context.Background()
	db := setupTestDB(t)
	cleanupOutbox(t, db)

	sender := &fakeSender{}
	sender.setFailNext()

	cacheService := setupCacheService(t)
	testLogger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	setupTestSubscribers(t, cacheService)

	cfg := outbox.Config{
		BatchSize:    10,
		LockTimeout:  1 * time.Minute,
		PollInterval: 100 * time.Millisecond,
		MaxRetries:   3,
		RetryBackoff: 1 * time.Second,
	}

	dispatcher := outbox.NewDispatcher(db, cacheService, sender, nil, testLogger, cfg)

	payload, _ := json.Marshal(map[string]string{
		"video_id": "retry123",
		"title":    "Retry Test Video",
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

	if err := db.Create(item).Error; err != nil {
		t.Fatalf("Failed to create test outbox item: %v", err)
	}
	t.Cleanup(func() {
		db.Delete(item)
	})

	dispatcher.ProcessOnceForTest(ctx)

	var updated domain.YouTubeNotificationOutbox
	if err := db.First(&updated, item.ID).Error; err != nil {
		t.Fatalf("Failed to fetch updated item: %v", err)
	}

	if updated.Status != domain.OutboxStatusPending {
		t.Errorf("Expected status PENDING (for retry), got %s", updated.Status)
	}

	if updated.AttemptCount != 1 {
		t.Errorf("Expected attempt_count 1, got %d", updated.AttemptCount)
	}

	if updated.NextAttemptAt.Before(time.Now()) {
		t.Error("Expected next_attempt_at to be in the future")
	}

	if updated.LockedAt != nil {
		t.Error("Expected locked_at to be nil after failure")
	}
}

func TestDispatcher_NoSubscribers_MarkedAsSent(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TEST=true to run)")
	}

	ctx := context.Background()
	db := setupTestDB(t)
	cleanupOutbox(t, db)

	sender := &fakeSender{}
	cacheService := setupCacheService(t)
	testLogger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	cfg := outbox.Config{
		BatchSize:    10,
		LockTimeout:  1 * time.Minute,
		PollInterval: 100 * time.Millisecond,
		MaxRetries:   3,
		RetryBackoff: 1 * time.Second,
	}

	dispatcher := outbox.NewDispatcher(db, cacheService, sender, nil, testLogger, cfg)

	payload, _ := json.Marshal(map[string]string{
		"video_id": "nosub123",
		"title":    "No Subscribers Test",
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

	if err := db.Create(item).Error; err != nil {
		t.Fatalf("Failed to create test outbox item: %v", err)
	}
	t.Cleanup(func() {
		db.Delete(item)
	})

	dispatcher.ProcessOnceForTest(ctx)

	var updated domain.YouTubeNotificationOutbox
	if err := db.First(&updated, item.ID).Error; err != nil {
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
	if os.Getenv("INTEGRATION_TEST") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TEST=true to run)")
	}

	ctx := context.Background()
	db := setupTestDB(t)
	cleanupOutbox(t, db)

	sender := &fakeSender{}
	cacheService := setupCacheService(t)
	testLogger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	setupChannelSubscribers(t, cacheService, "alarm:channel_subscribers:SHORTS:UCperroom_success", []string{"roomA", "roomB"})
	setupMemberName(t, cacheService, "UCperroom_success", "PerRoomMember")

	cfg := outbox.Config{
		BatchSize:    10,
		LockTimeout:  1 * time.Minute,
		PollInterval: 100 * time.Millisecond,
		MaxRetries:   3,
		RetryBackoff: 50 * time.Millisecond,
	}

	dispatcher := outbox.NewDispatcher(db, cacheService, sender, nil, testLogger, cfg)

	payload, _ := json.Marshal(map[string]string{
		"video_id": "perroom_success_video",
		"title":    "PerRoom Success Video",
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
	if err := db.Create(item).Error; err != nil {
		t.Fatalf("Failed to create test outbox item: %v", err)
	}
	t.Cleanup(func() { db.Delete(item) })

	dispatcher.ProcessOnceForTest(ctx)

	var updated domain.YouTubeNotificationOutbox
	if err := db.First(&updated, item.ID).Error; err != nil {
		t.Fatalf("Failed to fetch updated item: %v", err)
	}
	if updated.Status != domain.OutboxStatusSent {
		t.Fatalf("Expected status SENT, got %s", updated.Status)
	}

	deliveries := fetchDeliveryRows(t, db, item.ID)
	if len(deliveries) != 2 {
		t.Fatalf("Expected 2 delivery rows, got %d", len(deliveries))
	}
	for i := range deliveries {
		if deliveries[i].Status != domain.OutboxStatusSent {
			t.Fatalf("Expected delivery[%d] status SENT, got %s", i, deliveries[i].Status)
		}
	}

	msgs := sender.getMessages()
	if len(msgs) != 2 {
		t.Fatalf("Expected 2 messages sent, got %d", len(msgs))
	}
}

func TestDispatcher_PerRoomMode_PartialFailureThenRetry(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TEST=true to run)")
	}

	ctx := context.Background()
	db := setupTestDB(t)
	cleanupOutbox(t, db)

	sender := &fakeSender{}
	sender.setFailRoom("roomB")
	cacheService := setupCacheService(t)
	testLogger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	setupChannelSubscribers(t, cacheService, "alarm:channel_subscribers:UCperroom_retry", []string{"roomA", "roomB"})
	setupMemberName(t, cacheService, "UCperroom_retry", "PerRoomRetryMember")

	cfg := outbox.Config{
		BatchSize:    10,
		LockTimeout:  1 * time.Minute,
		PollInterval: 100 * time.Millisecond,
		MaxRetries:   3,
		RetryBackoff: 30 * time.Millisecond,
	}

	dispatcher := outbox.NewDispatcher(db, cacheService, sender, nil, testLogger, cfg)

	payload, _ := json.Marshal(map[string]string{
		"video_id": "perroom_retry_video",
		"title":    "PerRoom Retry Video",
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
	if err := db.Create(item).Error; err != nil {
		t.Fatalf("Failed to create test outbox item: %v", err)
	}
	t.Cleanup(func() { db.Delete(item) })

	dispatcher.ProcessOnceForTest(ctx)

	var first domain.YouTubeNotificationOutbox
	if err := db.First(&first, item.ID).Error; err != nil {
		t.Fatalf("Failed to fetch first state: %v", err)
	}
	if first.Status != domain.OutboxStatusPending {
		t.Fatalf("Expected outbox status PENDING after partial failure, got %s", first.Status)
	}

	deliveries := fetchDeliveryRows(t, db, item.ID)
	if len(deliveries) != 2 {
		t.Fatalf("Expected 2 delivery rows, got %d", len(deliveries))
	}

	time.Sleep(40 * time.Millisecond)
	dispatcher.ProcessOnceForTest(ctx)

	var second domain.YouTubeNotificationOutbox
	if err := db.First(&second, item.ID).Error; err != nil {
		t.Fatalf("Failed to fetch second state: %v", err)
	}
	if second.Status != domain.OutboxStatusSent {
		t.Fatalf("Expected outbox status SENT after retry success, got %s", second.Status)
	}

	finalDeliveries := fetchDeliveryRows(t, db, item.ID)
	for i := range finalDeliveries {
		if finalDeliveries[i].Status != domain.OutboxStatusSent {
			t.Fatalf("Expected final delivery[%d] status SENT, got %s", i, finalDeliveries[i].Status)
		}
	}
}

func TestDispatcher_PerRoomMode_NoSubscribers_MarkedAsSentWithoutDeliveryRows(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TEST=true to run)")
	}

	ctx := context.Background()
	db := setupTestDB(t)
	cleanupOutbox(t, db)

	sender := &fakeSender{}
	cacheService := setupCacheService(t)
	testLogger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	cfg := outbox.Config{
		BatchSize:    10,
		LockTimeout:  1 * time.Minute,
		PollInterval: 100 * time.Millisecond,
		MaxRetries:   3,
		RetryBackoff: 50 * time.Millisecond,
	}

	dispatcher := outbox.NewDispatcher(db, cacheService, sender, nil, testLogger, cfg)

	payload, _ := json.Marshal(map[string]string{
		"video_id": "perroom_no_sub_video",
		"title":    "PerRoom No Subscribers Video",
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
	if err := db.Create(item).Error; err != nil {
		t.Fatalf("Failed to create test outbox item: %v", err)
	}
	t.Cleanup(func() { db.Delete(item) })

	dispatcher.ProcessOnceForTest(ctx)

	var updated domain.YouTubeNotificationOutbox
	if err := db.First(&updated, item.ID).Error; err != nil {
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
	if os.Getenv("INTEGRATION_TEST") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TEST=true to run)")
	}

	ctx := context.Background()
	db := setupTestDB(t)
	cleanupOutbox(t, db)

	sender := &fakeSender{}
	sender.setFailRoom("roomB")
	cacheService := setupCacheService(t)
	testLogger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	setupChannelSubscribers(t, cacheService, "alarm:channel_subscribers:UCperroom_terminal_fail", []string{"roomA", "roomB"})
	setupMemberName(t, cacheService, "UCperroom_terminal_fail", "PerRoomTerminalFailMember")

	cfg := outbox.Config{
		BatchSize:    10,
		LockTimeout:  1 * time.Minute,
		PollInterval: 100 * time.Millisecond,
		MaxRetries:   1,
		RetryBackoff: 30 * time.Millisecond,
	}

	dispatcher := outbox.NewDispatcher(db, cacheService, sender, nil, testLogger, cfg)

	payload, _ := json.Marshal(map[string]string{
		"video_id": "perroom_terminal_fail_video",
		"title":    "PerRoom Terminal Fail Video",
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
	if err := db.Create(item).Error; err != nil {
		t.Fatalf("Failed to create test outbox item: %v", err)
	}
	t.Cleanup(func() { db.Delete(item) })

	dispatcher.ProcessOnceForTest(ctx)

	var updated domain.YouTubeNotificationOutbox
	if err := db.First(&updated, item.ID).Error; err != nil {
		t.Fatalf("Failed to fetch updated outbox: %v", err)
	}
	if updated.Status != domain.OutboxStatusFailed {
		t.Fatalf("Expected outbox status FAILED, got %s", updated.Status)
	}

	deliveries := fetchDeliveryRows(t, db, item.ID)
	if len(deliveries) != 2 {
		t.Fatalf("Expected 2 delivery rows, got %d", len(deliveries))
	}
	failedCount := 0
	sentCount := 0
	for i := range deliveries {
		switch deliveries[i].Status {
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

func TestDispatcher_Cleanup_RemovesOldFailedRows(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TEST=true to run)")
	}

	ctx := context.Background()
	db := setupTestDB(t)
	cleanupOutbox(t, db)

	sender := &fakeSender{}
	cacheService := setupCacheService(t)
	testLogger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	cfg := outbox.Config{
		BatchSize:      10,
		LockTimeout:    1 * time.Minute,
		PollInterval:   100 * time.Millisecond,
		MaxRetries:     3,
		RetryBackoff:   1 * time.Second,
		CleanupAfter:   1 * time.Hour,
		CleanupEnabled: false,
	}

	dispatcher := outbox.NewDispatcher(db, cacheService, sender, nil, testLogger, cfg)

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

	if err := db.Create(oldFailed).Error; err != nil {
		t.Fatalf("Failed to create old failed outbox item: %v", err)
	}
	if err := db.Create(recentFailed).Error; err != nil {
		t.Fatalf("Failed to create recent failed outbox item: %v", err)
	}

	dispatcher.CleanupForTest(ctx)

	var oldCount int64
	if err := db.Model(&domain.YouTubeNotificationOutbox{}).Where("id = ?", oldFailed.ID).Count(&oldCount).Error; err != nil {
		t.Fatalf("Failed to count old failed item: %v", err)
	}
	if oldCount != 0 {
		t.Fatalf("Expected old failed item to be deleted, still exists")
	}

	var recentCount int64
	if err := db.Model(&domain.YouTubeNotificationOutbox{}).Where("id = ?", recentFailed.ID).Count(&recentCount).Error; err != nil {
		t.Fatalf("Failed to count recent failed item: %v", err)
	}
	if recentCount != 1 {
		t.Fatalf("Expected recent failed item to remain, count=%d", recentCount)
	}
}

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "host=localhost port=5432 user=twentyq_app password=twentyq_password dbname=hololive sslmode=disable"
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	if err := db.AutoMigrate(&domain.YouTubeNotificationDelivery{}); err != nil {
		t.Fatalf("Failed to migrate youtube_notification_delivery table: %v", err)
	}

	return db
}

func cleanupOutbox(t *testing.T, db *gorm.DB) {
	t.Helper()
	db.Exec(`
		DELETE FROM youtube_notification_delivery
		WHERE outbox_id IN (
			SELECT id FROM youtube_notification_outbox WHERE content_id LIKE 'test%'
		)
	`)
	db.Exec("DELETE FROM youtube_notification_outbox WHERE content_id LIKE 'test%'")
}

func setupCacheService(t *testing.T) *cache.Service {
	t.Helper()

	valkeyHost := os.Getenv("TEST_VALKEY_HOST")
	if valkeyHost == "" {
		valkeyHost = "localhost"
	}

	cfg := cache.Config{
		Host:         valkeyHost,
		Port:         6379,
		DisableCache: true,
	}

	testLogger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cacheService, err := cache.NewCacheService(context.Background(), cfg, testLogger)
	if err != nil {
		t.Fatalf("Failed to create cache service: %v", err)
	}

	t.Cleanup(func() {
		cacheService.Close()
	})

	return cacheService
}

func setupTestSubscribers(t *testing.T, cacheService *cache.Service) {
	t.Helper()
	ctx := context.Background()

	cacheService.SAdd(ctx, "alarm:channel_subscribers:SHORTS:UCtest123", []string{"testroom"})
	cacheService.SAdd(ctx, "alarm:channel_subscribers:UCtest456", []string{"testroom"})
	cacheService.HSet(ctx, "alarm:member_names", "UCtest123", "TestMember")
	cacheService.HSet(ctx, "alarm:member_names", "UCtest456", "TestMember2")

	t.Cleanup(func() {
		cacheService.Del(ctx, "alarm:channel_subscribers:SHORTS:UCtest123")
		cacheService.Del(ctx, "alarm:channel_subscribers:UCtest456")
	})
}

func setupChannelSubscribers(t *testing.T, cacheService *cache.Service, key string, subscribers []string) {
	t.Helper()
	ctx := context.Background()
	cacheService.SAdd(ctx, key, subscribers)
	t.Cleanup(func() { cacheService.Del(ctx, key) })
}

func setupMemberName(t *testing.T, cacheService *cache.Service, channelID, name string) {
	t.Helper()
	ctx := context.Background()
	cacheService.HSet(ctx, "alarm:member_names", channelID, name)
}

func fetchDeliveryRows(t *testing.T, db *gorm.DB, outboxID int64) []domain.YouTubeNotificationDelivery {
	t.Helper()
	var rows []domain.YouTubeNotificationDelivery
	if err := db.Where("outbox_id = ?", outboxID).Order("room_id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("Failed to fetch delivery rows: %v", err)
	}
	return rows
}
