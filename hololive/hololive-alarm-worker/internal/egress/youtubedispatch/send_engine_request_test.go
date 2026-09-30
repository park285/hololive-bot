package youtubedispatch

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v3/iris"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type frozenProviderSender struct {
	route  string
	body   string
	err    error
	ids    []string
	routes []string
	bodies []string
}

func (s *frozenProviderSender) SendMessage(context.Context, string, string) error {
	panic("prepared sender expected")
}

func (s *frozenProviderSender) PrepareMessageRequest(context.Context, string, string) (string, string, error) {
	return s.body, s.route, nil
}

func (s *frozenProviderSender) SendPreparedMessage(_ context.Context, _, body, route, id string) error {
	s.ids = append(s.ids, id)
	s.routes = append(s.routes, route)
	s.bodies = append(s.bodies, body)

	return s.err
}

func TestFrozenProviderRequestReissuesOnlyAcrossBoundedAttempts(t *testing.T) {
	ctx := t.Context()
	pool := newDeliveryPool(t)
	config := dispatchstate.DefaultConfig()

	config.MaxRetries = 3
	config.RetryBackoff = time.Millisecond

	transition, err := store.NewTransitionStore(pool, slog.Default(), store.TransitionConfig{MaxRetries: 3, RetryBackoff: time.Millisecond, LockTimeout: time.Minute, ClaimFreshnessWindow: time.Hour, LogicalGroupLimit: 10})
	require.NoError(t, err)

	sender := &frozenProviderSender{body: "final body", route: "markdown", err: &iris.HTTPError{StatusCode: 409, Body: `{"code":"CLIENT_REQUEST_ID_FAILED"}`}}
	engine := &SendEngine{sender: sender, transition: transition, config: config, logger: slog.New(slog.DiscardHandler)}

	var outboxID int64

	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO youtube_notification_outbox(kind,channel_id,content_id,payload) VALUES('NEW_VIDEO','reissue-channel','reissue-content','{}') RETURNING id`).Scan(&outboxID))

	_, err = pool.Exec(ctx, `INSERT INTO youtube_notification_delivery(outbox_id,room_id) VALUES($1,'reissue-room')`, outboxID)
	require.NoError(t, err)

	outboxes := map[int64]domain.YouTubeNotificationOutbox{outboxID: {ID: outboxID, Kind: domain.OutboxKindNewVideo, ChannelID: "reissue-channel", ContentID: "reissue-content", Payload: "{}"}}

	for attempt := range 3 {
		_, err = pool.Exec(ctx, `UPDATE youtube_notification_delivery SET next_attempt_at=now()-interval '1 second'`)
		require.NoError(t, err)

		runFrozenProviderAttempt(t, engine, transition, outboxes, outboxes[outboxID], attempt)

		require.Len(t, sender.ids, attempt+1, "each delivery attempt must call provider once")

		sender.body = "changed config body"
		sender.route = "text"
	}

	require.Equal(t, []string{"markdown", "markdown", "markdown"}, sender.routes)
	require.Equal(t, []string{"final body", "final body", "final body"}, sender.bodies)
	require.Equal(t, sender.ids[0]+":r1", sender.ids[1])
	require.Equal(t, sender.ids[0]+":r2", sender.ids[2])

	revived, err := transition.ReviveFailedLogicalGroups(ctx, time.Hour, 10)
	require.NoError(t, err)
	require.Zero(t, revived.RevivedDeliveries)

	var (
		status   string
		attempts int
	)

	require.NoError(t, pool.QueryRow(ctx, `SELECT status,attempt_count FROM youtube_notification_delivery WHERE outbox_id=$1`, outboxID).Scan(&status, &attempts))
	require.Equal(t, "FAILED", status)
	require.Equal(t, 3, attempts)
}

func runFrozenProviderAttempt(t *testing.T, engine *SendEngine, transition *store.TransitionStore, outboxes map[int64]domain.YouTubeNotificationOutbox, outbox domain.YouTubeNotificationOutbox, attempt int) {
	t.Helper()

	ctx := t.Context()
	rows, claimErr := transition.ClaimPending(ctx, 1)
	require.NoError(t, claimErr)
	require.Len(t, rows, 1)

	var req deliverySendRequest

	if attempt == 0 {
		candidate, buildErr := buildDeliverySendRequest("reissue-room", "template body", []domain.YouTubeNotificationOutbox{outbox})
		require.NoError(t, buildErr)

		var frozen bool

		req, frozen = engine.freezeDeliveryRequest(ctx, rows, candidate)
		require.True(t, frozen)
	} else {
		restored, loadErr := transition.LoadFrozenRequests(ctx, []int64{rows[0].ID})
		require.NoError(t, loadErr)
		require.Len(t, restored, 1)

		var restoreErr error

		req, restoreErr = deliveryRequestFromFrozen(restored[0])
		require.NoError(t, restoreErr)
	}

	operation, applied, beginErr := transition.BeginSending(ctx, rows, outboxes)
	require.NoError(t, beginErr)
	require.Equal(t, store.ApplyApplied, applied.Outcome)

	sendErr := engine.sendFrozenDelivery(ctx, operation, req)

	if attempt < 2 {
		require.ErrorIs(t, sendErr, errRequestReissued)
	} else {
		require.ErrorIs(t, sendErr, errRequestGenerationsExhausted)

		kind, reason, after := lifecycleProviderFailure(sendErr)
		result, applyErr := transition.ApplyStartedFailure(ctx, operation, kind, reason, after, store.DeliveryModePerRoom)
		require.NoError(t, applyErr)
		require.Equal(t, store.ApplyApplied, result.Outcome)
	}
}
