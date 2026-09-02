package sourceobservation

import (
	"context"
	"errors"
	"fmt"

	"github.com/kapu/hololive-shared/internal/service/youtube/reconcile/live"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

func persistLiveDecision(ctx context.Context, tx dbx.Tx, decision *live.Decision) error {
	for i := range decision.Sessions {
		if err := upsertLiveSession(ctx, tx, &decision.Sessions[i]); err != nil {
			return fmt.Errorf("upsert live session: %w", err)
		}

		if err := upsertLiveHead(ctx, tx, &decision.Sessions[i]); err != nil {
			return fmt.Errorf("upsert live head: %w", err)
		}
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
	); err != nil {
		return fmt.Errorf("upsert live head: %w", err)
	}

	return nil
}
