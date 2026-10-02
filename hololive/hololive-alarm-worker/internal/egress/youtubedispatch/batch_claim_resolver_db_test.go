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

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	messagedelivery "github.com/kapu/hololive-shared/pkg/service/delivery"
)

const batchFollowerRoom = "batch-follower-room"

func newBatchClaimTestDispatcher(t *testing.T, sender messagedelivery.MessageSender, logger *slog.Logger, grouped bool, parallelism int) (*Dispatcher, *pgxpool.Pool, []domain.YouTubeNotificationDelivery, map[int64]domain.YouTubeNotificationOutbox) {
	t.Helper()

	pool := newDeliveryPool(t)
	renderer := newGroupedTemplateRenderer(t, domain.TemplateKeyOutboxShorts, "{{.Title}} {{.URL}}")
	dispatcher := newDispatcherForTest(t, pool, cachemocks.NewLenientClient(), sender, renderer, logger, &dispatchstate.Config{
		BatchSize: 10, LockTimeout: time.Minute, PollInterval: time.Second,
		MaxRetries: 3, RetryBackoff: time.Minute, DeliveryParallelism: parallelism,
	})
	outboxes := map[int64]domain.YouTubeNotificationOutbox{
		1: {ID: 1, ChannelID: testChannelCh1, Kind: domain.OutboxKindNewShort, ContentID: testShortOne, Payload: testPayloadShortOne},
		2: {ID: 2, ChannelID: testChannelCh1, Kind: domain.OutboxKindNewShort, ContentID: testShortTwo, Payload: testPayloadShortTwo},
	}
	rows := []domain.YouTubeNotificationDelivery{{ID: 101, OutboxID: 1, RoomID: testRoom1}}

	if grouped {
		rows = append(rows, domain.YouTubeNotificationDelivery{ID: 102, OutboxID: 2, RoomID: testRoom1})
	}

	rows = append(rows, domain.YouTubeNotificationDelivery{ID: 103, OutboxID: 1, RoomID: batchFollowerRoom})

	for _, id := range []int64{1, 2} {
		outbox := outboxes[id]
		require.NoError(t, insertDeliveryTestRows(pool, &outbox).Error)

		outboxes[id] = outbox
	}

	require.NoError(t, insertDeliveryTestRows(pool, rows).Error)

	claimedAt := time.Now().UTC().Truncate(time.Microsecond)

	for i := range rows {
		rows[i].RowVersion = 1
		rows[i].LockedAt = &claimedAt

		_, err := pool.Exec(t.Context(), "UPDATE youtube_notification_delivery SET row_version=1, locked_at=$1 WHERE id=$2", claimedAt, rows[i].ID)
		require.NoError(t, err)
	}

	return dispatcher, pool, rows, outboxes
}

func batchClaimAuthorization(t *testing.T, pool *pgxpool.Pool) *time.Time {
	t.Helper()

	postID, err := store.CanonicalDeliveryPostID(domain.OutboxKindNewShort, testShortOne)
	require.NoError(t, err)

	var authorized *time.Time

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT authorized_at FROM youtube_community_shorts_alarm_states WHERE kind=$1 AND post_id=$2", string(domain.OutboxKindNewShort), postID).Scan(&authorized))

	return authorized
}

func batchFollowerStatus(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()

	var status string

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT status FROM youtube_notification_delivery WHERE id=103").Scan(&status))

	return status
}

func TestBatchClaimGroupedFormatFailureKeepsSequentialFollowerProof(t *testing.T) {
	sender := &testSender{failRoom: map[string]bool{}}
	dispatcher, pool, rows, outboxes := newBatchClaimTestDispatcher(t, sender, slog.New(slog.DiscardHandler), true, 1)
	result := dispatcher.send.dispatchDeliveryRows(t.Context(), rows, outboxes)
	require.Equal(t, 2, result.FailedDeliveries)
	require.Equal(t, []int64{103}, result.SuccessDeliveryIDs)
	require.Equal(t, string(domain.OutboxStatusSent), batchFollowerStatus(t, pool))
	sender.mu.Lock()

	count := len(sender.messages)
	sender.mu.Unlock()
	require.Equal(t, 1, count)
}

func TestBatchClaimKnownSendFailureKeepsSequentialFollowerProof(t *testing.T) {
	sender := &testSender{failRoom: map[string]bool{testRoom1: true}}
	dispatcher, pool, rows, outboxes := newBatchClaimTestDispatcher(t, sender, slog.New(slog.DiscardHandler), false, 1)
	result := dispatcher.send.dispatchDeliveryRows(t.Context(), rows, outboxes)
	require.Equal(t, 1, result.FailedDeliveries)
	require.Equal(t, []int64{103}, result.SuccessDeliveryIDs)
	require.Equal(t, string(domain.OutboxStatusSent), batchFollowerStatus(t, pool))
}

func TestBatchClaimFullyFailedConsumersReleaseAuthorization(t *testing.T) {
	sender := &testSender{failRoom: map[string]bool{testRoom1: true, batchFollowerRoom: true}}
	dispatcher, pool, rows, outboxes := newBatchClaimTestDispatcher(t, sender, slog.New(slog.DiscardHandler), false, 2)
	result := dispatcher.send.dispatchDeliveryRows(t.Context(), rows, outboxes)
	require.Equal(t, 2, result.FailedDeliveries)
	require.Empty(t, result.SuccessDeliveryIDs)
	require.Nil(t, batchClaimAuthorization(t, pool))
}

