package sourceobservation

import (
	"context"
	"encoding/json"
	"fmt"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/reconcile/profile"
)

func (c *Consumer) reconcileProfile(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
) (profile.Decision, ReconcileResult, error) {
	evidence, err := profileEvidenceFromObservation(claimed)
	if err != nil {
		return profile.Decision{}, ReconcileResult{}, err
	}
	if err := lockLiveSubject(ctx, tx, "profile:"+evidence.Sample.ChannelID); err != nil {
		return profile.Decision{}, ReconcileResult{}, err
	}
	state, err := loadProfileState(ctx, tx, evidence.Sample.ChannelID)
	if err != nil {
		return profile.Decision{}, ReconcileResult{}, err
	}
	decision, err := profile.Reduce(state, evidence, profile.Policy{
		ClearMinObservations: c.channel.ProfileClearMinObservations,
		ClearStability:       c.channel.ProfileClearStability,
	})
	if err != nil {
		return profile.Decision{}, ReconcileResult{}, err
	}
	if err := persistProfileDecision(ctx, tx, claimed, &decision); err != nil {
		return profile.Decision{}, ReconcileResult{}, err
	}
	return decision, ReconcileResult{Applications: mapProfileApplications(decision.Applications)}, nil
}

func profileEvidenceFromObservation(observation *Observation) (profile.Evidence, error) {
	var payload contract.ChannelProfileV1
	if err := json.Unmarshal(observation.Payload, &payload); err != nil {
		return profile.Evidence{}, fmt.Errorf("decode channel profile payload: %w", err)
	}
	return profile.Evidence{
		ObservationID: observation.ID,
		Provider:      observation.Provider,
		Sample: profile.Sample{
			ChannelID: observation.SubjectKey,
			Provider:  observation.Provider,
			Handle:    profile.Field{Present: payload.Handle.Present, Value: payload.Handle.Value},
			Description: profile.Field{
				Present: payload.Description.Present, Value: payload.Description.Value,
			},
			Country:       profile.Field{Present: payload.Country.Present, Value: payload.Country.Value},
			JoinedDate:    profile.Field{Present: payload.JoinedDate.Present, Value: payload.JoinedDate.Value},
			Complete:      observation.Completeness == contract.CompletenessComplete,
			ObservationID: observation.ID,
			ScheduledFor:  observation.ScheduledFor,
			EffectiveAt:   observation.EffectiveAt,
			ReceivedAt:    observation.ReceivedAt,
		},
	}, nil
}

func mapProfileApplications(items []profile.Application) []Application {
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
