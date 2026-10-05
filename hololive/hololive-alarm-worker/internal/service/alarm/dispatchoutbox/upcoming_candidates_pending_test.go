package dispatchoutbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestUpcomingCandidateOutcomePriorityAndFactClocks(t *testing.T) {
	now := time.Date(2026, time.October, 4, 0, 0, 0, 0, time.UTC)
	base := upcomingCandidateFacts{PayloadHash: "original", ScheduledAt: now.Add(time.Minute), SelectedAt: now.Add(-time.Minute)}

	for _, tc := range []struct {
		name   string
		change func(*upcomingCandidateFacts)
		want   string
	}{
		{"missing evidence stays pending", func(*upcomingCandidateFacts) {}, string(StatusPending)},
		{"collision beats sent and expiry", func(f *upcomingCandidateFacts) {
			f.EventPayloadHash = new("other")
			f.DeliveryStatus = new(StatusSent)
			f.ScheduledAt = now
		}, "rejected_collision"},
		{"sent beats expiry", func(f *upcomingCandidateFacts) { f.DeliveryStatus = new(StatusSent); f.ScheduledAt = now }, "accepted"},
		{"quarantine beats expiry", func(f *upcomingCandidateFacts) { f.DeliveryStatus = new(StatusQuarantined); f.ScheduledAt = now }, "rejected_terminal"},
		{"unknown delivery stays terminal", func(f *upcomingCandidateFacts) { f.DeliveryStatus = new(Status("unknown")) }, "rejected_terminal"},
		{"expiry boundary beats premiere", func(f *upcomingCandidateFacts) { f.ScheduledAt = now; f.IsPremiere = new(true) }, "expired"},
		{"premiere needs no clock", func(f *upcomingCandidateFacts) { f.IsPremiere = new(true) }, "stream_ended"},
		{"live without clock stays pending", func(f *upcomingCandidateFacts) { f.LiveStatus = new("LIVE") }, string(StatusPending)},
		{"equal status clock stays pending", func(f *upcomingCandidateFacts) { f.LiveStatus = new("ENDED"); f.StatusObservedAt = &f.SelectedAt }, string(StatusPending)},
		{"new live ends candidate", func(f *upcomingCandidateFacts) { f.LiveStatus = new("LIVE"); f.StatusObservedAt = &now }, "stream_ended"},
		{"end beats changed schedule", func(f *upcomingCandidateFacts) {
			f.LiveStatus = new("ENDED")
			f.StatusObservedAt = &now
			f.ScheduleObservedAt = &now
			f.LiveScheduledAt = new(now.Add(2 * time.Minute))
		}, "stream_ended"},
		{"equal schedule clock stays pending", func(f *upcomingCandidateFacts) {
			f.ScheduleObservedAt = &f.SelectedAt
			f.LiveScheduledAt = new(now.Add(2 * time.Minute))
		}, string(StatusPending)},
		{"new unchanged schedule stays pending", func(f *upcomingCandidateFacts) { f.ScheduleObservedAt = &now; f.LiveScheduledAt = &f.ScheduledAt }, string(StatusPending)},
		{"new null schedule stays pending", func(f *upcomingCandidateFacts) { f.ScheduleObservedAt = &now }, string(StatusPending)},
		{"new changed schedule ends candidate", func(f *upcomingCandidateFacts) {
			f.ScheduleObservedAt = &now
			f.LiveScheduledAt = new(now.Add(2 * time.Minute))
		}, "schedule_changed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts := base
			tc.change(&facts)
			require.Equal(t, tc.want, facts.outcome(now))
		})
	}

	for _, status := range []Status{StatusPending, StatusLeased, StatusRetry, StatusSending, StatusSent} {
		facts := base

		facts.DeliveryStatus = &status
		require.Equal(t, "accepted", facts.outcome(now), status)
	}
}

type candidateTransactionDB struct {
	*pgxpool.Pool

	wrap func(pgx.Tx) pgx.Tx
}

func (db candidateTransactionDB) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}

	return db.wrap(tx), nil
}

type candidateApplyTx struct {
	pgx.Tx

	beforeApply func(context.Context) error
}

func (tx candidateApplyTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if err := tx.beforeApply(ctx); err != nil {
		return pgconn.CommandTag{}, err
	}

	return tx.Tx.Exec(ctx, sql, args...)
}

func TestUpcomingCandidatesApplyFailureRollsBackEntireBatch(t *testing.T) {
	pool := dbtest.NewPool(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	notifications := []*domain.AlarmNotification{
		candidateNotification("expired", now.Add(-time.Minute)),
		candidateNotification(string(StatusPending), now.Add(time.Minute)),
	}
	store := NewUpcomingCandidates(pool)
	require.NoError(t, store.Stage(t.Context(), "staged-channel", now, notifications))

	_, err := pool.Exec(t.Context(), "ALTER TABLE alarm_upcoming_candidates ADD CONSTRAINT test_reject_expired CHECK (outcome <> 'expired')")
	require.NoError(t, err)

	pending, err := store.Pending(t.Context(), now.Add(time.Second))
	require.ErrorContains(t, err, "test_reject_expired")
	require.Nil(t, pending)

	var unchanged int

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM alarm_upcoming_candidates WHERE outcome='pending' AND checked_at=$1 AND terminal_at IS NULL", now).Scan(&unchanged))
	require.Equal(t, 2, unchanged)

	_, err = pool.Exec(t.Context(), "ALTER TABLE alarm_upcoming_candidates DROP CONSTRAINT test_reject_expired")
	require.NoError(t, err)

	pending, err = store.Pending(t.Context(), now.Add(time.Second))
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, string(StatusPending), pending[0].Notification.RoomID)
}

