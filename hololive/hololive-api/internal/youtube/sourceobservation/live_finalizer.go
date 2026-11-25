package sourceobservation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/live"
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
	if err := lockLiveRuntimeState(ctx, tx); err != nil {
		return false, err
	}

	var (
		videoID string
		dbNow   time.Time
	)

	err := pgxdb.QueryRow(ctx, tx, finalizeNextDueLiveEndTxSQL).Scan(&videoID, &dbNow)

	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("claim due live end: %w", err)
	}

	state, err := loadLiveState(ctx, tx, nil, []string{videoID})
	if err != nil {
		return false, fmt.Errorf("load live state: %w", err)
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
	if err := persistLiveDecision(ctx, tx, state.Sessions, &decision); err != nil {
		return false, fmt.Errorf("persist live decision: %w", err)
	}

	if err := refreshLiveRuntimeState(ctx, tx, liveDecisionRuntimeVideoIDs(&decision)); err != nil {
		return false, err
	}

	return true, nil
}
