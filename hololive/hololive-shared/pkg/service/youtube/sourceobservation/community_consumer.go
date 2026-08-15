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
	PersistTx(context.Context, dbx.Tx, *community.Batch) error
	AfterCommit(context.Context, *community.Batch)
	PersistVideosTx(context.Context, dbx.Tx, []*domain.YouTubeVideo, []*domain.YouTubeNotificationOutbox, []*domain.YouTubeContentAlarmTracking, *domain.YouTubeContentWatermark) error
	AfterCommitVideos(context.Context, []*domain.YouTubeContentAlarmTracking)
}

type ChannelPolicy struct {
	ProfileClearMinObservations int
	ProfileClearStability       time.Duration
	PhotoChangeMinObservations  int
	PhotoChangeStability        time.Duration
}

type Consumer struct {
	repo      observationClaimFinalizer
	writer    CanonicalWriter
	keywords  []string
	grace     time.Duration
	liveGrace time.Duration
	channel   ChannelPolicy
}

func NewConsumer(repo observationClaimFinalizer, writer CanonicalWriter, keywords []string) *Consumer {
	return NewConsumerWithAbsenceGrace(repo, writer, keywords, 0)
}

func NewConsumerWithAbsenceGrace(
	repo observationClaimFinalizer,
	writer CanonicalWriter,
	keywords []string,
	grace time.Duration,
) *Consumer {
	return NewConsumerWithGraces(repo, writer, keywords, grace, 0)
}

func NewConsumerWithGraces(
	repo observationClaimFinalizer,
	writer CanonicalWriter,
	keywords []string,
	grace time.Duration,
	liveGrace time.Duration,
) *Consumer {
	return &Consumer{
		repo:      repo,
		writer:    writer,
		keywords:  community.NormalizeKeywords(keywords),
		grace:     grace,
		liveGrace: liveGrace,
	}
}

func (c *Consumer) WithChannelPolicy(policy ChannelPolicy) *Consumer {
	if c == nil {
		return nil
	}
	c.channel = policy
	return c
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
	for i := range batch.Claims {
		if err := c.ConsumeClaim(ctx, batch.Claims[i].Claim(batch.ConsumerName)); err != nil {
			consumeErrors = append(consumeErrors, err)
		}
	}
	return errors.Join(consumeErrors...)
}

func (c *Consumer) ConsumeClaim(ctx context.Context, claim Claim) error {
	if c.writer == nil {
		return fmt.Errorf("consume source observation: canonical writer is not configured")
	}
	var persisted community.Batch
	var contentDecision content.Decision
	var appliedKind contract.ObservationKind
	canonicalApplied := false
	result, err := c.repo.Finalize(ctx, claim, func(
		ctx context.Context,
		tx dbx.Tx,
		claimed *Observation,
	) (ReconcileResult, error) {
		return c.finalizeObservation(ctx, tx, claimed, &persisted, &contentDecision, &appliedKind, &canonicalApplied)
	})
	if err != nil {
		return err
	}
	if result.Unsupported || !canonicalApplied {
		return nil
	}
	c.afterCommit(ctx, appliedKind, &persisted, &contentDecision)
	return nil
}

func (c *Consumer) afterCommit(
	ctx context.Context,
	appliedKind contract.ObservationKind,
	persisted *community.Batch,
	contentDecision *content.Decision,
) {
	if appliedKind == contract.KindCommunityPage {
		c.writer.AfterCommit(ctx, persisted)
		return
	}
	if appliedKind == contract.KindVideoList || appliedKind == contract.KindShortsList {
		_, _, tracking := contentArtifacts(time.Time{}, contentDecision)
		c.writer.AfterCommitVideos(ctx, tracking)
	}
}

func (c *Consumer) finalizeObservation(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
	persisted *community.Batch,
	contentDecision *content.Decision,
	appliedKind *contract.ObservationKind,
	canonicalApplied *bool,
) (ReconcileResult, error) {
	if claimed.ObservationKind == contract.KindCommunityPage {
		return c.finalizeCommunity(ctx, tx, claimed, persisted, appliedKind, canonicalApplied)
	}
	if claimed.ObservationKind == contract.KindVideoList || claimed.ObservationKind == contract.KindShortsList {
		return c.finalizeContent(ctx, tx, claimed, contentDecision, appliedKind, canonicalApplied)
	}
	return c.finalizeDomainObservation(ctx, tx, claimed, appliedKind, canonicalApplied)
}

