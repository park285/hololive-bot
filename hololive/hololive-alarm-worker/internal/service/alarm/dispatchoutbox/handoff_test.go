package dispatchoutbox

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/iris-client-go/v5/iris"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/handoffcheck"
	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	handoffTestOwner     = "handoff-owner"
	handoffTestSoftClaim = 15 * time.Second
)

type handoffTestUnit struct {
	id        int64
	members   []int64
	claimKeys []string
}

type handoffUnitState struct {
	irisRequestID   *string
	clientRequestID string
	generation      int
	nextCheck       *time.Time
	checked         bool
}

func handoffTestIrisRequestID(unit int64, generation int) string {
	return fmt.Sprintf("iris-%d-g%d", unit, generation)
}

func claimSendingUnits(t *testing.T, repo *PgxRepository, rooms ...string) [][]*Record {
	t.Helper()

	envelopes := make([]domain.AlarmQueueEnvelope, 0, 2*len(rooms))
	for _, room := range rooms {
		first := receiptEnvelope(room, "first")
		second := receiptEnvelope(room, "second")

		second.Notification.Stream.ID = room + "-second"
		first.ClaimKeys = []string{"notified:claim:" + room + ":first:100:live"}
		second.ClaimKeys = []string{"notified:claim:" + room + ":second:100:live"}
		envelopes = append(envelopes, first, second)
	}

	_, err := repo.InsertBatch(t.Context(), PublishBatchInput{Envelopes: envelopes})
	require.NoError(t, err)

	records, err := repo.ClaimDue(t.Context(), handoffTestOwner, len(envelopes)+1, time.Minute)
	require.NoError(t, err)
	require.Len(t, records, len(envelopes))
	beginSendingFixture(t, repo, handoffTestOwner, records...)

	byRoom := make([][]*Record, len(rooms))

	for _, record := range records {
		byRoom[slices.Index(rooms, record.RoomID)] = append(byRoom[slices.Index(rooms, record.RoomID)], record)
	}

	return byRoom
}

func seedSentHandoffUnits(t *testing.T, pool *pgxpool.Pool, repo *PgxRepository, rooms ...string) []handoffTestUnit {
	t.Helper()

	units := make([]handoffTestUnit, 0, len(rooms))
	for _, records := range claimSendingUnits(t, repo, rooms...) {
		unit := handoffTestUnit{id: records[0].SendUnitID}
		for _, record := range records {
			unit.members = append(unit.members, record.ID)
			unit.claimKeys = append(unit.claimKeys, record.ClaimKeys...)
		}

		slices.Sort(unit.members)
		require.NoError(t, repo.MarkSent(t.Context(), unit.id, recordClaims(records...), handoffTestOwner,
			handoffTestIrisRequestID(unit.id, 0), handoffcheck.FirstCheckDelay))

		units = append(units, unit)
	}

	for _, unit := range units {
		makeHandoffDue(t, pool, unit.id)
	}

	return units
}

func makeHandoffDue(t *testing.T, pool *pgxpool.Pool, unit int64) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `UPDATE alarm_dispatch_send_units
		SET handoff_next_check_at = clock_timestamp() - interval '1 second' WHERE id = $1`, unit)
	require.NoError(t, err)
}

