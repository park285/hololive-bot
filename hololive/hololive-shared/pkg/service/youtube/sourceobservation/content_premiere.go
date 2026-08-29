package sourceobservation

import (
	"context"
	"fmt"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/reconcile/content"
	"github.com/kapu/hololive-shared/pkg/service/youtube/reconcile/live"
)

func mergeContentPremieres(
	ctx context.Context,
	tx dbx.Tx,
	observation *Observation,
	evidence *content.Evidence,
) ([]Application, error) {
	if evidence.Kind != contract.KindVideoList {
		return nil, nil
	}

	facts := confirmedPremiereFacts(evidence)
	if len(facts) == 0 {
		return nil, nil
	}

	if err := lockLiveSubject(ctx, tx, observation.SubjectKey); err != nil {
		return nil, fmt.Errorf("lock live subject: %w", err)
	}

	state := live.State{Sessions: map[string]live.SessionState{}}
	if err := loadLiveSessions(ctx, tx, &state, nil, premiereVideoIDs(facts)); err != nil {
		return nil, fmt.Errorf("load live sessions: %w", err)
	}

	decision := live.MergeConfirmedPremieres(state, facts)
	if err := persistPremiereDecision(ctx, tx, observation, &decision); err != nil {
		return nil, fmt.Errorf("persist Premiere decision: %w", err)
	}

	return mapLiveApplications(decision.Applications), nil
}

func confirmedPremiereFacts(evidence *content.Evidence) []live.ConfirmedPremiereFact {
	facts := make([]live.ConfirmedPremiereFact, 0)
	for i := range evidence.Videos {
		video := &evidence.Videos[i]
		if video.IsPremiere == nil || !*video.IsPremiere {
			continue
		}

		facts = append(facts, live.ConfirmedPremiereFact{
			VideoID:     video.VideoID,
			ChannelID:   video.ChannelID,
			Title:       boundedVideoTitle(video.Title),
			ScheduledAt: video.ScheduledFor,
			ReceivedAt:  evidence.ReceivedAt,
		})
	}

	return facts
}

func premiereVideoIDs(facts []live.ConfirmedPremiereFact) []string {
	videoIDs := make([]string, len(facts))
	for i := range facts {
		videoIDs[i] = facts[i].VideoID
	}

	return videoIDs
}

func persistPremiereDecision(
	ctx context.Context,
	tx dbx.Tx,
	observation *Observation,
	decision *live.PremiereDecision,
) error {
	for i := range decision.Sessions {
		if err := upsertConfirmedPremiereSession(ctx, tx, &decision.Sessions[i]); err != nil {
			return fmt.Errorf("upsert live session: %w", err)
		}
	}

	for i := range decision.Conflicts {
		conflict := &decision.Conflicts[i]
		if err := persistReconcileConflict(
			ctx,
			tx,
			observation,
			"youtube_live_session",
			conflict.VideoID,
			conflict.FieldName,
			conflict.ExistingValueSHA256,
			conflict.AttemptedValueSHA256,
			"KEEP_EXISTING",
		); err != nil {
			return fmt.Errorf("persist Premiere conflict: %w", err)
		}
	}

	return nil
}
