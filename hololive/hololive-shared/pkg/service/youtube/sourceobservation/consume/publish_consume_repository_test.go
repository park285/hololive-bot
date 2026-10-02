package consume

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
)

// testRepository는 발행 저장소와 소비 저장소를 같은 DB에 함께 연결한 테스트 전용 조합이다. 운영에서는 collector가
// 발행하고 API가 소비하지만, 통합 테스트는 실제 발행 경로로 넣은 관측을 소비 경로로 처리해 두 계약을 함께 검증한다.
type testRepository struct {
	*Repository

	publisher *sourceobservation.Repository
}

func newTestRepository(pool *pgxpool.Pool) *testRepository {
	return &testRepository{Repository: NewRepository(pool), publisher: sourceobservation.NewRepository(pool)}
}

func newTestRepositoryWithContracts(
	pool *pgxpool.Pool,
	supported SupportedContractSet,
	jobContracts sourceobservation.JobContractSet,
) *testRepository {
	return &testRepository{
		Repository: NewRepositoryWithContracts(pool, supported),
		publisher:  sourceobservation.NewRepositoryWithContracts(pool, jobContracts, nil),
	}
}

func (r *testRepository) PublishBatch(
	ctx context.Context,
	input *sourceobservation.PublishBatchInput,
) (sourceobservation.PublishBatchResult, error) {
	out, err := r.publisher.PublishBatch(ctx, input)
	if err != nil {
		return out, fmt.Errorf("publish batch: %w", err)
	}

	return out, nil
}

func (r *testRepository) PublishBatchAndDefer(
	ctx context.Context,
	input *sourceobservation.PublishBatchInput,
	deferInput sourceobservation.DeferCollectionInput,
) (sourceobservation.PublishBatchResult, error) {
	out, err := r.publisher.PublishBatchAndDefer(ctx, input, deferInput)
	if err != nil {
		return out, fmt.Errorf("publish batch and defer: %w", err)
	}

	return out, nil
}