func loadHandoffUnit(t *testing.T, pool *pgxpool.Pool, unit int64) handoffUnitState {
	t.Helper()

	var state handoffUnitState

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT handoff_request_id, client_request_id, request_generation,
		handoff_next_check_at, handoff_checked_at IS NOT NULL
		FROM alarm_dispatch_send_units WHERE id = $1`, unit,
	).Scan(&state.irisRequestID, &state.clientRequestID, &state.generation, &state.nextCheck, &state.checked))

	return state
}

func memberStatuses(t *testing.T, pool *pgxpool.Pool, unit int64) []string {
	t.Helper()

	rows, err := pool.Query(t.Context(), `SELECT status FROM alarm_dispatch_deliveries WHERE send_unit_id = $1 ORDER BY id`, unit)
	require.NoError(t, err)

	statuses, err := pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(t, err)

	return statuses
}

func claimOneHandoff(t *testing.T, repo *PgxRepository) HandoffClaim {
	t.Helper()

	claims, err := repo.ClaimHandoffDue(t.Context(), 10, handoffTestSoftClaim)
	require.NoError(t, err)
	require.Len(t, claims, 1)

	return claims[0]
}

func TestHandoffMarkSentRecordsCheckOnUnit(t *testing.T) {
	pool := dbtest.NewPool(t)
	repo := newTestRepository(t, pool)
	records := claimSendingUnits(t, repo, "handoff-marksent")[0]
	unit := records[0].SendUnitID

	require.NoError(t, repo.MarkSent(t.Context(), unit, recordClaims(records...), handoffTestOwner, "iris-request", handoffcheck.FirstCheckDelay))

	var delayMS int64

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT (EXTRACT(EPOCH FROM (u.handoff_next_check_at - max(d.sent_at))) * 1000)::BIGINT
		FROM alarm_dispatch_send_units u JOIN alarm_dispatch_deliveries d ON d.send_unit_id = u.id
		WHERE u.id = $1 GROUP BY u.id`, unit).Scan(&delayMS))
	require.Equal(t, handoffcheck.FirstCheckDelay.Milliseconds(), delayMS)

	state := loadHandoffUnit(t, pool, unit)
	require.Equal(t, new("iris-request"), state.irisRequestID)
	require.False(t, state.checked)
	require.Equal(t, []string{string(StatusSent), string(StatusSent)}, memberStatuses(t, pool, unit))
}

func TestConsumerMarkDispatchedClipsFirstCheckToHorizon(t *testing.T) {
	cases := []struct {
		name    string
		horizon time.Duration
		want    time.Duration
	}{
		{name: "horizon shorter than first check", horizon: 1500 * time.Millisecond, want: 1500 * time.Millisecond},
		{name: "horizon longer than first check", horizon: 10 * time.Minute, want: handoffcheck.FirstCheckDelay},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			repo := newTestRepository(t, pool)
			records := claimSendingUnits(t, repo, "handoff-first-check")[0]
			unit := records[0].SendUnitID

			envelopes := make([]domain.AlarmQueueEnvelope, len(records))
			for i, record := range records {
				attachRecordMetadata(&envelopes[i], record)
			}

			consumer := mustNewConsumer(t, repo, nil, nil, WithWorkerID(handoffTestOwner))
			require.NoError(t, consumer.MarkDispatched(t.Context(), envelopes, "iris-request", tc.horizon))

			var delayMS int64

			require.NoError(t, pool.QueryRow(t.Context(), `SELECT (EXTRACT(EPOCH FROM (u.handoff_next_check_at - max(d.sent_at))) * 1000)::BIGINT
				FROM alarm_dispatch_send_units u JOIN alarm_dispatch_deliveries d ON d.send_unit_id = u.id
				WHERE u.id = $1 GROUP BY u.id`, unit).Scan(&delayMS))
			require.Equal(t, tc.want.Milliseconds(), delayMS)
			require.Equal(t, new("iris-request"), loadHandoffUnit(t, pool, unit).irisRequestID)
			require.Equal(t, []string{string(StatusSent), string(StatusSent)}, memberStatuses(t, pool, unit))
		})
	}
}

func TestHandoffPartialMarkSentRecordsNoCheck(t *testing.T) {
	pool := dbtest.NewPool(t)
	repo := newTestRepository(t, pool)
	records := claimSendingUnits(t, repo, "handoff-partial")[0]
	unit := records[0].SendUnitID
	claims := []DeliveryClaim{{ID: records[0].ID, AttemptCount: records[0].AttemptCount}, {ID: records[1].ID, AttemptCount: records[1].AttemptCount + 1}}

	miss := requireFenceMiss(t, repo.MarkSent(t.Context(), unit, claims, handoffTestOwner, "iris-request", handoffcheck.FirstCheckDelay), sendfence.ReasonVersionChanged)
	require.Equal(t, 1, miss.Applied)

	state := loadHandoffUnit(t, pool, unit)
	require.Nil(t, state.irisRequestID)
	require.Nil(t, state.nextCheck)
	require.False(t, state.checked)
}

