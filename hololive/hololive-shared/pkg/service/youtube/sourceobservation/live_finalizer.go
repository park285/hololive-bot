package sourceobservation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/reconcile/live"
)

func (r *Repository) FinalizeNextDueLiveEnd(ctx context.Context, grace time.Duration) (bool, error) {
	if err := r.validate(); err != nil {
		return false, err
	}
	return dbx.InPgxTxWithResult(ctx, r.pool, func(tx dbx.Tx) (bool, error) {
		return finalizeNextDueLiveEndTx(ctx, tx, grace)
	})
}

func finalizeNextDueLiveEndTx(ctx context.Context, tx dbx.Tx, grace time.Duration) (bool, error) {
	var videoID string
	err := tx.QueryRow(ctx, mustSQL("repository_live_due_one_0049_49.sql")).Scan(&videoID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim due live end: %w", err)
	}
	var dbNow time.Time
	if err := tx.QueryRow(ctx, mustSQL("repository_live_now_0050_50.sql")).Scan(&dbNow); err != nil {
		return false, fmt.Errorf("load database now: %w", err)
	}
	state, err := loadLiveState(ctx, tx, nil, []string{videoID})
	if err != nil {
		return false, err
	}
	session, ok := state.Sessions[videoID]
	if !ok || session.Clock.EndCandidateObservationID == nil {
		return true, clearLiveCandidate(ctx, tx, &session, videoID)
	}
	if _, err := tx.Exec(ctx, mustSQL("repository_live_observation_lock_0051_51.sql"), *session.Clock.EndCandidateObservationID); err != nil {
		return false, fmt.Errorf("lock end candidate observation: %w", err)
	}
	decision := live.FinalizeDue(state, dbNow, grace)
	if err := persistLiveDecision(ctx, tx, &decision); err != nil {
		return false, err
	}
	return true, nil
}

func clearLiveCandidate(ctx context.Context, tx dbx.Tx, session *live.SessionState, videoID string) error {
	if session == nil {
		return fmt.Errorf("clear live candidate: session state is nil")
	}
	session.VideoID = videoID
	if session.Status == "" {
		session.Status = live.StatusUpcoming
	}
	session.Clock.EndCandidateKind = nil
	session.Clock.EndCandidateObservationID = nil
	session.Clock.NextEndCheckAt = nil
	return upsertLiveHead(ctx, tx, session)
}
