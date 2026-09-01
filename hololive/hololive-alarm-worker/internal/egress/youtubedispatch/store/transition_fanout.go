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

func (s *TransitionStore) ClaimOutboxesForFanout(ctx context.Context, batchSize int) ([]domain.YouTubeNotificationOutbox, error) {
	if err := s.ensureReady(ctx); err != nil {
		return nil, fmt.Errorf("claim outboxes for fanout: %w", err)
	}

	if batchSize <= 0 {
		return nil, errors.New("claim outboxes for fanout: batch size must be positive")
	}

	claimedAt := time.Now().UTC().Truncate(time.Microsecond)

	rows, err := s.db.Query(
		ctx,
		mustSQL("fanout_claim.sql"),
		domain.OutboxStatusPending,
		claimedAt.Add(-s.config.LockTimeout),
		claimedAt,
		claimedAt.Add(-s.config.ClaimFreshnessWindow),
		batchSize,
		claimedAt,
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
	if err := s.ensureReady(ctx); err != nil {
		return FanoutResult{ApplyResult: newApplyResult(ApplyIndeterminate, nil)}, fmt.Errorf("materialize fanout: %w", err)
	}

	if outbox.ID <= 0 || outbox.LockedAt == nil {
		return FanoutResult{ApplyResult: newApplyResult(ApplyConflict, nil)}, errors.New("materialize fanout: exact outbox lock is absent")
	}

	targets, err := canonicalFanoutTargets(roomIDs)
	if err != nil {
		return FanoutResult{ApplyResult: newApplyResult(ApplyConflict, nil)}, fmt.Errorf("materialize fanout: canonical targets: %w", err)
	}

	completedAt := time.Now().UTC().Truncate(time.Microsecond)

	var priorAdjudication CommitAdjudication

	for attempt := 0; attempt <= transitionOperationRetryLimit; attempt++ {
		result, applyErr := s.materializeFanoutOnce(ctx, outbox, targets, completedAt)
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

func (s *TransitionStore) ReviveFailedFanoutOutboxes(
	ctx context.Context,
	freshnessWindow time.Duration,
	limit int,
) (ApplyResult, int, error) {
	if err := s.ensureReady(ctx); err != nil {
		return newApplyResult(ApplyIndeterminate, nil), 0, fmt.Errorf("revive failed fanout outboxes: %w", err)
	}

	if freshnessWindow <= 0 || limit <= 0 {
		return newApplyResult(ApplyConflict, nil), 0, errors.New("revive failed fanout outboxes: invalid bounds")
	}

	at := time.Now().UTC().Truncate(time.Microsecond)

	rows, err := s.db.Query(
		ctx,
		mustSQL("fanout_revive_failed.sql"),
		domain.OutboxStatusFailed,
		at.Add(-freshnessWindow),
		at.Add(-s.config.LockTimeout),
		limit,
		domain.OutboxStatusPending,
		at,
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

func (s *TransitionStore) ApplyFanoutFailure(
	ctx context.Context,
	outbox domain.YouTubeNotificationOutbox,
	reason string,
) (ApplyResult, error) {
	if err := s.ensureReady(ctx); err != nil {
		return newApplyResult(ApplyIndeterminate, nil), fmt.Errorf("apply fanout failure: %w", err)
	}

	if outbox.ID <= 0 || outbox.LockedAt == nil || outbox.AttemptCount < 0 {
		return newApplyResult(ApplyConflict, nil), errors.New("apply fanout failure: exact outbox fence is invalid")
	}

	at := time.Now().UTC().Truncate(time.Microsecond)
	nextAttempt := outbox.AttemptCount + 1
	nextStatus := domain.OutboxStatusPending
	nextAttemptAt := at.Add(s.config.RetryBackoff * time.Duration(nextAttempt)).Truncate(time.Microsecond)

	var terminalAt *time.Time

	if nextAttempt >= s.config.MaxRetries {
		nextStatus = domain.OutboxStatusFailed
		terminalAt = &at
	}

	truncatedReason := deliverysql.TruncateString(reason, 500)

	var priorAdjudication CommitAdjudication

	for attempt := 0; attempt <= transitionOperationRetryLimit; attempt++ {
		applyErr := s.executeTx(ctx, "apply fanout failure", func(tx dbx.Querier) error {
			return applyFanoutFailureTx(
				ctx, tx, outbox, nextStatus, nextAttempt, nextAttemptAt, terminalAt, truncatedReason,
			)
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

		envelope := classifyFanoutFailureEnvelope(
			state, children, found, outbox, nextStatus, nextAttempt, nextAttemptAt, terminalAt, truncatedReason,
		)
		decision := adjudicateFanoutFailure(envelope, outbox.ID, applyErr, priorAdjudication, attempt)

		if decision.retry {
			priorAdjudication = decision.prior

			continue
		}

		return decision.result.ApplyResult, decision.err
	}

	return newApplyResult(ApplyIndeterminate, nil), errors.New("apply fanout failure: retry loop exhausted")
}

func applyFanoutFailureTx(
	ctx context.Context,
	tx dbx.Querier,
	outbox domain.YouTubeNotificationOutbox,
	nextStatus domain.OutboxStatus,
	nextAttempt int,
	nextAttemptAt time.Time,
	terminalAt *time.Time,
	reason string,
) error {
	rows, err := tx.Query(
		ctx,
		mustSQL("fanout_fail.sql"),
		nextStatus,
		nextAttempt,
		nextAttemptAt,
		terminalAt,
		reason,
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

func (s *TransitionStore) materializeFanoutOnce(
	ctx context.Context,
	outbox domain.YouTubeNotificationOutbox,
	targets []string,
	completedAt time.Time,
) (FanoutResult, error) {
	result := FanoutResult{DeliveryCount: len(targets), NoTargets: len(targets) == 0}

	err := s.executeTx(ctx, materializeFanoutOperation, func(tx dbx.Querier) error {
		if err := validateFanoutClaim(ctx, tx, outbox); err != nil {
			return fmt.Errorf("materialize fanout: validate claim: %w", err)
		}

		if len(targets) == 0 {
			if err := completeNoTargetFanout(ctx, tx, outbox, completedAt); err != nil {
				return fmt.Errorf("materialize fanout: complete no-target claim: %w", err)
			}
		} else if err := materializeFanoutTargets(ctx, tx, outbox, targets, completedAt); err != nil {
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
	completedAt time.Time,
) error {
	tag, err := tx.Exec(
		ctx,
		mustSQL("fanout_complete_no_targets.sql"),
		domain.OutboxStatusSent,
		completedAt,
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
	completedAt time.Time,
) error {
	if _, err := tx.Exec(
		ctx,
		mustSQL("fanout_insert_children.sql"),
		outbox.ID,
		domain.OutboxStatusPending,
		completedAt,
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

func classifyFanoutFailureEnvelope(
	state fanoutOutboxState,
	children []string,
	found bool,
	outbox domain.YouTubeNotificationOutbox,
	nextStatus domain.OutboxStatus,
	nextAttempt int,
	nextAttemptAt time.Time,
	terminalAt *time.Time,
	reason string,
) envelopeState {
	if !found {
		return envelopeMissing
	}

	if len(children) != 0 {
		return envelopeConflict
	}

	if state.status == nextStatus && state.attemptCount == nextAttempt &&
		state.lockedAt == nil && state.sentAt == nil &&
		state.nextAttemptAt.Equal(nextAttemptAt) && timesEqual(state.terminalAt, terminalAt) && state.errorText == reason {
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
