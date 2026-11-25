package sourceobservation

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/live"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func lockLiveSubject(ctx context.Context, tx dbx.Tx, subjectKey string) error {
	if _, err := pgxdb.Exec(ctx, tx, lockLiveSubjectSQL, subjectKey); err != nil {
		return fmt.Errorf("lock live subject: %w", err)
	}

	return nil
}

func loadLiveState(ctx context.Context, tx dbx.Tx, channelIDs, videoIDs []string) (live.State, error) {
	state := live.State{Sessions: map[string]live.SessionState{}, PendingEnds: map[string]live.PendingEnd{}}
	if err := loadLiveSessions(ctx, tx, &state, channelIDs, videoIDs); err != nil {
		return live.State{}, err
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

	if err := loadLiveHeads(ctx, tx, &state, ids, ids); err != nil {
		return live.State{}, fmt.Errorf("load live heads: %w", err)
	}

	markHeadlessHistoriesLoaded(&state)

	if err := loadLivePendingEnds(ctx, tx, &state, pendingLoadIDs(&state, ids)); err != nil {
		return live.State{}, fmt.Errorf("load live pending ends: %w", err)
	}

	return state, nil
}

func pendingLoadIDs(state *live.State, ids []string) []string {
	return slices.DeleteFunc(ids, func(videoID string) bool {
		session, ok := state.Sessions[videoID]

		return ok && frozenEndedSession(&session)
	})
}

func frozenEndedSession(session *live.SessionState) bool {
	return session.Present && session.Status == domain.LiveStatusEnded &&
		session.Clock.EndCandidateObservationID == nil && session.Clock.NextEndCheckAt == nil
}

func loadLiveSessions(ctx context.Context, tx dbx.Tx, state *live.State, channelIDs, videoIDs []string) error {
	if len(channelIDs) == 0 && len(videoIDs) == 0 {
		return nil
	}

	rows, err := pgxdb.Query(ctx, tx, loadLiveSessionsSQL, channelIDs, videoIDs)
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

	rows, err := pgxdb.Query(ctx, tx, loadLiveHeadsSQL, videoIDs, omitIgnoredFor)
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

func loadLiveIgnoredAbsences(ctx context.Context, tx dbx.Tx, state *live.State, videoIDs []string) error {
	if len(videoIDs) == 0 {
		return nil
	}

	rows, err := pgxdb.Query(ctx, tx, repositoryLiveIgnoredAbsencesSQL, videoIDs)
	if err != nil {
		return fmt.Errorf("query ignored absences: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			videoID string
			slots   []time.Time
		)

		if err := rows.Scan(&videoID, &slots); err != nil {
			return fmt.Errorf("scan ignored absences: %w", err)
		}

		session := state.Sessions[videoID]

		session.IgnoredAbsences = live.LoadedIgnoredAbsences(slots)
		state.Sessions[videoID] = session
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate ignored absences: %w", err)
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
