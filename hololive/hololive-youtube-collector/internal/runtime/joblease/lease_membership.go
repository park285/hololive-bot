package joblease

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
)

func verifyLeaseMembership(ctx context.Context, q dbx.Querier, proof *contract.LeaseProof, scope collection.MembershipScope) error {
	if q == nil || proof == nil || len(scope.Kinds) == 0 {
		return errors.New("verify collection lease membership: request is invalid")
	}

	query, args := LeaseMembershipQuery(proof, scope)

	if err := ScanLeaseMembership(q.QueryRow(ctx, query, args...)); err != nil {
		return fmt.Errorf("scan lease membership: %w", err)
	}

	return nil
}

func LeaseMembershipQuery(proof *contract.LeaseProof, scope collection.MembershipScope) (string, []any) {
	return leaseMembershipQuerySQL, []any{
		proof.JobKey, proof.OwnerInstance, proof.FenceEpoch, proof.ProjectionGeneration, proof.ScheduledFor,
		scope.Kinds, scope.ExactSubject,
	}
}

func ScanLeaseMembership(row pgx.Row) error {
	var projectionCurrent, membershipValid bool

	err := row.Scan(&projectionCurrent, &membershipValid)
	if errors.Is(err, pgx.ErrNoRows) {
		return collection.ErrFenceLost
	}

	if err != nil {
		return fmt.Errorf("verify collection lease membership: %w", err)
	}

	if !projectionCurrent {
		return collection.ErrProjectionStale
	}

	if !membershipValid {
		return fmt.Errorf("verify collection lease membership: job membership changed: %w", collection.ErrTargetDisabled)
	}

	return nil
}
