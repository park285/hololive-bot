package sourceobservation

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/live"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func lockLiveSubject(ctx context.Context, tx dbx.Tx, subjectKey string) error {
	if _, err := tx.Exec(ctx, mustSQL("repository_live_subject_lock_0044_44.sql"), subjectKey); err != nil {
		return fmt.Errorf("lock live subject: %w", err)
	}

	return nil
}

func loadLiveState(ctx context.Context, tx dbx.Tx, channelIDs, videoIDs []string) (live.State, error) {
	state := live.State{Sessions: map[string]live.SessionState{}, PendingEnds: map[string]live.PendingEnd{}}
	if err := loadLiveSessions(ctx, tx, &state, channelIDs, videoIDs); err != nil {
		return live.State{}, fmt.Errorf("load live sessions: %w", err)
	}

	ids := make([]string, 0, len(state.Sessions)+len(videoIDs))
	seen := map[string]struct{}{}

	for videoID := range state.Sessions {
		ids = append(ids, videoID)
		seen[videoID] = struct{}{}
	}

	for _, videoID := range videoIDs {
		if _, ok := seen[videoID]; ok {
			continue
		}

		ids = append(ids, videoID)
	}

	// session 행을 잠근 뒤라 ENDED 판정은 이 트랜잭션 안에서 바뀌지 않는다.
	if err := loadLiveHeads(ctx, tx, &state, ids, endedSessionIDs(&state)); err != nil {
		return live.State{}, fmt.Errorf("load live heads: %w", err)
	}

	markHeadlessHistoriesLoaded(&state)

	// head를 읽은 뒤라 종료 후보 유무로 동결 여부를 판정할 수 있다. ids는 이후 쓰지 않으므로 제자리에서 거른다.
	if err := loadLivePendingEnds(ctx, tx, &state, pendingLoadIDs(&state, ids)); err != nil {
		return live.State{}, fmt.Errorf("load live pending ends: %w", err)
	}

	return state, nil
}

// pendingLoadIDs는 pending을 읽고 잠글 영상만 남긴다. 동결된 D2 pending은 어떤 판정도 읽거나 바꾸지
// 않으므로 잠그지도 읽지도 않는다. 읽지 않은 행은 Decision에 없어 다시 쓰이지 않고, pending 삭제는 dirty
// 세션으로 한정되는데 동결 세션은 dirty가 되지 않으므로 지워지지도 않는다.
func pendingLoadIDs(state *live.State, ids []string) []string {
	return slices.DeleteFunc(ids, func(videoID string) bool {
		session, ok := state.Sessions[videoID]

		return ok && frozenEndedSession(&session)
	})
}

// frozenEndedSession은 종료 후보가 남지 않은 저장된 ENDED 세션이다. 이 세션의 positive는 reducer가 KEEP_ENDED로
// 두고 반복 종료·취소와 부재·저장 종료 재적용은 건너뛰며, next_end_check_at이 없어 due 정리도 하지 않는다.
// 영상 확인과 finalizer도 이 세션에서는 pending을 비교하기 전에 멈춘다. 판정은 session·head 행을 잠근 뒤라
// 이 트랜잭션 안에서 바뀌지 않는다. 저장된 session 없이 head만 남은 영상과 후보가 남은 ENDED는 지금처럼
// pending을 읽는다.
func frozenEndedSession(session *live.SessionState) bool {
	return session.Present && session.Status == domain.LiveStatusEnded &&
		session.Clock.EndCandidateObservationID == nil && session.Clock.NextEndCheckAt == nil
}

