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

const liveSessionBatchSize = 64

func persistLiveDecision(ctx context.Context, tx dbx.Tx, decision *live.Decision) error {
	// 신규 행과 기존 행 모두 기존 consumer의 잠금 획득 순서를 유지한다.
	slices.SortFunc(decision.Sessions, func(a, b live.SessionState) int { return strings.Compare(a.VideoID, b.VideoID) })
	slices.SortFunc(decision.PendingEnds, func(a, b live.PendingEnd) int { return strings.Compare(a.VideoID, b.VideoID) })

	if err := persistLiveSessions(ctx, tx, decision.Sessions); err != nil {
		return fmt.Errorf("persist live sessions and heads: %w", err)
	}
	if err := persistLiveEvidence(ctx, tx, decision); err != nil {
		return fmt.Errorf("persist live evidence: %w", err)
	}
	return nil
}

func persistLiveSessions(ctx context.Context, tx dbx.Tx, sessions []live.SessionState) error {
	// 전송뿐 아니라 인자 배열 구성도 최대 64세션/128문장으로 제한한다.
	for start := 0; start < len(sessions); start += liveSessionBatchSize {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("persist live sessions at %d: %w", start, err)
		}
		end := min(start+liveSessionBatchSize, len(sessions))
		statements := liveDecisionStatements(sessions[start:end])
		if err := dbx.ExecStatements(ctx, tx, statements); err != nil {
			return fmt.Errorf("persist live session batch at %d: %w", start, err)
		}
	}
	return nil
}

func liveDecisionStatements(sessions []live.SessionState) []dbx.Statement {
	statements := make([]dbx.Statement, 0, 2*len(sessions))
	for i := range sessions {
		session := &sessions[i]
		if session.ChannelID != "" {
			statements = append(statements, liveSessionStatement(session, false))
		}
		// session A → head A → session B → head B의 기존 순서를 바꾸지 않는다.
		statements = append(statements, liveHeadStatement(session))
	}
	return statements
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

func executeLiveSessionUpsert(ctx context.Context, tx dbx.Tx, session *live.SessionState, classificationOnlyOnConflict bool) error {
	if session == nil {
		return errors.New("upsert live session: session state is nil")
	}
	if session.ChannelID == "" {
		return nil
	}
	statement := liveSessionStatement(session, classificationOnlyOnConflict)
	if _, err := tx.Exec(ctx, statement.SQL, statement.Args...); err != nil {
		return fmt.Errorf("upsert live session: %w", err)
	}
	return nil
}

func liveSessionStatement(session *live.SessionState, classificationOnlyOnConflict bool) dbx.Statement {
	return dbx.Statement{
		Operation: "upsert live session",
		SQL:       mustSQL("repository_live_session_upsert_0047_47.sql"),
		Args: []any{
			session.VideoID, session.ChannelID, string(session.Status), session.Title,
			session.TopicID, session.ThumbnailURL, session.ScheduledStartTime,
			session.StartedAt, session.EndedAt, session.LiveFirstSeenAt, session.LastSeenAt,
			session.IsPremiere, classificationOnlyOnConflict,
		},
	}
}

// 호출자는 sessions의 실제 원소만 전달한다. nil 오류를 만들고 다시 전달하지 않는다.
func liveHeadStatement(session *live.SessionState) dbx.Statement {
	var kind, observationID, nextCheck, reason any
	if session.Clock.EndCandidateKind != nil && session.Clock.EndCandidateObservationID != nil && session.Clock.NextEndCheckAt != nil {
		kind = string(*session.Clock.EndCandidateKind)
		observationID = *session.Clock.EndCandidateObservationID
		nextCheck = *session.Clock.NextEndCheckAt
	}
	if session.EndReason != nil {
		reason = string(*session.EndReason)
	}
	return dbx.Statement{
		Operation: "upsert live head",
		SQL:       mustSQL("repository_live_head_upsert_0048_48.sql"),
		Args: []any{
			session.VideoID, string(session.Status),
			session.Clock.LastUpcomingPositiveAt, session.Clock.LastUpcomingPositiveSeenAt,
			session.Clock.LastLivePositiveAt, session.Clock.LastLivePositiveSeenAt,
			session.Clock.LastEndEvidenceAt, session.Clock.LastCompleteAbsenceAt,
			session.LastAbsenceScheduledFor, session.Clock.ConsecutiveAbsenceSlots,
			kind, observationID, nextCheck, session.Clock.EndedAt, reason,
			session.FirstAbsenceScheduledFor, session.SecondAbsenceScheduledFor,
			session.LastAbsenceObservationID, session.IgnoredAbsenceScheduledFor,
		},
	}
}
