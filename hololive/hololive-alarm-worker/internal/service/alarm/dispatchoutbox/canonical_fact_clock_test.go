package dispatchoutbox_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	consumekit "github.com/kapu/hololive-api/testkit/sourceobservation"
	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
	publishkit "github.com/kapu/hololive-youtube-collector/testkit/sourceobservation"
)

func TestQueuedPremiereClassificationKeepsStagedCandidate(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	clockSeedContentWatermark(t, pool)

	repo := publishkit.NewPublisher(pool)
	proof := clockSeedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindVideoList, clockChannelID, "youtubejs_content")
	now := time.Now().UTC().Truncate(time.Second)

	proof = clockAdvanceLease(ctx, t, pool, &proof, now.Add(-time.Second).Sub(proof.ScheduledFor))

	seen := now.Add(-time.Minute)
	scheduled := now.Add(5 * time.Minute)
	_, err := pool.Exec(ctx, `INSERT INTO youtube_live_sessions(video_id,channel_id,status,title,scheduled_start_time,last_seen_at,lifecycle_origin)
 VALUES($1,$2,'UPCOMING','Candidate title',$3,$4,'metadata_only')`, clockVideoID, clockChannelID, scheduled, seen)
	require.NoError(t, err)

	published, err := repo.PublishBatch(ctx, clockPublishInput(clockPremiereVideoListEnvelope(t, &proof, scheduled)))
	require.NoError(t, err)

	selected := time.Now().UTC()

	var received time.Time

	require.NoError(t, pool.QueryRow(ctx, "SELECT received_at FROM source_observations WHERE id=$1", published.Results[0].ObservationID).Scan(&received))
	require.True(t, received.Before(selected), "content was queued before candidate selection")

	store := dispatchoutbox.NewUpcomingCandidates(pool)
	notification := domain.NewAlarmNotification("premiere-clock-room", &domain.Channel{ID: clockChannelID}, &domain.Stream{
		ID: clockVideoID, ChannelID: clockChannelID, Title: "Candidate title", Status: domain.StreamStatusUpcoming, StartScheduled: &scheduled,
	}, 5, nil, "")
	require.NoError(t, store.Stage(ctx, clockChannelID, selected, []*domain.AlarmNotification{notification}))

	consumer := consumekit.NewConsumer(pool, 0, 0)

	require.NoError(t, consumer.Consume(ctx, clockContentClaimOptions()))

	var (
		actualSeen time.Time
		isPremiere bool
	)

	require.NoError(t, pool.QueryRow(ctx, `SELECT last_seen_at,is_premiere
 FROM youtube_live_sessions WHERE video_id=$1`, clockVideoID).Scan(&actualSeen, &isPremiere))
	require.True(t, isPremiere)
	require.True(t, actualSeen.Equal(seen), "classification must preserve existing last_seen_at")

	// 예정 Premiere는 발견 시 NEW_VIDEO를 만들지 않으므로 분 전 알림이 유일한 공개 전 알림이다.
	var notices int

	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM youtube_notification_outbox WHERE content_id=$1", clockVideoID).Scan(&notices))
	require.Zero(t, notices)

	pending, err := store.Pending(ctx, now.Add(time.Second))
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, "pending", pending[0].Outcome)
}

func TestScheduleWriterUsesObservationClockForCandidateChanges(t *testing.T) {
	for _, fresh := range []bool{false, true} {
		t.Run(map[bool]string{false: "older metadata", true: "newer metadata"}[fresh], func(t *testing.T) {
			ctx := t.Context()
			pool := dbtest.NewPool(t)
			repo := publishkit.NewPublisher(pool)
			proof := clockSeedPublishLease(ctx, t, pool, contract.ProviderHololiveOfficial, contract.KindSchedule, "global:hololive-schedule", "official_schedule")
			now := time.Now().UTC().Truncate(time.Second)
			observed := now.Add(-20 * time.Second)

			if fresh {
				observed = now.Add(-time.Second)
			}

			proof = clockAdvanceLease(ctx, t, pool, &proof, observed.Sub(proof.ScheduledFor))

			selected := now.Add(-2 * time.Second)
			selectedStart := now.Add(5 * time.Minute)
			canonicalStart := now.Add(10 * time.Minute)
			store := dispatchoutbox.NewUpcomingCandidates(pool)
			notification := domain.NewAlarmNotification("schedule-clock-room", &domain.Channel{ID: clockChannelID}, &domain.Stream{
				ID: clockVideoID, ChannelID: clockChannelID, Title: "Selected title", Status: domain.StreamStatusUpcoming, StartScheduled: &selectedStart,
			}, 5, nil, "")
			require.NoError(t, store.Stage(ctx, clockChannelID, selected, []*domain.AlarmNotification{notification}))

			consumer := consumekit.NewConsumer(pool, 0, 0)
			published, err := repo.PublishBatch(ctx, clockPublishInput(clockScheduleEnvelope(t, &proof, contract.ScheduleItemV1{
				ExternalID: clockVideoID, VideoID: clockVideoID, ChannelID: clockChannelID, Title: "Canonical title", ScheduledAt: canonicalStart,
			})))
			require.NoError(t, err)
			require.NoError(t, consumer.Consume(ctx, clockLiveClaimOptions()))

			var (
				scheduleObserved, effective time.Time
				statusObserved              *time.Time
			)

			require.NoError(t, pool.QueryRow(ctx, `SELECT schedule_observed_at,status_observed_at
 FROM youtube_live_sessions WHERE video_id=$1`, clockVideoID).Scan(&scheduleObserved, &statusObserved))
			require.NoError(t, pool.QueryRow(ctx, `SELECT o.scheduled_for FROM source_observation_applications a
 JOIN source_observations o ON o.id=a.observation_id
 WHERE a.observation_id=$1 AND a.entity_kind_id=(SELECT kind.id FROM source_observation_entity_kinds AS kind WHERE kind.name = 'youtube_live_session') AND a.entity_key=$2`, published.Results[0].ObservationID, clockVideoID).Scan(&effective))
			require.True(t, scheduleObserved.Equal(effective))
			require.Nil(t, statusObserved, "metadata does not prove observed liveness")

			pending, err := store.Pending(ctx, now.Add(time.Second))
			require.NoError(t, err)

			var outcome string

			require.NoError(t, pool.QueryRow(ctx, "SELECT outcome FROM alarm_upcoming_candidates WHERE room_id=$1", notification.RoomID).Scan(&outcome))

			if fresh {
				require.Empty(t, pending)
				require.Equal(t, "schedule_changed", outcome)
			} else {
				require.Len(t, pending, 1)
				require.Equal(t, "pending", outcome)
			}
		})
	}
}