func TestHandoffClaimSkipsLockedAndSoftClaimedUnits(t *testing.T) {
	pool := dbtest.NewPool(t)
	repo := newTestRepository(t, pool)
	units := seedSentHandoffUnits(t, pool, repo, "handoff-a", "handoff-b", "handoff-c")

	blocker, err := pool.Begin(t.Context())
	require.NoError(t, err)

	t.Cleanup(func() { assert.NoError(t, pgxdb.Rollback(context.WithoutCancel(t.Context()), blocker)) })

	_, err = blocker.Exec(t.Context(), `SELECT id FROM alarm_dispatch_send_units WHERE id = $1 FOR UPDATE`, units[0].id)
	require.NoError(t, err)

	claims, err := repo.ClaimHandoffDue(t.Context(), 10, handoffTestSoftClaim)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{units[1].id, units[2].id}, handoffClaimUnits(claims))

	for i := range claims {
		claim := &claims[i]
		unit := units[slices.IndexFunc(units, func(u handoffTestUnit) bool { return u.id == claim.UnitID })]
		state := loadHandoffUnit(t, pool, claim.UnitID)

		require.Equal(t, handoffTestIrisRequestID(unit.id, 0), claim.IrisRequestID)
		require.Equal(t, state.clientRequestID, claim.ClientRequestID)
		require.Equal(t, claim.ClientRequestID, claim.BaseClientRequestID)
		require.Zero(t, claim.Generation)
		require.ElementsMatch(t, unit.members, claim.DeliveryIDs)
		require.Zero(t, claim.AttemptCount)
		require.ElementsMatch(t, unit.claimKeys, claim.ClaimKeys)
		require.GreaterOrEqual(t, claim.Elapsed, time.Duration(0))
		require.NotNil(t, state.nextCheck)
		require.True(t, claim.CheckToken.Equal(*state.nextCheck), "claim token must be the stored next check")
	}

	again, err := repo.ClaimHandoffDue(t.Context(), 10, handoffTestSoftClaim)
	require.NoError(t, err)
	require.Empty(t, again, "soft-claimed and locked units must not be picked")

	require.NoError(t, blocker.Rollback(t.Context()))

	released, err := repo.ClaimHandoffDue(t.Context(), 10, handoffTestSoftClaim)
	require.NoError(t, err)
	require.Equal(t, []int64{units[0].id}, handoffClaimUnits(released))
}

func TestHandoffConcurrentClaimsAreExclusive(t *testing.T) {
	pool := dbtest.NewPool(t)
	repo := newTestRepository(t, pool)
	rooms := []string{"handoff-1", "handoff-2", "handoff-3", "handoff-4", "handoff-5", "handoff-6"}
	units := seedSentHandoffUnits(t, pool, repo, rooms...)

	var (
		mu      sync.Mutex
		claimed []int64
		errs    []error
		wg      sync.WaitGroup
	)

	for range 3 {
		wg.Go(func() {
			claims, err := repo.ClaimHandoffDue(t.Context(), 2, handoffTestSoftClaim)

			mu.Lock()
			defer mu.Unlock()

			errs = append(errs, err)
			claimed = append(claimed, handoffClaimUnits(claims)...)
		})
	}

	wg.Wait()
	require.NoError(t, errors.Join(errs...))

	rest, err := repo.ClaimHandoffDue(t.Context(), 10, handoffTestSoftClaim)
	require.NoError(t, err)

	claimed = append(claimed, handoffClaimUnits(rest)...)

	want := make([]int64, 0, len(units))

	for _, unit := range units {
		want = append(want, unit.id)
	}

	slices.Sort(claimed)
	slices.Sort(want)
	require.Equal(t, want, claimed, "every due unit must be claimed exactly once")
}

func TestHandoffApplyRejectsReplacedClaimToken(t *testing.T) {
	pool := dbtest.NewPool(t)
	repo := newTestRepository(t, pool)
	unit := seedSentHandoffUnits(t, pool, repo, "handoff-token")[0]

	stale := claimOneHandoff(t, repo)

	makeHandoffDue(t, pool, unit.id)

	current := claimOneHandoff(t, repo)

	staleErr := repo.CompleteHandoff(t.Context(), &stale)
	miss := requireFenceMiss(t, staleErr, sendfence.ReasonVersionChanged)
	require.Zero(t, miss.Applied)
	require.NotErrorIs(t, staleErr, ErrHandoffCheckClosed)

	state := loadHandoffUnit(t, pool, unit.id)
	require.False(t, state.checked)
	require.True(t, current.CheckToken.Equal(*state.nextCheck))

	require.NoError(t, repo.CompleteHandoff(t.Context(), &current))

	state = loadHandoffUnit(t, pool, unit.id)
	require.True(t, state.checked)
	require.Nil(t, state.nextCheck)
	require.Equal(t, []string{string(StatusSent), string(StatusSent)}, memberStatuses(t, pool, unit.id))
}

