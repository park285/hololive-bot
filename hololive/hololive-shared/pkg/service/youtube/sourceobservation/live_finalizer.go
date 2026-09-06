package sourceobservation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/internal/service/youtube/reconcile/live"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

func (r *Repository) FinalizeNextDueLiveEnd(ctx context.Context, grace time.Duration) (bool, error) {
	if err := r.validate(); err != nil {
		return false, fmt.Errorf("validate: %w", err)
	}

	out, err := dbx.InPgxTxWithResult(ctx, r.pool, func(tx dbx.Tx) (bool, error) {
		return finalizeNextDueLiveEndTx(ctx, tx, grace)
	})
	if err != nil {
		return out, fmt.Errorf("in pgx tx with result: %w", err)
	}

	return out, nil
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

	// due 조회에서는 head를 선잠그지 않는다. consumer와 같은 session→head→evidence
	// 순서로 잠근 뒤 최신 candidate와 due 조건을 다시 확인한다.
	state, err := loadLiveState(ctx, tx, nil, []string{videoID})
	if err != nil {
		return false, fmt.Errorf("load live state: %w", err)
	}

	var dbNow time.Time

	if nowErr := tx.QueryRow(ctx, mustSQL("repository_live_now_0050_50.sql")).Scan(&dbNow); nowErr != nil {
		return false, fmt.Errorf("load database now: %w", nowErr)
	}

	session, ok := state.Sessions[videoID]
	if !ok || session.Clock.EndCandidateObservationID == nil ||
		session.Clock.NextEndCheckAt == nil || session.Clock.NextEndCheckAt.After(dbNow) {
		return false, nil
	}

	pending, ok := state.PendingEnds[videoID]
	if !ok || pending.ObservationID != *session.Clock.EndCandidateObservationID {
		return false, errors.New("live end candidate evidence is missing or inconsistent")
	}

	decision := live.FinalizeDue(state, dbNow, grace)
	if err := persistLiveDecision(ctx, tx, &decision); err != nil {
		return false, fmt.Errorf("persist live decision: %w", err)
	}

	return true, nil
}
