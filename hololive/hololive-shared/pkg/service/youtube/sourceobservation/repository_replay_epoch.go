package sourceobservation

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/pkg/dbx"
)

const replayEpochExpiredCode = "replay_epoch_expired"

func (r *Repository) ActivateReplayEpoch(
	ctx context.Context,
	input ReplayEpochInput,
) (ReplayEpochActivation, error) {
	if err := r.validate(); err != nil {
		return ReplayEpochActivation{}, fmt.Errorf("validate: %w", err)
	}

	if err := validateText("activated by", input.ActivatedBy, 128); err != nil {
		return ReplayEpochActivation{}, fmt.Errorf("activate source observation replay epoch: %w", err)
	}

	if err := validateText("reason", input.Reason, 1024); err != nil {
		return ReplayEpochActivation{}, fmt.Errorf("activate source observation replay epoch: %w", err)
	}

	out, err := dbx.InPgxTxWithResult(ctx, r.pool, func(tx dbx.Tx) (ReplayEpochActivation, error) {
		return activateReplayEpochTx(ctx, tx, input)
	})
	if err != nil {
		return out, fmt.Errorf("in pgx tx with result: %w", err)
	}

	return out, nil
}

func activateReplayEpochTx(
	ctx context.Context,
	tx dbx.Tx,
	input ReplayEpochInput,
) (ReplayEpochActivation, error) {
	epoch, err := scanReplayEpoch(tx.QueryRow(
		ctx,
		mustSQL("repository_replay_epoch_activate_0085_85.sql"),
		input.ActivatedBy,
		input.Reason,
	))
	if err == nil {
		return ReplayEpochActivation{Epoch: epoch, Activated: true}, nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return ReplayEpochActivation{}, fmt.Errorf("activate source observation replay epoch: %w", err)
	}

	epoch, err = scanReplayEpoch(tx.QueryRow(
		ctx,
		mustSQL("repository_replay_epoch_load_0086_86.sql"),
	))
	if err != nil {
		return ReplayEpochActivation{}, fmt.Errorf("load active source observation replay epoch: %w", err)
	}

	return ReplayEpochActivation{Epoch: epoch}, nil
}

func scanReplayEpoch(row pgx.Row) (ReplayEpoch, error) {
	var epoch ReplayEpoch

	if err := row.Scan(&epoch.CutoffReceivedAt, &epoch.ActivatedBy, &epoch.Reason); err != nil {
		return ReplayEpoch{}, fmt.Errorf("scan source observation replay epoch: %w", err)
	}

	epoch.CutoffReceivedAt = epoch.CutoffReceivedAt.UTC()

	return epoch, nil
}