func TestHandoffFailedReissuesUntilGenerationsRunOut(t *testing.T) {
	pool := dbtest.NewPool(t)
	repo := newTestRepository(t, pool)
	unit := seedSentHandoffUnits(t, pool, repo, "handoff-reissue")[0]
	base := loadHandoffUnit(t, pool, unit.id).clientRequestID

	for generation := range iris.ReplyReissueMaxGenerations {
		claim := claimOneHandoff(t, repo)
		require.Equal(t, generation, claim.Generation)
		require.Equal(t, generation, claim.AttemptCount)

		require.NoError(t, repo.ReissueAfterHandoff(t.Context(), &claim, 5*time.Second, handoffcheck.CodeFailed, "iris reply status failed"))

		nextID, err := iris.ReissuedClientRequestID(base, generation+1)
		require.NoError(t, err)
		require.Equal(t, handoffUnitState{clientRequestID: nextID, generation: generation + 1}, loadHandoffUnit(t, pool, unit.id))
		assertHandoffReissuedMembers(t, pool, unit, generation+1)

		resendHandoffUnit(t, pool, repo, unit, nextID, generation+1)
	}

	claim := claimOneHandoff(t, repo)
	require.ErrorIs(t, repo.ReissueAfterHandoff(t.Context(), &claim, 5*time.Second, handoffcheck.CodeFailed, "failed"), sendfence.ErrGenerationExhausted)
	require.Equal(t, []string{string(StatusSent), string(StatusSent)}, memberStatuses(t, pool, unit.id))

	releaser := &fakeClaimKeyReleaser{}
	consumer, err := NewHandoffConsumer(repo, releaser)
	require.NoError(t, err)
	require.NoError(t, consumer.DLQAfterHandoff(t.Context(), &claim, handoffcheck.CodeFailed, "iris reply status failed; reissue budget exhausted"))

	var dlq int

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(id) FROM alarm_dispatch_deliveries
		WHERE send_unit_id = $1 AND status = 'dlq' AND dlq_at IS NOT NULL AND attempt_count = $2 AND last_error_code = $3`,
		unit.id, iris.ReplyReissueMaxGenerations+1, handoffcheck.CodeFailed).Scan(&dlq))
	require.Len(t, unit.members, dlq)
	require.ElementsMatch(t, unit.claimKeys, releaser.lastKeys)

	state := loadHandoffUnit(t, pool, unit.id)
	require.Equal(t, iris.ReplyReissueMaxGenerations, state.generation)
	require.True(t, state.checked)
	require.Nil(t, state.nextCheck)
}

func assertHandoffReissuedMembers(t *testing.T, pool *pgxpool.Pool, unit handoffTestUnit, attempt int) {
	t.Helper()

	var (
		retried, distinctDelays int
		delay                   time.Duration
	)

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(id), count(DISTINCT next_attempt_at - updated_at), max(next_attempt_at - updated_at)
		FROM alarm_dispatch_deliveries
		WHERE send_unit_id = $1 AND status = 'retry' AND sent_at IS NULL AND attempt_count = $2 AND last_error_code = $3`,
		unit.id, attempt, handoffcheck.CodeFailed).Scan(&retried, &distinctDelays, &delay))
	require.Len(t, unit.members, retried)
	require.Equal(t, 1, distinctDelays, "unit members must share one next_attempt_at")
	require.Equal(t, 5*time.Second, delay)
}

