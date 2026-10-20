package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
)

const materializeFanoutOperation = "materialize fanout"

type FanoutResult struct {
	ApplyResult

	DeliveryCount int
	NoTargets     bool
}

type fanoutOutboxState struct {
	status        domain.OutboxStatus
	attemptCount  int
	nextAttemptAt time.Time
	lockedAt      *time.Time
	sentAt        *time.Time
	terminalAt    *time.Time
	errorText     string
}

// ClaimOutboxesForFanout은 fanout 대기 outbox를 잠그며, lease 만료·due·신선도 판정과 새 locked_at을 DB 시계로 정한다.
func (s *TransitionStore) ClaimOutboxesForFanout(ctx context.Context, batchSize int) ([]domain.YouTubeNotificationOutbox, error) {
	if batchSize <= 0 {
		return nil, errors.New("claim outboxes for fanout: batch size must be positive")
	}

	rows, err := s.db.Query(
		ctx,
		mustSQL("fanout_claim.sql"),
		domain.OutboxStatusPending,
		s.config.LockTimeout.Milliseconds(),
		s.config.ClaimFreshnessWindow.Milliseconds(),
		batchSize,
	)
	if err != nil {
		return nil, fmt.Errorf("claim outboxes for fanout: %w", err)
	}
	defer rows.Close()

	claimed, err := pgx.CollectRows(rows, deliverysql.ScanOutboxRow)
	if err != nil {
		return nil, fmt.Errorf("claim outboxes for fanout: collect rows: %w", err)
	}

	return claimed, nil
}

func (s *TransitionStore) MaterializeFanout(
	ctx context.Context,
	outbox domain.YouTubeNotificationOutbox,
	roomIDs []string,
) (FanoutResult, error) {
	if outbox.ID <= 0 || outbox.LockedAt == nil {
		return FanoutResult{ApplyResult: newApplyResult(ApplyConflict, nil)}, errors.New("materialize fanout: exact outbox lock is absent")
	}

	targets, err := canonicalFanoutTargets(roomIDs)
	if err != nil {
		return FanoutResult{ApplyResult: newApplyResult(ApplyConflict, nil)}, fmt.Errorf("materialize fanout: canonical targets: %w", err)
	}

	var priorAdjudication CommitAdjudication

	for attempt := 0; attempt <= transitionOperationRetryLimit; attempt++ {
		result, applyErr := s.materializeFanoutOnce(ctx, outbox, targets)
		if applyErr == nil {
			result.CommitAdjudication = priorAdjudication

			return result, nil
		}

		state, children, found, stateErr := loadFanoutState(ctx, s.db, outbox.ID)
		if stateErr != nil {
			return FanoutResult{ApplyResult: adjudicated(newApplyResult(ApplyIndeterminate, nil), applyErr, CommitIndeterminate)}, errors.Join(applyErr, stateErr)
		}

		envelope := classifyFanoutEnvelope(state, children, found, outbox, targets)
		decision := adjudicateMaterializeFanout(envelope, outbox.ID, targets, applyErr, priorAdjudication, attempt)

		if decision.retry {
			priorAdjudication = decision.prior

			continue
		}

		return decision.result, decision.err
	}

	return FanoutResult{ApplyResult: newApplyResult(ApplyIndeterminate, nil)}, errors.New("materialize fanout: retry loop exhausted")
}

type fanoutAdjudication struct {
	result FanoutResult
	err    error
	retry  bool
	prior  CommitAdjudication
}

