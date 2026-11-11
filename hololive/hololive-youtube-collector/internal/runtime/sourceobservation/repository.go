package sourceobservation

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
)

type publishFaultPoint string

const (
	faultAfterFenceVerify    publishFaultPoint = "after_fence_verify"
	faultAfterObservationSet publishFaultPoint = "after_observation_set"
	faultBeforeTerminal      publishFaultPoint = "before_terminal"
	faultBeforeCommit        publishFaultPoint = "before_commit"
)

type Repository struct {
	pool                 *pgxpool.Pool
	fenceVerifier        sqlPublishFenceVerifier
	publishFault         func(context.Context, dbx.Tx, publishFaultPoint) error
	rewritePublishResult func(PublishBatchResult) PublishBatchResult
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, fenceVerifier: sqlPublishFenceVerifier{jobs: collection.InitialJobContracts()}}
}

func (r *Repository) validate() error {
	if r == nil || r.pool == nil || r.fenceVerifier.jobs == nil {
		return fmt.Errorf("validate source observation repository: %w", ErrInvalidRepository)
	}

	return nil
}
