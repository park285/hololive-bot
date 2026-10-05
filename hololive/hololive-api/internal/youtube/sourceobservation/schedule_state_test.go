package sourceobservation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/schedule"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestScheduleStateDoesNotWaitForUnusedHead(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	_, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions(video_id, channel_id, status, title)
		VALUES ('schedule-existing', 'channel', 'LIVE', NULL);
		INSERT INTO youtube_live_reconciliation_heads(video_id, status)
		VALUES ('schedule-existing', 'UPCOMING')
	`)
	require.NoError(t, err)

	blocker, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer rollbackPublishTestTx(ctx, t, blocker, "unused schedule head")

	_, err = blocker.Exec(ctx, "SELECT video_id FROM youtube_live_reconciliation_heads FOR UPDATE")
	require.NoError(t, err)

	err = dbx.InPgxTx(ctx, pool, func(tx dbx.Tx) error {
		if _, execErr := tx.Exec(ctx, "SET LOCAL lock_timeout = '100ms'"); execErr != nil {
			return fmt.Errorf("set schedule lock timeout: %w", execErr)
		}

		state, loadErr := loadScheduleState(ctx, tx, []schedule.Item{{VideoID: "schedule-existing"}})
		if loadErr != nil {
			return loadErr
		}

		require.Equal(t, domain.LiveStatusLive, state.Sessions["schedule-existing"].Status)
		require.Empty(t, state.Sessions["schedule-existing"].Title)

		return nil
	})
	require.NoError(t, err)
}

func TestScheduleStateRetainsStatusFromHeadWithoutSession(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	_, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_reconciliation_heads(video_id, status)
		VALUES ('head-upcoming', 'UPCOMING'), ('head-live', 'LIVE'), ('head-ended', 'ENDED')
	`)
	require.NoError(t, err)

	items := []schedule.Item{
		{VideoID: "head-upcoming", ChannelID: testChannelID},
		{VideoID: "head-live", ChannelID: testChannelID},
		{VideoID: "head-ended", ChannelID: testChannelID},
		{VideoID: "missing", ChannelID: testChannelID},
		{ExternalID: "temporary"},
	}

	err = dbx.InPgxTx(ctx, pool, func(tx dbx.Tx) error {
		state, loadErr := loadScheduleState(ctx, tx, items)
		if loadErr != nil {
			return loadErr
		}

		require.Len(t, state.Sessions, 3)
		require.Equal(t, domain.LiveStatusUpcoming, state.Sessions["head-upcoming"].Status)
		require.Equal(t, domain.LiveStatusLive, state.Sessions["head-live"].Status)
		require.Equal(t, domain.LiveStatusEnded, state.Sessions["head-ended"].Status)

		decision, reduceErr := schedule.Reduce(state, schedule.Evidence{GroupKey: "test", Items: items})
		if reduceErr != nil {
			return fmt.Errorf("reduce head-only schedule: %w", reduceErr)
		}

		require.Len(t, decision.Sessions, 3)
		require.Equal(t, "head-upcoming", decision.Sessions[0].VideoID)
		require.Equal(t, "head-live", decision.Sessions[1].VideoID)
		require.Equal(t, "missing", decision.Sessions[2].VideoID)

		return nil
	})
	require.NoError(t, err)
}
