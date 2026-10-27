package youtubedispatch

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/iris-client-go/v3/iris"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/tracking/observation"
	"github.com/kapu/hololive-shared/pkg/testutil"
)

const (
	settlementOutcomePermanent  = "permanent"
	settlementOutcomeRetryable  = "retryable"
	settlementOutcomeUnknown    = "unknown"
	settlementOutcomeReissue    = "reissue"
	settlementOutcomeCommitLost = "commit_response_lost"
	settlementPathFallback      = "fallback"
	settlementPathPerRoom       = "per_room"
)

// 실제 provider 반환 뒤 engine이 저장소에 진입하는 경계에서만 parent를 취소한다.
// 저장 결과는 모두 실제 PostgreSQL 전이·재조회가 결정한다.
type settlementTransition struct {
	*store.TransitionStore

	db       *settlementDB
	cancel   context.CancelFunc
	calls    int
	cancelAt int
	results  []store.ApplyResult
}

func (s *settlementTransition) boundary() {
	s.calls++
	if s.calls == s.cancelAt {
		s.cancel()
	}

	s.db.armed = true
}

func (s *settlementTransition) CompleteSent(ctx context.Context, op store.StartedOperation, mode store.DeliveryMode, admission store.HandoffAdmission) (store.ApplyResult, error) {
	s.boundary()

	result, err := s.TransitionStore.CompleteSent(ctx, op, mode, admission)

	s.results = append(s.results, result)

	if err != nil {
		return result, fmt.Errorf("complete settlement send: %w", err)
	}

	return result, nil
}

func (s *settlementTransition) ApplyStartedFailure(ctx context.Context, op store.StartedOperation, kind lifecycle.FailureKind, reason lifecycle.Reason, after time.Duration, mode store.DeliveryMode) (store.ApplyResult, error) {
	s.boundary()

	result, err := s.TransitionStore.ApplyStartedFailure(ctx, op, kind, reason, after, mode)

	s.results = append(s.results, result)

	if err != nil {
		return result, fmt.Errorf("apply settlement failure: %w", err)
	}

	return result, nil
}

func (s *settlementTransition) AdvanceRequestGeneration(ctx context.Context, op store.StartedOperation, req store.FrozenRequest) (store.FrozenRequest, error) {
	s.boundary()

	request, err := s.TransitionStore.AdvanceRequestGeneration(ctx, op, req)
	if err != nil {
		return request, fmt.Errorf("advance settlement request: %w", err)
	}

	return request, nil
}

type settlementDB struct {
	*pgxpool.Pool

	armed               bool
	loseCommit          bool
	rollbackFirstCommit bool
	contexts            []context.Context
	commits             int
}

func (d *settlementDB) capture(ctx context.Context) {
	if d.armed {
		d.contexts = append(d.contexts, ctx)
	}
}

func (d *settlementDB) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	d.capture(ctx)

	tx, err := d.Pool.BeginTx(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("begin settlement transaction: %w", err)
	}

	if !d.armed {
		return tx, nil
	}

	return &settlementTx{Tx: tx, db: d}, nil
}

func (d *settlementDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	d.capture(ctx)

	rows, err := d.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query settlement database: %w", err)
	}

	return rows, nil
}

func (d *settlementDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	d.capture(ctx)

	return d.Pool.QueryRow(ctx, sql, args...)
}

type settlementTx struct {
	pgx.Tx

	db *settlementDB
}

func (tx *settlementTx) Commit(ctx context.Context) error {
	if tx.db.rollbackFirstCommit {
		tx.db.rollbackFirstCommit = false
		if err := pgxdb.Rollback(ctx, tx); err != nil {
			return fmt.Errorf("roll back settlement pre-state: %w", err)
		}

		return errors.New("injected unconfirmed commit with real rolled-back pre-state")
	}

	if err := tx.Tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit settlement transaction: %w", err)
	}

	tx.db.commits++
	if tx.db.loseCommit {
		return errors.New("injected response loss after real PostgreSQL COMMIT")
	}

	return nil
}

type settlementSender struct {
	frozenProviderSender

	beforeReturn      func(context.Context, int) error
	cancelPreparation bool
	prepareCalls      int
}

