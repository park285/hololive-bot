package sourceobservation

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

type replayObservation struct {
	provider           contract.Provider
	kind               contract.ObservationKind
	subjectKey         string
	observationKey     string
	schemaVersion      int16
	contractGeneration int64
	evidenceSHA256     string
}

type replayQueueState struct {
	status           string
	previousAttempts int
	replayCount      int
}

func (r *Repository) RequestReplay(ctx context.Context, input ReplayInput) (ReplayResult, error) {
	if err := r.validate(); err != nil {
		return ReplayResult{}, fmt.Errorf("validate: %w", err)
	}

	if input.ObservationID <= 0 {
		return ReplayResult{}, errors.New("request source observation replay: observation id must be positive")
	}

	if err := validateText("requested by", input.RequestedBy, 128); err != nil {
		return ReplayResult{}, fmt.Errorf("request source observation replay: %w", err)
	}

	if err := validateText("reason", input.Reason, 1024); err != nil {
		return ReplayResult{}, fmt.Errorf("request source observation replay: %w", err)
	}

	out, err := dbx.InPgxTxWithResult(ctx, r.pool, func(tx dbx.Tx) (ReplayResult, error) {
		return r.requestReplayTx(ctx, tx, input)
	})
	if err != nil {
		return out, fmt.Errorf("in pgx tx with result: %w", err)
	}

	return out, nil
}

func (r *Repository) ProcessNextReplay(ctx context.Context) (bool, error) {
	if err := r.validate(); err != nil {
		return false, fmt.Errorf("validate: %w", err)
	}

	out, err := dbx.InPgxTxWithResult(ctx, r.pool, func(tx dbx.Tx) (bool, error) {
		return r.processNextReplayTx(ctx, tx)
	})
	if err != nil {
		return out, fmt.Errorf("in pgx tx with result: %w", err)
	}

	return out, nil
}

func (r *Repository) requestReplayTx(
	ctx context.Context,
	tx dbx.Tx,
	input ReplayInput,
) (ReplayResult, error) {
	observation, err := loadReplayObservation(ctx, tx, input.ObservationID)
	if err != nil {
		return ReplayResult{}, fmt.Errorf("load replay observation: %w", err)
	}

	queue, err := loadReplayQueueState(ctx, tx, input.ObservationID)
	if err != nil {
		return ReplayResult{}, fmt.Errorf("load replay queue state: %w", err)
	}

	requestID, err := insertReplayRequest(ctx, tx, input, &observation, queue.previousAttempts)
	if err != nil {
		return ReplayResult{}, fmt.Errorf("insert replay request: %w", err)
	}

	out, err := r.applyReplayDecision(ctx, tx, requestID, input.ObservationID, &observation, queue)
	if err != nil {
		return out, fmt.Errorf("apply replay decision: %w", err)
	}

	return out, nil
}

func (r *Repository) processNextReplayTx(ctx context.Context, tx dbx.Tx) (bool, error) {
	requestID, observationID, err := lockPendingReplay(ctx, tx)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("lock pending replay: %w", err)
	}

	if observationID <= 0 {
		rejectErr := rejectMissingReplay(ctx, tx, requestID)

		return true, errors.Join(rejectErr)
	}

	observation, err := loadReplayObservation(ctx, tx, observationID)
	if err != nil {
		if errors.Is(err, errReplayObservationMissing) {
			rejectErr := rejectMissingReplay(ctx, tx, requestID)

			return true, errors.Join(rejectErr)
		}

		return false, fmt.Errorf("reject replay: %w", err)
	}

	queue, err := loadReplayQueueState(ctx, tx, observationID)
	if err != nil {
		return false, fmt.Errorf("load replay queue state: %w", err)
	}

	if _, err := r.applyReplayDecision(ctx, tx, requestID, observationID, &observation, queue); err != nil {
		return false, fmt.Errorf("apply replay decision: %w", err)
	}

	return true, nil
}

func rejectMissingReplay(ctx context.Context, tx dbx.Tx, requestID int64) error {
	if _, err := rejectReplay(ctx, tx, ReplayResult{RequestID: requestID}, "observation_not_found"); err != nil {
		return fmt.Errorf("reject replay: %w", err)
	}

	return nil
}

func (r *Repository) applyReplayDecision(
	ctx context.Context,
	tx dbx.Tx,
	requestID int64,
	observationID int64,
	observation *replayObservation,
	queue replayQueueState,
) (ReplayResult, error) {
	result := ReplayResult{RequestID: requestID}
	version := ContractVersion{
		Provider:   observation.provider,
		Kind:       observation.kind,
		Schema:     observation.schemaVersion,
		Generation: observation.contractGeneration,
	}

	if reason := replayRejectionReason(r.supported.Supports(version), queue); reason != "" {
		out, rejectErr := rejectReplayResult(ctx, tx, result, reason)

		return out, errors.Join(rejectErr)
	}

	result, activated, err := activateReplayDecision(ctx, tx, result, observationID, queue.replayCount+1)
	if err != nil {
		return result, fmt.Errorf("%w", err)
	}

	if !activated {
		return result, nil
	}

	if _, err := tx.Exec(ctx, mustSQL("repository_replay_request_apply_0025_25.sql"), requestID); err != nil {
		return ReplayResult{}, fmt.Errorf("request source observation replay: apply audit: %w", err)
	}

	result.Applied = true

	return result, nil
}

