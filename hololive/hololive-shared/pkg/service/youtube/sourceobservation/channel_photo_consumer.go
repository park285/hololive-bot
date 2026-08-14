package sourceobservation

import (
	"context"
	"encoding/json"
	"fmt"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/reconcile/photo"
)

func (c *Consumer) reconcilePhoto(
	ctx context.Context,
	tx dbx.Tx,
	claimed Observation,
) (photo.Decision, ReconcileResult, error) {
	evidence, err := photoEvidenceFromObservation(claimed)
	if err != nil {
		return photo.Decision{}, ReconcileResult{}, err
	}
	if err := lockLiveSubject(ctx, tx, "photo:"+evidence.Sample.ChannelID); err != nil {
		return photo.Decision{}, ReconcileResult{}, err
	}
	state, err := loadPhotoState(ctx, tx, evidence.Sample.ChannelID)
	if err != nil {
		return photo.Decision{}, ReconcileResult{}, err
	}
	decision, err := photo.Reduce(state, evidence, photo.Policy{
		ChangeMinObservations: c.channel.PhotoChangeMinObservations,
		ChangeStability:       c.channel.PhotoChangeStability,
	})
	if err != nil {
		return photo.Decision{}, ReconcileResult{}, err
	}
	if err := persistPhotoDecision(ctx, tx, claimed, decision); err != nil {
		return photo.Decision{}, ReconcileResult{}, err
	}
	return decision, ReconcileResult{Applications: mapPhotoApplications(decision.Applications)}, nil
}

func photoEvidenceFromObservation(observation Observation) (photo.Evidence, error) {
	var payload contract.ChannelPhotoV1
	if err := json.Unmarshal(observation.Payload, &payload); err != nil {
		return photo.Evidence{}, fmt.Errorf("decode channel photo payload: %w", err)
	}
	variants := make([]photo.Variant, 0, len(payload.Variants))
	for i := range payload.Variants {
		variants = append(variants, photo.Variant{
			Kind:               payload.Variants[i].Kind,
			URL:                payload.Variants[i].URL,
			Width:              payload.Variants[i].Width,
			Height:             payload.Variants[i].Height,
			StableMediaID:      payload.Variants[i].StableMediaID,
			ContentFingerprint: payload.Variants[i].ContentFingerprint,
		})
	}
	return photo.Evidence{
		ObservationID: observation.ID,
		Provider:      observation.Provider,
		Sample: photo.Sample{
			ChannelID:     payload.ChannelID,
			Provider:      observation.Provider,
			Variants:      variants,
			Complete:      observation.Completeness == contract.CompletenessComplete,
			ObservationID: observation.ID,
			ScheduledFor:  observation.ScheduledFor,
			EffectiveAt:   observation.EffectiveAt,
			ReceivedAt:    observation.ReceivedAt,
		},
	}, nil
}

func mapPhotoApplications(items []photo.Application) []Application {
	applications := make([]Application, len(items))
	for i := range items {
		applications[i] = Application{
			EntityKind: items[i].EntityKind,
			EntityKey:  items[i].EntityKey,
			Decision:   items[i].Decision,
		}
	}
	return applications
}