func adjudicateMaterializeFanout(
	envelope envelopeState,
	outboxID int64,
	targets []string,
	applyErr error,
	prior CommitAdjudication,
	attempt int,
) fanoutAdjudication {
	switch envelope {
	case envelopePost:
		result := FanoutResult{
			ApplyResult:   adjudicated(newApplyResult(ApplyApplied, []int64{outboxID}), applyErr, CommitConfirmedPost),
			DeliveryCount: len(targets), NoTargets: len(targets) == 0,
		}

		return fanoutAdjudication{result: result, prior: prior}
	case envelopePre:
		return adjudicateFanoutPre(applyErr, prior, attempt)
	case envelopeMissing:
		result := FanoutResult{ApplyResult: adjudicated(newApplyResult(ApplyMissing, nil), applyErr, CommitMissing)}

		return fanoutAdjudication{result: result, prior: prior}
	case envelopeMixed:
		result := FanoutResult{ApplyResult: adjudicated(newApplyResult(ApplyIndeterminate, nil), applyErr, CommitMixed)}
		breach := &AtomicityBreachError{Operation: materializeFanoutOperation, Detail: "canonical child set or outbox completion is partial"}

		return fanoutAdjudication{result: result, err: errors.Join(applyErr, breach), prior: prior}
	case envelopeConflict:
		result := FanoutResult{ApplyResult: adjudicated(newApplyResult(ApplyConflict, nil), applyErr, CommitConflict)}

		return fanoutAdjudication{result: result, prior: prior}
	}

	return fanoutAdjudication{
		result: FanoutResult{ApplyResult: newApplyResult(ApplyConflict, nil)},
		err:    fmt.Errorf("materialize fanout: unknown envelope %d", envelope),
		prior:  prior,
	}
}

func adjudicateFanoutPre(applyErr error, prior CommitAdjudication, attempt int) fanoutAdjudication {
	confirmed := adjudicated(newApplyResult(ApplyConflict, nil), applyErr, CommitConfirmedPre)
	if confirmed.CommitAdjudication != "" {
		prior = confirmed.CommitAdjudication
	}

	if attempt < transitionOperationRetryLimit {
		return fanoutAdjudication{retry: true, prior: prior}
	}

	result := newApplyResult(ApplyConflict, nil)

	result.CommitAdjudication = prior

	return fanoutAdjudication{result: FanoutResult{ApplyResult: result}, err: applyErr, prior: prior}
}

// ReviveFailedFanoutOutboxes는 신선한 FAILED outbox를 되살린다. 신선도·lease 만료 판정과 새 next_attempt_at은 DB
// 시계로 정한다.
func (s *TransitionStore) ReviveFailedFanoutOutboxes(
	ctx context.Context,
	freshnessWindow time.Duration,
	limit int,
) (ApplyResult, int, error) {
	if freshnessWindow <= 0 || limit <= 0 {
		return newApplyResult(ApplyConflict, nil), 0, errors.New("revive failed fanout outboxes: invalid bounds")
	}

	rows, err := s.db.Query(
		ctx,
		mustSQL("fanout_revive_failed.sql"),
		domain.OutboxStatusFailed,
		freshnessWindow.Milliseconds(),
		s.config.LockTimeout.Milliseconds(),
		limit,
		domain.OutboxStatusPending,
	)
	if err != nil {
		return newApplyResult(ApplyIndeterminate, nil), 0, fmt.Errorf("revive failed fanout outboxes: %w", err)
	}
	defer rows.Close()

	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return newApplyResult(ApplyIndeterminate, nil), 0, fmt.Errorf("revive failed fanout outboxes: collect: %w", err)
	}

	return newApplyResult(ApplyApplied, ids), len(ids), nil
}

