package sourceobservation

import (
	"context"
	"fmt"

	"github.com/park285/shared-go/postgres/pgxdb"

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
	var activation ReplayEpochActivation

	err := pgxdb.QueryRow(ctx, tx,
		activateReplayEpochTxSQL,
		input.ActivatedBy,
		input.Reason,
	).Scan(&activation.Epoch.CutoffReceivedAt, &activation.Epoch.ActivatedBy, &activation.Epoch.Reason, &activation.Activated)
	if err != nil {
		return ReplayEpochActivation{}, fmt.Errorf("activate source observation replay epoch: %w", err)
	}

	activation.Epoch.CutoffReceivedAt = activation.Epoch.CutoffReceivedAt.UTC()

	return activation, nil
}
