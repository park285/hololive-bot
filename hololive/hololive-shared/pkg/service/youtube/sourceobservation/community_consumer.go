package sourceobservation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/community"
)

type CanonicalWriter interface {
	PersistTx(context.Context, dbx.Tx, community.Batch) error
	AfterCommit(context.Context, community.Batch)
}

type Consumer struct {
	repo     *Repository
	writer   CanonicalWriter
	keywords []string
}

func NewConsumer(repo *Repository, writer CanonicalWriter, keywords []string) *Consumer {
	return &Consumer{repo: repo, writer: writer, keywords: community.NormalizeKeywords(keywords)}
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
		if err := c.consumeOne(ctx, batch.Observations[i], batch.ConsumerName); err != nil {
			consumeErrors = append(consumeErrors, err)
		}
	}
	return errors.Join(consumeErrors...)
}

func (c *Consumer) consumeOne(
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
	result, err := c.repo.Finalize(ctx, claim, func(
		ctx context.Context,
		tx dbx.Tx,
		claimed Observation,
	) (ReconcileResult, error) {
		if claimed.ObservationKind != contract.KindCommunityPage {
			return ReconcileResult{}, fmt.Errorf("community consumer received kind %q", claimed.ObservationKind)
		}
		payload, err := decodeCommunityPayload(claimed)
		if err != nil {
			return ReconcileResult{}, err
		}
		watermark, initialized, err := loadCommunityWatermark(ctx, tx, payload.ChannelID)
		if err != nil {
			return ReconcileResult{}, err
		}
		persisted = community.ArtifactsFromPayload(
			payload,
			initialized,
			watermark,
			claimed.EffectiveAt,
			c.keywords,
		)
		if err := c.writer.PersistTx(ctx, tx, persisted); err != nil {
			return ReconcileResult{}, err
		}
		applications := make([]Application, 0, len(persisted.Posts))
		for i := range persisted.Posts {
			applications = append(applications, Application{
				EntityKind: "community_post",
				EntityKey:  persisted.Posts[i].PostID,
				Decision:   "APPLIED",
			})
		}
		return ReconcileResult{Applications: applications}, nil
	})
	if err != nil {
		return err
	}
	if !result.Unsupported && persisted.Posts != nil {
		c.writer.AfterCommit(ctx, persisted)
	}
	return nil
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
