package checking

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dedup"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/tier"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
)

func TestUpcomingRecoverySeparatesCurrentProviderFromStaleCanonical(t *testing.T) {
	const unavailable = "5xx"

	for _, scenario := range []string{unavailable, "ok"} {
		t.Run(scenario, func(t *testing.T) {
			result := runStagedUpcomingCandidateCheck(t, func(*pgxpool.Pool, *domain.Stream, time.Time) (string, time.Duration) {
				return scenario, 7 * time.Minute
			})

			if scenario == unavailable {
				require.Equal(t, "pending", result.outcome)
				require.Len(t, result.recovered, 1)
				require.Equal(t, &result.selectedStart, result.recovered[0].Stream.StartScheduled)

				return
			}

			require.Equal(t, "schedule_changed", result.outcome)
			require.Empty(t, result.recovered)
		})
	}
}

func TestUpcomingRecoveryPremiereNoticeDecidesStagedCandidate(t *testing.T) {
	// 후보 선정은 now-2s다. 그 뒤 생긴 공개 예정 안내만 분 전 알림을 대신하고, 이미 받은 이른 공지는 분 전 알림을 막지 않는다.
	for name, noticeOffset := range map[string]time.Duration{
		"notice after selection":  0,
		"notice before selection": -time.Hour,
	} {
		t.Run(name, func(t *testing.T) {
			result := runStagedUpcomingCandidateCheck(t, func(pool *pgxpool.Pool, stream *domain.Stream, now time.Time) (string, time.Duration) {
				_, err := pool.Exec(t.Context(), `UPDATE youtube_live_sessions SET scheduled_start_time=$1,is_premiere=true WHERE video_id=$2`, stream.StartScheduled, stream.ID)
				require.NoError(t, err)

				_, err = pool.Exec(t.Context(), `INSERT INTO youtube_notification_outbox(kind,channel_id,content_id,payload,status,created_at,terminal_at) VALUES('NEW_VIDEO',$1,$2,'{}','SENT',$3,$3)`, stream.ChannelID, stream.ID, now.Add(noticeOffset))
				require.NoError(t, err)

				return "ok", 5 * time.Minute
			})

			if noticeOffset == 0 {
				require.Equal(t, "stream_ended", result.outcome)
				require.Empty(t, result.recovered)

				return
			}

			require.Equal(t, "pending", result.outcome)
			require.Len(t, result.recovered, 1)
			require.True(t, result.recovered[0].Stream.StartScheduled.Equal(result.selectedStart))
		})
	}
}

type stagedUpcomingCandidateResult struct {
	outcome       string
	recovered     []*domain.AlarmNotification
	selectedStart time.Time
}

// runStagedUpcomingCandidateCheck는 5분 전 후보를 staging하고 canonical 세션을 10분 뒤로 기록한 다음 Check 한 번의 결과를 돌려준다.
// 응답 시나리오와 provider 예정 시각(now 기준)은 prepare가 고른다.
func runStagedUpcomingCandidateCheck(
	t *testing.T,
	prepare func(pool *pgxpool.Pool, stream *domain.Stream, now time.Time) (providerScenario string, providerOffset time.Duration),
) stagedUpcomingCandidateResult {
	t.Helper()

	pool := dbtest.NewPool(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Second)
	selectedStart := now.Add(5 * time.Minute)
	stream := &domain.Stream{ID: "stale-fact-stream", ChannelID: "stale-fact-channel", Title: "Snapshot", Status: domain.StreamStatusUpcoming, StartScheduled: &selectedStart}
	notification := domain.NewAlarmNotification("stale-fact-room", &domain.Channel{ID: stream.ChannelID}, stream, 5, nil, "")
	store := dispatchoutbox.NewUpcomingCandidates(pool)
	require.NoError(t, store.Stage(ctx, stream.ChannelID, now.Add(-2*time.Second), []*domain.AlarmNotification{notification}))

	_, err := pool.Exec(ctx, `INSERT INTO youtube_live_sessions(video_id,channel_id,status,title,scheduled_start_time,last_seen_at,lifecycle_origin,schedule_observed_at) VALUES($1,$2,'UPCOMING','Snapshot',$3,$3,'metadata_only',$4)`, stream.ID, stream.ChannelID, now.Add(10*time.Minute), now.Add(-time.Minute))
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO alarms(room_id,channel_id,user_id,alarm_types) VALUES($1,$2,'',ARRAY['LIVE']::alarm_type[])`, notification.RoomID, stream.ChannelID)
	require.NoError(t, err)

	providerScenario, providerOffset := prepare(pool, stream, now)
	server := newYouTubeCheckerScenarioServer(t, providerScenario, stream.ChannelID, stream.ID, now.Add(providerOffset))
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

	return stagedUpcomingCandidateResult{outcome: outcome, recovered: recovered, selectedStart: selectedStart}
}