// ApplyFanoutFailure는 정확한 claim을 fence로 fanout 실패를 기록한다. 다음 시도는 outbox 행을 잠근 뒤의 DB 시계에서
// RetryBackoff × 다음 시도 횟수 뒤이고, 종결 실패의 terminal_at도 같은 DB 시계다.
func (s *TransitionStore) ApplyFanoutFailure(
	ctx context.Context,
	outbox domain.YouTubeNotificationOutbox,
	reason string,
) (ApplyResult, error) {
	if outbox.ID <= 0 || outbox.LockedAt == nil || outbox.AttemptCount < 0 {
		return newApplyResult(ApplyConflict, nil), errors.New("apply fanout failure: exact outbox fence is invalid")
	}

	next := fanoutFailureState{
		status:  domain.OutboxStatusPending,
		attempt: outbox.AttemptCount + 1,
		reason:  deliverysql.TruncateString(reason, 500),
	}

	next.delay = s.config.RetryBackoff * time.Duration(next.attempt)

	if next.attempt >= s.config.MaxRetries {
		next.status = domain.OutboxStatusFailed
		next.terminal = true
	}

	var priorAdjudication CommitAdjudication

	for attempt := 0; attempt <= transitionOperationRetryLimit; attempt++ {
		applyErr := s.executeTx(ctx, "apply fanout failure", func(tx dbx.Querier) error {
			return applyFanoutFailureTx(ctx, tx, outbox, next)
		})
		if applyErr == nil {
			result := newApplyResult(ApplyApplied, []int64{outbox.ID})

			result.CommitAdjudication = priorAdjudication

			return result, nil
		}

		if _, ok := errors.AsType[*transitionConflictError](applyErr); ok {
			return newApplyResult(ApplyConflict, nil), nil
		}

		state, children, found, stateErr := loadFanoutState(ctx, s.db, outbox.ID)
		if stateErr != nil {
			return adjudicated(newApplyResult(ApplyIndeterminate, nil), applyErr, CommitIndeterminate), errors.Join(applyErr, stateErr)
		}

		envelope := classifyFanoutFailureEnvelope(state, children, found, outbox, next)
		decision := adjudicateFanoutFailure(envelope, outbox.ID, applyErr, priorAdjudication, attempt)

		if decision.retry {
			priorAdjudication = decision.prior

			continue
		}

		return decision.result.ApplyResult, decision.err
	}

	return newApplyResult(ApplyIndeterminate, nil), errors.New("apply fanout failure: retry loop exhausted")
}

// fanoutFailureState는 fanout 실패 전이의 다음 상태다. 시각 열은 DB 시계가 정하므로 지연과 종결 여부만 담는다.
type fanoutFailureState struct {
	status   domain.OutboxStatus
	attempt  int
	delay    time.Duration
	terminal bool
	reason   string
}

func applyFanoutFailureTx(
	ctx context.Context,
	tx dbx.Querier,
	outbox domain.YouTubeNotificationOutbox,
	next fanoutFailureState,
) error {
	rows, err := tx.Query(
		ctx,
		mustSQL("fanout_fail.sql"),
		next.status,
		next.attempt,
		next.delay.Milliseconds(),
		next.terminal,
		next.reason,
		outbox.ID,
		domain.OutboxStatusPending,
		outbox.AttemptCount,
		*outbox.LockedAt,
	)
	if err != nil {
		return fmt.Errorf("apply fanout failure: update: %w", err)
	}
	defer rows.Close()

	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return fmt.Errorf("apply fanout failure: collect: %w", err)
	}

	if len(ids) != 1 || ids[0] != outbox.ID {
		return &transitionConflictError{operation: "apply fanout failure", detail: "exact outbox fence changed"}
	}

	return nil
}

func adjudicateFanoutFailure(
	envelope envelopeState,
	outboxID int64,
	applyErr error,
	prior CommitAdjudication,
	attempt int,
) fanoutAdjudication {
	switch envelope {
	case envelopePost:
		result := adjudicated(newApplyResult(ApplyApplied, []int64{outboxID}), applyErr, CommitConfirmedPost)

		return fanoutAdjudication{result: FanoutResult{ApplyResult: result}, prior: prior}
	case envelopePre:
		return adjudicateFanoutPre(applyErr, prior, attempt)
	case envelopeMissing:
		result := adjudicated(newApplyResult(ApplyMissing, nil), applyErr, CommitMissing)

		return fanoutAdjudication{result: FanoutResult{ApplyResult: result}, prior: prior}
	case envelopeConflict, envelopeMixed:
		result := adjudicated(newApplyResult(ApplyConflict, nil), applyErr, CommitConflict)

		return fanoutAdjudication{result: FanoutResult{ApplyResult: result}, prior: prior}
	}

	return fanoutAdjudication{
		result: FanoutResult{ApplyResult: newApplyResult(ApplyConflict, nil)},
		err:    fmt.Errorf("apply fanout failure: unknown envelope %d", envelope),
		prior:  prior,
	}
}