type batchClaimFailureSignal struct {
	message string
	done    chan struct{}
	once    sync.Once
}

func (*batchClaimFailureSignal) Enabled(context.Context, slog.Level) bool { return true }
func (h *batchClaimFailureSignal) Handle(_ context.Context, record slog.Record) error {
	if record.Message == h.message {
		h.once.Do(func() { close(h.done) })
	}

	return nil
}
func (h *batchClaimFailureSignal) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *batchClaimFailureSignal) WithGroup(string) slog.Handler      { return h }

type batchOwnerFirstClaims struct {
	ClaimResolver

	selected chan struct{}
	once     sync.Once
}

func (c *batchOwnerFirstClaims) selectClaimedDeliveries(ctx context.Context, rows []domain.YouTubeNotificationDelivery, outboxes []domain.YouTubeNotificationOutbox, cache *claimDecisionCache) deliveryClaimSelection {
	if rows[0].RoomID != testRoom1 {
		select {
		case <-c.selected:
		case <-ctx.Done():
		}
	}

	selection := c.ClaimResolver.selectClaimedDeliveries(ctx, rows, outboxes, cache)
	if rows[0].RoomID == testRoom1 {
		c.once.Do(func() { close(c.selected) })
	}

	return selection
}

type batchFollowerSender struct {
	entered     chan struct{}
	release     chan struct{}
	enteredOnce sync.Once
	releaseOnce sync.Once
	followerErr error
}

func (s *batchFollowerSender) unblock() { s.releaseOnce.Do(func() { close(s.release) }) }
func (s *batchFollowerSender) SendMessage(ctx context.Context, room, _ string) error {
	if room == testRoom1 {
		select {
		case <-s.entered:
		case <-ctx.Done():
			return fmt.Errorf("wait for follower: %w", ctx.Err())
		}

		return iris.ErrRateLimited
	}

	s.enteredOnce.Do(func() { close(s.entered) })

	select {
	case <-s.release:
	case <-ctx.Done():
		return fmt.Errorf("wait for follower release: %w", ctx.Err())
	}

	return s.followerErr
}

func (s *batchFollowerSender) SendMessageWithClientRequestID(ctx context.Context, room, message, _ string) error {
	return s.SendMessage(ctx, room, message)
}

type batchFailureAfterFollower struct {
	deliveryTransition

	entered <-chan struct{}
}

func (s *batchFailureAfterFollower) ApplyPreparedFailure(ctx context.Context, rows []domain.YouTubeNotificationDelivery, outboxes map[int64]domain.YouTubeNotificationOutbox, kind lifecycle.FailureKind, reason lifecycle.Reason, retryAfter time.Duration, mode store.DeliveryMode) (store.ApplyResult, error) {
	if rows[0].RoomID == testRoom1 {
		select {
		case <-s.entered:
		case <-ctx.Done():
			return store.ApplyResult{}, fmt.Errorf("wait for follower: %w", ctx.Err())
		}
	}

	result, err := s.deliveryTransition.ApplyPreparedFailure(ctx, rows, outboxes, kind, reason, retryAfter, mode)
	if err != nil {
		return result, fmt.Errorf("apply prepared failure after follower: %w", err)
	}

	return result, nil
}

func TestBatchClaimFailureKeepsAlreadySendingConcurrentFollowerProof(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		grouped bool
		unknown bool
	}{
		{name: "grouped format failure", grouped: true},
		{name: "single send failure"},
		{name: "single send failure and unknown follower", unknown: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			message := "Failed to send per-room delivery"

			if testCase.grouped {
				message = "Failed to format grouped delivery"
			}

			signal := &batchClaimFailureSignal{message: message, done: make(chan struct{})}
			sender := &batchFollowerSender{entered: make(chan struct{}), release: make(chan struct{})}

			if testCase.unknown {
				sender.followerErr = errDeliverySendOutcomeUnknown
			}

			dispatcher, pool, rows, outboxes := newBatchClaimTestDispatcher(t, sender, slog.New(signal), testCase.grouped, 2)

			dispatcher.send.claims = &batchOwnerFirstClaims{ClaimResolver: dispatcher.send.claims, selected: make(chan struct{})}

			if testCase.grouped {
				dispatcher.send.transition = &batchFailureAfterFollower{deliveryTransition: dispatcher.send.transition, entered: sender.entered}
			}

			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)

			defer cancel()

			done := make(chan struct{})
			results := make(chan dispatchstate.DispatchResult, 1)

			go func() {
				defer close(done)

				results <- dispatcher.send.dispatchDeliveryRows(ctx, rows, outboxes)
			}()

			t.Cleanup(func() { sender.unblock(); <-done })

			select {
			case <-signal.done:
			case <-ctx.Done():
				t.Fatal("owner did not finish failure transition")
			}

			require.Equal(t, string(store.DeliveryStatusSending), batchFollowerStatus(t, pool))
			require.NotNil(t, batchClaimAuthorization(t, pool), "a failed owner must not revoke its live follower's claim")
			sender.unblock()

			result := <-results

			if testCase.unknown {
				require.Empty(t, result.SuccessDeliveryIDs)
				require.Equal(t, string(store.DeliveryStatusSending), batchFollowerStatus(t, pool))
				require.NotNil(t, batchClaimAuthorization(t, pool), "outcome-unknown follower must retain authorization after batch join")
			} else {
				require.Contains(t, result.SuccessDeliveryIDs, int64(103))
				require.Equal(t, string(domain.OutboxStatusSent), batchFollowerStatus(t, pool))
			}
		})
	}
}
