package sourceobservation

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/reconcile/stats"
)

func (c *Consumer) reconcileStats(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
) (ReconcileResult, error) {
	evidence, err := statsEvidenceFromObservation(claimed)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("stats evidence from observation: %w", err)
	}

	if lockErr := lockLiveSubject(ctx, tx, "stats:"+evidence.Sample.ChannelID); lockErr != nil {
		return ReconcileResult{}, fmt.Errorf("lock live subject: %w", lockErr)
	}

	state, err := loadStatsState(ctx, tx, evidence.Sample.ChannelID, evidence.Sample.ScheduledFor)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("load stats state: %w", err)
	}

	decision, err := stats.Reduce(state, evidence)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("reduce: %w", err)
	}

	if persistErr := persistStatsDecision(ctx, tx, claimed, &decision); persistErr != nil {
		return ReconcileResult{}, fmt.Errorf("persist stats decision: %w", persistErr)
	}

	return ReconcileResult{Applications: mapStatsApplications(decision.Applications)}, nil
}

func statsEvidenceFromObservation(observation *Observation) (stats.Evidence, error) {
	var payload contract.ChannelStatsV1

	if err := jsonv2.Unmarshal(observation.Payload, &payload); err != nil {
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