func resendHandoffUnit(t *testing.T, pool *pgxpool.Pool, repo *PgxRepository, unit handoffTestUnit, clientRequestID string, generation int) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `UPDATE alarm_dispatch_deliveries
		SET next_attempt_at = clock_timestamp() - interval '1 second' WHERE send_unit_id = $1`, unit.id)
	require.NoError(t, err)

	records, err := repo.ClaimDue(t.Context(), handoffTestOwner, 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, records, len(unit.members))
	require.Equal(t, clientRequestID, records[0].ClientRequestID)

	claims := recordClaims(records...)
	request, err := repo.LoadSendRequest(t.Context(), unit.id, claims, handoffTestOwner, records[0].ClientRequestID)
	require.NoError(t, err)
	require.Equal(t, clientRequestID, request.ClientRequestID)
	require.Equal(t, generation, request.Generation)
	require.Equal(t, "fixture", request.Body)

	require.NoError(t, repo.BeginSendRequest(t.Context(), unit.id, claims, handoffTestOwner, request.ClientRequestID, time.Minute))
	require.NoError(t, repo.MarkSent(t.Context(), unit.id, claims, handoffTestOwner, handoffTestIrisRequestID(unit.id, generation), handoffcheck.FirstCheckDelay))
	makeHandoffDue(t, pool, unit.id)
}

func TestHandoffMemberMismatchClosesCheckWithoutTransition(t *testing.T) {
	pool := dbtest.NewPool(t)
	repo := newTestRepository(t, pool)
	unit := seedSentHandoffUnits(t, pool, repo, "handoff-member")[0]
	claim := claimOneHandoff(t, repo)

	_, err := pool.Exec(t.Context(), `UPDATE alarm_dispatch_deliveries
		SET status = 'quarantined', quarantined_at = clock_timestamp(), last_error_code = 'ops_review' WHERE id = $1`, unit.members[1])
	require.NoError(t, err)

	closeErr := repo.ReissueAfterHandoff(t.Context(), &claim, 5*time.Second, handoffcheck.CodeFailed, "failed")
	miss := requireFenceMiss(t, closeErr, sendfence.ReasonStatusChanged)
	require.Zero(t, miss.Applied)
	require.ErrorIs(t, closeErr, ErrHandoffCheckClosed)
	require.Equal(t, []string{string(StatusSent), string(StatusQuarantined)}, memberStatuses(t, pool, unit.id))

	state := loadHandoffUnit(t, pool, unit.id)
	require.True(t, state.checked)
	require.Nil(t, state.nextCheck)
	require.Equal(t, new(handoffTestIrisRequestID(unit.id, 0)), state.irisRequestID)
	require.Zero(t, state.generation)

	again, err := repo.ClaimHandoffDue(t.Context(), 10, handoffTestSoftClaim)
	require.NoError(t, err)
	require.Empty(t, again, "a closed check must not be claimed again")
}

func TestHandoffClosesCheckWhenAllMembersAreGone(t *testing.T) {
	pool := dbtest.NewPool(t)
	repo := newTestRepository(t, pool)
	unit := seedSentHandoffUnits(t, pool, repo, "handoff-gone")[0]
	stale := claimOneHandoff(t, repo)

	makeHandoffDue(t, pool, unit.id)

	current := claimOneHandoff(t, repo)

	_, err := pool.Exec(t.Context(), `DELETE FROM alarm_dispatch_deliveries WHERE send_unit_id = $1`, unit.id)
	require.NoError(t, err)

	staleErr := repo.CompleteHandoff(t.Context(), &stale)
	require.Zero(t, requireFenceMiss(t, staleErr, sendfence.ReasonMissing).Applied)
	require.NotErrorIs(t, staleErr, ErrHandoffCheckClosed)

	state := loadHandoffUnit(t, pool, unit.id)
	require.False(t, state.checked)
	require.True(t, current.CheckToken.Equal(*state.nextCheck))

	closeErr := repo.QuarantineAfterHandoff(t.Context(), &current, handoffcheck.CodeStatusMissing, "iris reply status missing")
	miss := requireFenceMiss(t, closeErr, sendfence.ReasonMissing)
	require.Zero(t, miss.Applied)
	require.ErrorIs(t, closeErr, ErrHandoffCheckClosed)

	state = loadHandoffUnit(t, pool, unit.id)
	require.True(t, state.checked)
	require.Nil(t, state.nextCheck)

	again, err := repo.ClaimHandoffDue(t.Context(), 10, handoffTestSoftClaim)
	require.NoError(t, err)
	require.Empty(t, again, "a closed check must not be claimed again")
}

