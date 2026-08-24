package sourceobservation

import (
	"context"
	"fmt"
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

func (r *PublishRepository) PublishBatch(ctx context.Context, input *PublishBatchInput) (PublishBatchResult, error) {
	if r == nil || r.inner == nil {
		return PublishBatchResult{}, ErrInvalidRepository
	}

	out, err := r.inner.PublishBatch(ctx, input)
	if err != nil {
		return out, fmt.Errorf("publish batch: %w", err)
	}

	return out, nil
}

func (r *PublishRepository) PublishBatchAndDefer(
	ctx context.Context,
	input *PublishBatchInput,
	deferInput DeferCollectionInput,
) (PublishBatchResult, error) {
	if r == nil || r.inner == nil {
		return PublishBatchResult{}, ErrInvalidRepository
	}

	out, err := r.inner.PublishBatchAndDefer(ctx, input, deferInput)
	if err != nil {
		return out, fmt.Errorf("publish batch and defer: %w", err)
	}

	return out, nil
}

type ConsumeRepository struct {
	inner *Repository
}

func NewConsumeRepository(pool *pgxpool.Pool) *ConsumeRepository {
	return &ConsumeRepository{inner: NewRepository(pool)}
}

func (r *ConsumeRepository) store() (*Repository, error) {
	if r == nil || r.inner == nil {
		return nil, ErrInvalidRepository
	}

	return r.inner, nil
}

func (r *ConsumeRepository) ClaimBatch(ctx context.Context, options ClaimOptions) (ClaimedBatch, error) {
	store, err := r.store()
	if err != nil {
		return ClaimedBatch{}, fmt.Errorf("store: %w", err)
	}

	out, err := store.ClaimBatch(ctx, options)
	if err != nil {
		return out, fmt.Errorf("claim batch: %w", err)
	}

	return out, nil
}

func (r *ConsumeRepository) ProbeClaim(ctx context.Context, options ClaimOptions) error {
	store, err := r.store()
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}

	if err := store.ProbeClaim(ctx, options); err != nil {
		return fmt.Errorf("probe claim: %w", err)
	}

	return nil
}

func (r *ConsumeRepository) EnsureClaimBudget(ctx context.Context, claim Claim, timeout time.Duration) error {
	store, err := r.store()
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}

	if err := store.EnsureClaimBudget(ctx, claim, timeout); err != nil {
		return fmt.Errorf("ensure claim budget: %w", err)
	}

	return nil
}

func (r *ConsumeRepository) Retry(ctx context.Context, input RetryInput) (contract.Status, error) {
	store, err := r.store()
	if err != nil {
		return "", fmt.Errorf("store: %w", err)
	}

	out, err := store.Retry(ctx, input)
	if err != nil {
		return out, fmt.Errorf("retry: %w", err)
	}

	return out, nil
}

func (r *ConsumeRepository) DeadLetter(ctx context.Context, input DeadLetterInput) error {
	store, err := r.store()
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}

	if err := store.DeadLetter(ctx, input); err != nil {
		return fmt.Errorf("dead letter: %w", err)
	}

	return nil
}

func (r *ConsumeRepository) Finalize(ctx context.Context, claim Claim, reconcile ReconcileWrite) (ReconcileResult, error) {
	store, err := r.store()
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("store: %w", err)
	}

	out, err := store.Finalize(ctx, claim, reconcile)
	if err != nil {
		return out, fmt.Errorf("finalize: %w", err)
	}

	return out, nil
}

func (r *ConsumeRepository) ProcessNextReplay(ctx context.Context) (bool, error) {
	store, err := r.store()
	if err != nil {
		return false, fmt.Errorf("store: %w", err)
	}

	out, err := store.ProcessNextReplay(ctx)
	if err != nil {
		return out, fmt.Errorf("process next replay: %w", err)
	}

	return out, nil
}

func (r *ConsumeRepository) RequestReplay(ctx context.Context, input ReplayInput) (ReplayResult, error) {
	store, err := r.store()
	if err != nil {
		return ReplayResult{}, fmt.Errorf("store: %w", err)
	}

	out, err := store.RequestReplay(ctx, input)
	if err != nil {
		return out, fmt.Errorf("request replay: %w", err)
	}

	return out, nil
}

func (r *ConsumeRepository) RunRetentionTick(ctx context.Context, cfg RetentionConfig, now time.Time) (RetentionResult, error) {
	store, err := r.store()
	if err != nil {
		return RetentionResult{}, fmt.Errorf("store: %w", err)
	}

	out, err := store.RunRetentionTick(ctx, cfg, now)
	if err != nil {
		return out, fmt.Errorf("run retention tick: %w", err)
	}

	return out, nil
}

func (r *ConsumeRepository) FinalizeNextDueLiveEnd(ctx context.Context, grace time.Duration) (bool, error) {
	store, err := r.store()
	if err != nil {
		return false, fmt.Errorf("store: %w", err)
	}

	out, err := store.FinalizeNextDueLiveEnd(ctx, grace)
	if err != nil {
		return out, fmt.Errorf("finalize next due live end: %w", err)
	}

	return out, nil
}

var (
	_ observationClaimFinalizer = (*Repository)(nil)
	_ observationClaimFinalizer = (*ConsumeRepository)(nil)
)
