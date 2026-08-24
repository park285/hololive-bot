package sourceobservation

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/reconcile/profile"
)

func (c *Consumer) reconcileProfile(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
) (ReconcileResult, error) {
	result, err := reconcileChannelSubject(ctx, tx, claimed, channelReconcileSteps[profile.Evidence, profile.State, profile.Decision]{
		name:      "profile",
		subject:   func(evidence profile.Evidence) string { return evidence.Sample.ChannelID },
		evidence:  profileEvidenceFromObservation,
		loadState: loadProfileState,
		reduce: func(state profile.State, evidence profile.Evidence) (profile.Decision, error) {
			return profile.Reduce(state, evidence, c.profilePolicy())
		},
		persist:      persistProfileDecision,
		applications: func(decision profile.Decision) []Application { return mapProfileApplications(decision.Applications) },
	})
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("reconcile profile: %w", err)
	}

	return result, nil
}

func (c *Consumer) profilePolicy() profile.Policy {
	return profile.Policy{
		ClearMinObservations: c.channel.ProfileClearMinObservations,
		ClearStability:       c.channel.ProfileClearStability,
	}
}

func profileEvidenceFromObservation(observation *Observation) (profile.Evidence, error) {
	var payload contract.ChannelProfileV1

	if err := jsonv2.Unmarshal(observation.Payload, &payload); err != nil {
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
