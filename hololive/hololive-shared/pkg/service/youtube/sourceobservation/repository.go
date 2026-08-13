package sourceobservation

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) LoadAuthority(ctx context.Context, sourceKind contract.SourceKind) (AuthorityFence, error) {
	if r == nil || r.pool == nil {
		return AuthorityFence{}, ErrInvalidRepository
	}
	if err := validateSourceKind(sourceKind); err != nil {
		return AuthorityFence{}, fmt.Errorf("load source observation authority: %w", err)
	}
	return loadAuthority(ctx, r.pool, sourceKind, false)
}
