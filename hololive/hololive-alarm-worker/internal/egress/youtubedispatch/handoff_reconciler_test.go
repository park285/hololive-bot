package youtubedispatch

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/iris-client-go/v3/iris"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/handoffcheck"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	testYouTubeHandoffHorizon = 2 * time.Minute
	testYouTubeHandoffRoom    = "room-youtube-handoff"
	testYouTubeHandoffFailed  = "failed"
)

type youtubeStatusClient struct {
	mu       sync.Mutex
	queried  []string
	snapshot func(requestID string) (*iris.ReplyStatusSnapshot, error)
}

func (c *youtubeStatusClient) GetReplyStatus(_ context.Context, requestID string) (*iris.ReplyStatusSnapshot, error) {
	c.mu.Lock()

	c.queried = append(c.queried, requestID)
	c.mu.Unlock()

	return c.snapshot(requestID)
}

func youtubeStatus(state string) func(string) (*iris.ReplyStatusSnapshot, error) {
	return func(requestID string) (*iris.ReplyStatusSnapshot, error) {
		return &iris.ReplyStatusSnapshot{RequestID: requestID, State: state}, nil
	}
}

// youtubeHandoffFixture는 send engine이 202로 보내고 SENT로 정산한 뒤 부모 outbox 집계도 SENT가 된, handoff 확인
// 대기 고정 요청 하나다.
type youtubeHandoffFixture struct {
	pool          *pgxpool.Pool
	transition    *store.TransitionStore
	deliveryID    int64
	outboxID      int64
	baseID        string
	irisRequestID string
}