func (s *settlementSender) PrepareMessageRequest(ctx context.Context, room, message string) (string, string, error) {
	s.prepareCalls++
	if s.cancelPreparation && ctx.Err() != nil {
		return "", "", ctx.Err()
	}

	return s.frozenProviderSender.PrepareMessageRequest(ctx, room, message)
}

func (s *settlementSender) SendPreparedMessage(ctx context.Context, room, body, route, id string) (string, error) {
	requestID, err := s.frozenProviderSender.SendPreparedMessage(ctx, room, body, route, id)
	if err != nil {
		return "", fmt.Errorf("send prepared settlement message: %w", err)
	}

	return requestID, s.beforeReturn(ctx, len(s.ids))
}

type settlementFixture struct {
	pool       *pgxpool.Pool
	db         *settlementDB
	transition *settlementTransition
	engine     *SendEngine
	sender     *settlementSender
	rows       []domain.YouTubeNotificationDelivery
	outboxes   []domain.YouTubeNotificationOutbox
	messages   map[int64]string
	cancel     context.CancelFunc
}

func newSettlementFixture(t *testing.T, count int, cancel context.CancelFunc) *settlementFixture {
	t.Helper()

	pool := newDeliveryPool(t)
	db := &settlementDB{Pool: pool}
	sender := &settlementSender{body: "immutable final body", route: "markdown"}
	engine := newOutcomeUnknownTestEngine(sender, nil, time.Minute)

	engine.config.MaxRetries = 3
	engine.config.RetryBackoff = time.Minute

	transition, err := store.NewTransitionStore(db, engine.logger, store.TransitionConfig{Owner: testTransitionOwner, MaxRetries: 3, RetryBackoff: time.Minute, LockTimeout: time.Minute, ClaimFreshnessWindow: time.Hour, LogicalGroupLimit: 10})
	require.NoError(t, err)

	t.Cleanup(cancel)

	f := &settlementFixture{pool: pool, db: db, engine: engine, sender: sender, cancel: cancel, messages: make(map[int64]string)}

	f.transition = &settlementTransition{TransitionStore: transition, db: db, cancel: cancel, cancelAt: 1}
	engine.transition = f.transition

	detectedAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	byID := make(map[int64]domain.YouTubeNotificationOutbox)

	for i := range count {
		contentID := fmt.Sprintf("settlement-short-%d", i)
		outbox := domain.YouTubeNotificationOutbox{Kind: domain.OutboxKindNewShort, ChannelID: "settlement-channel", ContentID: contentID, Payload: fmt.Sprintf(`{"canonical_post_id":%q,"video_id":%q,"title":"settlement"}`, "short:"+contentID, contentID)}
		require.NoError(t, insertDeliveryTestRows(pool, &outbox).Error)

		row := domain.YouTubeNotificationDelivery{OutboxID: outbox.ID, RoomID: "settlement-room"}
		require.NoError(t, insertDeliveryTestRows(pool, &row).Error)
		require.NoError(t, observation.NewRepository(pool).UpsertBatch(t.Context(), []*domain.YouTubeContentAlarmTracking{{
			Kind: outbox.Kind, ContentID: contentID, ChannelID: outbox.ChannelID, DetectedAt: detectedAt,
		}}))

		state := domain.YouTubeCommunityShortsAlarmState{Kind: outbox.Kind, PostID: "short:" + contentID, ContentID: contentID, ChannelID: outbox.ChannelID, DetectedAt: detectedAt}
		_, stateErr := insertDomainAlarmState(t.Context(), pool, &state)
		require.NoError(t, stateErr)

		f.outboxes = append(f.outboxes, outbox)
		f.messages[outbox.ID] = "template body"
		byID[outbox.ID] = outbox
	}

	claimed, err := transition.ClaimPending(t.Context(), count)
	require.NoError(t, err)

	prepared, err := transition.PrepareClaimed(t.Context(), claimed, byID)
	require.NoError(t, err)
	require.Empty(t, prepared.Blocked)
	require.Len(t, prepared.ActiveRows, count)

	f.rows = prepared.ActiveRows
	slices.SortFunc(f.rows, func(a, b domain.YouTubeNotificationDelivery) int { return cmp.Compare(a.OutboxID, b.OutboxID) })

	return f
}