// materializeFanoutOnce는 claim한 outbox의 자식 delivery를 만들거나 대상 없음으로 완료한다. 자식의 첫 시도 시각과
// 대상 없음 완료 시각은 DB 시계로 기록한다.
func (s *TransitionStore) materializeFanoutOnce(
	ctx context.Context,
	outbox domain.YouTubeNotificationOutbox,
	targets []string,
) (FanoutResult, error) {
	result := FanoutResult{DeliveryCount: len(targets), NoTargets: len(targets) == 0}

	err := s.executeTx(ctx, materializeFanoutOperation, func(tx dbx.Querier) error {
		if err := validateFanoutClaim(ctx, tx, outbox); err != nil {
			return fmt.Errorf("materialize fanout: validate claim: %w", err)
		}

		if len(targets) == 0 {
			if err := completeNoTargetFanout(ctx, tx, outbox); err != nil {
				return fmt.Errorf("materialize fanout: complete no-target claim: %w", err)
			}
		} else if err := materializeFanoutTargets(ctx, tx, outbox, targets); err != nil {
			return fmt.Errorf("materialize fanout: materialize targets: %w", err)
		}

		result.ApplyResult = newApplyResult(ApplyApplied, []int64{outbox.ID})

		return nil
	})
	if err != nil {
		return result, fmt.Errorf("materialize fanout: execute transaction: %w", err)
	}

	return result, nil
}

func validateFanoutClaim(ctx context.Context, tx dbx.Querier, outbox domain.YouTubeNotificationOutbox) error {
	state, children, found, err := loadFanoutState(ctx, tx, outbox.ID)
	if err != nil {
		return fmt.Errorf("materialize fanout: load claimed state: %w", err)
	}

	if !found {
		return &transitionMissingError{operation: materializeFanoutOperation, detail: "outbox is absent"}
	}

	if state.status != domain.OutboxStatusPending || !timesEqual(state.lockedAt, outbox.LockedAt) {
		return &transitionConflictError{operation: materializeFanoutOperation, detail: "exact outbox lock changed"}
	}

	if len(children) != 0 {
		return &AtomicityBreachError{Operation: materializeFanoutOperation, Detail: "claimed outbox already has a child subset"}
	}

	return nil
}

func completeNoTargetFanout(
	ctx context.Context,
	tx dbx.Querier,
	outbox domain.YouTubeNotificationOutbox,
) error {
	tag, err := tx.Exec(
		ctx,
		mustSQL("fanout_complete_no_targets.sql"),
		domain.OutboxStatusSent,
		outbox.ID,
		domain.OutboxStatusPending,
		*outbox.LockedAt,
	)
	if err != nil {
		return fmt.Errorf("materialize fanout: complete no-target outbox: %w", err)
	}

	if tag.RowsAffected() != 1 {
		return &transitionConflictError{operation: materializeFanoutOperation, detail: "no-target completion lost exact lock"}
	}

	return nil
}

func materializeFanoutTargets(
	ctx context.Context,
	tx dbx.Querier,
	outbox domain.YouTubeNotificationOutbox,
	targets []string,
) error {
	if _, err := tx.Exec(
		ctx,
		mustSQL("fanout_insert_children.sql"),
		outbox.ID,
		domain.OutboxStatusPending,
		targets,
	); err != nil {
		return fmt.Errorf("materialize fanout: insert children: %w", err)
	}

	_, inserted, found, err := loadFanoutState(ctx, tx, outbox.ID)
	if err != nil {
		return fmt.Errorf("materialize fanout: load inserted children: %w", err)
	}

	if !found {
		return &AtomicityBreachError{Operation: materializeFanoutOperation, Detail: "outbox disappeared after child insertion"}
	}

	if !slices.Equal(inserted, targets) {
		return &AtomicityBreachError{Operation: materializeFanoutOperation, Detail: "inserted child set does not match target snapshot"}
	}

	if err := releaseFanoutOutbox(ctx, tx, outbox); err != nil {
		return fmt.Errorf("materialize fanout: release materialized outbox: %w", err)
	}

	return nil
}

func releaseFanoutOutbox(ctx context.Context, tx dbx.Querier, outbox domain.YouTubeNotificationOutbox) error {
	tag, err := tx.Exec(
		ctx,
		mustSQL("fanout_release_outbox.sql"),
		outbox.ID,
		domain.OutboxStatusPending,
		*outbox.LockedAt,
	)
	if err != nil {
		return fmt.Errorf("materialize fanout: release outbox: %w", err)
	}

	if tag.RowsAffected() != 1 {
		return &transitionConflictError{operation: materializeFanoutOperation, detail: "outbox release lost exact lock"}
	}

	return nil
}

