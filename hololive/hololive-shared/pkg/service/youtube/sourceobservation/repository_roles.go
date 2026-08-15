package sourceobservation

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

type observationClaimFinalizer interface {
	ClaimBatch(context.Context, ClaimOptions) (ClaimedBatch, error)
	Finalize(context.Context, Claim, ReconcileWrite) (ReconcileResult, error)
}

type PublishRepository struct {
	inner *Repository
}

func NewPublishRepository(pool *pgxpool.Pool) *PublishRepository {
	return &PublishRepository{inner: NewRepository(pool)}
}

func (r *PublishRepository) PublishBatch(ctx context.Context, input PublishBatchInput) (PublishBatchResult, error) {
	if r == nil {
		return PublishBatchResult{}, ErrInvalidRepository
	}
	return r.inner.PublishBatch(ctx, input)
}

type ConsumeRepository struct {
	inner *Repository
}

func NewConsumeRepository(pool *pgxpool.Pool) *ConsumeRepository {
	return &ConsumeRepository{inner: NewRepository(pool)}
}

func (r *ConsumeRepository) store() *Repository {
	if r == nil {
		return nil
	}
	return r.inner
}

func (r *ConsumeRepository) ClaimBatch(ctx context.Context, options ClaimOptions) (ClaimedBatch, error) {
	return r.store().ClaimBatch(ctx, options)
}

func (r *ConsumeRepository) ProbeClaim(ctx context.Context, options ClaimOptions) error {
	return r.store().ProbeClaim(ctx, options)
}

func (r *ConsumeRepository) EnsureClaimBudget(ctx context.Context, claim Claim, timeout time.Duration) error {
	return r.store().EnsureClaimBudget(ctx, claim, timeout)
}

func (r *ConsumeRepository) Retry(ctx context.Context, input RetryInput) (contract.Status, error) {
	return r.store().Retry(ctx, input)
}

func (r *ConsumeRepository) DeadLetter(ctx context.Context, input DeadLetterInput) error {
	return r.store().DeadLetter(ctx, input)
}

func (r *ConsumeRepository) Finalize(ctx context.Context, claim Claim, reconcile ReconcileWrite) (ReconcileResult, error) {
	return r.store().Finalize(ctx, claim, reconcile)
}

func (r *ConsumeRepository) ProcessNextReplay(ctx context.Context) (bool, error) {
	return r.store().ProcessNextReplay(ctx)
}

func (r *ConsumeRepository) RequestReplay(ctx context.Context, input ReplayInput) (ReplayResult, error) {
	return r.store().RequestReplay(ctx, input)
}

func (r *ConsumeRepository) RunRetentionTick(ctx context.Context, cfg RetentionConfig, now time.Time) (RetentionResult, error) {
	return r.store().RunRetentionTick(ctx, cfg, now)
}

func (r *ConsumeRepository) FinalizeNextDueLiveEnd(ctx context.Context, grace time.Duration) (bool, error) {
	return r.store().FinalizeNextDueLiveEnd(ctx, grace)
}

var _ observationClaimFinalizer = (*Repository)(nil)
var _ observationClaimFinalizer = (*ConsumeRepository)(nil)