func (f *settlementFixture) dispatch(ctx context.Context, t *testing.T, path string) DispatchResult {
	t.Helper()

	result := newDeliveryDispatchResult(len(f.rows))
	group := &deliveryGroup{roomID: f.rows[0].RoomID, channelID: f.outboxes[0].ChannelID, kind: f.outboxes[0].Kind, rows: f.rows, outboxes: f.outboxes}
	members := make([]claimedDelivery, len(f.rows))

	for i := range f.rows {
		members[i] = claimedDelivery{row: f.rows[i], outbox: f.outboxes[i]}
	}

	attempt := newDeliveryAttempt(members)

	switch path {
	case settlementPathPerRoom:
		f.engine.dispatchClaimedDeliveryRow(ctx, &attempt, f.messages, nil, &result)
	case "frozen_per_room", "frozen_grouped":
		candidate, err := buildDeliverySendRequest(group.roomID, "original template", f.outboxes)
		require.NoError(t, err)

		attempt.request = candidate
		require.True(t, f.engine.freezeDeliveryRequest(ctx, &attempt))

		group.frozen = attempt.request.frozen
		f.sender.body, f.sender.route = "changed template", "text"
		f.engine.dispatchFrozenGroup(ctx, group, f.messages, nil, &result)
	default:
		f.engine.dispatchGroupedClaimedRows(ctx, group, &attempt, "group template", f.messages, nil, &result)
	}

	return result.DispatchResult
}

func TestSendEngineKnownOutcomeSurvivesParentCancellation(t *testing.T) {
	for _, path := range []string{settlementPathPerRoom, "grouped", "frozen_per_room", "frozen_grouped", settlementPathFallback} {
		for _, outcome := range []string{sendResultSuccess, settlementOutcomePermanent, settlementOutcomeRetryable, settlementOutcomeUnknown, settlementOutcomeReissue, settlementOutcomeCommitLost} {
			t.Run(path+"/"+outcome, func(t *testing.T) {
				runSettlementCancellation(t, path, outcome)
			})
		}
	}
}

func settlementProviderError(ctx context.Context, t *testing.T, outcome string) error {
	t.Helper()

	switch outcome {
	case settlementOutcomePermanent:
		return iris.ErrAuthFailed
	case settlementOutcomeRetryable:
		return iris.ErrRateLimited
	case settlementOutcomeUnknown:
		return context.Canceled
	case settlementOutcomeReissue:
		return fmt.Errorf("settlement provider conflict: %w", testutil.IrisHTTPError(ctx, t, 409, `{"code":"CLIENT_REQUEST_ID_FAILED"}`))
	default:
		return nil
	}
}

func runSettlementCancellation(t *testing.T, path, outcome string) {
	t.Helper()

	count := 2

	if path == settlementPathPerRoom || path == "frozen_per_room" {
		count = 1
	}

	ctx, cancel := context.WithCancel(t.Context())
	f := newSettlementFixture(t, count, cancel)
	fallback := path == settlementPathFallback

	if fallback {
		f.transition.cancelAt = count
	}

	f.db.loseCommit = outcome == settlementOutcomeCommitLost

	providerErr := settlementProviderError(ctx, t, outcome)

	f.sender.beforeReturn = func(providerCtx context.Context, call int) error {
		require.NoError(t, providerCtx.Err(), "provider must not inherit detached state work")

		if fallback && call == 1 {
			return iris.ErrPermanent
		}

		if outcome == settlementOutcomeUnknown {
			f.cancel()
		}

		return providerErr
	}

	result := f.dispatch(ctx, t, path)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	assertSettlementProviderCalls(t, f, path, outcome)
	assertSettlementState(t, f, path, outcome, result)
	assertSettlementBudgets(t, f, outcome)
}

func assertSettlementProviderCalls(t *testing.T, f *settlementFixture, path, outcome string) {
	t.Helper()

	wantCalls := 1

	if path == settlementPathFallback {
		wantCalls = 3

		if outcome == settlementOutcomeUnknown {
			wantCalls = 2
		}
	}

	require.Len(t, f.sender.ids, wantCalls)

	for i := range f.sender.ids {
		require.Equal(t, "immutable final body", f.sender.bodies[i])
		require.Equal(t, "markdown", f.sender.routes[i])
	}
}

