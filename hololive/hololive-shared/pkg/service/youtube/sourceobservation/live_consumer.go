package sourceobservation

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/reconcile/live"
)

func (c *Consumer) reconcileLive(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
) (ReconcileResult, error) {
	evidence, err := liveEvidenceFromObservation(claimed)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("live evidence from observation: %w", err)
	}

	if lockErr := lockLiveSubject(ctx, tx, claimed.SubjectKey); lockErr != nil {
		return ReconcileResult{}, fmt.Errorf("lock live subject: %w", lockErr)
	}

	state, err := loadLiveState(ctx, tx, evidence.Coverage.RequestedChannelIDs, videoIDsOf(&evidence))
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("load live state: %w", err)
	}

	decision, err := live.Reduce(state, evidence, c.liveGrace, claimed.ReceivedAt)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("reduce: %w", err)
	}

	if persistErr := persistLiveDecision(ctx, tx, &decision); persistErr != nil {
		return ReconcileResult{}, fmt.Errorf("persist live decision: %w", persistErr)
	}

	return ReconcileResult{Applications: mapLiveApplications(decision.Applications)}, nil
}

func liveEvidenceFromObservation(observation *Observation) (live.Evidence, error) {
	var payload contract.LiveSnapshotV1

	if err := jsonv2.Unmarshal(observation.Payload, &payload); err != nil {
		return live.Evidence{}, fmt.Errorf("decode live snapshot payload: %w", err)
	}

	facts := make([]live.SessionFact, 0, len(payload.Sessions))
	for i := range payload.Sessions {
		facts = append(facts, live.SessionFact{
			VideoID:      payload.Sessions[i].VideoID,
			ChannelID:    payload.Sessions[i].ChannelID,
			Status:       payload.Sessions[i].Status,
			Title:        payload.Sessions[i].Title,
			TopicID:      payload.Sessions[i].TopicID,
			ThumbnailURL: payload.Sessions[i].ThumbnailURL,
			ScheduledAt:  payload.Sessions[i].ScheduledAt,
			StartedAt:    payload.Sessions[i].StartedAt,
			EndedAt:      payload.Sessions[i].EndedAt,
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
