package consume

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/internal/service/youtube/reconcile/live"
	"github.com/kapu/hololive-shared/internal/service/youtube/reconcile/schedule"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

func assertLifecycleOrigin(t *testing.T, pool *pgxpool.Pool, want string) {
	t.Helper()

	var origin string

	if err := pool.QueryRow(t.Context(), `SELECT lifecycle_origin FROM youtube_live_sessions WHERE video_id=$1`, testVideoID).Scan(&origin); err != nil {
		t.Fatal(err)
	}

	if origin != want {
		t.Fatalf("lifecycle origin = %q, want %q", origin, want)
	}
}

func TestLifecycleOriginMetadataMergePreservesObservedAndLegacy(t *testing.T) {
	for _, origin := range []live.LifecycleOrigin{live.OriginObserved, live.OriginLegacyUnknown} {
		t.Run(string(origin), func(t *testing.T) {
			pool, _, _, _ := startLivePersist(t)
			ctx := t.Context()
			seen := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)

			if _, err := pool.Exec(ctx, `INSERT INTO youtube_live_sessions
                (video_id,channel_id,status,title,last_seen_at,lifecycle_origin)
                VALUES ($1,$2,'UPCOMING','old',$3,$4)`, testVideoID, testChannelID, seen, string(origin)); err != nil {
				t.Fatal(err)
			}

			if err := dbx.InPgxTx(ctx, pool, func(tx dbx.Tx) error {
				return persistScheduleDecision(ctx, tx, &Observation{}, &schedule.Decision{Sessions: []schedule.Session{{
					VideoID: testVideoID, ChannelID: testChannelID, Status: live.StatusUpcoming,
					Title: "new schedule", LastSeenAt: seen.Add(time.Hour),
				}}})
			}); err != nil {
				t.Fatal(err)
			}

			assertLifecycleOrigin(t, pool, string(origin))

			if err := dbx.InPgxTx(ctx, pool, func(tx dbx.Tx) error {
				state, err := loadLiveState(ctx, tx, nil, []string{testVideoID})
				if err != nil {
					return err
				}

				decision := live.MergeConfirmedPremieres(state, []live.ConfirmedPremiereFact{{
					VideoID: testVideoID, ChannelID: testChannelID, ReceivedAt: seen.Add(2 * time.Hour),
				}})

				return persistPremiereDecision(ctx, tx, &Observation{}, &decision)
			}); err != nil {
				t.Fatal(err)
			}

			assertLifecycleOrigin(t, pool, string(origin))
			assertTableCount(t, pool, "youtube_live_reconciliation_heads", 0)
		})
	}
}

func TestLifecycleOriginSnapshotAbsenceKeepsMetadataAndLegacyHeadless(t *testing.T) {
	for _, origin := range []string{"metadata_only", "legacy_unknown"} {
		t.Run(origin, func(t *testing.T) {
			pool, repo, consumer, proof := startLivePersist(t)
			ctx := t.Context()

			if _, err := pool.Exec(ctx, `INSERT INTO youtube_live_sessions
                (video_id,channel_id,status,title,last_seen_at,scheduled_start_time,lifecycle_origin)
                VALUES ($1,$2,'UPCOMING','',now()-interval '1 year',now()-interval '1 year',$3)`, testVideoID, testChannelID, origin); err != nil {
				t.Fatal(err)
			}

			for range 2 {
				proof = publishConsumeLive(ctx, t, pool, repo, consumer, &proof)
			}

			assertLifecycleOrigin(t, pool, origin)
			assertTableCount(t, pool, "youtube_live_reconciliation_heads", 0)
			assertTableCount(t, pool, "youtube_live_pending_ends", 0)
			assertTableCount(t, pool, "youtube_notification_outbox", 0)

			proof = publishConsumeLive(ctx, t, pool, repo, consumer, &proof, liveSession(testVideoID, "UPCOMING"))
			assertLifecycleOrigin(t, pool, "observed")
			assertTableCount(t, pool, "youtube_live_reconciliation_heads", 1)

			var upcoming, started, positive *time.Time

			if err := pool.QueryRow(ctx, `SELECT head.last_upcoming_positive_at,session.started_at,head.last_live_positive_at
                FROM youtube_live_sessions session JOIN youtube_live_reconciliation_heads head USING(video_id)
                WHERE video_id=$1`, testVideoID).Scan(&upcoming, &started, &positive); err != nil {
				t.Fatal(err)
			}

			if upcoming == nil || started != nil || positive != nil {
				t.Fatalf("upcoming promotion invented start: %v/%v/%v", upcoming, started, positive)
			}

			publishConsumeLive(ctx, t, pool, repo, consumer, &proof, liveSession(testVideoID, testStatusLive))
			assertLifecycleOrigin(t, pool, "observed")
			assertTableCount(t, pool, "youtube_live_reconciliation_heads", 1)
		})
	}
}

func TestLifecycleOriginPromotionRollsBackWithHeadFailure(t *testing.T) {
	pool, repo, consumer, proof := startLivePersist(t)
	ctx := t.Context()

	if _, err := pool.Exec(ctx, `INSERT INTO youtube_live_sessions (video_id,channel_id,status,title)
        VALUES ($1,$2,'UPCOMING','')`, testVideoID, testChannelID); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `ALTER TABLE youtube_live_reconciliation_heads
        ADD CONSTRAINT reject_origin_promotion CHECK (video_id <> 'vid-a')`); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.PublishBatch(ctx, publishInput(liveSnapshotEnvelope(t, &proof, liveSession(testVideoID, testStatusLive)))); err != nil {
		t.Fatal(err)
	}

	if err := consumer.Consume(ctx, liveClaimOptions()); err == nil {
		t.Fatal("head failure must reject the owner transaction")
	}

	assertLifecycleOrigin(t, pool, "legacy_unknown")
	assertTableCount(t, pool, "youtube_live_reconciliation_heads", 0)
	assertTableCount(t, pool, "source_observation_applications", 0)
	assertTableCount(t, pool, "youtube_notification_outbox", 0)
}
