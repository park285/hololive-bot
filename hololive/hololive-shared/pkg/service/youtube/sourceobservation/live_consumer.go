package sourceobservation

import (
	"context"
	"encoding/json"
	"fmt"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/reconcile/live"
)

func (c *Consumer) reconcileLive(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
) (live.Decision, ReconcileResult, error) {
	evidence, err := liveEvidenceFromObservation(claimed)
	if err != nil {
		return live.Decision{}, ReconcileResult{}, err
	}
	if err := lockLiveSubject(ctx, tx, claimed.SubjectKey); err != nil {
		return live.Decision{}, ReconcileResult{}, err
	}
	state, err := loadLiveState(ctx, tx, evidence.Coverage.RequestedChannelIDs, videoIDsOf(&evidence))
	if err != nil {
		return live.Decision{}, ReconcileResult{}, err
	}
	decision, err := live.Reduce(state, evidence, c.liveGrace, claimed.ReceivedAt)
	if err != nil {
		return live.Decision{}, ReconcileResult{}, err
	}
	if err := persistLiveDecision(ctx, tx, &decision); err != nil {
		return live.Decision{}, ReconcileResult{}, err
	}
	return decision, ReconcileResult{Applications: mapLiveApplications(decision.Applications)}, nil
}

func liveEvidenceFromObservation(observation *Observation) (live.Evidence, error) {
	var payload contract.LiveSnapshotV1
	if err := json.Unmarshal(observation.Payload, &payload); err != nil {
		return live.Evidence{}, fmt.Errorf("decode live snapshot payload: %w", err)
	}
	facts := make([]live.SessionFact, 0, len(payload.Sessions))
	for i := range payload.Sessions {
		facts = append(facts, live.SessionFact{
			VideoID:     payload.Sessions[i].VideoID,
			ChannelID:   payload.Sessions[i].ChannelID,
			Status:      payload.Sessions[i].Status,
			ScheduledAt: payload.Sessions[i].ScheduledAt,
			StartedAt:   payload.Sessions[i].StartedAt,
			EndedAt:     payload.Sessions[i].EndedAt,
		})
	}
	return live.Evidence{
		Kind:           observation.ObservationKind,
		ObservationID:  observation.ID,
		ObservationKey: observation.ObservationKey,
		EvidenceSHA256: observation.EvidenceSHA256,
		ScopeSHA256:    observation.ScopeSHA256,
		ScheduledFor:   observation.ScheduledFor,
		EffectiveAt:    observation.EffectiveAt,
		ReceivedAt:     observation.ReceivedAt,
		Completeness:   observation.Completeness,
		Continuity:     observation.Continuity,
		Sessions:       facts,
		Coverage:       payload.Coverage,
	}, nil
}

func videoIDsOf(evidence *live.Evidence) []string {
	ids := make([]string, 0, len(evidence.Sessions))
	for i := range evidence.Sessions {
		ids = append(ids, evidence.Sessions[i].VideoID)
	}
	return ids
}

func mapLiveApplications(items []live.Application) []Application {
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
