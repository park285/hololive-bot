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

// verifyLeaseMembership은 lease 소유 증명과 job별 membership 유효성을 한 문장으로 판정한다.
// 소유를 잃었으면 ErrFenceLost, 유효 CURRENT가 없으면 ErrProjectionStale,
// 소유는 유지되지만 범위가 바뀌었으면 ErrTargetDisabled를 반환한다. 호출자는 필요한 guard·lease 잠금을
// 먼저 얻어야 이 문장이 잠금 이후 snapshot으로 평가된다.
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

// LeaseMembershipQuery는 verifyLeaseMembership과 같은 판정 문장을 발행 검증·target snapshot의 pipeline batch에 쌓을 수 있게 반환한다.
func LeaseMembershipQuery(proof *contract.LeaseProof, scope collection.MembershipScope) (string, []any) {
	return leaseMembershipQuerySQL, []any{
		proof.JobKey, proof.OwnerInstance, proof.FenceEpoch, proof.ProjectionGeneration, proof.ScheduledFor,
		scope.Kinds, scope.ExactSubject,
	}
}

// ScanLeaseMembership은 LeaseMembershipQuery 결과 한 행을 lease 결과 sentinel로 판정한다.
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
