package alarmdispatch

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const retentionMetricDeliveryTable = "alarm_dispatch_deliveries"

// 등록된 전역 counter의 delta를 검사하므로 이 테스트와 하위 테스트는 병렬 실행하지 않습니다.
func TestAlarmDispatchRetentionMetricsRollbackEveryPhase(t *testing.T) {
	for _, phase := range []struct {
		name  string
		table string
		when  string
	}{
		{"sent", retentionMetricDeliveryTable, "WHEN (OLD.status = 'sent')"},
		{"dlq", retentionMetricDeliveryTable, "WHEN (OLD.status = 'dlq')"},
		{"quarantined", retentionMetricDeliveryTable, "WHEN (OLD.status = 'quarantined')"},
		{string(dispatchoutbox.StatusCancelled), retentionMetricDeliveryTable, fmt.Sprintf("WHEN (OLD.status = '%s')", dispatchoutbox.StatusCancelled)},
		{"send_unit", "alarm_dispatch_send_units", ""},
		{"upcoming_candidates", "alarm_upcoming_candidates", ""},
		{"event", "alarm_dispatch_events", ""},
	} {
		for _, timeout := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/timeout=%t", phase.name, timeout), func(t *testing.T) {
				pool := dbtest.NewPool(t)
				seedAlarmDispatchRetentionMetrics(t, pool)

				runner := newAlarmDispatchRetentionMetricsRunner(pool)
				body := "RAISE EXCEPTION 'retention phase failure' USING ERRCODE = 'P0001';"

				if timeout {
					body = "PERFORM pg_sleep(10);"
					runner.queryTimeout = time.Second
				}

				installAlarmDispatchRetentionFault(t, pool, phase.table, phase.when, body, false)

				before := alarmDispatchRetentionMetricValues(t)

				err := runner.RunOnce(t.Context())

				if timeout {
					require.ErrorIs(t, err, context.DeadlineExceeded)
				} else {
					pgErr, ok := errors.AsType[*pgconn.PgError](err)
					require.True(t, ok, "error chain: %v", err)
					require.Equal(t, "P0001", pgErr.Code)
				}

				require.Equal(t, before, alarmDispatchRetentionMetricValues(t))
				assertAlarmDispatchRetentionFixtureRows(t, pool, 4, 4, 1, 2)

				_, err = pool.Exec(t.Context(), "DROP TRIGGER retention_metric_fault ON "+pgx.Identifier{phase.table}.Sanitize())
				require.NoError(t, err)

				runner.queryTimeout = 5 * time.Second
				require.NoError(t, runner.RunOnce(t.Context()))
				assertAlarmDispatchRetentionMetricDelta(t, before)
				assertAlarmDispatchRetentionFixtureRows(t, pool, 0, 0, 0, 0)
			})
		}
	}
}

func TestAlarmDispatchRetentionMetricsCommitBoundary(t *testing.T) {
	for _, outcome := range []string{"confirmed", "commit_rejected", "commit_response_lost"} {
		t.Run(outcome, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			seedAlarmDispatchRetentionMetrics(t, pool)

			runner := newAlarmDispatchRetentionMetricsRunner(pool)
			before := alarmDispatchRetentionMetricValues(t)
			store := &alarmDispatchRetentionCommitObserver{
				store: alarmDispatchMaintenancePgxStore{db: pool, beginner: pool},
				beforeCommit: func() {
					require.Equal(t, before, alarmDispatchRetentionMetricValues(t), "callback must not publish uncommitted counts")
				},
			}

			runner.store = store

			lostResponse := errors.New("commit response lost")

			switch outcome {
			case "commit_rejected":
				installAlarmDispatchRetentionFault(t, pool, "alarm_dispatch_events", "", "RAISE EXCEPTION 'deferred retention failure' USING ERRCODE = 'P0001';", true)
			case "commit_response_lost":
				// 실제 commit 후 응답만 유실시켜 caller가 삭제를 재시도하거나 확정으로 계상하지 않음을 검사합니다.
				store.resultErr = lostResponse
			}

			err := runner.RunOnce(t.Context())
			require.Equal(t, 1, store.calls)
			require.Equal(t, 1, store.callbacks)

			switch outcome {
			case "confirmed":
				require.NoError(t, err)
				assertAlarmDispatchRetentionMetricDelta(t, before)
				assertAlarmDispatchRetentionFixtureRows(t, pool, 0, 0, 0, 0)
			case "commit_rejected":
				pgErr, ok := errors.AsType[*pgconn.PgError](err)
				require.True(t, ok, "error chain: %v", err)
				require.Equal(t, "P0001", pgErr.Code)
				require.Equal(t, before, alarmDispatchRetentionMetricValues(t))
				assertAlarmDispatchRetentionFixtureRows(t, pool, 4, 4, 1, 2)
			case "commit_response_lost":
				require.ErrorIs(t, err, lostResponse)
				require.Equal(t, before, alarmDispatchRetentionMetricValues(t))
				assertAlarmDispatchRetentionFixtureRows(t, pool, 0, 0, 0, 0)
			}
		})
	}
}

