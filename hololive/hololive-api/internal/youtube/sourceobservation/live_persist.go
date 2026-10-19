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
	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	liveSessionBatchSize   = 64
	liveSessionColumnCount = 16
	// 갱신은 새 행과 잠금 안에서 읽은 행(video_id 제외)을 함께 보낸다.
	liveSessionUpdateArgCount = 2*liveSessionColumnCount - 1
)

var (
	liveSessionInsertSQL          = mustSQL("repository_live_session_insert.sql")
	liveSessionUpdateSQL          = mustSQL("repository_live_session_update.sql")
	livePremiereClassificationSQL = mustSQL("repository_live_premiere_classification_upsert.sql")
	liveHeadUpsertSQL             = mustSQL("repository_live_head_upsert_0048_48.sql")
)

// LiveSessionConflictError는 잠금 안에서 읽은 세션 행을 기준으로 한 저장이 어떤 행에도 적용되지 않았음을 뜻한다.
// 읽은 뒤 다른 writer가 행을 만들거나 바꿨으므로 이번 판정은 버리고 트랜잭션을 되돌려야 한다.
type LiveSessionConflictError struct {
	VideoID   string
	Operation string
}

func (e *LiveSessionConflictError) Error() string {
	return fmt.Sprintf("%s %s: row changed after locked read", e.Operation, e.VideoID)
}

// persistLiveDecision은 stored(이 트랜잭션이 잠그고 읽은 세션 행)를 기준으로 decision을 저장한다.
func persistLiveDecision(ctx context.Context, tx dbx.Tx, stored map[string]live.SessionState, decision *live.Decision) error {
	// 신규 행과 기존 행 모두 기존 consumer의 잠금 획득 순서를 유지한다.
	slices.SortFunc(decision.Sessions, func(a, b live.SessionState) int { return strings.Compare(a.VideoID, b.VideoID) })
	slices.SortFunc(decision.PendingEnds, func(a, b live.PendingEnd) int { return strings.Compare(a.VideoID, b.VideoID) })

	if err := persistLiveSessions(ctx, tx, stored, decision.Sessions); err != nil {
		return fmt.Errorf("persist live sessions and heads: %w", err)
	}

	if err := persistLiveEvidence(ctx, tx, decision); err != nil {
		return fmt.Errorf("persist live evidence: %w", err)
	}

	return nil
}

func persistLiveSessions(ctx context.Context, tx dbx.Tx, stored map[string]live.SessionState, sessions []live.SessionState) error {
	// 전송뿐 아니라 인자 배열 구성도 최대 64세션/128문장으로 제한한다.
	for start := 0; start < len(sessions); start += liveSessionBatchSize {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("persist live sessions at %d: %w", start, err)
		}

		end := min(start+liveSessionBatchSize, len(sessions))
		statements := liveDecisionStatements(stored, sessions[start:end])

		if err := execLiveSessionStatements(ctx, tx, statements); err != nil {
			return fmt.Errorf("persist live session batch at %d: %w", start, err)
		}
	}

	return nil
}

// execLiveSessionStatements는 조건부 세션 저장의 0행 적용을 영상 단위 충돌로 보고한다.
// RequireRow를 켠 문장은 세션 저장뿐이며 첫 인자가 video_id다.
func execLiveSessionStatements(ctx context.Context, tx dbx.Tx, statements []dbx.Statement) error {
	err := dbx.ExecStatements(ctx, tx, statements)
	if err == nil {
		return nil
	}

	missed, ok := errors.AsType[*dbx.NoRowsError](err)
	if !ok {
		return fmt.Errorf("exec session statements: %w", err)
	}

	return &LiveSessionConflictError{VideoID: fmt.Sprint(statements[missed.Index].Args[0]), Operation: missed.Operation}
}

func liveDecisionStatements(stored map[string]live.SessionState, sessions []live.SessionState) []dbx.Statement {
	statements := make([]dbx.Statement, 0, 2*len(sessions))
	for i := range sessions {
		session := &sessions[i]
		if session.ChannelID != "" {
			if statement, write := liveSessionWrite(stored, session); write {
				statements = append(statements, statement)
			}
		}

		// session A → head A → session B → head B의 기존 순서를 바꾸지 않는다.
		// 미확정 LIVE 메타데이터와 부재는 새 authoritative head의 근거가 아니다.
		if session.HeadPresent || session.LifecycleOrigin == live.OriginObserved {
			statements = append(statements, liveHeadStatement(session))
		}
	}

	return statements
}

