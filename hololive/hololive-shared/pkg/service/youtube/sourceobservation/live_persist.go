package sourceobservation

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/kapu/hololive-shared/internal/service/youtube/reconcile/live"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

func persistLiveDecision(ctx context.Context, tx dbx.Tx, decision *live.Decision) error {
	// 여러 영상의 신규 행도 consumer마다 같은 순서로 생성한다.
	slices.SortFunc(decision.Sessions, func(a, b live.SessionState) int { return strings.Compare(a.VideoID, b.VideoID) })
	slices.SortFunc(decision.PendingEnds, func(a, b live.PendingEnd) int { return strings.Compare(a.VideoID, b.VideoID) })

	for i := range decision.Sessions {
		if err := upsertLiveSession(ctx, tx, &decision.Sessions[i]); err != nil {
			return fmt.Errorf("upsert live session: %w", err)
		}

		if err := upsertLiveHead(ctx, tx, &decision.Sessions[i]); err != nil {
			return fmt.Errorf("upsert live head: %w", err)
		}
	}

	if err := persistLiveEvidence(ctx, tx, decision); err != nil {
		return fmt.Errorf("persist live evidence: %w", err)
	}

	return nil
}

func upsertLiveSession(ctx context.Context, tx dbx.Tx, session *live.SessionState) error {
	if err := executeLiveSessionUpsert(ctx, tx, session, false); err != nil {
		return fmt.Errorf("execute live session upsert: %w", err)
	}

	return nil
}

func upsertConfirmedPremiereSession(ctx context.Context, tx dbx.Tx, session *live.SessionState) error {
	if err := executeLiveSessionUpsert(ctx, tx, session, true); err != nil {
		return fmt.Errorf("execute confirmed Premiere session upsert: %w", err)
	}

	return nil
}

func executeLiveSessionUpsert(
	ctx context.Context,
	tx dbx.Tx,
	session *live.SessionState,
	classificationOnlyOnConflict bool,
) error {
	if session.ChannelID == "" {
		return nil
	}

	if _, err := tx.Exec(
		ctx,
		mustSQL("repository_live_session_upsert_0047_47.sql"),
		session.VideoID,
		session.ChannelID,
		string(session.Status),
		session.Title,
		session.TopicID,
		session.ThumbnailURL,
		session.ScheduledStartTime,
		session.StartedAt,
		session.EndedAt,
		session.LiveFirstSeenAt,
		session.LastSeenAt,
		session.IsPremiere,
		classificationOnlyOnConflict,
	); err != nil {
		return fmt.Errorf("upsert live session: %w", err)
	}

	return nil
}

func upsertLiveHead(ctx context.Context, tx dbx.Tx, session *live.SessionState) error {
	if session == nil {
		return errors.New("upsert live head: session state is nil")
	}

	var (
		kind          any
		observationID any
		nextCheck     any
	)

	if session.Clock.EndCandidateKind != nil && session.Clock.EndCandidateObservationID != nil && session.Clock.NextEndCheckAt != nil {
		kind = string(*session.Clock.EndCandidateKind)
		observationID = *session.Clock.EndCandidateObservationID
		nextCheck = *session.Clock.NextEndCheckAt
	}

	var reason any

	if session.EndReason != nil {
		reason = string(*session.EndReason)
	}

	if _, err := tx.Exec(
		ctx,
		mustSQL("repository_live_head_upsert_0048_48.sql"),
		session.VideoID,
		string(session.Status),
		session.Clock.LastUpcomingPositiveAt,
		session.Clock.LastUpcomingPositiveSeenAt,
		session.Clock.LastLivePositiveAt,
		session.Clock.LastLivePositiveSeenAt,
		session.Clock.LastEndEvidenceAt,
		session.Clock.LastCompleteAbsenceAt,
		session.LastAbsenceScheduledFor,
		session.Clock.ConsecutiveAbsenceSlots,
		kind,
		observationID,
		nextCheck,
		session.Clock.EndedAt,
		reason,
		session.FirstAbsenceScheduledFor,
		session.SecondAbsenceScheduledFor,
		session.LastAbsenceObservationID,
		session.IgnoredAbsenceScheduledFor,
	); err != nil {
		return fmt.Errorf("upsert live head: %w", err)
	}

	return nil
}
