package consume

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation/observationtest"
)

func TestQueuedPremiereClassificationRetiresStagedCandidate(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedContentWatermark(t, pool)

	repo := newTestRepository(pool)
	proof := observationtest.SeedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindVideoList, testChannelID, "youtubejs_content")
	now := time.Now().UTC().Truncate(time.Second)

	proof = observationtest.AdvanceLease(ctx, t, pool, &proof, now.Add(-time.Second).Sub(proof.ScheduledFor))

	seen := now.Add(-time.Minute)
	scheduled := now.Add(5 * time.Minute)
	_, err := pool.Exec(ctx, `INSERT INTO youtube_live_sessions(video_id,channel_id,status,title,scheduled_start_time,last_seen_at,lifecycle_origin)
 VALUES($1,$2,'UPCOMING','Candidate title',$3,$4,'metadata_only')`, testVideoID, testChannelID, scheduled, seen)
	require.NoError(t, err)

	published, err := repo.PublishBatch(ctx, publishInput(premiereVideoListEnvelope(t, &proof, scheduled)))
	require.NoError(t, err)

	selected := time.Now().UTC()

	var received time.Time

	require.NoError(t, pool.QueryRow(ctx, "SELECT received_at FROM source_observations WHERE id=$1", published.Results[0].ObservationID).Scan(&received))
	require.True(t, received.Before(selected), "content was queued before candidate selection")

	store := dispatchoutbox.NewUpcomingCandidates(pool)
	notification := domain.NewAlarmNotification("premiere-clock-room", &domain.Channel{ID: testChannelID}, &domain.Stream{
		ID: testVideoID, ChannelID: testChannelID, Title: "Candidate title", Status: domain.StreamStatusUpcoming, StartScheduled: &scheduled,
	}, 5, nil, "")
	require.NoError(t, store.Stage(ctx, testChannelID, selected, []*domain.AlarmNotification{notification}))

	consumer := NewConsumerWithGraces(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil, 0, 0)

	require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))

	var (
		actualSeen time.Time
		isPremiere bool
	)

	require.NoError(t, pool.QueryRow(ctx, `SELECT last_seen_at,is_premiere
 FROM youtube_live_sessions WHERE video_id=$1`, testVideoID).Scan(&actualSeen, &isPremiere))
	require.True(t, isPremiere)
	require.True(t, actualSeen.Equal(seen), "classification must preserve existing last_seen_at")

	pending, err := store.Pending(ctx, now.Add(time.Second))
	require.NoError(t, err)
	require.Empty(t, pending)

	var outcome string

	require.NoError(t, pool.QueryRow(ctx, "SELECT outcome FROM alarm_upcoming_candidates WHERE room_id=$1", notification.RoomID).Scan(&outcome))
	require.Equal(t, "stream_ended", outcome)
}

func TestScheduleWriterUsesObservationClockForCandidateChanges(t *testing.T) {
	for _, fresh := range []bool{false, true} {
		t.Run(map[bool]string{false: "older metadata", true: "newer metadata"}[fresh], func(t *testing.T) {
			ctx := t.Context()
			pool := dbtest.NewPool(t)
			repo := newTestRepository(pool)
			proof := observationtest.SeedPublishLease(ctx, t, pool, contract.ProviderHololiveOfficial, contract.KindSchedule, "global:hololive-schedule", "official_schedule")
			now := time.Now().UTC().Truncate(time.Second)
			observed := now.Add(-20 * time.Second)

			if fresh {
				observed = now.Add(-time.Second)
			}

			proof = observationtest.AdvanceLease(ctx, t, pool, &proof, observed.Sub(proof.ScheduledFor))

			selected := now.Add(-2 * time.Second)
			selectedStart := now.Add(5 * time.Minute)
			canonicalStart := now.Add(10 * time.Minute)
			store := dispatchoutbox.NewUpcomingCandidates(pool)
			notification := domain.NewAlarmNotification("schedule-clock-room", &domain.Channel{ID: testChannelID}, &domain.Stream{
				ID: testVideoID, ChannelID: testChannelID, Title: "Selected title", Status: domain.StreamStatusUpcoming, StartScheduled: &selectedStart,
			}, 5, nil, "")
			require.NoError(t, store.Stage(ctx, testChannelID, selected, []*domain.AlarmNotification{notification}))

			consumer := NewConsumerWithGraces(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil, 0, 0)
			published, err := repo.PublishBatch(ctx, publishInput(scheduleEnvelope(t, &proof, contract.ScheduleItemV1{
				ExternalID: testVideoID, VideoID: testVideoID, ChannelID: testChannelID, Title: "Canonical title", ScheduledAt: canonicalStart,
			})))
			require.NoError(t, err)
			require.NoError(t, consumer.Consume(ctx, liveClaimOptions()))

			var (
				lastSeen, scheduleObserved, effective time.Time
				statusObserved                        *time.Time
			)

			require.NoError(t, pool.QueryRow(ctx, `SELECT last_seen_at,schedule_observed_at,status_observed_at
 FROM youtube_live_sessions WHERE video_id=$1`, testVideoID).Scan(&lastSeen, &scheduleObserved, &statusObserved))
			require.NoError(t, pool.QueryRow(ctx, `SELECT effective_at FROM source_observation_applications
 WHERE observation_id=$1 AND entity_kind='youtube_live_session' AND entity_key=$2`, published.Results[0].ObservationID, testVideoID).Scan(&effective))
			require.True(t, lastSeen.Equal(canonicalStart), "legacy metadata timestamp contract is preserved")
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
