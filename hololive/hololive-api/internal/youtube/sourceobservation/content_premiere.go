package sourceobservation

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/content"
	"github.com/kapu/hololive-api/internal/youtube/reconcile/live"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

// mergeContentPremieres는 확정된 최초공개를 세션에 반영하고 적용 결정과 수명 모델 입력을 바꿀 수 있는 영상 key를 돌려준다.
func mergeContentPremieres(
	ctx context.Context,
	tx dbx.Tx,
	observation *Observation,
	evidence *content.Evidence,
) ([]Application, []string, error) {
	if evidence.Kind != contract.KindVideoList {
		return []Application{}, nil, nil
	}

	facts := confirmedPremiereFacts(evidence)
	if len(facts) == 0 {
		return []Application{}, nil, nil
	}

	if err := lockLiveSubject(ctx, tx, observation.SubjectKey); err != nil {
		return nil, nil, err
	}

	state := live.State{Sessions: map[string]live.SessionState{}}
	if err := loadLiveSessions(ctx, tx, &state, nil, premiereVideoIDs(facts)); err != nil {
		return nil, nil, err
	}

	decision := live.MergeConfirmedPremieres(state, facts)
	if err := persistPremiereDecision(ctx, tx, observation, &decision); err != nil {
		return nil, nil, fmt.Errorf("persist Premiere decision: %w", err)
	}

	videoIDs := make([]string, len(decision.Sessions))
	for i := range decision.Sessions {
		videoIDs[i] = decision.Sessions[i].VideoID
	}

	return mapLiveApplications(decision.Applications), videoIDs, nil
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
			EffectiveAt: evidence.EffectiveAt,
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
		); err != nil {
			return fmt.Errorf("persist Premiere conflict: %w", err)
		}
	}

	return nil
}
