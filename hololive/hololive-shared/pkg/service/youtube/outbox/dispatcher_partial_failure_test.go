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

package outbox

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

type testSender struct {
	mu       sync.Mutex
	failRoom map[string]bool
	messages []string
}

type sqliteOutboxModel struct {
	ID            int64     `gorm:"primaryKey;autoIncrement"`
	Kind          string    `gorm:"type:text;not null"`
	ChannelID     string    `gorm:"type:text;not null"`
	ContentID     string    `gorm:"type:text;not null"`
	Payload       string    `gorm:"type:text;not null"`
	Status        string    `gorm:"type:text;not null"`
	AttemptCount  int       `gorm:"not null"`
	NextAttemptAt time.Time `gorm:"not null"`
	CreatedAt     time.Time
	LockedAt      *time.Time
	SentAt        *time.Time
	Error         string `gorm:"type:text"`
}

func (sqliteOutboxModel) TableName() string {
	return "youtube_notification_outbox"
}

func (s *testSender) SendMessage(_ context.Context, roomID, message string, _ ...iris.SendOption) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failRoom[roomID] {
		return assert.AnError
	}
	s.messages = append(s.messages, roomID+":"+message)
	return nil
}

func (s *testSender) SendImage(context.Context, string, string) error { return nil }
func (s *testSender) Ping(context.Context) bool                       { return true }
func (s *testSender) GetConfig(context.Context) (*iris.Config, error) { return &iris.Config{}, nil }
func (s *testSender) Decrypt(_ context.Context, data string) (string, error) {
	return data, nil
}

func TestEnqueueDeliveries_SubscriberLookupFailureReleasesLockWithoutMarkingSent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&sqliteOutboxModel{}))

	cacheSvc, mini := newDispatcherTestCache(t)
	defer mini.Close()
	defer func() { require.NoError(t, cacheSvc.Close()) }()

	now := time.Now()
	item := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewVideo,
		ChannelID:     "UC_lookup_fail",
		ContentID:     "test_lookup_fail",
		Payload:       `{"video_id":"vid1","title":"test-title"}`,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: now,
		LockedAt:      &now,
	}
	require.NoError(t, db.Create(&item).Error)

	sender := &testSender{failRoom: map[string]bool{}}
	dispatcher := NewDispatcher(db, cacheSvc, sender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 2,
	})

	dispatcher.enqueueDeliveries(ctx, []domain.YouTubeNotificationOutbox{item}, map[string]map[string]bool{})

	var updated domain.YouTubeNotificationOutbox
	require.NoError(t, db.First(&updated, item.ID).Error)
	assert.Equal(t, domain.OutboxStatusPending, updated.Status)
	assert.Nil(t, updated.LockedAt)
}

func TestEnqueueDeliveries_NoSubscribersMarksSent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&sqliteOutboxModel{}))

	cacheSvc, mini := newDispatcherTestCache(t)
	defer mini.Close()
	defer func() { require.NoError(t, cacheSvc.Close()) }()

	now := time.Now()
	item := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewVideo,
		ChannelID:     "UC_no_subscribers",
		ContentID:     "test_no_subscribers",
		Payload:       `{"video_id":"vid2","title":"test-title"}`,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: now,
		LockedAt:      &now,
	}
	require.NoError(t, db.Create(&item).Error)

	sender := &testSender{failRoom: map[string]bool{}}
	dispatcher := NewDispatcher(db, cacheSvc, sender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 2,
	})

	dispatcher.enqueueDeliveries(ctx, []domain.YouTubeNotificationOutbox{item}, map[string]map[string]bool{
		item.ChannelID: {},
	})

	var updated domain.YouTubeNotificationOutbox
	require.NoError(t, db.First(&updated, item.ID).Error)
	assert.Equal(t, domain.OutboxStatusSent, updated.Status)
}

func newDispatcherTestCache(t *testing.T) (*cache.Service, *miniredis.Miniredis) {
	t.Helper()

	mini := miniredis.RunT(t)
	host, rawPort, err := net.SplitHostPort(mini.Addr())
	require.NoError(t, err)

	port, err := strconv.Atoi(rawPort)
	require.NoError(t, err)

	svc, err := cache.NewCacheService(context.Background(), cache.Config{
		Host:         host,
		Port:         port,
		DisableCache: true,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)

	return svc, mini
}
