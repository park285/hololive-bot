package sourceobservation

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type observationClaimFinalizer interface {
	ClaimBatch(context.Context, ClaimOptions) (ClaimedBatch, error)
	Finalize(context.Context, Claim, ReconcileWrite) (ReconcileResult, error)
}

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
		return fmt.Errorf("validate source observation repository: %w", ErrInvalidRepository)
	}

	return nil
}

var _ observationClaimFinalizer = (*Repository)(nil)