func assertSettlementBudgets(t *testing.T, f *settlementFixture, outcome string) {
	t.Helper()

	if outcome == settlementOutcomeUnknown {
		require.Zero(t, f.transition.calls)
		require.Empty(t, f.db.contexts)

		return
	}

	require.Equal(t, f.transition.cancelAt, f.transition.calls)

	budgets := make(map[context.Context]struct{})

	for _, ctx := range f.db.contexts {
		_, ok := ctx.Deadline()
		require.True(t, ok)

		budgets[ctx] = struct{}{}
	}

	require.Len(t, budgets, f.transition.calls, "each transition including read-back uses one state context")

	if outcome == settlementOutcomeCommitLost {
		require.Equal(t, f.transition.calls, f.db.commits, "read-back must not repeat committed transition")

		for i := range f.transition.results {
			require.Equal(t, store.CommitConfirmedPost, f.transition.results[i].CommitAdjudication)
		}

		require.Greater(t, len(f.db.contexts), f.db.commits, "actual primary read-back must run")
	}
}

func assertSettlementState(t *testing.T, f *settlementFixture, path, outcome string, result DispatchResult) {
	t.Helper()

	success := outcome == sendResultSuccess || outcome == settlementOutcomeCommitLost
	unknown := outcome == settlementOutcomeUnknown
	assertSettlementRows(t, f, outcome, success, unknown)
	assertSettlementEffects(t, f, path, success, unknown, result)
	assertSettlementFrozenRequests(t, f, path, outcome)
}

