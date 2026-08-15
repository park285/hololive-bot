package sourceobservation

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/reconcile/live"
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
	if err := loadLiveHeads(ctx, tx, &state, ids); err != nil {
		return live.State{}, err
	}
	return state, nil
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
			return err
		}
		state.Sessions[session.VideoID] = session
	}
	return rows.Err()
}

func scanLiveSession(rows pgx.Rows) (live.SessionState, error) {
	var session live.SessionState
	var status string
	if err := rows.Scan(
		&session.VideoID, &session.ChannelID, &status, &session.Title,
		&session.ScheduledStartTime, &session.StartedAt, &session.EndedAt,
		&session.LiveFirstSeenAt, &session.LastSeenAt,
	); err != nil {
		return live.SessionState{}, fmt.Errorf("scan live session: %w", err)
	}
	session.Status = domain.LiveStatus(status)
	session.Present = true
	return session, nil
}

func loadLiveHeads(ctx context.Context, tx dbx.Tx, state *live.State, videoIDs []string) error {
	if len(videoIDs) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, mustSQL("repository_live_heads_0046_46.sql"), videoIDs)
	if err != nil {
		return fmt.Errorf("load live heads: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		if err := applyLiveHeadRow(rows, state); err != nil {
			return err
		}
	}
	return rows.Err()
}

func applyLiveHeadRow(rows pgx.Rows, state *live.State) error {
	head, pending, err := scanLiveHead(rows)
	if err != nil {
		return err
	}
	existing := state.Sessions[head.VideoID]
	existing.VideoID = head.VideoID
	if existing.Status == "" {
		existing.Status = head.Status
	}
	existing.Clock = head.Clock
	existing.EndReason = head.EndReason
	existing.LastAbsenceScheduledFor = head.LastAbsenceScheduledFor
	applyAbsenceSlotHints(&existing)
	state.Sessions[head.VideoID] = existing
	if pending != nil {
		state.PendingEnds[head.VideoID] = *pending
	}
	return nil
}

func applyAbsenceSlotHints(existing *live.SessionState) {
	if existing.Clock.ConsecutiveAbsenceSlots == 1 {
		existing.FirstAbsenceScheduledFor = existing.LastAbsenceScheduledFor
	}
	if existing.Clock.ConsecutiveAbsenceSlots >= 2 {
		existing.SecondAbsenceScheduledFor = existing.LastAbsenceScheduledFor
	}
}

func scanLiveHead(rows pgx.Rows) (live.SessionState, *live.PendingEnd, error) {
	var (
		session      live.SessionState
		status       string
		candidate    *string
		candidateID  *int64
		endReason    *string
		absenceSched *time.Time
	)
	if err := rows.Scan(
		&session.VideoID, &status,
		&session.Clock.LastUpcomingPositiveAt, &session.Clock.LastUpcomingPositiveSeenAt,
		&session.Clock.LastLivePositiveAt, &session.Clock.LastLivePositiveSeenAt,
		&session.Clock.LastEndEvidenceAt, &session.Clock.LastCompleteAbsenceAt, &absenceSched,
		&session.Clock.ConsecutiveAbsenceSlots, &candidate, &candidateID,
		&session.Clock.NextEndCheckAt, &session.Clock.EndedAt, &endReason,
	); err != nil {
		return live.SessionState{}, nil, fmt.Errorf("scan live head: %w", err)
	}
	session.Status = domain.LiveStatus(status)
	session.LastAbsenceScheduledFor = absenceSched
	if endReason != nil {
		reason := live.EndReason(*endReason)
		session.EndReason = &reason
	}
	if candidate != nil && candidateID != nil {
		kind := live.EndEvidenceKind(*candidate)
		session.Clock.EndCandidateKind = &kind
		session.Clock.EndCandidateObservationID = candidateID
		pending := live.PendingEnd{
			Kind:             kind,
			VideoID:          session.VideoID,
			ObservationID:    *candidateID,
			NegativeEligible: kind == live.EndEvidenceScopedAbsence,
			ScopeCovers:      kind == live.EndEvidenceScopedAbsence || kind == live.EndEvidenceExplicitEnd || kind == live.EndEvidenceExplicitCancel,
		}
		if session.Clock.LastEndEvidenceAt != nil {
			pending.EffectiveAt = *session.Clock.LastEndEvidenceAt
		}
		return session, &pending, nil
	}
	return session, nil, nil
}
