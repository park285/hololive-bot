package sourceobservation

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"

	"github.com/kapu/hololive-shared/internal/service/youtube/reconcile/viewer"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

func (c *Consumer) reconcileViewer(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
) (ReconcileResult, error) {
	evidence, err := viewerEvidenceFromObservation(claimed)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("viewer evidence from observation: %w", err)
	}

	if lockErr := lockViewerSubject(ctx, tx, evidence.Sample.VideoID); lockErr != nil {
		return ReconcileResult{}, fmt.Errorf("lock viewer subject: %w", lockErr)
	}

	state, err := loadViewerState(ctx, tx, evidence.Sample.VideoID, evidence.Sample.WindowStart)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("load viewer state: %w", err)
	}

	decision, err := viewer.Reduce(state, evidence)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("reduce: %w", err)
	}

	channelID, err := loadViewerChannelID(ctx, tx, evidence.Sample.VideoID)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("load viewer channel ID: %w", err)
	}

	if persistErr := persistViewerDecision(ctx, tx, claimed, &decision, channelID); persistErr != nil {
		return ReconcileResult{}, fmt.Errorf("persist viewer decision: %w", persistErr)
	}

	return ReconcileResult{Applications: mapViewerApplications(decision.Applications)}, nil
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