func TestHandoffRescheduleStopsAtHorizonThenQuarantines(t *testing.T) {
	pool := dbtest.NewPool(t)
	repo := newTestRepository(t, pool)
	unit := seedSentHandoffUnits(t, pool, repo, "handoff-horizon")[0]
	horizon := 10 * time.Minute

	claim := claimOneHandoff(t, repo)
	require.NoError(t, repo.RescheduleHandoff(t.Context(), &claim, time.Hour, horizon))

	var untilCheckMS int64

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT (EXTRACT(EPOCH FROM (u.handoff_next_check_at - min(d.sent_at))) * 1000)::BIGINT
		FROM alarm_dispatch_send_units u JOIN alarm_dispatch_deliveries d ON d.send_unit_id = u.id
		WHERE u.id = $1 GROUP BY u.id`, unit.id).Scan(&untilCheckMS))
	require.Equal(t, horizon.Milliseconds(), untilCheckMS)
	require.False(t, loadHandoffUnit(t, pool, unit.id).checked)

	_, err := pool.Exec(t.Context(), `UPDATE alarm_dispatch_deliveries SET sent_at = sent_at - interval '20 minutes' WHERE send_unit_id = $1`, unit.id)
	require.NoError(t, err)
	makeHandoffDue(t, pool, unit.id)

	claim = claimOneHandoff(t, repo)
	require.GreaterOrEqual(t, claim.Elapsed, 20*time.Minute)
	require.Equal(t, handoffcheck.ActionQuarantine, handoffcheck.Decide(handoffcheck.Input{
		RequestID: claim.IrisRequestID, Snapshot: &iris.ReplyStatusSnapshot{RequestID: claim.IrisRequestID, State: "queued"},
		Elapsed: claim.Elapsed, Horizon: horizon,
	}).Action)
	require.NoError(t, repo.QuarantineAfterHandoff(t.Context(), &claim, handoffcheck.CodeUnconfirmed, "iris handoff unconfirmed at horizon: iris reply status queued"))

	var quarantined int

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(id) FROM alarm_dispatch_deliveries
		WHERE send_unit_id = $1 AND status = 'quarantined' AND quarantined_at IS NOT NULL AND last_error_code = $2`,
		unit.id, handoffcheck.CodeUnconfirmed).Scan(&quarantined))
	require.Len(t, unit.members, quarantined)
	require.True(t, loadHandoffUnit(t, pool, unit.id).checked)
}

type budgetEndingHandoffRepository struct {
	HandoffRepository
}

func (budgetEndingHandoffRepository) DLQAfterHandoff(ctx context.Context, _ *HandoffClaim, _, _ string) error {
	<-ctx.Done()

	return nil
}

type liveContextReleaser struct {
	released []string
	err      error
}

func (r *liveContextReleaser) DelMany(ctx context.Context, keys []string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	if r.err != nil {
		return 0, r.err
	}

	r.released = append(r.released, keys...)

	return int64(len(keys)), nil
}

func TestHandoffDLQReleasesClaimKeysOutsideSettlementBudget(t *testing.T) {
	claim := func() *HandoffClaim {
		return &HandoffClaim{UnitID: 1, DeliveryIDs: []int64{1}, ClaimKeys: []string{"notified:claim:handoff-dlq:stream-1:100:live"}}
	}

	t.Run("released after the settlement budget ends", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			releaser := &liveContextReleaser{}
			consumer, err := NewHandoffConsumer(budgetEndingHandoffRepository{}, releaser)
			require.NoError(t, err)

			settleCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			require.NoError(t, consumer.DLQAfterHandoff(settleCtx, claim(), handoffcheck.CodeFailed, "failed"))
			require.Equal(t, claim().ClaimKeys, releaser.released)
		})
	})

	t.Run("release failure is reported after the dlq commit", func(t *testing.T) {
		releaseErr := errors.New("valkey down")
		consumer, err := NewHandoffConsumer(budgetEndingHandoffRepository{}, &liveContextReleaser{err: releaseErr})
		require.NoError(t, err)

		settleCtx, cancel := context.WithCancel(t.Context())
		cancel()

		err = consumer.DLQAfterHandoff(settleCtx, claim(), handoffcheck.CodeFailed, "failed")
		require.ErrorIs(t, err, ErrHandoffClaimKeysNotReleased)
		require.ErrorIs(t, err, releaseErr)
	})
}

func handoffClaimUnits(claims []HandoffClaim) []int64 {
	units := make([]int64, 0, len(claims))
	for i := range claims {
		units = append(units, claims[i].UnitID)
	}

	return units
}
