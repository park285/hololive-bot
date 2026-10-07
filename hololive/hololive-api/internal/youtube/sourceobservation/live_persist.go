package sourceobservation

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/live"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

const liveSessionBatchSize = 64

var (
	liveSessionUpsertSQL          = mustSQL("repository_live_session_upsert_0047_47.sql")
	livePremiereClassificationSQL = mustSQL("repository_live_premiere_classification_upsert.sql")
	liveHeadUpsertSQL             = mustSQL("repository_live_head_upsert_0048_48.sql")
)

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
		// 미확정 LIVE 메타데이터와 부재는 새 authoritative head의 근거가 아니다.
		if session.HeadPresent || session.LifecycleOrigin == live.OriginObserved {
			statements = append(statements, liveHeadStatement(session))
		}
	}

	return statements
}

func upsertConfirmedPremiereSession(ctx context.Context, tx dbx.Tx, session *live.SessionState) error {
	if session == nil {
		return errors.New("upsert live session: session state is nil")
	}

	if session.ChannelID == "" {
		return nil
	}

	statement := liveSessionStatement(session, true)
	if _, err := tx.Exec(ctx, statement.SQL, statement.Args...); err != nil {
		return fmt.Errorf("upsert live session: %w", err)
	}

	return nil
}

func liveSessionStatement(session *live.SessionState, classificationOnlyOnConflict bool) dbx.Statement {
	query := liveSessionUpsertSQL

	if classificationOnlyOnConflict {
		query = livePremiereClassificationSQL
	}

	return dbx.Statement{
		Operation: "upsert live session",
		SQL:       query,
		Args: []any{
			session.VideoID, session.ChannelID, string(session.Status), session.Title,
			session.TopicID, session.ThumbnailURL, session.ScheduledStartTime,
			session.StartedAt, session.EndedAt, session.LiveFirstSeenAt, session.LastSeenAt,
			session.IsPremiere,
			string(session.LifecycleOrigin),
			session.StatusObservedAt, session.ScheduleObservedAt, session.TitleObservedAt,
		},
	}
}

// 호출자는 sessions의 실제 원소만 전달한다. Nil 오류를 만들고 다시 전달하지 않는다.
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
		SQL:       liveHeadUpsertSQL,
		Args: []any{
			session.VideoID, string(session.Status),
			session.Clock.LastUpcomingPositiveAt, session.Clock.LastUpcomingPositiveSeenAt,
			session.Clock.LastLivePositiveAt, session.Clock.LastLivePositiveSeenAt,
			session.Clock.LastEndEvidenceAt, session.Clock.LastCompleteAbsenceAt,
			session.LastAbsenceScheduledFor, session.Clock.ConsecutiveAbsenceSlots,
			kind, observationID, nextCheck, session.Clock.EndedAt, reason,
			session.FirstAbsenceScheduledFor, session.SecondAbsenceScheduledFor,
			session.LastAbsenceObservationID, ignoredAbsenceArg(&session.IgnoredAbsences),
		},
	}
}

// ignoredAbsenceArg는 미적재 이력을 SQL NULL로 보내 upsert가 기존 배열을 유지하게 한다.
// 적재된 빈 이력은 nil slice가 NULL로 바뀌지 않도록 '{}'로 보내 기존 배열을 지운다.
func ignoredAbsenceArg(history *live.IgnoredAbsenceHistory) any {
	slots, loaded := history.Slots()
	if !loaded {
		return nil
	}

	if slots == nil {
		return []time.Time{}
	}

	return slots
}