func loadLiveSessions(ctx context.Context, tx dbx.Tx, state *live.State, channelIDs, videoIDs []string) error {
	if len(channelIDs) == 0 && len(videoIDs) == 0 {
		return nil
	}

	rows, err := tx.Query(ctx, mustSQL("repository_live_sessions_0045_45.sql"), channelIDs, videoIDs)
	if err != nil {
		return fmt.Errorf("load live sessions: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		session, err := scanLiveSession(rows)
		if err != nil {
			return fmt.Errorf("scan live session: %w", err)
		}

		state.Sessions[session.VideoID] = session
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("load live sessions: %w", err)
	}

	return nil
}

func scanLiveSession(rows pgx.Rows) (live.SessionState, error) {
	var (
		session live.SessionState
		status  string
	)

	if err := rows.Scan(
		&session.VideoID, &session.ChannelID, &status, &session.Title,
		&session.TopicID, &session.ThumbnailURL,
		&session.ScheduledStartTime, &session.StartedAt, &session.EndedAt,
		&session.LiveFirstSeenAt, &session.LastSeenAt, &session.IsPremiere,
		&session.LifecycleOrigin,
		&session.StatusObservedAt, &session.ScheduleObservedAt, &session.TitleObservedAt,
	); err != nil {
		return live.SessionState{}, fmt.Errorf("scan live session: %w", err)
	}

	session.Status = domain.LiveStatus(status)
	session.Present = true

	return session, nil
}

// endedSessionIDs는 무시한 부재 이력을 읽지 않을 세션이다. ENDED 세션의 이력은 reducer가
// 읽거나 늘리지 않으므로 미적재 상태로 두어 저장 시 기존 배열을 유지한다.
func endedSessionIDs(state *live.State) []string {
	ids := make([]string, 0)

	for videoID := range state.Sessions {
		if session := state.Sessions[videoID]; session.Present && session.Status == domain.LiveStatusEnded {
			ids = append(ids, videoID)
		}
	}

	return ids
}

// markHeadlessHistoriesLoaded는 head 행이 없는 세션의 이력을 적재된 빈 이력으로 둔다.
// 저장된 head가 없으면 저장된 이력도 없다. 미적재로 남기면 reducer가 부재 적용에서 실패한다.
func markHeadlessHistoriesLoaded(state *live.State) {
	for videoID := range state.Sessions {
		session := state.Sessions[videoID]
		if session.HeadPresent {
			continue
		}

		session.IgnoredAbsences = live.LoadedIgnoredAbsences(nil)
		state.Sessions[videoID] = session
	}
}

func loadLiveHeads(ctx context.Context, tx dbx.Tx, state *live.State, videoIDs, omitIgnoredFor []string) error {
	if len(videoIDs) == 0 {
		return nil
	}

	rows, err := tx.Query(ctx, mustSQL("repository_live_heads_0046_46.sql"), videoIDs, omitIgnoredFor)
	if err != nil {
		return fmt.Errorf("load live heads: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		if err := applyLiveHeadRow(rows, state); err != nil {
			return fmt.Errorf("apply live head row: %w", err)
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("load live heads: %w", err)
	}

	return nil
}

func applyLiveHeadRow(rows pgx.Rows, state *live.State) error {
	head, err := scanLiveHead(rows)
	if err != nil {
		return fmt.Errorf("scan live head: %w", err)
	}

	existing := state.Sessions[head.VideoID]

	existing.VideoID = head.VideoID

	if existing.Status == "" {
		existing.Status = head.Status
	}

	existing.Clock = head.Clock
	existing.HeadPresent = true
	existing.EndReason = head.EndReason
	existing.LastAbsenceScheduledFor = head.LastAbsenceScheduledFor
	existing.FirstAbsenceScheduledFor = head.FirstAbsenceScheduledFor
	existing.SecondAbsenceScheduledFor = head.SecondAbsenceScheduledFor
	existing.LastAbsenceObservationID = head.LastAbsenceObservationID
	existing.IgnoredAbsences = head.IgnoredAbsences
	applyAbsenceSlotHints(&existing)

	state.Sessions[head.VideoID] = existing

	return nil
}

func applyAbsenceSlotHints(existing *live.SessionState) {
	if existing == nil {
		return
	}

	if existing.Clock.ConsecutiveAbsenceSlots == 1 && existing.FirstAbsenceScheduledFor == nil {
		existing.FirstAbsenceScheduledFor = existing.LastAbsenceScheduledFor
	}

	if existing.Clock.ConsecutiveAbsenceSlots >= 2 && existing.SecondAbsenceScheduledFor == nil {
		existing.SecondAbsenceScheduledFor = existing.LastAbsenceScheduledFor
	}
}

func scanLiveHead(rows pgx.Rows) (live.SessionState, error) {
	var (
		session      live.SessionState
		status       string
		candidate    *string
		candidateID  *int64
		endReason    *string
		absenceSched *time.Time
		ignored      *[]time.Time
	)

	if err := rows.Scan(
		&session.VideoID, &status,
		&session.Clock.LastUpcomingPositiveAt, &session.Clock.LastUpcomingPositiveSeenAt,
		&session.Clock.LastLivePositiveAt, &session.Clock.LastLivePositiveSeenAt,
		&session.Clock.LastEndEvidenceAt, &session.Clock.LastCompleteAbsenceAt, &absenceSched,
		&session.Clock.ConsecutiveAbsenceSlots, &candidate, &candidateID,
		&session.Clock.NextEndCheckAt, &session.Clock.EndedAt, &endReason,
		&session.FirstAbsenceScheduledFor, &session.SecondAbsenceScheduledFor,
		&session.LastAbsenceObservationID, &session.Clock.UnresolvableSince, &ignored,
	); err != nil {
		return live.SessionState{}, fmt.Errorf("scan live head: %w", err)
	}

	session.Status = domain.LiveStatus(status)
	session.LastAbsenceScheduledFor = absenceSched

	// 열은 NOT NULL이므로 NULL은 질의가 이력을 생략했다는 뜻이다. 0값(미적재)으로 둔다.
	if ignored != nil {
		session.IgnoredAbsences = live.LoadedIgnoredAbsences(*ignored)
	}

	if endReason != nil {
		reason := live.EndReason(*endReason)

		session.EndReason = &reason
	}

	if candidate != nil && candidateID != nil {
		kind := live.EndEvidenceKind(*candidate)

		session.Clock.EndCandidateKind = &kind
		session.Clock.EndCandidateObservationID = candidateID
	}

	return session, nil
}
