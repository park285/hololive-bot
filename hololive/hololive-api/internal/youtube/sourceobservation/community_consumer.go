package sourceobservation

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

type ChannelPolicy struct {
	ProfileClearMinObservations int
	ProfileClearStability       time.Duration
	PhotoChangeMinObservations  int
	PhotoChangeStability        time.Duration
}

type Consumer struct {
	repo      observationClaimFinalizer
	writer    canonicalWriter
	grace     time.Duration
	liveGrace time.Duration
	channel   ChannelPolicy
}

func NewConsumer(repo observationClaimFinalizer) *Consumer {
	return NewConsumerWithAbsenceGrace(repo, 0)
}

func NewConsumerWithAbsenceGrace(repo observationClaimFinalizer, grace time.Duration) *Consumer {
	return NewConsumerWithGraces(repo, grace, 0)
}

func NewConsumerWithGraces(repo observationClaimFinalizer, grace, liveGrace time.Duration) *Consumer {
	return &Consumer{
		repo:      repo,
		writer:    canonicalTxWriter{},
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
		return fmt.Errorf("claim batch: %w", err)
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
	if _, err := c.repo.Finalize(ctx, claim, c.finalizeObservation); err != nil {
		return fmt.Errorf("finalize: %w", err)
	}

	return nil
}

func (c *Consumer) finalizeObservation(ctx context.Context, tx dbx.Tx, claimed *Observation) (ReconcileResult, error) {
	if claimed.ObservationKind == contract.KindCommunityPage {
		out, err := c.reconcileCommunity(ctx, tx, claimed)
		if err != nil {
			return ReconcileResult{}, fmt.Errorf("finalize community: %w", err)
		}

		return out, nil
	}

	if claimed.ObservationKind == contract.KindVideoList || claimed.ObservationKind == contract.KindShortsList {
		out, err := c.reconcileContent(ctx, tx, claimed)
		if err != nil {
			return ReconcileResult{}, fmt.Errorf("finalize content: %w", err)
		}

		return out, nil
	}

	out, err := c.finalizeDomainObservation(ctx, tx, claimed)
	if err != nil {
		return out, fmt.Errorf("finalize domain observation: %w", err)
	}

	return out, nil
}

func (c *Consumer) finalizeDomainObservation(ctx context.Context, tx dbx.Tx, claimed *Observation) (ReconcileResult, error) {
	reconcile, ok := c.domainReconcile(claimed.ObservationKind)
	if !ok {
		return ReconcileResult{}, fmt.Errorf("youtube consumer received kind %q", claimed.ObservationKind)
	}

	out, err := reconcile(ctx, tx, claimed)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("finalize kind: %w", err)
	}

	return out, nil
}

func (c *Consumer) domainReconcile(kind contract.ObservationKind) (func(context.Context, dbx.Tx, *Observation) (ReconcileResult, error), bool) {
	if fn, ok := c.liveReconcile(kind); ok {
		return fn, true
	}

	if fn, ok := c.liveCheckReconcile(kind); ok {
		return fn, true
	}

	return c.channelReconcile(kind)
}

func (c *Consumer) liveReconcile(kind contract.ObservationKind) (func(context.Context, dbx.Tx, *Observation) (ReconcileResult, error), bool) {
	if kind == contract.KindLiveSnapshot {
		return c.reconcileLive, true
	}

	if kind == contract.KindViewerSample {
		return c.reconcileViewer, true
	}

	if kind == contract.KindSchedule {
		return c.reconcileSchedule, true
	}

	return nil, false
}

func (c *Consumer) channelReconcile(kind contract.ObservationKind) (func(context.Context, dbx.Tx, *Observation) (ReconcileResult, error), bool) {
	if kind == contract.KindChannelProfile {
		return c.reconcileProfile, true
	}

	if kind == contract.KindChannelPhoto {
		return c.reconcilePhoto, true
	}

	return nil, false
}

func decodeCommunityPayload(observation *Observation) (contract.CommunityPayloadV1, error) {
	var payload contract.CommunityPayloadV1

	if err := jsonv2.Unmarshal(observation.Payload, &payload); err != nil {
		return contract.CommunityPayloadV1{}, fmt.Errorf("decode community payload: %w", err)
	}

	if err := payload.Validate(observation.SubjectKey); err != nil {
		return contract.CommunityPayloadV1{}, fmt.Errorf("validate: %w", err)
	}

	return payload, nil
}
