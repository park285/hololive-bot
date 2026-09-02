package youtubedispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v2/iris"

	dispatchstate "github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
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

			dispatcher := NewDispatcher(nil, cache, sentinelFailureSender{err: tt.err}, newSendTestRenderer(t), slog.New(slog.DiscardHandler), &dispatchstate.Config{
				DeliveryParallelism: 1,
			})
			rows := []domain.YouTubeNotificationDelivery{{ID: 101, OutboxID: 1, RoomID: testRoom1}}
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

			result := dispatcher.send.dispatchDeliveryRows(t.Context(), rows, outboxByID)

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

func TestDispatcherMarksAuthSentinelDeliveryFAILEDImmediately(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newDeliveryPool(t)
	cache, mini := newDispatcherTestCache(t)

	defer mini.Close()
	defer func() {
		if err := cache.Close(); err != nil {
			t.Fatalf("close cache service: %v", err)
		}
	}()

	outbox, delivery := seedAuthSentinelFailureRows(t, db)
	dispatcher := NewDispatcher(db, cache, sentinelFailureSender{err: fmt.Errorf("wrapped auth: %w", &iris.HTTPError{StatusCode: 401})}, nil, slog.New(slog.DiscardHandler), &dispatchstate.Config{
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

	assertAuthSentinelRowsFailed(t, db, outbox.ID, delivery.ID)
}

func seedAuthSentinelFailureRows(t *testing.T, db *deliveryTestDB) (deliveryTestOutboxModel, deliveryTestDeliveryModel) {
	t.Helper()

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

	if err := insertDeliveryTestRows(db, &outbox).Error; err != nil {
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
	if err := insertDeliveryTestRows(db, &delivery).Error; err != nil {
		t.Fatalf("create delivery row: %v", err)
	}

	return outbox, delivery
}

func assertAuthSentinelRowsFailed(t *testing.T, db *deliveryTestDB, outboxID, deliveryID int64) {
	t.Helper()

	var updatedDelivery deliveryTestDeliveryModel

	if err := firstDeliveryTestRow(db, &updatedDelivery, deliveryID).Error; err != nil {
		t.Fatalf("load updated delivery row: %v", err)
	}

	if updatedDelivery.Status != string(domain.OutboxStatusFailed) {
		t.Fatalf("delivery status = %q, want %q", updatedDelivery.Status, domain.OutboxStatusFailed)
	}

	if updatedDelivery.AttemptCount != 1 {
		t.Fatalf("delivery attempt_count = %d, want 1", updatedDelivery.AttemptCount)
	}

	if updatedDelivery.LockedAt != nil {
		t.Fatalf("delivery locked_at = %v, want nil", updatedDelivery.LockedAt)
	}

	var updatedOutbox deliveryTestOutboxModel

	if err := firstDeliveryTestRow(db, &updatedOutbox, outboxID).Error; err != nil {
		t.Fatalf("load updated outbox row: %v", err)
	}

	if updatedOutbox.Status != string(domain.OutboxStatusFailed) {
		t.Fatalf("outbox status = %q, want %q", updatedOutbox.Status, domain.OutboxStatusFailed)
	}
}