func TestAlarmDispatchRetentionMetricsSkippedLock(t *testing.T) {
	pool := dbtest.NewPool(t)
	seedAlarmDispatchRetentionMetrics(t, pool)

	runner := newAlarmDispatchRetentionMetricsRunner(pool)
	store := &alarmDispatchRetentionCommitObserver{store: alarmDispatchMaintenancePgxStore{db: pool, beginner: pool}}

	runner.store = store

	holder, err := pool.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		if rollbackErr := pgxdb.Rollback(t.Context(), holder); rollbackErr != nil {
			t.Errorf("rollback retention lock holder: %v", rollbackErr)
		}
	})

	_, err = holder.Exec(t.Context(), "SELECT pg_advisory_xact_lock($1)", runner.retentionLockKey)
	require.NoError(t, err)

	before := alarmDispatchRetentionMetricValues(t)

	require.NoError(t, runner.RunOnce(t.Context()))
	require.Equal(t, 1, store.calls)
	require.Zero(t, store.callbacks)
	require.Equal(t, before, alarmDispatchRetentionMetricValues(t))
	assertAlarmDispatchRetentionFixtureRows(t, pool, 4, 4, 1, 2)
}

func newAlarmDispatchRetentionMetricsRunner(pool *pgxpool.Pool) *alarmDispatchMaintenanceRunner {
	return &alarmDispatchMaintenanceRunner{
		store:            alarmDispatchMaintenancePgxStore{db: pool, beginner: pool},
		retentionEnabled: true,
		queryTimeout:     5 * time.Second,
		limit:            100,
		sentDays:         1,
		dlqDays:          1,
		quarantinedDays:  1,
		cancelledDays:    1,
		eventDays:        1,
		retentionLockKey: 42,
	}
}

func seedAlarmDispatchRetentionMetrics(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	eventID := seedAlarmDispatchRetentionEvent(t, pool)
	old := time.Now().UTC().Add(-72 * time.Hour)
	_, err := pool.Exec(t.Context(), "UPDATE alarm_dispatch_events SET created_at = $2 WHERE id = $1", eventID, old)
	require.NoError(t, err)

	for _, status := range []string{"sent", "dlq", "quarantined", string(dispatchoutbox.StatusCancelled)} {
		id := seedAlarmDispatchRetentionDLQ(t, pool, eventID, "retention-metric-"+status, old)
		//nolint:misspell // DB 스키마의 영속 열 이름을 유지한다.
		_, err := pool.Exec(t.Context(), `
			UPDATE alarm_dispatch_deliveries
			SET status = $2, sent_at = $3, dlq_at = $3, quarantined_at = $3, cancelled_at = $3
			WHERE id = $1`, id, status, old)
		require.NoError(t, err)
	}

	candidates := dispatchoutbox.NewUpcomingCandidates(pool)
	start := time.Now().UTC().Add(time.Hour)

	for i := range 2 {
		notification := domain.NewAlarmNotification("retention-metric-room", &domain.Channel{ID: "retention-metric-channel"}, &domain.Stream{
			ID: fmt.Sprintf("retention-metric-stream-%d", i), ChannelID: "retention-metric-channel", Status: domain.StreamStatusUpcoming, StartScheduled: &start,
		}, 5, nil, "")
		require.NoError(t, candidates.Stage(t.Context(), "retention-metric-channel", start.Add(-time.Hour), []*domain.AlarmNotification{notification}))

		key := dispatchoutbox.BuildDedupeKeyFromEnvelope(&domain.AlarmQueueEnvelope{Notification: *notification})
		require.NoError(t, candidates.Finish(t.Context(), key, "subscription_removed", old))
	}

	assertAlarmDispatchRetentionFixtureRows(t, pool, 4, 4, 1, 2)
}

