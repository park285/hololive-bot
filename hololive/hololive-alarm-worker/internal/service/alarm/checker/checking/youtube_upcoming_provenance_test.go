package checking

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/tier"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dedup"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
)

func TestUpcomingRecoverySeparatesCurrentProviderFromStaleCanonical(t *testing.T) {
	const unavailable = "5xx"

	for _, scenario := range []string{unavailable, "ok", "premiere"} {
		t.Run(scenario, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			ctx := t.Context()
			now := time.Now().UTC().Truncate(time.Second)
			selectedStart := now.Add(5 * time.Minute)
			canonicalStart := now.Add(10 * time.Minute)
			providerStart := now.Add(7 * time.Minute)
			stream := &domain.Stream{ID: "stale-fact-stream", ChannelID: "stale-fact-channel", Title: "Snapshot", Status: domain.StreamStatusUpcoming, StartScheduled: &selectedStart}
			notification := domain.NewAlarmNotification("stale-fact-room", &domain.Channel{ID: stream.ChannelID}, stream, 5, nil, "")
			store := dispatchoutbox.NewUpcomingCandidates(pool)
			require.NoError(t, store.Stage(ctx, stream.ChannelID, now.Add(-2*time.Second), []*domain.AlarmNotification{notification}))

			_, err := pool.Exec(ctx, `INSERT INTO youtube_live_sessions(video_id,channel_id,status,title,scheduled_start_time,last_seen_at,lifecycle_origin) VALUES($1,$2,'UPCOMING','Snapshot',$3,$3,'metadata_only')`, stream.ID, stream.ChannelID, canonicalStart)
			require.NoError(t, err)

			_, err = pool.Exec(ctx, `INSERT INTO alarms(room_id,channel_id,user_id,alarm_types) VALUES($1,$2,'',ARRAY['LIVE']::alarm_type[])`, notification.RoomID, stream.ChannelID)
			require.NoError(t, err)

			providerScenario := scenario
			if scenario == "premiere" {
				providerScenario = "ok"
				providerStart = selectedStart
				_, err = pool.Exec(ctx, `UPDATE youtube_live_sessions SET scheduled_start_time=$1,is_premiere=true WHERE video_id=$2`, selectedStart, stream.ID)
				require.NoError(t, err)
			}

			server := newYouTubeCheckerScenarioServer(t, providerScenario, stream.ChannelID, stream.ID, providerStart)
			t.Cleanup(server.Close)

			cacheClient := newCheckerTestCacheClient(t)
			logger := newCheckerTestLogger()
			provider, err := holodexprovider.NewHolodexService(server.URL, "test-key", cacheClient, nil, logger)
			require.NoError(t, err)

			checker, err := NewYouTubeCheckerWithPersistedLiveSource(cacheClient, provider, tier.NewTieredScheduler(logger), dedup.NewService(cacheClient, []int{5}, logger), []int{5}, 0, newPgYouTubeLiveSessionSource(pool, PgYouTubeLiveSessionSourceOptions{}), pool, logger)
			require.NoError(t, err)

			_, err = cacheClient.SAdd(ctx, sharedalarmkeys.AlarmChannelRegistryKey, []string{stream.ChannelID})
			require.NoError(t, err)

			recovered, err := checker.Check(ctx)
			require.NoError(t, err)

			var outcome string

			require.NoError(t, pool.QueryRow(ctx, "SELECT outcome FROM alarm_upcoming_candidates WHERE room_id=$1", notification.RoomID).Scan(&outcome))

			if scenario == unavailable {
				require.Equal(t, "pending", outcome)
				require.Len(t, recovered, 1)
				require.Equal(t, &selectedStart, recovered[0].Stream.StartScheduled)
			} else {
				expected := "schedule_changed"

				if scenario == "premiere" {
					expected = "stream_ended"
				}

				require.Equal(t, expected, outcome)
				require.Empty(t, recovered)
			}
		})
	}
}
