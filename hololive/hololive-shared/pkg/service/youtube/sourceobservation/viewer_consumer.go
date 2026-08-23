package sourceobservation

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/reconcile/viewer"
)

func (c *Consumer) reconcileViewer(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
) (viewer.Decision, ReconcileResult, error) {
	evidence, err := viewerEvidenceFromObservation(claimed)
	if err != nil {
		return viewer.Decision{}, ReconcileResult{}, err
	}
	if err := lockViewerSubject(ctx, tx, evidence.Sample.VideoID); err != nil {
		return viewer.Decision{}, ReconcileResult{}, err
	}
	state, err := loadViewerState(ctx, tx, evidence.Sample.VideoID, evidence.Sample.WindowStart)
	if err != nil {
		return viewer.Decision{}, ReconcileResult{}, err
	}
	decision, err := viewer.Reduce(state, evidence)
	if err != nil {
		return viewer.Decision{}, ReconcileResult{}, err
	}
	channelID, err := loadViewerChannelID(ctx, tx, evidence.Sample.VideoID)
	if err != nil {
		return viewer.Decision{}, ReconcileResult{}, err
	}
	if err := persistViewerDecision(ctx, tx, claimed, &decision, channelID); err != nil {
		return viewer.Decision{}, ReconcileResult{}, err
	}
	return decision, ReconcileResult{Applications: mapViewerApplications(decision.Applications)}, nil
}

func viewerEvidenceFromObservation(observation *Observation) (viewer.Evidence, error) {
	var payload contract.ViewerSampleV1
	if err := jsonv2.Unmarshal(observation.Payload, &payload); err != nil {
		return viewer.Evidence{}, fmt.Errorf("decode viewer sample payload: %w", err)
	}
	return viewer.Evidence{
		ObservationID: observation.ID,
		Provider:      observation.Provider,
		Sample: viewer.Sample{
			VideoID:       payload.VideoID,
			Provider:      observation.Provider,
			ViewerCount:   payload.ViewerCount,
			Availability:  payload.Availability,
			WindowStart:   payload.SampleWindowStart,
			WindowSeconds: payload.SampleWindowSeconds,
			ObservationID: observation.ID,
			ScheduledFor:  observation.ScheduledFor,
			EffectiveAt:   observation.EffectiveAt,
			ReceivedAt:    observation.ReceivedAt,
		},
	}, nil
}

func mapViewerApplications(items []viewer.Application) []Application {
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
