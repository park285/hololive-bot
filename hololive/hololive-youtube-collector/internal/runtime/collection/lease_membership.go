package collection

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

// MembershipScope는 acquire가 lease에 기록하는 job별 membership 범위다.
// Kinds는 cadence·roster kind의 정렬된 합집합이고(emission은 cadence의 부분집합),
// ExactSubject면 lease subject 하나, 아니면 해당 kind의 CURRENT 전체 target이 범위다.
// lease 저장소와 발행 저장소가 같은 판정을 쓰므로 두 adapter 어느 쪽도 소유하지 않습니다.
type MembershipScope struct {
	Kinds        []string
	ExactSubject bool
}

// MembershipScopeFor는 컴파일된 job 계약에서 lease 범위를 만든다. 저장된 범위가 이 값과 다르면
// renew·snapshot·complete·publish가 모두 membership 무효로 거절한다.
func MembershipScopeFor(job JobContract) MembershipScope {
	requested := job.RequestedKinds()
	kinds := make([]string, len(requested))

	for i, kind := range requested {
		kinds[i] = string(kind)
	}

	return MembershipScope{Kinds: kinds, ExactSubject: job.Membership() == JobMembershipExactSubject}
}

// VerifyLeaseMembership은 lease 소유 증명과 job별 membership 유효성을 한 문장으로 판정한다.
// 소유를 잃었으면 ErrFenceLost, 유효 CURRENT가 없으면 ErrProjectionStale,
// 소유는 유지되지만 범위가 바뀌었으면 ErrTargetDisabled를 반환한다. 호출자는 필요한 guard·lease 잠금을
// 먼저 얻어야 이 문장이 잠금 이후 snapshot으로 평가된다.
func VerifyLeaseMembership(ctx context.Context, q dbx.Querier, proof *contract.LeaseProof, scope MembershipScope) error {
	if q == nil || proof == nil || len(scope.Kinds) == 0 {
		return errors.New("verify collection lease membership: request is invalid")
	}

	query, args := LeaseMembershipQuery(proof, scope)

	if err := ScanLeaseMembership(q.QueryRow(ctx, query, args...)); err != nil {
		return fmt.Errorf("scan lease membership: %w", err)
	}

	return nil
}

// LeaseMembershipQuery는 VerifyLeaseMembership과 같은 판정 문장을 pipeline batch에 쌓을 수 있게 반환한다.
func LeaseMembershipQuery(proof *contract.LeaseProof, scope MembershipScope) (string, []any) {
	return sqlLeaseMembership, []any{
		proof.JobKey, proof.OwnerInstance, proof.FenceEpoch, proof.ProjectionGeneration, proof.ScheduledFor,
		scope.Kinds, scope.ExactSubject,
	}
}

// ScanLeaseMembership은 LeaseMembershipQuery 결과 한 행을 lease 결과 sentinel로 판정한다.
func ScanLeaseMembership(row pgx.Row) error {
	var projectionCurrent, membershipValid bool

	err := row.Scan(&projectionCurrent, &membershipValid)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrFenceLost
	}

	if err != nil {
		return fmt.Errorf("verify collection lease membership: %w", err)
	}

	if !projectionCurrent {
		return ErrProjectionStale
	}

	if !membershipValid {
		return fmt.Errorf("verify collection lease membership: job membership changed: %w", ErrTargetDisabled)
	}

	return nil
}
