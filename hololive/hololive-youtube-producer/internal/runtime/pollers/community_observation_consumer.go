package pollers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/community"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
)

const (
	communityObservationConsumerName = "youtube-community-processor"
	communityObservationInterval     = 2 * time.Second
	communityObservationClaimLimit   = 10
	communityObservationLease        = 30 * time.Second
)

type CommunityObservationConsumer struct {
	repo     *sourceobservation.Repository
	consumer *sourceobservation.Consumer
	claim    sourceobservation.ClaimOptions
	interval time.Duration
	logger   *slog.Logger
	keywords []string
}

func NewCommunityObservationConsumer(
	pool *pgxpool.Pool,
	keywords []string,
	leaseOwner string,
	logger *slog.Logger,
) *CommunityObservationConsumer {
	if pool == nil {
		return nil
	}
	owner := strings.TrimSpace(leaseOwner)
	if owner == "" {
		owner = "youtube-producer"
	}
	if logger == nil {
		logger = slog.Default()
	}
	repo := sourceobservation.NewRepository(pool)
	batchRepo := batchrepo.NewPgxBatchRepositoryWithPersister(
		pool,
		newDeliveryTelemetryLatencyPersisterAdapter(pool),
	)
	normalized := community.NormalizeKeywords(keywords)
	return &CommunityObservationConsumer{
		repo:     repo,
		consumer: sourceobservation.NewConsumer(repo, sourceobservation.NewBatchCanonicalWriter(batchRepo), normalized),
		claim: sourceobservation.ClaimOptions{
			ConsumerName:  communityObservationConsumerName,
			LeaseOwner:    owner,
			Kinds:         []contract.ObservationKind{contract.KindCommunityPage},
			Limit:         communityObservationClaimLimit,
			LeaseDuration: communityObservationLease,
		},
		interval: communityObservationInterval,
		logger:   logger,
		keywords: normalized,
	}
}

func (c *CommunityObservationConsumer) Start(ctx context.Context) {
	if c == nil {
		return
	}
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		if err := c.tick(ctx); err != nil {
			c.logger.Error("community observation consume failed", slog.Any("error", err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *CommunityObservationConsumer) tick(ctx context.Context) error {
	if c == nil || c.repo == nil || c.consumer == nil {
		return fmt.Errorf("consume community observation: consumer is not configured")
	}
	if err := c.consumer.Consume(ctx, c.claim); err != nil {
		return fmt.Errorf("consume community observation: %w", err)
	}
	return nil
}