func TestUpcomingCandidatesKeepLocksUntilApply(t *testing.T) {
	pool := dbtest.NewPool(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	store := NewUpcomingCandidates(pool)
	require.NoError(t, store.Stage(t.Context(), "staged-channel", now, []*domain.AlarmNotification{candidateNotification("locked", now.Add(time.Minute))}))

	selected := make(chan struct{})
	apply := make(chan struct{})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)

	defer cancel()

	held := NewUpcomingCandidates(candidateTransactionDB{Pool: pool, wrap: func(tx pgx.Tx) pgx.Tx {
		return candidateApplyTx{Tx: tx, beforeApply: func(ctx context.Context) error {
			close(selected)

			select {
			case <-apply:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}}
	}})

	type result struct {
		candidates []UpcomingCandidate
		err        error
	}

	done := make(chan result, 1)

	go func() { candidates, err := held.Pending(ctx, now.Add(time.Second)); done <- result{candidates, err} }()

	select {
	case <-selected:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	pending, err := store.Pending(ctx, now.Add(2*time.Second))
	// 실패하더라도 먼저 진행 중인 transaction을 풀고 결과를 회수한다.
	close(apply)

	first := <-done

	require.NoError(t, err)
	require.Empty(t, pending, "다른 evaluator는 Go 판정 중인 후보를 건너뛰어야 한다")
	require.NoError(t, first.err)
	require.Len(t, first.candidates, 1)
}

type lostCandidateCommitTx struct{ pgx.Tx }

func (tx lostCandidateCommitTx) Commit(ctx context.Context) error {
	if err := tx.Tx.Commit(ctx); err != nil {
		return err
	}

	return errors.New("injected commit response loss")
}

func TestUpcomingCandidatesCommitResponseLossReturnsNoCandidates(t *testing.T) {
	pool := dbtest.NewPool(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	store := NewUpcomingCandidates(pool)
	require.NoError(t, store.Stage(t.Context(), "staged-channel", now, []*domain.AlarmNotification{candidateNotification("commit-loss", now.Add(time.Minute))}))

	lost := NewUpcomingCandidates(candidateTransactionDB{Pool: pool, wrap: func(tx pgx.Tx) pgx.Tx { return lostCandidateCommitTx{Tx: tx} }})
	pending, err := lost.Pending(t.Context(), now.Add(time.Second))
	require.ErrorContains(t, err, "injected commit response loss")
	require.Nil(t, pending)

	pending, err = store.Pending(t.Context(), now.Add(2*time.Second))
	require.NoError(t, err)
	require.Len(t, pending, 1)
}

// batch 시작 전에도 같은 fault를 주입하여 SELECT부터 저장까지의 잠금을 확인한다.
func (tx candidateApplyTx) SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults {
	if err := tx.beforeApply(ctx); err != nil {
		return candidateFailedBatch{err: err}
	}

	return tx.Tx.SendBatch(ctx, batch)
}

type candidateFailedBatch struct{ err error }

func (b candidateFailedBatch) Exec() (pgconn.CommandTag, error) { return pgconn.CommandTag{}, b.err }
func (b candidateFailedBatch) Query() (pgx.Rows, error)         { return nil, b.err }
func (b candidateFailedBatch) QueryRow() pgx.Row                { return b }
func (b candidateFailedBatch) Scan(...any) error                { return b.err }
func (b candidateFailedBatch) Close() error                     { return b.err }

func TestUpcomingCandidatesDecodeFailurePreservesCommittedOutcomes(t *testing.T) {
	pool := dbtest.NewPool(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	store := NewUpcomingCandidates(pool)
	require.NoError(t, store.Stage(t.Context(), "staged-channel", now, []*domain.AlarmNotification{
		candidateNotification("decode-expired", now.Add(-time.Minute)),
		candidateNotification("decode-invalid", now.Add(time.Minute)),
	}))

	_, err := pool.Exec(t.Context(), `UPDATE alarm_upcoming_candidates SET notification='{"stream":1}'::jsonb WHERE room_id='decode-invalid'`)
	require.NoError(t, err)

	pending, err := store.Pending(t.Context(), now.Add(time.Second))
	require.ErrorContains(t, err, "decode upcoming candidate")
	require.Nil(t, pending)

	var outcome string

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT outcome FROM alarm_upcoming_candidates WHERE room_id='decode-expired'").Scan(&outcome))
	require.Equal(t, "expired", outcome)

	var checkedAt time.Time

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT checked_at FROM alarm_upcoming_candidates WHERE room_id='decode-invalid'").Scan(&checkedAt))
	require.True(t, checkedAt.Equal(now.Add(time.Second)))
}