func assertSettlementRows(t *testing.T, f *settlementFixture, outcome string, success, unknown bool) {
	t.Helper()

	wantStatus, wantAttempts, wantVersion := "SENT", 0, int64(3)

	if outcome == settlementOutcomePermanent {
		wantStatus, wantAttempts = "FAILED", 1
	}

	if outcome == settlementOutcomeRetryable || outcome == settlementOutcomeReissue {
		wantStatus, wantAttempts = "PENDING", 1
	}

	if unknown {
		wantStatus, wantVersion = "SENDING", 2
	}

	for i := range f.rows {
		row := &f.rows[i]

		var (
			status       string
			attempts     int
			version      int64
			sent, locked bool
			next         time.Time
		)

		require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT status,attempt_count,row_version,sent_at IS NOT NULL,locked_at IS NOT NULL,next_attempt_at FROM youtube_notification_delivery WHERE id=$1`, row.ID).Scan(&status, &attempts, &version, &sent, &locked, &next))
		require.Equal(t, wantStatus, status)
		require.Equal(t, wantAttempts, attempts)
		require.Equal(t, wantVersion, version)
		require.Equal(t, success, sent)
		require.Equal(t, unknown, locked)

		if wantStatus == "PENDING" {
			require.True(t, next.After(time.Now()), "normal retry backoff remains in force")
		}
	}
}

func assertSettlementEffects(t *testing.T, f *settlementFixture, path string, success, unknown bool, result DispatchResult) {
	t.Helper()

	var ledger, alarmTracking, telemetry int

	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*) FROM youtube_notification_delivery_ledger WHERE status='SENT'`).Scan(&ledger))
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*) FROM youtube_community_shorts_alarm_states WHERE alarm_sent_at IS NOT NULL`).Scan(&alarmTracking))
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*) FROM youtube_notification_delivery_telemetry`).Scan(&telemetry))

	wantSuccess, wantTelemetry := 0, len(f.rows)

	if success {
		wantSuccess = len(f.rows)
	}

	if unknown {
		wantTelemetry = 0
	}

	require.Equal(t, wantSuccess, ledger)
	require.Equal(t, wantSuccess, alarmTracking)

	for i := range f.outboxes {
		outbox := &f.outboxes[i]
		tracking, err := observation.NewRepository(f.pool).FindByIdentity(t.Context(), outbox.Kind, outbox.ContentID)
		require.NoError(t, err)
		require.NotNil(t, tracking)
		require.Equal(t, success, tracking.AlarmSentAt != nil)
	}

	require.Len(t, result.SuccessDeliveryIDs, wantSuccess)
	require.Equal(t, wantTelemetry, telemetry)

	if !success && !unknown {
		require.Equal(t, len(f.rows), result.FailedDeliveries)
	}

	if unknown {
		require.Zero(t, result.FailedDeliveries)
	}

	if !unknown {
		mode, sendResult := string(store.DeliveryModePerRoom), sendResultFailure

		if path == "grouped" || path == "frozen_grouped" {
			mode = "grouped"
		}

		if success {
			sendResult = sendResultSuccess
		}

		var matching int

		require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*) FROM youtube_notification_delivery_telemetry WHERE delivery_mode=$1 AND send_result=$2 AND attempt_ordinal=1`, mode, sendResult).Scan(&matching))
		require.Equal(t, wantTelemetry, matching)
	}
}

func assertSettlementFrozenRequests(t *testing.T, f *settlementFixture, path, outcome string) {
	t.Helper()

	unknown := outcome == settlementOutcomeUnknown

	f.db.armed = false

	requests, err := f.transition.LoadFrozenRequests(t.Context(), collectDeliveryIDs(f.rows))
	require.NoError(t, err)

	wantRequests := 1

	if path == settlementPathFallback {
		wantRequests = len(f.rows)
	}

	require.Len(t, requests, wantRequests)

	members := make([]int64, 0, len(f.rows))

	for i := range requests {
		request := &requests[i]
		require.Equal(t, "immutable final body", request.Message)
		require.Equal(t, "markdown", request.Route)

		generation := 0

		if outcome == settlementOutcomeReissue {
			generation = 1
		}

		require.Equal(t, generation, request.Generation)

		id, idErr := request.ClientRequestID()
		require.NoError(t, idErr)

		if !unknown || path != settlementPathFallback {
			if generation == 0 {
				require.Contains(t, f.sender.ids, id)
			} else {
				require.Contains(t, f.sender.ids, request.BaseID)
			}
		}

		members = append(members, request.MemberIDs...)
	}

	require.ElementsMatch(t, collectDeliveryIDs(f.rows), members)
}

func TestSendEngineSettlementUsesOneBoundedBudget(t *testing.T) {
	for _, outcome := range []string{sendResultSuccess, settlementOutcomePermanent, settlementOutcomeReissue} {
		t.Run(outcome, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			f := newSettlementFixture(t, 1, cancel)
			providerErr := settlementProviderError(ctx, t, outcome)

			var blocker pgx.Tx

			f.sender.beforeReturn = func(providerCtx context.Context, _ int) error {
				var err error

				blocker, err = f.pool.Begin(providerCtx)
				require.NoError(t, err)

				_, err = blocker.Exec(providerCtx, `SELECT id FROM youtube_notification_delivery WHERE id=$1 FOR UPDATE`, f.rows[0].ID)
				require.NoError(t, err)

				return providerErr
			}

			t.Cleanup(func() {
				if blocker != nil {
					rollbackSettlementBlocker(ctx, t, blocker)
				}
			})

			started := time.Now()
			result := f.dispatch(ctx, t, settlementPathPerRoom)
			require.GreaterOrEqual(t, time.Since(started), deliveryStateTimeout)
			require.Less(t, time.Since(started), deliveryStateTimeout+2*time.Second)
			require.Len(t, f.sender.ids, 1)
			require.Empty(t, result.SuccessDeliveryIDs)
			require.Zero(t, result.FailedDeliveries)
			require.NotEmpty(t, f.db.contexts)

			first := f.db.contexts[0]
			deadline, ok := first.Deadline()
			require.True(t, ok)
			require.WithinDuration(t, started.Add(deliveryStateTimeout), deadline, time.Second)

			for _, budgetCtx := range f.db.contexts {
				require.Same(t, first, budgetCtx, "retry and read-back must consume the original budget")
				require.ErrorIs(t, budgetCtx.Err(), context.DeadlineExceeded)
			}

			if outcome != settlementOutcomeReissue {
				require.Len(t, f.transition.results, 1)
				require.Equal(t, store.ApplyIndeterminate, f.transition.results[0].Outcome)
			}

			assertSettlementState(t, f, settlementPathPerRoom, settlementOutcomeUnknown, result)
		})
	}
}

func rollbackSettlementBlocker(ctx context.Context, t *testing.T, blocker pgx.Tx) {
	t.Helper()

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deliveryStateTimeout)
	defer cancel()

	require.NoError(t, pgxdb.Rollback(cleanupCtx, blocker))
}

func TestSendEngineFallbackStopsBeforeNextProviderAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	f := newSettlementFixture(t, 2, cancel)

	f.sender.beforeReturn = func(providerCtx context.Context, call int) error {
		require.NoError(t, providerCtx.Err())

		if call == 1 {
			return iris.ErrPermanent
		}

		return nil
	}

	result := f.dispatch(ctx, t, settlementPathFallback)
	require.Len(t, f.sender.ids, 2, "group and first singleton only")
	require.Len(t, result.SuccessDeliveryIDs, 1)
	require.Equal(t, 1, f.transition.calls, "unsent sibling must not allocate another detached budget")

	var statuses []string

	rows, err := f.pool.Query(t.Context(), `SELECT status FROM youtube_notification_delivery ORDER BY id`)
	require.NoError(t, err)

	statuses, err = pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(t, err)
	require.Equal(t, []string{"SENT", "SENDING"}, statuses)
}

func TestSendEngineSettlesAlreadyCanceledProviderParent(t *testing.T) {
	for _, outcome := range []string{sendResultSuccess, settlementOutcomePermanent, settlementOutcomeRetryable, settlementOutcomeReissue} {
		t.Run(outcome, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			f := newSettlementFixture(t, 1, cancel)
			providerErr := settlementProviderError(ctx, t, outcome)

			f.sender.beforeReturn = func(providerCtx context.Context, _ int) error {
				f.cancel()
				require.ErrorIs(t, providerCtx.Err(), context.Canceled)

				return providerErr
			}

			result := f.dispatch(ctx, t, settlementPathPerRoom)
			require.Len(t, f.sender.ids, 1)
			assertSettlementState(t, f, settlementPathPerRoom, outcome, result)
		})
	}
}

func TestSendEngineSettlementRetainsExistingPreStateRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	f := newSettlementFixture(t, 1, cancel)

	f.db.rollbackFirstCommit = true
	f.sender.beforeReturn = func(context.Context, int) error { return nil }

	result := f.dispatch(ctx, t, settlementPathPerRoom)
	assertSettlementState(t, f, settlementPathPerRoom, sendResultSuccess, result)
	require.Len(t, f.sender.ids, 1)
	require.Equal(t, 1, f.db.commits)
	require.Len(t, f.transition.results, 1)
	require.Equal(t, store.CommitConfirmedPre, f.transition.results[0].CommitAdjudication)
	require.Greater(t, len(f.db.contexts), 2)

	for _, budgetCtx := range f.db.contexts {
		require.Same(t, f.db.contexts[0], budgetCtx, "existing retry and read-back share the original deadline")
	}
}

func TestSendEngineKnownFailureAdjudicatesLostCommitResponse(t *testing.T) {
	for _, outcome := range []string{settlementOutcomePermanent, settlementOutcomeRetryable} {
		t.Run(outcome, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			f := newSettlementFixture(t, 1, cancel)

			f.db.loseCommit = true
			f.sender.beforeReturn = func(context.Context, int) error {
				if outcome == settlementOutcomePermanent {
					return iris.ErrAuthFailed
				}

				return iris.ErrRateLimited
			}

			result := f.dispatch(ctx, t, settlementPathPerRoom)
			assertSettlementState(t, f, settlementPathPerRoom, outcome, result)
			require.Len(t, f.sender.ids, 1)
			require.Equal(t, 1, f.db.commits)
			require.Len(t, f.transition.results, 1)
			require.Equal(t, store.CommitConfirmedPost, f.transition.results[0].CommitAdjudication)

			for _, budgetCtx := range f.db.contexts {
				require.Same(t, f.db.contexts[0], budgetCtx)
			}
		})
	}
}

func TestSendEngineCanceledParentPreservesExhaustedReissueBudget(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	f := newSettlementFixture(t, 1, cancel)

	f.engine.config.MaxRetries = 1

	conflict := settlementProviderError(ctx, t, settlementOutcomeReissue)

	f.sender.beforeReturn = func(context.Context, int) error {
		f.cancel()

		return conflict
	}

	result := f.dispatch(ctx, t, settlementPathPerRoom)
	require.Len(t, f.sender.ids, 1)
	assertSettlementState(t, f, settlementPathPerRoom, settlementOutcomePermanent, result)
	require.Len(t, f.transition.results, 1, "exhausted budget must finalize failure, not advance generation")
	require.Equal(t, store.ApplyApplied, f.transition.results[0].Outcome)
}

// Fallback의 준비·고정 실패는 기존 계약상 SENDING을 보존한다. Grouped 거절을
// 새 terminal/retry 정책으로 바꾸거나 취소된 parent에서 singleton을 보내지 않는다.
func TestSendEngineCanceledGroupedRejectionPreservesFallbackPreparationHold(t *testing.T) {
	for _, prepareFailure := range []bool{true, false} {
		name, message, prepareCalls := "freeze_failed", "Failed to freeze grouped fallback", 3

		if prepareFailure {
			name, message, prepareCalls = "prepare_failed", "Failed to prepare grouped fallback", 2
		}

		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			f := newSettlementFixture(t, 2, cancel)

			var logs bytes.Buffer

			f.engine.logger = slog.New(slog.NewTextHandler(&logs, nil))
			f.sender.cancelPreparation = prepareFailure
			f.sender.beforeReturn = func(providerCtx context.Context, _ int) error {
				f.cancel()
				require.ErrorIs(t, providerCtx.Err(), context.Canceled)

				return iris.ErrPermanent
			}

			result := f.dispatch(ctx, t, "grouped")
			require.Len(t, f.sender.ids, 1, "only the rejected grouped request may reach provider")
			require.Equal(t, prepareCalls, f.sender.prepareCalls)
			require.Contains(t, logs.String(), message)
			require.Zero(t, f.transition.calls, "fallback preparation does not invent terminal/retry settlement")
			assertSettlementState(t, f, "grouped", settlementOutcomeUnknown, result)
		})
	}
}

func TestSendEngineCanceledReissueCommitLossReplaysPersistedGeneration(t *testing.T) {
	for _, scenario := range []struct {
		path  string
		count int
	}{
		{path: settlementPathPerRoom, count: 1},
		{path: "grouped", count: 2},
	} {
		t.Run(scenario.path, func(t *testing.T) {
			runCanceledReissueCommitLoss(t, scenario.path, scenario.count)
		})
	}
}

func runCanceledReissueCommitLoss(t *testing.T, path string, count int) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	f := newSettlementFixture(t, count, cancel)
	conflict := settlementProviderError(ctx, t, settlementOutcomeReissue)

	f.db.loseCommit = true
	f.sender.beforeReturn = func(context.Context, int) error { return conflict }

	first := f.dispatch(ctx, t, path)
	require.ErrorIs(t, ctx.Err(), context.Canceled)

	request := assertReissueCommitLossPending(t, f, first)

	f.db.loseCommit = false

	group := reclaimReissuedSettlement(t, f)

	f.sender.beforeReturn = func(providerCtx context.Context, _ int) error {
		require.NoError(t, providerCtx.Err())

		return nil
	}
	f.sender.body, f.sender.route = "new template must not replace persisted request", "text"

	preparations := f.sender.prepareCalls
	result := newDeliveryDispatchResult(count)
	f.engine.dispatchFrozenGroup(t.Context(), group, f.messages, nil, &result)
	require.Equal(t, preparations, f.sender.prepareCalls)
	assertReissuedSettlementSent(t, f, &request, result.DispatchResult)
}

func assertReissueCommitLossPending(t *testing.T, f *settlementFixture, result DispatchResult) store.FrozenRequest {
	t.Helper()
	require.Len(t, f.sender.ids, 1)
	require.Equal(t, 1, f.transition.calls, "ambiguous reissue must not trigger another settlement")
	require.Equal(t, 1, f.db.commits)
	require.Empty(t, f.transition.results)
	require.Empty(t, result.SuccessDeliveryIDs)
	require.Zero(t, result.FailedDeliveries)
	assertSettlementRows(t, f, settlementOutcomeReissue, false, false)
	assertSettlementBudgets(t, f, settlementOutcomeReissue)

	f.db.armed = false

	requests, err := f.transition.LoadFrozenRequests(t.Context(), collectDeliveryIDs(f.rows))
	require.NoError(t, err)
	require.Len(t, requests, 1)
	require.Equal(t, 1, requests[0].Generation)
	require.Equal(t, f.sender.ids[0], requests[0].BaseID)
	require.Equal(t, f.sender.bodies[0], requests[0].Message)
	require.Equal(t, f.sender.routes[0], requests[0].Route)
	require.ElementsMatch(t, collectDeliveryIDs(f.rows), requests[0].MemberIDs)

	return requests[0]
}

func reclaimReissuedSettlement(t *testing.T, f *settlementFixture) *deliveryGroup {
	t.Helper()

	claimed, err := f.transition.ClaimPending(t.Context(), len(f.rows))
	require.NoError(t, err)
	require.Empty(t, claimed, "committed reissue retains normal retry backoff")

	// 테스트에서 due 시각만 앞당겨 대기 경과를 재현한다. 상태·잠금·version은 실제 claim이 전이한다.
	_, err = f.pool.Exec(t.Context(), `UPDATE youtube_notification_delivery SET next_attempt_at=clock_timestamp()-interval '1 second' WHERE id=ANY($1)`, collectDeliveryIDs(f.rows))
	require.NoError(t, err)

	claimed, err = f.transition.ClaimPending(t.Context(), len(f.rows))
	require.NoError(t, err)
	require.Len(t, claimed, len(f.rows))

	outboxes := make(map[int64]domain.YouTubeNotificationOutbox, len(f.outboxes))
	for i := range f.outboxes {
		outboxes[f.outboxes[i].ID] = f.outboxes[i]
	}

	prepared, err := f.transition.PrepareClaimed(t.Context(), claimed, outboxes)
	require.NoError(t, err)
	require.Empty(t, prepared.Blocked)
	require.Len(t, prepared.ActiveRows, len(f.rows))

	f.rows = prepared.ActiveRows

	groups, remaining, err := f.engine.frozenDeliveryGroups(t.Context(), f.rows, outboxes)
	require.NoError(t, err)
	require.Empty(t, remaining)
	require.Len(t, groups, 1)

	return &groups[0]
}

func assertReissuedSettlementSent(t *testing.T, f *settlementFixture, request *store.FrozenRequest, result DispatchResult) {
	t.Helper()

	nextID, err := request.ClientRequestID()
	require.NoError(t, err)
	require.NotEqual(t, request.BaseID, nextID)
	require.Equal(t, []string{request.BaseID, nextID}, f.sender.ids)
	require.Equal(t, []string{request.Message, request.Message}, f.sender.bodies)
	require.Equal(t, []string{request.Route, request.Route}, f.sender.routes)
	require.ElementsMatch(t, collectDeliveryIDs(f.rows), result.SuccessDeliveryIDs)
	require.Zero(t, result.FailedDeliveries)
	require.Equal(t, 2, f.transition.calls)
	require.Equal(t, 2, f.db.commits)
	require.Len(t, f.transition.results, 1)
	require.Equal(t, store.ApplyApplied, f.transition.results[0].Outcome)

	var sent, ledger, failedAttempts, successfulAttempts int

	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT
		(SELECT count(*) FROM youtube_notification_delivery WHERE status='SENT' AND attempt_count=1 AND row_version=6 AND locked_at IS NULL),
		(SELECT count(*) FROM youtube_notification_delivery_ledger WHERE status='SENT'),
		(SELECT count(*) FROM youtube_notification_delivery_telemetry WHERE send_result='failure' AND attempt_ordinal=1),
		(SELECT count(*) FROM youtube_notification_delivery_telemetry WHERE send_result='success' AND attempt_ordinal=2)
	`).Scan(&sent, &ledger, &failedAttempts, &successfulAttempts))
	require.Equal(t, []int{len(f.rows), len(f.rows), len(f.rows), len(f.rows)}, []int{sent, ledger, failedAttempts, successfulAttempts})
}
