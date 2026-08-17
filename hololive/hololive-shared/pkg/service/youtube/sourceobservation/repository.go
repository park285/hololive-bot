package sourceobservation

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

type PublishFenceVerifier interface {
	Verify(ctx context.Context, tx dbx.Tx, proof *contract.LeaseProof, observations []contract.Envelope) error
}

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
	supported            SupportedContractSet
	jobContracts         JobContractSet
	fenceVerifier        PublishFenceVerifier
	publishFault         func(ctx context.Context, tx dbx.Tx, point publishFaultPoint) error
	rewritePublishResult func(PublishBatchResult) PublishBatchResult
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return NewRepositoryWithContracts(pool, InitialSupportedContracts(), InitialJobContracts(), nil)
}

func NewRepositoryWithContracts(
	pool *pgxpool.Pool,
	supported SupportedContractSet,
	jobContracts JobContractSet,
	fenceVerifier PublishFenceVerifier,
) *Repository {
	repository := &Repository{
		pool: pool, supported: supported, jobContracts: jobContracts, fenceVerifier: fenceVerifier,
	}
	if repository.fenceVerifier == nil {
		repository.fenceVerifier = sqlPublishFenceVerifier{jobs: jobContracts}
	}
	return repository
}

func (r *Repository) validate() error {
	if r == nil || r.pool == nil || r.supported == nil || r.jobContracts == nil || r.fenceVerifier == nil {
		return fmt.Errorf("validate source observation repository: %w", ErrInvalidRepository)
	}
	return nil
}