func loadFanoutState(
	ctx context.Context,
	db dbx.Querier,
	outboxID int64,
) (fanoutOutboxState, []string, bool, error) {
	var state fanoutOutboxState

	err := db.QueryRow(ctx, mustSQL("fanout_load_outbox.sql"), outboxID).Scan(
		&state.status,
		&state.attemptCount,
		&state.nextAttemptAt,
		&state.lockedAt,
		&state.sentAt,
		&state.terminalAt,
		&state.errorText,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return fanoutOutboxState{}, nil, false, nil
	}

	if err != nil {
		return fanoutOutboxState{}, nil, false, fmt.Errorf("load fanout outbox state: %w", err)
	}

	rows, err := db.Query(ctx, mustSQL("fanout_load_children.sql"), outboxID)
	if err != nil {
		return fanoutOutboxState{}, nil, false, fmt.Errorf("load fanout children: %w", err)
	}
	defer rows.Close()

	children, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fanoutOutboxState{}, nil, false, fmt.Errorf("load fanout children: collect: %w", err)
	}

	return state, children, true, nil
}

// classifyFanoutFailureEnvelope은 실패 전이의 commit 결과를 판정한다. 시각 열은 DB 시계가 정해 호출자가 미리 알 수
// 없으므로 종결 여부(terminal_at 유무)만 비교한다.
func classifyFanoutFailureEnvelope(
	state fanoutOutboxState,
	children []string,
	found bool,
	outbox domain.YouTubeNotificationOutbox,
	next fanoutFailureState,
) envelopeState {
	if !found {
		return envelopeMissing
	}

	if len(children) != 0 {
		return envelopeConflict
	}

	if state.status == next.status && state.attemptCount == next.attempt &&
		state.lockedAt == nil && state.sentAt == nil &&
		(state.terminalAt != nil) == next.terminal && state.errorText == next.reason {
		return envelopePost
	}

	if state.status == domain.OutboxStatusPending && state.attemptCount == outbox.AttemptCount &&
		timesEqual(state.lockedAt, outbox.LockedAt) {
		return envelopePre
	}

	return envelopeConflict
}

func classifyFanoutEnvelope(
	state fanoutOutboxState,
	children []string,
	found bool,
	outbox domain.YouTubeNotificationOutbox,
	targets []string,
) envelopeState {
	if !found {
		return envelopeMissing
	}

	if len(targets) == 0 {
		return classifyNoTargetFanoutEnvelope(state, children, outbox)
	}

	return classifyTargetFanoutEnvelope(state, children, outbox, targets)
}

func classifyNoTargetFanoutEnvelope(
	state fanoutOutboxState,
	children []string,
	outbox domain.YouTubeNotificationOutbox,
) envelopeState {
	if state.status == domain.OutboxStatusSent && state.lockedAt == nil && state.sentAt != nil && state.terminalAt != nil && len(children) == 0 {
		return envelopePost
	}

	if state.status == domain.OutboxStatusPending && timesEqual(state.lockedAt, outbox.LockedAt) && len(children) == 0 {
		return envelopePre
	}

	return envelopeConflict
}

func classifyTargetFanoutEnvelope(
	state fanoutOutboxState,
	children []string,
	outbox domain.YouTubeNotificationOutbox,
	targets []string,
) envelopeState {
	if state.status == domain.OutboxStatusPending && state.lockedAt == nil && slices.Equal(children, targets) {
		return envelopePost
	}

	if state.status == domain.OutboxStatusPending && timesEqual(state.lockedAt, outbox.LockedAt) && len(children) == 0 {
		return envelopePre
	}

	if len(children) > 0 && !slices.Equal(children, targets) {
		return envelopeMixed
	}

	return envelopeConflict
}

func canonicalFanoutTargets(roomIDs []string) ([]string, error) {
	targets := make([]string, 0, len(roomIDs))
	for i := range roomIDs {
		roomID := strings.TrimSpace(roomIDs[i])
		if roomID == "" {
			return nil, fmt.Errorf("materialize fanout: room id at index %d is empty", i)
		}

		targets = append(targets, roomID)
	}

	slices.Sort(targets)

	targets = slices.Compact(targets)

	return targets, nil
}