func sendYouTubeHandoffFixture(t *testing.T) youtubeHandoffFixture {
	t.Helper()

	ctx := t.Context()
	pool := newDeliveryPool(t)
	logger := slog.New(slog.DiscardHandler)

	transition, err := store.NewTransitionStore(pool, logger, store.TransitionConfig{
		Owner: testTransitionOwner, MaxRetries: 3, RetryBackoff: time.Minute, LockTimeout: time.Minute,
		ClaimFreshnessWindow: time.Hour, LogicalGroupLimit: 10,
	})
	require.NoError(t, err)

	config := testDispatchConfig()

	config.DeliverySendTimeout = testYouTubeHandoffHorizon

	sender := &frozenProviderSender{body: "handoff body", route: testPreparedTextRoute}
	engine := &SendEngine{sender: sender, transition: transition, config: config, logger: logger}

	var outboxID int64

	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO youtube_notification_outbox(kind, channel_id, content_id, payload)
		VALUES('NEW_VIDEO', 'handoff-channel', 'handoff-content', '{}') RETURNING id`).Scan(&outboxID))

	_, err = pool.Exec(ctx, `INSERT INTO youtube_notification_delivery(outbox_id, room_id) VALUES($1, $2)`, outboxID, testYouTubeHandoffRoom)
	require.NoError(t, err)

	outbox := domain.YouTubeNotificationOutbox{
		ID: outboxID, Kind: domain.OutboxKindNewVideo, ChannelID: "handoff-channel", ContentID: "handoff-content", Payload: "{}",
	}
	outboxes := map[int64]domain.YouTubeNotificationOutbox{outboxID: outbox}

	rows, err := transition.ClaimPending(ctx, 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	attempt := newDeliveryAttempt([]claimedDelivery{{row: rows[0], outbox: outbox}})

	attempt.request, err = buildDeliverySendRequest(testYouTubeHandoffRoom, "template body", []domain.YouTubeNotificationOutbox{outbox})
	require.NoError(t, err)
	require.True(t, engine.freezeDeliveryRequest(ctx, &attempt))

	operation, begun, err := transition.BeginSending(ctx, rows, outboxes)
	require.NoError(t, err)
	require.Equal(t, store.ApplyApplied, begun.Outcome)

	attempt.operation = operation

	require.NoError(t, engine.sendFrozenDelivery(ctx, &attempt))

	result := newDeliveryDispatchResult(1)
	require.True(t, engine.completeLifecycleSent(ctx, &attempt, store.DeliveryModePerRoom, &result))
	require.Len(t, sender.ids, 1)

	// dispatcher가 발송 정산 뒤 하듯 부모 outbox 집계를 SENT로 맞춘다. 실패 정산은 이 SENT 부모를 되돌려야 한다.
	require.NoError(t, newOutboxAggregateProjector(store.NewDeliveryRepository(pool, logger)).Project(ctx, result.TouchedOutboxIDs))

	return youtubeHandoffFixture{
		pool: pool, transition: transition, deliveryID: rows[0].ID, outboxID: outboxID,
		baseID: attempt.request.frozen.BaseID, irisRequestID: testIrisRequestPrefix + sender.ids[0],
	}
}

// makeDue는 고정 요청의 다음 확인을 DB 시각 직전으로 당기고, age만큼 admission(sent_at)을 과거로 옮긴다.
func (f youtubeHandoffFixture) makeDue(t *testing.T, age time.Duration) {
	t.Helper()

	_, err := f.pool.Exec(t.Context(), `UPDATE youtube_notification_send_request
		SET handoff_next_check_at = clock_timestamp() - INTERVAL '1 millisecond'
		WHERE base_id = $1`, f.baseID)
	require.NoError(t, err)

	_, err = f.pool.Exec(t.Context(), `UPDATE youtube_notification_delivery
		SET sent_at = sent_at - $2::BIGINT * INTERVAL '1 millisecond'
		WHERE id = $1`, f.deliveryID, age.Milliseconds())
	require.NoError(t, err)
}

func (f youtubeHandoffFixture) reconciler(t *testing.T, client ReplyStatusClient, maxRetries int) *HandoffReconciler {
	t.Helper()

	reconciler, err := newHandoffReconciler(
		f.transition,
		client,
		handoffReconcilerConfig{Horizon: testYouTubeHandoffHorizon, PollInterval: time.Second, MaxRetries: maxRetries},
		slog.New(slog.DiscardHandler),
	)
	require.NoError(t, err)

	return reconciler
}

type youtubeHandoffState struct {
	deliveryStatus domain.OutboxStatus
	attempts       int
	errText        string
	outboxStatus   domain.OutboxStatus
	generation     int
	requestID      *string
	sentAt         *time.Time
	nextCheck      *time.Time
	checkedAt      *time.Time
}

func (f youtubeHandoffFixture) load(t *testing.T) youtubeHandoffState {
	t.Helper()

	var state youtubeHandoffState

	require.NoError(t, f.pool.QueryRow(t.Context(), `
		SELECT d.status, d.attempt_count, COALESCE(d.error, ''), o.status, r.generation, r.handoff_request_id,
		       d.sent_at, r.handoff_next_check_at, r.handoff_checked_at
		FROM youtube_notification_delivery d
		JOIN youtube_notification_outbox o ON o.id = d.outbox_id
		JOIN youtube_notification_send_request r ON r.base_id = $2
		WHERE d.id = $1
	`, f.deliveryID, f.baseID).Scan(&state.deliveryStatus, &state.attempts, &state.errText, &state.outboxStatus,
		&state.generation, &state.requestID, &state.sentAt, &state.nextCheck, &state.checkedAt))

	return state
}

// send engine의 202 성공은 SENT 정산과 함께 requestId·첫 확인을 남기고, reconciler는 그 requestId로 조회한 결과를
// 고정 요청·delivery·SENT였던 부모 outbox 집계에 한 트랜잭션으로 정산한다. 지평 전 미확인은 SENT 확인 대기로 두되 다음
// 확인을 admission + 지평 안에 둔다. 지평에 닿은 미확인과 결과 불명은 격리, 예산이 남은 failed는 다음 세대 PENDING,
// 예산이 없는 failed는 FAILED다.
func TestYouTubeHandoffReconcilerSettlesFrozenRequest(t *testing.T) {
	cases := []struct {
		name       string
		state      string
		age        time.Duration
		maxRetries int
		check      func(*testing.T, youtubeHandoffState)
	}{
		{
			name: "completed closes the check", state: "handoff_completed", maxRetries: 3,
			check: requireYouTubeHandoffCompleted,
		},
		{
			name: "queued before horizon stays awaiting within horizon", state: "queued", maxRetries: 3,
			check: requireYouTubeHandoffAwaiting,
		},
		{
			name: "queued at horizon quarantines", state: "queued", age: testYouTubeHandoffHorizon, maxRetries: 3,
			check: func(t *testing.T, state youtubeHandoffState) {
				t.Helper()
				requireYouTubeHandoffQuarantined(t, state, handoffcheck.CodeUnconfirmed)
			},
		},
		{
			name: "outcome unknown quarantines", state: "outcome_unknown", maxRetries: 3,
			check: func(t *testing.T, state youtubeHandoffState) {
				t.Helper()
				requireYouTubeHandoffQuarantined(t, state, handoffcheck.CodeOutcomeUnknown)
			},
		},
		{
			name: "failed with attempt budget reissues the next generation", state: testYouTubeHandoffFailed, maxRetries: 3,
			check: requireYouTubeHandoffReissued,
		},
		{
			name: "failed without attempt budget exhausts", state: testYouTubeHandoffFailed, maxRetries: 1,
			check: requireYouTubeHandoffExhausted,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := sendYouTubeHandoffFixture(t)
			fixture.makeDue(t, tc.age)

			client := &youtubeStatusClient{snapshot: youtubeStatus(tc.state)}

			claimed, err := fixture.reconciler(t, client, tc.maxRetries).RunOnce(t.Context())
			require.NoError(t, err)
			require.Equal(t, 1, claimed)
			require.Equal(t, []string{fixture.irisRequestID}, client.queried)

			tc.check(t, fixture.load(t))
		})
	}
}

// 종료로 끊긴 상태 조회는 관측이 아니므로 정산하지 않는다. 요청은 soft claim만 잡힌 확인 대기로 남고 soft claim이
// 끝나기 전에는 다른 확인도 고르지 않는다.
func TestYouTubeHandoffReconcilerCanceledQueryLeavesRequestUnsettled(t *testing.T) {
	fixture := sendYouTubeHandoffFixture(t)
	fixture.makeDue(t, 0)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	client := &youtubeStatusClient{snapshot: func(string) (*iris.ReplyStatusSnapshot, error) {
		cancel()

		return nil, context.Canceled
	}}

	claimed, err := fixture.reconciler(t, client, 3).RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, claimed)

	state := fixture.load(t)
	require.Equal(t, domain.OutboxStatusSent, state.deliveryStatus)
	require.Nil(t, state.checkedAt)
	require.Equal(t, new(fixture.irisRequestID), state.requestID)
	require.NotNil(t, state.nextCheck)
	require.NotNil(t, state.sentAt)
	require.True(t, state.nextCheck.After(state.sentAt.Add(10*time.Second)), "soft claim token must stay in place")

	again, err := fixture.transition.ClaimHandoffDue(t.Context(), 10, time.Second)
	require.NoError(t, err)
	require.Empty(t, again)
}

func requireYouTubeHandoffCompleted(t *testing.T, state youtubeHandoffState) {
	t.Helper()
	require.Equal(t, domain.OutboxStatusSent, state.deliveryStatus)
	require.Nil(t, state.nextCheck)
	require.NotNil(t, state.checkedAt)
	require.Equal(t, domain.OutboxStatusSent, state.outboxStatus)
}

func requireYouTubeHandoffAwaiting(t *testing.T, state youtubeHandoffState) {
	t.Helper()
	require.Equal(t, domain.OutboxStatusSent, state.deliveryStatus)
	require.Nil(t, state.checkedAt)
	require.NotNil(t, state.nextCheck)
	require.NotNil(t, state.sentAt)
	require.False(t, state.nextCheck.After(state.sentAt.Add(testYouTubeHandoffHorizon)))
}

func requireYouTubeHandoffQuarantined(t *testing.T, state youtubeHandoffState, code string) {
	t.Helper()
	require.Equal(t, store.DeliveryStatusQuarantined, state.deliveryStatus)
	require.True(t, strings.HasPrefix(state.errText, code+": "), state.errText)
	require.Equal(t, domain.OutboxStatusFailed, state.outboxStatus)
	require.NotNil(t, state.checkedAt)
}

func requireYouTubeHandoffReissued(t *testing.T, state youtubeHandoffState) {
	t.Helper()
	require.Equal(t, domain.OutboxStatusPending, state.deliveryStatus)
	require.Equal(t, 1, state.attempts)
	require.Equal(t, 1, state.generation)
	require.Nil(t, state.requestID)
	require.Nil(t, state.nextCheck)
	require.Equal(t, domain.OutboxStatusPending, state.outboxStatus)
}

func requireYouTubeHandoffExhausted(t *testing.T, state youtubeHandoffState) {
	t.Helper()
	require.Equal(t, domain.OutboxStatusFailed, state.deliveryStatus)
	require.Equal(t, 1, state.attempts)
	require.Zero(t, state.generation)
	require.NotNil(t, state.checkedAt)
	require.Equal(t, domain.OutboxStatusFailed, state.outboxStatus)
}
