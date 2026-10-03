// Package sourceobservation은 peer 모듈의 DB 시험에서 실제 API consume·canonical 저장을 연결합니다.
package sourceobservation

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	store "github.com/kapu/hololive-api/internal/youtube/sourceobservation"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"
)

type ClaimOptions struct {
	ConsumerName  string
	LeaseOwner    string
	Kinds         []contract.ObservationKind
	Limit         int
	LeaseDuration time.Duration
}

type Consumer struct {
	consumer *store.Consumer
}

func NewConsumer(pool *pgxpool.Pool, liveEndGrace, absenceGrace time.Duration) *Consumer {
	repository := store.NewRepository(pool)
	writer := store.NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil))

	return &Consumer{consumer: store.NewConsumerWithGraces(repository, writer, nil, absenceGrace, liveEndGrace)}
}

func (c *Consumer) Consume(ctx context.Context, options ClaimOptions) error {
	if err := c.consumer.Consume(ctx, store.ClaimOptions(options)); err != nil {
		return fmt.Errorf("consume fixture observations: %w", err)
	}

	return nil
}