func (c *Consumer) finalizeDomainObservation(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
	appliedKind *contract.ObservationKind,
	canonicalApplied *bool,
) (ReconcileResult, error) {
	reconcile, ok := c.domainReconcile(claimed.ObservationKind)
	if !ok {
		return ReconcileResult{}, fmt.Errorf("youtube consumer received kind %q", claimed.ObservationKind)
	}
	return c.finalizeKind(ctx, tx, claimed, appliedKind, canonicalApplied, reconcile)
}

func (c *Consumer) domainReconcile(kind contract.ObservationKind) (func(context.Context, dbx.Tx, *Observation) (ReconcileResult, error), bool) {
	if fn, ok := c.liveReconcile(kind); ok {
		return fn, true
	}
	return c.channelReconcile(kind)
}

func (c *Consumer) liveReconcile(kind contract.ObservationKind) (func(context.Context, dbx.Tx, *Observation) (ReconcileResult, error), bool) {
	if kind == contract.KindLiveSnapshot {
		return func(ctx context.Context, tx dbx.Tx, claimed *Observation) (ReconcileResult, error) {
			_, rec, err := c.reconcileLive(ctx, tx, claimed)
			return rec, err
		}, true
	}
	if kind == contract.KindViewerSample {
		return func(ctx context.Context, tx dbx.Tx, claimed *Observation) (ReconcileResult, error) {
			_, rec, err := c.reconcileViewer(ctx, tx, claimed)
			return rec, err
		}, true
	}
	if kind == contract.KindSchedule {
		return func(ctx context.Context, tx dbx.Tx, claimed *Observation) (ReconcileResult, error) {
			_, rec, err := c.reconcileSchedule(ctx, tx, claimed)
			return rec, err
		}, true
	}
	return nil, false
}

func (c *Consumer) channelReconcile(kind contract.ObservationKind) (func(context.Context, dbx.Tx, *Observation) (ReconcileResult, error), bool) {
	if kind == contract.KindChannelStats {
		return func(ctx context.Context, tx dbx.Tx, claimed *Observation) (ReconcileResult, error) {
			_, rec, err := c.reconcileStats(ctx, tx, claimed)
			return rec, err
		}, true
	}
	if kind == contract.KindChannelProfile {
		return func(ctx context.Context, tx dbx.Tx, claimed *Observation) (ReconcileResult, error) {
			_, rec, err := c.reconcileProfile(ctx, tx, claimed)
			return rec, err
		}, true
	}
	if kind == contract.KindChannelPhoto {
		return func(ctx context.Context, tx dbx.Tx, claimed *Observation) (ReconcileResult, error) {
			_, rec, err := c.reconcilePhoto(ctx, tx, claimed)
			return rec, err
		}, true
	}
	return nil, false
}

func (c *Consumer) finalizeKind(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
	appliedKind *contract.ObservationKind,
	canonicalApplied *bool,
	reconcile func(context.Context, dbx.Tx, *Observation) (ReconcileResult, error),
) (ReconcileResult, error) {
	rec, err := reconcile(ctx, tx, claimed)
	if err != nil {
		return ReconcileResult{}, err
	}
	*appliedKind = claimed.ObservationKind
	*canonicalApplied = true
	return rec, nil
}

func (c *Consumer) finalizeCommunity(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
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
	claimed *Observation,
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

func decodeCommunityPayload(observation *Observation) (contract.CommunityPayloadV1, error) {
	var payload contract.CommunityPayloadV1
	if err := json.Unmarshal(observation.Payload, &payload); err != nil {
		return contract.CommunityPayloadV1{}, fmt.Errorf("decode community payload: %w", err)
	}
	if err := payload.Validate(observation.SubjectKey); err != nil {
		return contract.CommunityPayloadV1{}, err
	}
	return payload, nil
}
