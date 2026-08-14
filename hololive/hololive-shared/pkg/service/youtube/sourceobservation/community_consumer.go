package sourceobservation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/community"
	"github.com/kapu/hololive-shared/pkg/service/youtube/reconcile/content"
)

type CanonicalWriter interface {
	PersistTx(context.Context, dbx.Tx, community.Batch) error
	AfterCommit(context.Context, community.Batch)
	PersistVideosTx(context.Context, dbx.Tx, []*domain.YouTubeVideo, []*domain.YouTubeNotificationOutbox, []*domain.YouTubeContentAlarmTracking, *domain.YouTubeContentWatermark) error
	AfterCommitVideos(context.Context, []*domain.YouTubeContentAlarmTracking)
}

type Consumer struct {
	repo     *Repository
	writer   CanonicalWriter
	keywords []string
	grace    time.Duration
}

func NewConsumer(repo *Repository, writer CanonicalWriter, keywords []string) *Consumer {
	return NewConsumerWithAbsenceGrace(repo, writer, keywords, 0)
}

func NewConsumerWithAbsenceGrace(
	repo *Repository,
	writer CanonicalWriter,
	keywords []string,
	grace time.Duration,
) *Consumer {
	return &Consumer{repo: repo, writer: writer, keywords: community.NormalizeKeywords(keywords), grace: grace}
}

func (c *Consumer) Consume(ctx context.Context, options ClaimOptions) error {
	if c == nil || c.repo == nil {
		return ErrInvalidRepository
	}
	batch, err := c.repo.ClaimBatch(ctx, options)
	if err != nil {
		return err
	}
	var consumeErrors []error
	for i := range batch.Observations {
		if err := c.ConsumeObservation(ctx, batch.Observations[i], batch.ConsumerName); err != nil {
			consumeErrors = append(consumeErrors, err)
		}
	}
	return errors.Join(consumeErrors...)
}

func (c *Consumer) ConsumeObservation(
	ctx context.Context,
	observation Observation,
	consumerName string,
) error {
	if c.writer == nil {
		return fmt.Errorf("consume source observation: canonical writer is not configured")
	}
	claim := Claim{
		ConsumerName:  consumerName,
		ObservationID: observation.ID,
		LeaseToken:    observation.LeaseToken,
	}
	var persisted community.Batch
	var contentDecision content.Decision
	var appliedKind contract.ObservationKind
	canonicalApplied := false
	result, err := c.repo.Finalize(ctx, claim, func(
		ctx context.Context,
		tx dbx.Tx,
		claimed Observation,
	) (ReconcileResult, error) {
		return c.finalizeObservation(ctx, tx, claimed, &persisted, &contentDecision, &appliedKind, &canonicalApplied)
	})
	if err != nil {
		return err
	}
	if result.Unsupported || !canonicalApplied {
		return nil
	}
	if appliedKind == contract.KindCommunityPage {
		c.writer.AfterCommit(ctx, persisted)
		return nil
	}
	_, _, tracking := contentArtifacts(Observation{}, contentDecision)
	c.writer.AfterCommitVideos(ctx, tracking)
	return nil
}

func (c *Consumer) finalizeObservation(
	ctx context.Context,
	tx dbx.Tx,
	claimed Observation,
	persisted *community.Batch,
	contentDecision *content.Decision,
	appliedKind *contract.ObservationKind,
	canonicalApplied *bool,
) (ReconcileResult, error) {
	switch claimed.ObservationKind {
	case contract.KindCommunityPage:
		return c.finalizeCommunity(ctx, tx, claimed, persisted, appliedKind, canonicalApplied)
	case contract.KindVideoList, contract.KindShortsList:
		return c.finalizeContent(ctx, tx, claimed, contentDecision, appliedKind, canonicalApplied)
	default:
		return ReconcileResult{}, fmt.Errorf("youtube consumer received kind %q", claimed.ObservationKind)
	}
}

func (c *Consumer) finalizeCommunity(
	ctx context.Context,
	tx dbx.Tx,
	claimed Observation,
	persisted *community.Batch,
	appliedKind *contract.ObservationKind,
	canonicalApplied *bool,
) (ReconcileResult, error) {
	batch, rec, applied, err := c.reconcileCommunity(ctx, tx, claimed)
	if err != nil {
		return ReconcileResult{}, err
	}
	*persisted = batch
	*appliedKind = claimed.ObservationKind
	*canonicalApplied = applied
	return rec, nil
}

func (c *Consumer) finalizeContent(
	ctx context.Context,
	tx dbx.Tx,
	claimed Observation,
	contentDecision *content.Decision,
	appliedKind *contract.ObservationKind,
	canonicalApplied *bool,
) (ReconcileResult, error) {
	decision, rec, err := c.reconcileContent(ctx, tx, claimed)
	if err != nil {
		return ReconcileResult{}, err
	}
	*contentDecision = decision
	*appliedKind = claimed.ObservationKind
	*canonicalApplied = true
	return rec, nil
}

func decodeCommunityPayload(observation Observation) (contract.CommunityPayloadV1, error) {
	var payload contract.CommunityPayloadV1
	if err := json.Unmarshal(observation.Payload, &payload); err != nil {
		return contract.CommunityPayloadV1{}, fmt.Errorf("decode community payload: %w", err)
	}
	if err := payload.Validate(observation.SubjectKey); err != nil {
		return contract.CommunityPayloadV1{}, err
	}
	return payload, nil
}
