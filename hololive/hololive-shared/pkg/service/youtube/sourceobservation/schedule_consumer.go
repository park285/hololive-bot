package sourceobservation

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/reconcile/schedule"
)

func (c *Consumer) reconcileSchedule(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
) (schedule.Decision, ReconcileResult, error) {
	evidence, err := scheduleEvidenceFromObservation(claimed)
	if err != nil {
		return schedule.Decision{}, ReconcileResult{}, err
	}
	if err := lockScheduleSubject(ctx, tx, evidence.GroupKey); err != nil {
		return schedule.Decision{}, ReconcileResult{}, err
	}
	state, err := loadScheduleState(ctx, tx, evidence.GroupKey, evidence.Items)
	if err != nil {
		return schedule.Decision{}, ReconcileResult{}, err
	}
	decision, err := schedule.Reduce(state, evidence)
	if err != nil {
		return schedule.Decision{}, ReconcileResult{}, err
	}
	if err := persistScheduleDecision(ctx, tx, claimed, &decision); err != nil {
		return schedule.Decision{}, ReconcileResult{}, err
	}
	return decision, ReconcileResult{Applications: mapScheduleApplications(decision.Applications)}, nil
}

func scheduleEvidenceFromObservation(observation *Observation) (schedule.Evidence, error) {
	var payload contract.ScheduleSnapshotV1
	if err := jsonv2.Unmarshal(observation.Payload, &payload); err != nil {
		return schedule.Evidence{}, fmt.Errorf("decode schedule payload: %w", err)
	}
	items := make([]schedule.Item, 0, len(payload.Items))
	for i := range payload.Items {
		items = append(items, schedule.Item{
			GroupKey:           payload.GroupKey,
			Provider:           observation.Provider,
			ExternalID:         payload.Items[i].ExternalID,
			VideoID:            payload.Items[i].VideoID,
			ChannelID:          payload.Items[i].ChannelID,
			Title:              payload.Items[i].Title,
			ScheduledAt:        payload.Items[i].ScheduledAt,
			EndedAt:            payload.Items[i].EndedAt,
			IsLive:             payload.Items[i].IsLive,
			CollaboTalentNames: persistedCollaboTalentNames(payload.Items[i].CollaboTalentNames),
		})
	}
	return schedule.Evidence{
		ObservationID: observation.ID,
		Provider:      observation.Provider,
		GroupKey:      payload.GroupKey,
		Items:         items,
		EffectiveAt:   observation.EffectiveAt,
		ReceivedAt:    observation.ReceivedAt,
	}, nil
}

func mapScheduleApplications(items []schedule.Application) []Application {
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
