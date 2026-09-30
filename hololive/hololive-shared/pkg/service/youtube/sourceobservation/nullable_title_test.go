package sourceobservation

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/dbx"
)

func TestNullableLiveTitleConsumerPreservesLifecycle(t *testing.T) {
	for _, origin := range []string{"metadata_only", "legacy_unknown"} {
		t.Run(origin, func(t *testing.T) {
			pool, repo, consumer, proof := startLivePersist(t)
			ctx := t.Context()
			_, err := pool.Exec(ctx, `INSERT INTO youtube_live_sessions
    (video_id,channel_id,status,title,last_seen_at,scheduled_start_time,lifecycle_origin)
    VALUES ($1,$2,'UPCOMING',NULL,now()-interval '1 year',now()-interval '1 year',$3)`, testVideoID, testChannelID, origin)
			require.NoError(t, err)

			proof = publishConsumeLive(ctx, t, pool, repo, consumer, &proof)
			assertLifecycleOrigin(t, pool, origin)

			for _, table := range []string{"youtube_live_reconciliation_heads", "youtube_live_pending_ends", "youtube_notification_outbox"} {
				assertTableCount(t, pool, table, 0)
			}

			proof = publishConsumeLive(ctx, t, pool, repo, consumer, &proof, liveSession(testVideoID, "UPCOMING"))
			assertLifecycleOrigin(t, pool, "observed")

			var upcoming, started, positive *time.Time

			require.NoError(t, pool.QueryRow(ctx, `SELECT head.last_upcoming_positive_at,session.started_at,head.last_live_positive_at
    FROM youtube_live_sessions session JOIN youtube_live_reconciliation_heads head USING(video_id)
    WHERE video_id=$1`, testVideoID).Scan(&upcoming, &started, &positive))
			require.NotNil(t, upcoming)
			require.Nil(t, started)
			require.Nil(t, positive)
			publishConsumeLive(ctx, t, pool, repo, consumer, &proof, liveSession(testVideoID, testStatusLive))
			assertTableCount(t, pool, "youtube_live_reconciliation_heads", 1)
		})
	}
}

func TestNullableLiveTitleSingleAndBatchLookup(t *testing.T) {
	for i, title := range []*string{nil, new(""), new("live title")} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			pool, _, _, _ := startLivePersist(t)
			ctx := t.Context()
			_, err := pool.Exec(ctx, `INSERT INTO youtube_live_sessions (video_id,channel_id,status,title,lifecycle_origin) VALUES ($1,$2,'UPCOMING',$3,'metadata_only')`, testVideoID, testChannelID, title)
			require.NoError(t, err)

			want := ""

			if title != nil {
				want = *title
			}

			require.NoError(t, dbx.InPgxTx(ctx, pool, func(tx dbx.Tx) error {
				rows, err := tx.Query(ctx, mustSQL("repository_live_session_one_0060_60.sql"), testVideoID)
				require.NoError(t, err)

				defer rows.Close()

				require.True(t, rows.Next())

				session, err := scanLiveSession(rows)
				rows.Close()
				require.NoError(t, err)
				require.Equal(t, want, session.Title)
				require.Equal(t, "metadata_only", string(session.LifecycleOrigin))

				state, err := loadLiveState(ctx, tx, nil, []string{testVideoID})
				require.NoError(t, err)
				require.Equal(t, want, state.Sessions[testVideoID].Title)

				return nil
			}))
		})
	}
}
