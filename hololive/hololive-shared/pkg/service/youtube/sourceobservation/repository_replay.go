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

func (r *Repository) RequestReplay(ctx context.Context, input ReplayInput) (ReplayResult, error) {
	if err := r.validate(); err != nil {
		return ReplayResult{}, err
	}
	if input.ObservationID <= 0 {
		return ReplayResult{}, fmt.Errorf("request source observation replay: observation id must be positive")
	}
	if err := validateText("requested by", input.RequestedBy, 128); err != nil {
		return ReplayResult{}, fmt.Errorf("request source observation replay: %w", err)
	}
	if err := validateText("reason", input.Reason, 1024); err != nil {
		return ReplayResult{}, fmt.Errorf("request source observation replay: %w", err)
	}
	return dbx.InPgxTxWithResult(ctx, r.pool, func(tx dbx.Tx) (ReplayResult, error) {
		return r.requestReplayTx(ctx, tx, input)
	})
}

func (r *Repository) requestReplayTx(
	ctx context.Context,
	tx dbx.Tx,
	input ReplayInput,
) (ReplayResult, error) {
	var observation replayObservation
	err := tx.QueryRow(
		ctx,
		mustSQL("repository_replay_observation_0020_20.sql"),
		input.ObservationID,
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
		return ReplayResult{}, fmt.Errorf("request source observation replay: observation not found")
	}
	if err != nil {
		return ReplayResult{}, fmt.Errorf("request source observation replay: load observation: %w", err)
	}
	var status string
	var previousAttempts int
	var replayCount int
	err = tx.QueryRow(
		ctx,
		mustSQL("repository_replay_queue_0021_21.sql"),
		input.ObservationID,
	).Scan(&status, &previousAttempts, &replayCount)
	if errors.Is(err, pgx.ErrNoRows) {
		status = "MISSING"
		if err := tx.QueryRow(
			ctx,
			mustSQL("repository_replay_count_0022_22.sql"),
			input.ObservationID,
		).Scan(&replayCount); err != nil {
			return ReplayResult{}, fmt.Errorf("request source observation replay: count prior replay: %w", err)
		}
	} else if err != nil {
		return ReplayResult{}, fmt.Errorf("request source observation replay: lock queue row: %w", err)
	}
	var requestID int64
	err = tx.QueryRow(
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
		return ReplayResult{}, fmt.Errorf("request source observation replay: insert audit: %w", err)
	}
	result := ReplayResult{RequestID: requestID}
	version := ContractVersion{
		Provider:   observation.provider,
		Kind:       observation.kind,
		Schema:     observation.schemaVersion,
		Generation: observation.contractGeneration,
	}
	if !r.supported.Supports(version) {
		return rejectReplay(ctx, tx, result, "unsupported_contract")
	}
	if status == string(contract.StatusProcessing) {
		return rejectReplay(ctx, tx, result, "observation_processing")
	}
	if status == string(contract.StatusPending) {
		return rejectReplay(ctx, tx, result, "observation_pending")
	}
	if replayCount >= MaxReplayCount {
		return rejectReplay(ctx, tx, result, "replay_limit_exhausted")
	}
	var observationID int64
	err = tx.QueryRow(
		ctx,
		mustSQL("repository_replay_queue_activate_0024_24.sql"),
		input.ObservationID,
		replayCount+1,
	).Scan(&observationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return rejectReplay(ctx, tx, result, "queue_state_changed")
	}
	if err != nil {
		return ReplayResult{}, fmt.Errorf("request source observation replay: reactivate queue: %w", err)
	}
	if _, err := tx.Exec(ctx, mustSQL("repository_replay_request_apply_0025_25.sql"), requestID); err != nil {
		return ReplayResult{}, fmt.Errorf("request source observation replay: apply audit: %w", err)
	}
	result.Applied = true
	return result, nil
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