func installAlarmDispatchRetentionFault(t *testing.T, pool *pgxpool.Pool, table, when, body string, deferred bool) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `CREATE FUNCTION retention_metric_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN `+body+` RETURN OLD; END $$`)
	require.NoError(t, err)

	trigger := "CREATE TRIGGER retention_metric_fault BEFORE DELETE ON "
	deferClause := ""

	if deferred {
		trigger = "CREATE CONSTRAINT TRIGGER retention_metric_fault AFTER DELETE ON "
		deferClause = " DEFERRABLE INITIALLY DEFERRED"
	}

	_, err = pool.Exec(t.Context(), trigger+pgx.Identifier{table}.Sanitize()+deferClause+" FOR EACH ROW "+when+" EXECUTE FUNCTION retention_metric_fault()")
	require.NoError(t, err)
}

func assertAlarmDispatchRetentionFixtureRows(t *testing.T, pool *pgxpool.Pool, deliveries, units, events, candidates int) {
	t.Helper()

	var gotDeliveries, gotUnits, gotEvents, gotCandidates int

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT
		(SELECT count(*) FROM alarm_dispatch_deliveries),
		(SELECT count(*) FROM alarm_dispatch_send_units),
		(SELECT count(*) FROM alarm_dispatch_events),
		(SELECT count(*) FROM alarm_upcoming_candidates)
	`).Scan(&gotDeliveries, &gotUnits, &gotEvents, &gotCandidates))
	require.Equal(t, []int{deliveries, units, events, candidates}, []int{gotDeliveries, gotUnits, gotEvents, gotCandidates})
}

func alarmDispatchRetentionMetricValues(t *testing.T) map[string]float64 {
	t.Helper()
	initAlarmDispatchRunnerMetrics()

	values := map[string]float64{"sent": 0, "dlq": 0, "quarantined": 0, string(dispatchoutbox.StatusCancelled): 0, "send_unit": 0, "event": 0, "upcoming_candidates": 0}
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	for _, family := range families {
		if family.GetName() != "alarm_dispatch_pg_retention_deleted_rows_total" {
			continue
		}

		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				if label.GetName() == "status" {
					values[label.GetValue()] = metric.GetCounter().GetValue()
				}
			}
		}
	}

	return values
}

func assertAlarmDispatchRetentionMetricDelta(t *testing.T, before map[string]float64) {
	t.Helper()

	after := alarmDispatchRetentionMetricValues(t)

	for label, delta := range map[string]float64{"sent": 1, "dlq": 1, "quarantined": 1, string(dispatchoutbox.StatusCancelled): 1, "send_unit": 4, "event": 1, "upcoming_candidates": 2} {
		require.InDelta(t, before[label]+delta, after[label], 0, "retention label %s", label)
	}
}

type alarmDispatchRetentionCommitObserver struct {
	store        alarmDispatchMaintenancePgxStore
	beforeCommit func()
	resultErr    error
	calls        int
	callbacks    int
}

func (s *alarmDispatchRetentionCommitObserver) WithAdvisoryLock(ctx context.Context, key int64, fn func(context.Context, alarmDispatchMaintenanceDataStore) error) error {
	s.calls++
	if err := s.store.WithAdvisoryLock(ctx, key, func(ctx context.Context, store alarmDispatchMaintenanceDataStore) error {
		s.callbacks++

		if err := fn(ctx, store); err != nil {
			return err
		}

		if s.beforeCommit != nil {
			s.beforeCommit()
		}

		return nil
	}); err != nil {
		return err
	}

	return s.resultErr
}
