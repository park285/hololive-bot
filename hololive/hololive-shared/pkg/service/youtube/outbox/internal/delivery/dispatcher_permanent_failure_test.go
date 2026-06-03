package delivery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/store"
	"github.com/park285/iris-client-go/iris"
)

type sentinelFailureSender struct {
	err error
}

func (s sentinelFailureSender) SendMessage(context.Context, string, string) error {
	return s.err
}

func TestDispatcherFlowCategorizesPermanentSentinel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		reason string
	}{
		{
			name:   "auth failed",
			err:    fmt.Errorf("wrapped auth: %w", &iris.HTTPError{StatusCode: 401}),
			reason: "auth",
		},
		{
			name:   "permanent http",
			err:    fmt.Errorf("wrapped permanent: %w", &iris.HTTPError{StatusCode: 400}),
			reason: "http-permanent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cache, mini := newDispatcherTestCache(t)
			defer mini.Close()
			defer func() {
				if err := cache.Close(); err != nil {
					t.Fatalf("close cache service: %v", err)
				}
			}()

			dispatcher := NewDispatcher(nil, cache, sentinelFailureSender{err: tt.err}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
				DeliveryParallelism: 1,
			})
			rows := []domain.YouTubeNotificationDelivery{{ID: 101, OutboxID: 1, RoomID: "room1"}}
			outboxByID := map[int64]domain.YouTubeNotificationOutbox{
				1: {
					ID:            1,
					Kind:          domain.OutboxKindNewVideo,
					ChannelID:     "UC_permanent",
					ContentID:     "video-permanent",
					Payload:       `{"video_id":"video-permanent","title":"permanent test"}`,
					Status:        domain.OutboxStatusPending,
					AttemptCount:  0,
					NextAttemptAt: time.Now(),
				},
			}

			result := dispatcher.send.dispatchDeliveryRows(context.Background(), rows, outboxByID)

			if !deliveryFailureReasonIsPermanent(tt.reason) {
				t.Fatalf("deliveryFailureReasonIsPermanent(%q) = false, want true", tt.reason)
			}
			if !reflect.DeepEqual(result.FailureBuckets[tt.reason], []int64{101}) {
				t.Fatalf("failure bucket %q = %#v, want []int64{101}", tt.reason, result.FailureBuckets[tt.reason])
			}
		})
	}
}

func TestDispatcherFlowKeepsRetryableSentinelsInRetryBucket(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "rate limited",
			err:  fmt.Errorf("wrapped rate limit: %w", &iris.HTTPError{StatusCode: 429}),
			want: "rate-limited",
		},
		{
			name: "transport",
			err:  fmt.Errorf("wrapped transport: %w", &iris.TransportError{Op: "dial", Err: errors.New("conn refused")}),
			want: "transport",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reason := deliveryFailureReason(tt.err)
			if reason != tt.want {
				t.Fatalf("deliveryFailureReason() = %q, want %q", reason, tt.want)
			}
			if deliveryFailureReasonIsPermanent(reason) {
				t.Fatalf("deliveryFailureReasonIsPermanent(%q) = true, want false", reason)
			}
		})
	}
}