// liveSessionWrite는 next를 저장할 문장을 만든다. 잠금 안에서 읽은 행이 없으면 새 행을, 있으면 그 행이
// 그대로일 때만 적용되는 갱신을 만들며, 둘 다 0행 적용을 충돌로 본다. 열 값이 읽은 행과 같으면 쓰지 않는다.
func liveSessionWrite(stored map[string]live.SessionState, next *live.SessionState) (dbx.Statement, bool) {
	prior, ok := stored[next.VideoID]
	if !ok || !prior.Present {
		return dbx.Statement{
			Operation:  "insert live session",
			SQL:        liveSessionInsertSQL,
			Args:       appendLiveSessionArgs(make([]any, 0, liveSessionColumnCount), next, next.Title),
			RequireRow: true,
		}, true
	}

	if sameLiveSessionRow(&prior, next) {
		return dbx.Statement{}, false
	}

	var title any

	if next.Title != prior.Title {
		title = next.Title
	}

	args := appendLiveSessionArgs(make([]any, 0, liveSessionUpdateArgCount), next, title)

	args = append(args,
		prior.ChannelID, string(prior.Status), prior.Title, prior.TopicID, prior.ThumbnailURL,
		prior.ScheduledStartTime, prior.StartedAt, prior.EndedAt, prior.LiveFirstSeenAt,
		prior.LastSeenAt, prior.IsPremiere, string(prior.LifecycleOrigin),
		prior.StatusObservedAt, prior.ScheduleObservedAt, prior.TitleObservedAt,
	)

	return dbx.Statement{Operation: "update live session", SQL: liveSessionUpdateSQL, Args: args, RequireRow: true}, true
}

func appendLiveSessionArgs(args []any, session *live.SessionState, title any) []any {
	return append(args,
		session.VideoID, session.ChannelID, string(session.Status), title,
		session.TopicID, session.ThumbnailURL, session.ScheduledStartTime,
		session.StartedAt, session.EndedAt, session.LiveFirstSeenAt, session.LastSeenAt,
		session.IsPremiere,
		string(session.LifecycleOrigin),
		session.StatusObservedAt, session.ScheduleObservedAt, session.TitleObservedAt,
	)
}

// sameLiveSessionRow는 youtube_live_sessions에 저장되는 열만 비교한다.
func sameLiveSessionRow(a, b *live.SessionState) bool {
	return a.ChannelID == b.ChannelID && a.Status == b.Status && a.Title == b.Title &&
		a.TopicID == b.TopicID && a.ThumbnailURL == b.ThumbnailURL &&
		sameOptionalTime(a.ScheduledStartTime, b.ScheduledStartTime) &&
		sameOptionalTime(a.StartedAt, b.StartedAt) && sameOptionalTime(a.EndedAt, b.EndedAt) &&
		sameOptionalTime(a.LiveFirstSeenAt, b.LiveFirstSeenAt) && a.LastSeenAt.Equal(b.LastSeenAt) &&
		sameOptionalBool(a.IsPremiere, b.IsPremiere) && a.LifecycleOrigin == b.LifecycleOrigin &&
		sameOptionalTime(a.StatusObservedAt, b.StatusObservedAt) &&
		sameOptionalTime(a.ScheduleObservedAt, b.ScheduleObservedAt) &&
		sameOptionalTime(a.TitleObservedAt, b.TitleObservedAt)
}

func sameOptionalTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}

	return a.Equal(*b)
}

func sameOptionalBool(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}

	return *a == *b
}

func upsertConfirmedPremiereSession(ctx context.Context, tx dbx.Tx, session *live.SessionState) error {
	if session == nil {
		return errors.New("upsert live session: session state is nil")
	}

	if session.ChannelID == "" {
		return nil
	}

	if _, err := tx.Exec(ctx, livePremiereClassificationSQL, appendLiveSessionArgs(make([]any, 0, liveSessionColumnCount), session, session.Title)...); err != nil {
		return fmt.Errorf("upsert live session: %w", err)
	}

	return nil
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
			session.LastAbsenceObservationID, ignoredAbsenceArg(session),
			session.Clock.UnresolvableSince,
		},
	}
}

// ENDED 이력은 다시 판정에 쓰이지 않으므로 적재 여부와 무관하게 비운다.
// 활성 세션의 미적재 이력은 SQL NULL로 보내 보존하고, 적재된 빈 이력만 명시적으로 지운다.
func ignoredAbsenceArg(session *live.SessionState) any {
	if session.Status == domain.LiveStatusEnded {
		return []time.Time{}
	}

	slots, loaded := session.IgnoredAbsences.Slots()
	if !loaded {
		return nil
	}

	if slots == nil {
		return []time.Time{}
	}

	return slots
}
