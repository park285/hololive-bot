package sourceobservation

import (
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/schedule"
	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestScheduleBatchesPreserveItemBeforeSessionAndDuplicateOrder(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	observation, decision := scheduleBatchFixture(129)

	// 첫 session 저장 전에 모든 item이 같은 트랜잭션에서 보여야 한다.
	_, err := pool.Exec(ctx, `
		CREATE FUNCTION assert_schedule_items_ready() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF (SELECT count(*) FROM youtube_schedule_items) <> 129 THEN
				RAISE EXCEPTION 'schedule sessions preceded items';
			END IF;
			RETURN NEW;
		END $$;
		CREATE TRIGGER schedule_items_ready BEFORE INSERT ON youtube_live_sessions
		FOR EACH ROW EXECUTE FUNCTION assert_schedule_items_ready()
	`)
	require.NoError(t, err)

	// 같은 시각의 중복은 최초 항목을 유지한다. 중복을 bulk upsert 한 문장으로 합치면
	// PostgreSQL cardinality 오류가 나므로 실제 저장 결과로 순차 문장 계약을 확인한다.
	duplicate := decision.Items[0]

	duplicate.Title = "must not replace first"
	decision.Items = append(decision.Items, duplicate)

	duplicateSession := decision.Sessions[0]

	duplicateSession.Title = duplicate.Title
	decision.Sessions = append(decision.Sessions, duplicateSession, schedule.Session{VideoID: "no-channel"})

	require.NoError(t, dbx.InPgxTx(ctx, pool, func(tx dbx.Tx) error {
		return persistScheduleDecision(ctx, tx, &observation, &decision)
	}))
	assertTableCount(t, pool, "youtube_schedule_items", 129)
	assertTableCount(t, pool, "youtube_live_sessions", 129)
	assertTableCount(t, pool, "youtube_live_reconciliation_heads", 0)

	var title, sessionTitle, origin string

	var names []string

	var observed, titleObserved, lastSeen time.Time

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT item.title, item.collabo_talent_names, item.observed_at,
		       session.title, session.title_observed_at, session.last_seen_at, session.lifecycle_origin
		FROM youtube_schedule_items item JOIN youtube_live_sessions session USING(video_id)
		WHERE item.external_id = $1
	`, decision.Items[0].ExternalID).Scan(&title, &names, &observed, &sessionTitle, &titleObserved, &lastSeen, &origin))
	require.Equal(t, "Title 0", title)
	require.Equal(t, title, sessionTitle)
	require.Empty(t, names)
	require.NotNil(t, names)
	require.True(t, observed.Equal(observation.EffectiveAt))
	require.True(t, titleObserved.Equal(observation.EffectiveAt))
	require.True(t, lastSeen.Equal(decision.Sessions[0].LastSeenAt))
	require.Equal(t, "metadata_only", origin)
}

func TestScheduleBatchFailureRollsBackAllItemsAndSessions(t *testing.T) {
	for _, target := range []string{"youtube_schedule_items", "youtube_live_sessions"} {
		t.Run(target, func(t *testing.T) {
			ctx := t.Context()
			pool := dbtest.NewPool(t)
			observation, decision := scheduleBatchFixture(260)

			// 선행 배치가 성공해도 후속 item/session 실패 시 전체를 롤백한다.
			_, err := pool.Exec(ctx, `
				CREATE FUNCTION fail_schedule_batch() RETURNS trigger LANGUAGE plpgsql AS $$
				BEGIN
					IF NEW.video_id = 'schedule-batch-0259' THEN
						RAISE EXCEPTION 'schedule batch rejected' USING ERRCODE = '23514';
					END IF;
					RETURN NEW;
				END $$
			`)
			require.NoError(t, err)

			_, err = pool.Exec(ctx, "CREATE TRIGGER reject_schedule_batch BEFORE INSERT OR UPDATE ON "+target+
				" FOR EACH ROW EXECUTE FUNCTION fail_schedule_batch()")
			require.NoError(t, err)

			err = dbx.InPgxTx(ctx, pool, func(tx dbx.Tx) error {
				return persistScheduleDecision(ctx, tx, &observation, &decision)
			})

			var pgErr *pgconn.PgError

			require.ErrorAs(t, err, &pgErr)
			require.Equal(t, "23514", pgErr.Code)
			assertTableCount(t, pool, "youtube_schedule_items", 0)
			assertTableCount(t, pool, "youtube_live_sessions", 0)

			_, err = pool.Exec(ctx, "DROP TRIGGER reject_schedule_batch ON "+target)
			require.NoError(t, err)
			require.NoError(t, dbx.InPgxTx(ctx, pool, func(tx dbx.Tx) error {
				return persistScheduleDecision(ctx, tx, &observation, &decision)
			}))
			assertTableCount(t, pool, "youtube_schedule_items", 260)
			assertTableCount(t, pool, "youtube_live_sessions", 260)
		})
	}
}

func scheduleBatchFixture(count int) (Observation, schedule.Decision) {
	observed := time.Date(2026, time.October, 5, 1, 0, 0, 0, time.UTC)
	observation := Observation{Provider: contract.ProviderHololiveOfficial, EffectiveAt: observed}
	decision := schedule.Decision{
		Items:    make([]schedule.Item, count),
		Sessions: make([]schedule.Session, count),
	}

	for i := range count {
		videoID := fmt.Sprintf("schedule-batch-%04d", i)

		decision.Items[i] = schedule.Item{
			GroupKey: "schedule-batch", ExternalID: videoID, VideoID: videoID,
			ChannelID: testChannelID, Title: fmt.Sprintf("Title %d", i), ScheduledAt: observed.Add(time.Hour),
		}
		decision.Sessions[i] = schedule.Session{
			VideoID: videoID, ChannelID: testChannelID, Status: domain.LiveStatusUpcoming,
			Title: decision.Items[i].Title, ScheduledStartTime: new(decision.Items[i].ScheduledAt),
			LastSeenAt: observed.Add(time.Second), ScheduleObservedAt: new(observed), TitleObservedAt: new(observed),
		}
	}

	return observation, decision
}
