package alarmdispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

// 실제 consumer 호출 직전에만 멈춰 DB 잠금 대기를 구성하며 저장소 결과는 대체하지 않는다.
type alarmLeaseClockConsumer struct {
	*dispatchoutbox.Consumer

	phase    string
	entered  chan struct{}
	resume   chan struct{}
	fenceErr error
}

func (c *alarmLeaseClockConsumer) awaitPhase(ctx context.Context, phase string) error {
	if c.phase != phase {
		return nil
	}

	close(c.entered)

	select {
	case <-c.resume:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *alarmLeaseClockConsumer) LoadSendRequest(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) (*dispatchoutbox.SendRequest, error) {
	if err := c.awaitPhase(ctx, "load"); err != nil {
		return nil, err
	}

	request, err := c.Consumer.LoadSendRequest(ctx, envelopes)
	if c.phase == "load" {
		c.fenceErr = err
	}

	if err != nil {
		return nil, fmt.Errorf("load lease-clock send request: %w", err)
	}

	return request, nil
}

func (c *alarmLeaseClockConsumer) PinSendRequest(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, candidate dispatchoutbox.SendRequest) (*dispatchoutbox.SendRequest, error) {
	if err := c.awaitPhase(ctx, "pin"); err != nil {
		return nil, err
	}

	request, err := c.Consumer.PinSendRequest(ctx, envelopes, candidate)
	if c.phase == "pin" {
		c.fenceErr = err
	}

	if err != nil {
		return nil, fmt.Errorf("pin lease-clock send request: %w", err)
	}

	return request, nil
}

func (c *alarmLeaseClockConsumer) MarkSending(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	if err := c.awaitPhase(ctx, "begin"); err != nil {
		return err
	}

	err := c.Consumer.MarkSending(ctx, envelopes)
	if c.phase == "begin" {
		c.fenceErr = err
	}

	if err != nil {
		return fmt.Errorf("begin lease-clock send request: %w", err)
	}

	return nil
}

func TestAlarmLeaseExpiryAfterUnitWaitPreservesRunnerRouting(t *testing.T) {
	for _, phase := range []string{"load", "pin", "begin"} {
		t.Run(phase, func(t *testing.T) {
			runAlarmLeaseClockScenario(t, phase)
		})
	}
}

func runAlarmLeaseClockScenario(t *testing.T, phase string) {
	t.Helper()

	pool := dbtest.NewPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)

	defer cancel()

	consumer, envelopes := seedAlarmLeaseClockConsumer(ctx, t, pool, phase)
	sender := &immutableRequestSender{route: dispatchoutbox.SendRouteText}
	runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, renderer: template.NewRenderer(pool, slog.Default())}
	group := alarmDispatchGroup{roomID: testAlarmRoomID, envelopes: envelopes}
	done := make(chan error, 1)

	var workers sync.WaitGroup

	workers.Go(func() { done <- runner.dispatchMessageGroup(ctx, group) })

	defer func() {
		cancel()
		workers.Wait()
	}()

	select {
	case <-consumer.entered:
	case dispatchErr := <-done:
		t.Fatalf("runner did not reach %s: %v", phase, dispatchErr)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	expireAlarmLeaseDuringUnitWait(ctx, t, pool, &envelopes[0], consumer, done)
	require.NoError(t, <-done)

	miss, ok := errors.AsType[*sendfence.MissError](consumer.fenceErr)
	require.True(t, ok, "want lease fence miss, got %v", consumer.fenceErr)
	require.True(t, miss.Has(sendfence.ReasonLeaseExpired), "want lease_expired, got %v", miss)
	require.Empty(t, sender.messages)
	require.Empty(t, sender.routes)
	require.Empty(t, sender.clientRequestIDs)
	assertAlarmLeaseClockRouting(ctx, t, pool, &envelopes[0])
}

