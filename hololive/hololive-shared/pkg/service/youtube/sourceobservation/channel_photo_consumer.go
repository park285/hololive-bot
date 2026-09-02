package sourceobservation

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"

	"github.com/kapu/hololive-shared/internal/service/youtube/reconcile/photo"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

func (c *Consumer) reconcilePhoto(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
) (ReconcileResult, error) {
	steps := channelReconcileSteps[photo.Evidence, photo.State, photo.Decision]{
		name:      "photo",
		subject:   func(evidence photo.Evidence) string { return evidence.Sample.ChannelID },
		evidence:  photoEvidenceFromObservation,
		loadState: loadPhotoState,
		reduce: func(state photo.State, evidence photo.Evidence) (photo.Decision, error) {
			return photo.Reduce(state, evidence, c.photoPolicy())
		},
		persist:      persistPhotoDecision,
		applications: func(decision photo.Decision) []Application { return mapPhotoApplications(decision.Applications) },
	}

	result, err := steps.reconcile(ctx, tx, claimed)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("reconcile photo: %w", err)
	}

	return result, nil
}

func (c *Consumer) photoPolicy() photo.Policy {
	return photo.Policy{
		ChangeMinObservations: c.channel.PhotoChangeMinObservations,
		ChangeStability:       c.channel.PhotoChangeStability,
	}
}

func photoEvidenceFromObservation(observation *Observation) (photo.Evidence, error) {
	var payload contract.ChannelPhotoV1

	if err := jsonv2.Unmarshal(observation.Payload, &payload); err != nil {
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
