package sourceobservation

import (
	"context"
	"encoding/json"
	"fmt"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/reconcile/stats"
)

func (c *Consumer) reconcileStats(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
) (stats.Decision, ReconcileResult, error) {
	evidence, err := statsEvidenceFromObservation(claimed)
	if err != nil {
		return stats.Decision{}, ReconcileResult{}, err
	}
	if err := lockLiveSubject(ctx, tx, "stats:"+evidence.Sample.ChannelID); err != nil {
		return stats.Decision{}, ReconcileResult{}, err
	}
	state, err := loadStatsState(ctx, tx, evidence.Sample.ChannelID, evidence.Sample.ScheduledFor)
	if err != nil {
		return stats.Decision{}, ReconcileResult{}, err
	}
	decision, err := stats.Reduce(state, evidence)
	if err != nil {
		return stats.Decision{}, ReconcileResult{}, err
	}
	if err := persistStatsDecision(ctx, tx, claimed, &decision); err != nil {
		return stats.Decision{}, ReconcileResult{}, err
	}
	return decision, ReconcileResult{Applications: mapStatsApplications(decision.Applications)}, nil
}

func statsEvidenceFromObservation(observation *Observation) (stats.Evidence, error) {
	var payload contract.ChannelStatsV1
	if err := json.Unmarshal(observation.Payload, &payload); err != nil {
		return stats.Evidence{}, fmt.Errorf("decode channel stats payload: %w", err)
	}
	covered := map[string]bool{}
	for _, field := range payload.Coverage.Fields {
		covered[field] = true
	}
	return stats.Evidence{
		ObservationID: observation.ID,
		Provider:      observation.Provider,
		Sample: stats.Sample{
			ChannelID:         payload.ChannelID,
			Provider:          observation.Provider,
			SubscriberCount:   payload.SubscriberCount,
			ViewCount:         payload.ViewCount,
			VideoCount:        payload.VideoCount,
			SubscriberCovered: covered["subscriber_count"],
			ViewCovered:       covered["view_count"],
			VideoCovered:      covered["video_count"],
			ObservationID:     observation.ID,
			ScheduledFor:      observation.ScheduledFor,
			EffectiveAt:       observation.EffectiveAt,
			ReceivedAt:        observation.ReceivedAt,
		},
	}, nil
}

func mapStatsApplications(items []stats.Application) []Application {
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