func seedAlarmLeaseClockConsumer(ctx context.Context, t *testing.T, pool *pgxpool.Pool, phase string) (*alarmLeaseClockConsumer, []domain.AlarmQueueEnvelope) {
	t.Helper()

	repo := newOutboxRepository(t, pool)
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	envelope.Notification.Channel.ID = "lease-clock-channel"

	_, err := repo.InsertBatch(ctx, dispatchoutbox.PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{envelope}})
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE alarm_dispatch_deliveries SET attempt_count=2`)
	require.NoError(t, err)

	base, err := dispatchoutbox.NewConsumer(repo, requestClaimReleaser{}, nil, dispatchoutbox.WithWorkerID("lease-clock-owner"), dispatchoutbox.WithQuarantineThreshold(testAlarmQuarantineThreshold))
	require.NoError(t, err)

	envelopes, err := base.DrainBatch(ctx, 10)
	require.NoError(t, err)
	require.Len(t, envelopes, 1)

	if phase != "pin" {
		_, err = base.PinSendRequest(ctx, envelopes, dispatchoutbox.SendRequest{Body: "frozen body", Route: dispatchoutbox.SendRouteText})
		require.NoError(t, err)
	}

	consumer := &alarmLeaseClockConsumer{Consumer: base, phase: phase, entered: make(chan struct{}), resume: make(chan struct{})}

	return consumer, envelopes
}

func expireAlarmLeaseDuringUnitWait(ctx context.Context, t *testing.T, pool *pgxpool.Pool, envelope *domain.AlarmQueueEnvelope, consumer *alarmLeaseClockConsumer, done <-chan error) {
	t.Helper()

	var expires time.Time

	require.NoError(t, pool.QueryRow(ctx, `UPDATE alarm_dispatch_deliveries
		SET lock_expires_at=clock_timestamp()+interval '1 second' WHERE id=$1 RETURNING lock_expires_at`, envelope.DispatchOutboxID).Scan(&expires))

	blocker, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cleanupCancel()

		if rollbackErr := pgxdb.Rollback(cleanupCtx, blocker); rollbackErr != nil {
			require.NoError(t, rollbackErr)
		}
	}()

	_, err = blocker.Exec(ctx, `SELECT id FROM alarm_dispatch_send_units WHERE id=$1 FOR UPDATE`, envelope.SendUnitID)
	require.NoError(t, err)
	close(consumer.resume)
	waitAlarmLeaseClockBlock(ctx, t, pool, blocker.Conn().PgConn().PID(), done)

	_, err = pool.Exec(ctx, `SELECT pg_sleep(GREATEST(0,EXTRACT(EPOCH FROM ($1::timestamptz-clock_timestamp())))::double precision+0.05)`, expires)
	require.NoError(t, err)
	require.NoError(t, blocker.Commit(ctx))
}

// lease가 만료된 미발송 claim은 Load·Pin·Begin 어느 단계에서 막혀도 행을 바꾸지 않고 만료 lease 회수에 맡긴다.
func assertAlarmLeaseClockRouting(ctx context.Context, t *testing.T, pool *pgxpool.Pool, envelope *domain.AlarmQueueEnvelope) {
	t.Helper()

	var (
		status, requestID    string
		attempts, generation int
		notSending, unowned  bool
	)

	require.NoError(t, pool.QueryRow(ctx, `SELECT d.status,d.attempt_count,d.sending_started_at IS NULL,
		d.locked_by IS NULL,u.client_request_id,u.request_generation
		FROM alarm_dispatch_deliveries d JOIN alarm_dispatch_send_units u ON u.id=d.send_unit_id WHERE d.id=$1`, envelope.DispatchOutboxID).
		Scan(&status, &attempts, &notSending, &unowned, &requestID, &generation))

	require.Equal(t, "leased", status, "expired unsent claims remain recoverable")
	require.False(t, unowned)

	require.Equal(t, 2, attempts)
	require.True(t, notSending)
	require.Equal(t, envelope.ClientRequestID, requestID)
	require.Zero(t, generation)
}

func waitAlarmLeaseClockBlock(ctx context.Context, t *testing.T, pool *pgxpool.Pool, blockerPID uint32, done <-chan error) {
	t.Helper()

	ticker := time.NewTicker(5 * time.Millisecond)

	defer ticker.Stop()

	for {
		var blocked bool

		require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
			WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)))`, blockerPID).Scan(&blocked))

		if blocked {
			return
		}

		select {
		case err := <-done:
			t.Fatalf("runner did not wait on unit lock: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
}
