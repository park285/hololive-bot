package consume

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// observationClaimFinalizer는 Consumer가 claim과 finalize에 쓰는 저장소 경계다.
type observationClaimFinalizer interface {
	ClaimBatch(context.Context, ClaimOptions) (ClaimedBatch, error)
	Finalize(context.Context, Claim, ReconcileWrite) (ReconcileResult, error)
}

// Repository는 API YouTube plane이 수집 관측을 claim·finalize·retry·replay·retention 처리하는 저장소다.
// 관측 발행은 sourceobservation.Repository가 맡으며, 두 저장소는 같은 테이블을 각자의 DB role 권한으로 다룬다.
type Repository struct {
	pool      *pgxpool.Pool
	supported SupportedContractSet
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return NewRepositoryWithContracts(pool, InitialSupportedContracts())
}

func NewRepositoryWithContracts(pool *pgxpool.Pool, supported SupportedContractSet) *Repository {
	return &Repository{pool: pool, supported: supported}
}

func (r *Repository) validate() error {
	if r == nil || r.pool == nil || r.supported == nil {
		return fmt.Errorf("validate source observation consume repository: %w", ErrInvalidRepository)
	}

	return nil
}

var _ observationClaimFinalizer = (*Repository)(nil)
