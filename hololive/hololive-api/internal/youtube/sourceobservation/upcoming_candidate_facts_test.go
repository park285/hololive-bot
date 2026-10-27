package sourceobservation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	publishkit "github.com/kapu/hololive-youtube-collector/testkit/sourceobservation"
)

// alarm-worker의 분 전 알림 후보(alarm_upcoming_candidates)는 이 consumer가 쓴 youtube_live_sessions 사실을 읽어
// 후보 유지 여부를 판정한다. worker 시험은 아래 canonical 상태를 SQL로 세우므로, consumer 쪽 의미는 여기서 고정한다.

// 예정 Premiere 분류는 기존 행의 last_seen_at을 바꾸지 않고 is_premiere만 세우며, 발견 시 NEW_VIDEO outbox를 만들지 않는다.
func TestPremiereClassificationKeepsCandidateFacts(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedContentWatermark(t, pool)

	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindVideoList, testChannelID, "youtubejs_content")
	now := time.Now().UTC().Truncate(time.Second)

	proof = advanceLease(ctx, t, pool, &proof, now.Add(-time.Second).Sub(proof.ScheduledFor))

	seen := now.Add(-time.Minute)
	scheduled := now.Add(5 * time.Minute)
	_, err := pool.Exec(ctx, `INSERT INTO youtube_live_sessions(video_id,channel_id,status,title,scheduled_start_time,last_seen_at,lifecycle_origin)
 VALUES($1,$2,'UPCOMING','Candidate title',$3,$4,'metadata_only')`, testVideoID, testChannelID, scheduled, seen)
	require.NoError(t, err)

	_, err = publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(premiereVideoListEnvelope(t, &proof, scheduled)))
	require.NoError(t, err)
	require.NoError(t, NewConsumerWithGraces(NewRepository(pool), 0, 0).Consume(ctx, contentClaimOptions()))

	var (
		actualSeen time.Time
		isPremiere bool
	)

	require.NoError(t, pool.QueryRow(ctx, `SELECT last_seen_at,is_premiere FROM youtube_live_sessions WHERE video_id=$1`, testVideoID).Scan(&actualSeen, &isPremiere))
	require.True(t, isPremiere)
	require.True(t, actualSeen.Equal(seen), "classification must preserve existing last_seen_at")

	var notices int

	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM youtube_notification_outbox WHERE content_id=$1", testVideoID).Scan(&notices))
	require.Zero(t, notices)
}

// schedule metadata는 관측 시각(scheduled_for)을 schedule_observed_at으로 남기고 liveness 관측 시각은 비워 둔다.
func TestScheduleMetadataRecordsObservationClockOnly(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderHololiveOfficial, contract.KindSchedule, "global:hololive-schedule", "official_schedule")
	now := time.Now().UTC().Truncate(time.Second)

	proof = advanceLease(ctx, t, pool, &proof, now.Add(-time.Second).Sub(proof.ScheduledFor))

	published, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(scheduleEnvelope(t, &proof, contract.ScheduleItemV1{
		ExternalID: testVideoID, VideoID: testVideoID, ChannelID: testChannelID, Title: "Canonical title", ScheduledAt: now.Add(10 * time.Minute),
	})))
	require.NoError(t, err)
	require.NoError(t, NewConsumerWithGraces(NewRepository(pool), 0, 0).Consume(ctx, liveClaimOptions()))

	var (
		scheduleObserved, effective time.Time
		statusObserved              *time.Time
	)

	require.NoError(t, pool.QueryRow(ctx, `SELECT schedule_observed_at,status_observed_at FROM youtube_live_sessions WHERE video_id=$1`, testVideoID).Scan(&scheduleObserved, &statusObserved))
	require.NoError(t, pool.QueryRow(ctx, `SELECT scheduled_for FROM source_observations WHERE id=$1`, published.Results[0].ObservationID).Scan(&effective))
	require.True(t, scheduleObserved.Equal(effective))
	require.Nil(t, statusObserved, "metadata does not prove observed liveness")
}
