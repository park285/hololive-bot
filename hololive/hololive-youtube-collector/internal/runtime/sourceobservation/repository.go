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
	faultAfterContractCheck  publishFaultPoint = "after_contract_check"
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
	return NewRepositoryWithContracts(pool, collection.InitialJobContracts())
}

// NewRepositoryWithContracts는 과거 계약 집합 주입을 유지한다. 발행 fence 검증은 항상 SQL 검증기를 쓴다.
func NewRepositoryWithContracts(pool *pgxpool.Pool, jobContracts collection.JobContractSet) *Repository {
	return &Repository{pool: pool, fenceVerifier: sqlPublishFenceVerifier{jobs: jobContracts}}
}

func (r *Repository) validate() error {
	if r == nil || r.pool == nil || r.fenceVerifier.jobs == nil {
		return fmt.Errorf("validate source observation repository: %w", ErrInvalidRepository)
	}

	return nil
}