func TestRepository_MarkPermanentFailureBatch_ImmediatelySetsFAILED(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newDeliveryTestDB(t)

	now := time.Now().Truncate(time.Microsecond)
	nextAttemptAt := now.Add(30 * time.Minute)
	lockedAt := now
	row := deliveryTestDeliveryModel{
		OutboxID:      1,
		RoomID:        "room-permanent",
		Status:        string(domain.OutboxStatusPending),
		AttemptCount:  0,
		NextAttemptAt: nextAttemptAt,
		CreatedAt:     now,
		LockedAt:      &lockedAt,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("create delivery row: %v", err)
	}

	repository := store.NewDeliveryRepository(db.Pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := repository.MarkPermanentFailureBatch(ctx, []int64{row.ID}, 3, "auth"); err != nil {
		t.Fatalf("MarkPermanentFailureBatch() error = %v", err)
	}

	var updated deliveryTestDeliveryModel
	if err := db.First(&updated, row.ID).Error; err != nil {
		t.Fatalf("load updated delivery row: %v", err)
	}
	if updated.Status != string(domain.OutboxStatusFailed) {
		t.Fatalf("status = %q, want %q", updated.Status, domain.OutboxStatusFailed)
	}
	if updated.AttemptCount != 3 {
		t.Fatalf("attempt_count = %d, want 3", updated.AttemptCount)
	}
	if updated.LockedAt != nil {
		t.Fatalf("locked_at = %v, want nil", updated.LockedAt)
	}
	if updated.Error != "auth" {
		t.Fatalf("error = %q, want auth", updated.Error)
	}
	if !updated.NextAttemptAt.Equal(nextAttemptAt) {
		t.Fatalf("next_attempt_at = %s, want unchanged %s", updated.NextAttemptAt, nextAttemptAt)
	}
}

func TestDispatcherMarksAuthSentinelDeliveryFAILEDImmediately(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newDeliveryTestDB(t)
	cache, mini := newDispatcherTestCache(t)
	defer mini.Close()
	defer func() {
		if err := cache.Close(); err != nil {
			t.Fatalf("close cache service: %v", err)
		}
	}()

	now := time.Now()
	outbox := deliveryTestOutboxModel{
		Kind:          string(domain.OutboxKindNewVideo),
		ChannelID:     "UC_auth_failed",
		ContentID:     "video-auth-failed",
		Payload:       `{"video_id":"video-auth-failed","title":"auth failed test"}`,
		Status:        string(domain.OutboxStatusPending),
		AttemptCount:  0,
		NextAttemptAt: now,
		CreatedAt:     now,
	}
	if err := db.Create(&outbox).Error; err != nil {
		t.Fatalf("create outbox row: %v", err)
	}
	delivery := deliveryTestDeliveryModel{
		OutboxID:      outbox.ID,
		RoomID:        "room-auth-failed",
		Status:        string(domain.OutboxStatusPending),
		AttemptCount:  0,
		NextAttemptAt: now,
		CreatedAt:     now,
	}
	if err := db.Create(&delivery).Error; err != nil {
		t.Fatalf("create delivery row: %v", err)
	}

	dispatcher := NewDispatcher(db.Pool, cache, sentinelFailureSender{err: fmt.Errorf("wrapped auth: %w", &iris.HTTPError{StatusCode: 401})}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		BatchSize:           1,
		LockTimeout:         time.Minute,
		MaxRetries:          3,
		RetryBackoff:        time.Hour,
		DeliveryParallelism: 1,
	})

	processed := dispatcher.claim.processPendingDeliveries(ctx)
	if processed != 1 {
		t.Fatalf("processed = %d, want 1", processed)
	}

	var updatedDelivery deliveryTestDeliveryModel
	if err := db.First(&updatedDelivery, delivery.ID).Error; err != nil {
		t.Fatalf("load updated delivery row: %v", err)
	}
	if updatedDelivery.Status != string(domain.OutboxStatusFailed) {
		t.Fatalf("delivery status = %q, want %q", updatedDelivery.Status, domain.OutboxStatusFailed)
	}
	if updatedDelivery.AttemptCount != 3 {
		t.Fatalf("delivery attempt_count = %d, want 3", updatedDelivery.AttemptCount)
	}
	if updatedDelivery.LockedAt != nil {
		t.Fatalf("delivery locked_at = %v, want nil", updatedDelivery.LockedAt)
	}

	var updatedOutbox deliveryTestOutboxModel
	if err := db.First(&updatedOutbox, outbox.ID).Error; err != nil {
		t.Fatalf("load updated outbox row: %v", err)
	}
	if updatedOutbox.Status != string(domain.OutboxStatusFailed) {
		t.Fatalf("outbox status = %q, want %q", updatedOutbox.Status, domain.OutboxStatusFailed)
	}
}