func replayRejectionReason(supported bool, queue replayQueueState) string {
	if !supported {
		return "unsupported_contract"
	}

	statusReasons := map[string]string{
		string(contract.StatusProcessing): "observation_processing",
		string(contract.StatusPending):    "observation_pending",
	}
	if reason := statusReasons[queue.status]; reason != "" {
		return reason
	}

	if queue.replayCount >= MaxReplayCount {
		return "replay_limit_exhausted"
	}

	return ""
}

func rejectReplayResult(ctx context.Context, tx dbx.Tx, result ReplayResult, reason string) (ReplayResult, error) {
	out, err := rejectReplay(ctx, tx, result, reason)
	if err != nil {
		return out, fmt.Errorf("reject replay: %w", err)
	}

	return out, nil
}

func activateReplayDecision(ctx context.Context, tx dbx.Tx, result ReplayResult, observationID int64, replayCount int) (ReplayResult, bool, error) {
	err := activateReplayQueue(ctx, tx, observationID, replayCount)
	if err == nil {
		return result, true, nil
	}

	if errors.Is(err, pgx.ErrNoRows) {
		out, rejectErr := rejectReplayResult(ctx, tx, result, "queue_state_changed")

		return out, false, errors.Join(rejectErr)
	}

	return ReplayResult{}, false, fmt.Errorf("activate replay queue: %w", err)
}

var errReplayObservationMissing = errors.New("request source observation replay: observation not found")

func loadReplayObservation(ctx context.Context, tx dbx.Tx, observationID int64) (replayObservation, error) {
	var observation replayObservation

	err := tx.QueryRow(
		ctx,
		mustSQL("repository_replay_observation_0020_20.sql"),
		observationID,
	).Scan(
		&observation.provider,
		&observation.kind,
		&observation.subjectKey,
		&observation.observationKey,
		&observation.schemaVersion,
		&observation.contractGeneration,
		&observation.evidenceSHA256,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return replayObservation{}, errReplayObservationMissing
	}

	if err != nil {
		return replayObservation{}, fmt.Errorf("request source observation replay: load observation: %w", err)
	}

	return observation, nil
}

func loadReplayQueueState(ctx context.Context, tx dbx.Tx, observationID int64) (replayQueueState, error) {
	var queue replayQueueState

	err := tx.QueryRow(
		ctx,
		mustSQL("repository_replay_queue_0021_21.sql"),
		observationID,
	).Scan(&queue.status, &queue.previousAttempts, &queue.replayCount)

	if errors.Is(err, pgx.ErrNoRows) {
		queue.status = "MISSING"
		if countErr := tx.QueryRow(
			ctx,
			mustSQL("repository_replay_count_0022_22.sql"),
			observationID,
		).Scan(&queue.replayCount); countErr != nil {
			return replayQueueState{}, fmt.Errorf("request source observation replay: count prior replay: %w", countErr)
		}

		return queue, nil
	}

	if err != nil {
		return replayQueueState{}, fmt.Errorf("request source observation replay: lock queue row: %w", err)
	}

	return queue, nil
}

func insertReplayRequest(
	ctx context.Context,
	tx dbx.Tx,
	input ReplayInput,
	observation *replayObservation,
	previousAttempts int,
) (int64, error) {
	var requestID int64

	err := tx.QueryRow(
		ctx,
		mustSQL("repository_replay_request_insert_0023_23.sql"),
		input.ObservationID,
		observation.provider,
		observation.kind,
		observation.subjectKey,
		observation.observationKey,
		observation.evidenceSHA256,
		input.RequestedBy,
		input.Reason,
		previousAttempts,
	).Scan(&requestID)
	if err != nil {
		return 0, fmt.Errorf("request source observation replay: insert audit: %w", err)
	}

	return requestID, nil
}

func lockPendingReplay(ctx context.Context, tx dbx.Tx) (replayRequestID, replayObservationID int64, scanErr error) {
	var (
		requestID     int64
		observationID *int64
	)

	err := tx.QueryRow(ctx, mustSQL("repository_replay_pending_lock_0080_80.sql")).Scan(&requestID, &observationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, 0, fmt.Errorf("scan: %w", err)
		}

		return 0, 0, fmt.Errorf("process source observation replay: lock pending: %w", err)
	}

	if observationID == nil {
		return requestID, 0, nil
	}

	return requestID, *observationID, nil
}

func activateReplayQueue(ctx context.Context, tx dbx.Tx, observationID int64, replayCount int) error {
	var activatedID int64

	err := tx.QueryRow(
		ctx,
		mustSQL("repository_replay_queue_activate_0024_24.sql"),
		observationID,
		replayCount,
	).Scan(&activatedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("scan: %w", err)
		}

		return fmt.Errorf("request source observation replay: reactivate queue: %w", err)
	}

	return nil
}

func rejectReplay(
	ctx context.Context,
	tx dbx.Tx,
	result ReplayResult,
	code string,
) (ReplayResult, error) {
	if _, err := tx.Exec(
		ctx,
		mustSQL("repository_replay_request_reject_0026_26.sql"),
		result.RequestID,
		code,
	); err != nil {
		return ReplayResult{}, fmt.Errorf("request source observation replay: reject audit: %w", err)
	}

	result.RejectionCode = code

	return result, nil
}
