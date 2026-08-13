package sourceobservation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

type AuthoritativeWrite func(context.Context, dbx.Tx) error

func (r *Repository) Finalize(
	ctx context.Context,
	completion Completion,
	authoritativeWrite AuthoritativeWrite,
) error {
	if r == nil || r.pool == nil {
		return ErrInvalidRepository
	}
	if err := completion.validate(); err != nil {
		return err
	}
	return dbx.InPgxTx(ctx, r.pool, func(tx dbx.Tx) error {
		return finalizeTx(ctx, tx, completion, authoritativeWrite)
	})
}

func finalizeTx(
	ctx context.Context,
	tx dbx.Tx,
	completion Completion,
	authoritativeWrite AuthoritativeWrite,
) error {
	fence, err := loadAuthority(ctx, tx, completion.SourceKind, true)
	if err != nil {
		return err
	}
	if fence.Generation != completion.ExpectedGeneration {
		return fmt.Errorf(
			"finalize source observation: %w: expected=%d actual=%d",
			ErrStaleGeneration,
			completion.ExpectedGeneration,
			fence.Generation,
		)
	}
	if err := validateFinalizeMode(fence.Mode, completion.ParityStatus, authoritativeWrite != nil); err != nil {
		return err
	}

	observedAt, err := lockClaim(ctx, tx, completion)
	if err != nil {
		return err
	}
	if fence.Mode == contract.AuthorityModeAuthoritative {
		if err := authoritativeWrite(ctx, tx); err != nil {
			return fmt.Errorf("finalize source observation: apply authoritative domain write: %w", err)
		}
	}
	if err := completeClaim(ctx, tx, completion); err != nil {
		return err
	}
	return updateConsumerOffset(ctx, tx, completion, observedAt)
}

func validateFinalizeMode(
	mode contract.AuthorityMode,
	parityStatus contract.ParityStatus,
	hasAuthoritativeWrite bool,
) error {
	switch mode {
	case contract.AuthorityModeShadow:
		if hasAuthoritativeWrite {
			return fmt.Errorf("finalize source observation: shadow mode rejects an authoritative write callback")
		}
		if parityStatus == contract.ParityStatusNotChecked {
			return fmt.Errorf("finalize source observation: shadow mode requires a parity result")
		}
		return nil
	case contract.AuthorityModeAuthoritative:
		if !hasAuthoritativeWrite {
			return fmt.Errorf("finalize source observation: authoritative mode requires a domain write callback")
		}
		return nil
	case contract.AuthorityModeLegacy:
		return ErrAuthorityInactive
	default:
		return fmt.Errorf("finalize source observation: invalid authority mode %q", mode)
	}
}

func lockClaim(ctx context.Context, tx dbx.Querier, completion Completion) (time.Time, error) {
	var observedAt time.Time
	err := tx.QueryRow(
		ctx,
		mustSQL("repository_claim_lock_0013_13.sql"),
		completion.ObservationID,
		completion.SourceKind,
		completion.LeaseToken,
		completion.ExpectedGeneration,
	).Scan(&observedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, ErrClaimLost
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("finalize source observation: lock claim: %w", err)
	}
	return observedAt, nil
}

func completeClaim(ctx context.Context, tx dbx.Querier, completion Completion) error {
	var observationID int64
	err := tx.QueryRow(
		ctx,
		mustSQL("repository_complete_0007_07.sql"),
		completion.ObservationID,
		completion.SourceKind,
		completion.LeaseToken,
		completion.ExpectedGeneration,
		completion.ParityStatus,
		nullableJSON(completion.ParityDetail),
	).Scan(&observationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrClaimLost
	}
	if err != nil {
		return fmt.Errorf("finalize source observation: complete claim: %w", err)
	}
	return nil
}

func updateConsumerOffset(
	ctx context.Context,
	tx dbx.Querier,
	completion Completion,
	observedAt time.Time,
) error {
	if _, err := tx.Exec(
		ctx,
		mustSQL("repository_offset_upsert_0008_08.sql"),
		completion.ConsumerName,
		completion.SourceKind,
		completion.ObservationID,
		observedAt,
	); err != nil {
		return fmt.Errorf("finalize source observation: update consumer offset: %w", err)
	}
	return nil
}
