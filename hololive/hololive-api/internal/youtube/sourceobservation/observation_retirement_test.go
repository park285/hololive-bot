package sourceobservation

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	publishkit "github.com/kapu/hololive-youtube-collector/testkit/sourceobservation"
)

// 원본 관측의 수명과 신규성 판단의 수명은 다릅니다. 원본을 지운 뒤 같은 slot과 다음 slot을 다시 받아도
// 기존 영상·clock·기준점·알림 종단 상태가 유지되어 새 알림을 만들거나 기존 알림을 재활성화하지 않아야 합니다.
func TestContentObservationDeletionDoesNotRenotify(t *testing.T) {
	for _, status := range []string{"baseline", "SENT", "FAILED"} {
		t.Run(status, func(t *testing.T) {
			ctx := t.Context()
			pool := dbtest.NewPool(t)

			if status != "baseline" {
				seedContentWatermark(t, pool)
			}

			proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindVideoList, testChannelID, "youtubejs_content")
			publisher := publishkit.NewPublisher(pool)
			consumer := NewConsumerWithGraces(NewRepository(pool), 0, 0)
			envelope := videoListEnvelope(t, &proof, contract.CompletenessComplete, testVideoID)
			published, err := publisher.PublishBatch(ctx, publishInput(envelope))
			require.NoError(t, err)
			require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))

			wantNotifications := 0

			if status != "baseline" {
				wantNotifications = 1
				_, err = pool.Exec(ctx, `UPDATE youtube_notification_outbox SET status=$1`, status)
				require.NoError(t, err)
			}

			assertTableCount(t, pool, "youtube_notification_outbox", wantNotifications)
			require.False(t, contentNoveltyPending(t, pool, testVideoID))

			before := retirementContentIdentity(t, pool)

			deleted, err := pool.Exec(ctx, `DELETE FROM source_observations WHERE id=$1`, published.Results[0].ObservationID)
			require.NoError(t, err)
			require.EqualValues(t, 1, deleted.RowsAffected())
			require.Equal(t, before, retirementContentIdentity(t, pool))

			// 삭제한 관측의 재수신과 이후 정기 수집을 모두 검사합니다.
			proof = moveContentLease(ctx, t, pool, &proof, proof.FenceEpoch+1, proof.ScheduledFor)
			_, err = publisher.PublishBatch(ctx, publishInput(videoListEnvelope(t, &proof, contract.CompletenessComplete, testVideoID)))
			require.NoError(t, err)
			require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))

			proof = advanceLease(ctx, t, pool, &proof, time.Minute)
			_, err = publisher.PublishBatch(ctx, publishInput(videoListEnvelope(t, &proof, contract.CompletenessComplete, testVideoID)))
			require.NoError(t, err)
			require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))

			assertTableCount(t, pool, "youtube_videos", 1)
			assertTableCount(t, pool, "youtube_notification_outbox", wantNotifications)
			require.Equal(t, before, retirementContentIdentity(t, pool))
		})
	}
}

func retirementContentIdentity(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()

	var identity string

	err := pool.QueryRow(t.Context(), `SELECT jsonb_build_object(
		'first_seen_at', v.first_seen_at,
		'first_positive', c.first_positive_effective_at,
		'novelty_pending', c.novelty_pending,
		'baseline', h.earliest_baseline_effective_at,
		'complete', h.earliest_complete_effective_at,
		'outbox', (SELECT jsonb_agg(jsonb_build_array(id,status) ORDER BY id) FROM youtube_notification_outbox)
	)::text FROM youtube_videos v
	JOIN youtube_content_evidence_clocks c USING(video_id)
	JOIN youtube_content_channel_heads h ON h.channel_id=v.channel_id AND h.observation_kind='video_list'
	WHERE v.video_id=$1`, testVideoID).Scan(&identity)
	require.NoError(t, err)

	return identity
}

// 관측 원본을 지워도 라이브 세션의 최초 감지·방송 식별·종단 판단은 초기화하지 않습니다.
func TestLiveObservationDeletionPreservesNotificationIdentity(t *testing.T) {
	for _, status := range []string{testStatusLive, testStatusEnded} {
		t.Run(status, func(t *testing.T) {
			pool, _, consumer, proof := startLivePersist(t)
			ctx := t.Context()
			publisher := publishkit.NewPublisher(pool)

			proof = publishConsumeLive(ctx, t, pool, publisher, consumer, &proof, liveSession(testVideoID, testStatusLive))

			if status == testStatusEnded {
				proof = publishConsumeLive(ctx, t, pool, publisher, consumer, &proof, liveSession(testVideoID, status))
			}

			before := retirementLiveIdentity(t, pool)
			_, err := pool.Exec(ctx, `DELETE FROM source_observations WHERE observation_kind='live_snapshot'`)
			require.NoError(t, err)
			require.Equal(t, before, retirementLiveIdentity(t, pool))

			publishConsumeLive(ctx, t, pool, publisher, consumer, &proof, liveSession(testVideoID, status))
			require.Equal(t, before, retirementLiveIdentity(t, pool))
			assertTableCount(t, pool, "youtube_live_sessions", 1)
			assertTableCount(t, pool, "youtube_notification_outbox", 0)
		})
	}
}

func retirementLiveIdentity(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()

	var identity string

	err := pool.QueryRow(t.Context(), `SELECT jsonb_build_array(
		s.status,s.live_first_seen_at,s.started_at,s.scheduled_start_time,s.lifecycle_origin,
		h.status,h.ended_at,h.end_reason
	)::text FROM youtube_live_sessions s JOIN youtube_live_reconciliation_heads h USING(video_id)
	WHERE s.video_id=$1`, testVideoID).Scan(&identity)
	require.NoError(t, err)

	return identity
}

// 복구는 payload와 원본을 먼저 되돌리고 NULL로 바뀐 감사 FK만 복원합니다. 발송 상태·신규성 판단은 건드리지 않습니다.
func TestObservationRetirementRestorePreservesNotificationState(t *testing.T) {
	pool, _, consumer, proof := startContentPersist(t)
	ctx := t.Context()
	published, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(videoListEnvelope(t, &proof, contract.CompletenessComplete, testVideoID)))
	require.NoError(t, err)
	require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))

	observationID := published.Results[0].ObservationID

	_, err = pool.Exec(ctx, `DELETE FROM source_observation_queue WHERE observation_id=$1 AND status='PROCESSED'`, observationID)
	require.NoError(t, err)

	var (
		observation, payload, applications []byte
		payloadID                          int64
	)

	err = pool.QueryRow(ctx, `SELECT to_jsonb(o),o.payload_id,to_jsonb(p)
		FROM source_observations o JOIN source_observation_payloads p ON p.id=o.payload_id WHERE o.id=$1`, observationID).
		Scan(&observation, &payloadID, &payload)
	require.NoError(t, err)

	err = pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(a)) FROM source_observation_applications a WHERE observation_id=$1`, observationID).Scan(&applications)
	require.NoError(t, err)

	before := retirementContentIdentity(t, pool)

	_, err = pool.Exec(ctx, `DELETE FROM source_observations WHERE id=$1`, observationID)
	require.NoError(t, err)

	deleted, err := pool.Exec(ctx, `DELETE FROM source_observation_payloads WHERE id=$1`, payloadID)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted.RowsAffected())

	_, err = pool.Exec(ctx, `INSERT INTO source_observation_payloads (id, observation_kind, schema_version, canonical_profile, payload_sha256, payload, created_at) OVERRIDING SYSTEM VALUE
		SELECT id, observation_kind, schema_version, canonical_profile, payload_sha256, payload, created_at FROM jsonb_populate_record(NULL::source_observation_payloads,$1)`, payload)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO source_observations (id, provider, observation_kind, subject_key, observation_key, schema_version, contract_generation, scheduled_for, observed_at, source_event_at, received_at, scope_sha256, completeness, continuity, evidence_sha256, collector_instance, job_key, collection_job_kind, fence_epoch, projection_generation, created_at, payload_id) OVERRIDING SYSTEM VALUE
		SELECT id, provider, observation_kind, subject_key, observation_key, schema_version, contract_generation, scheduled_for, observed_at, source_event_at, received_at, scope_sha256, completeness, continuity, evidence_sha256, collector_instance, job_key, collection_job_kind, fence_epoch, projection_generation, created_at, payload_id FROM jsonb_populate_record(NULL::source_observations,$1)`, observation)
	require.NoError(t, err)

	updated, err := pool.Exec(ctx, `UPDATE source_observation_applications a SET observation_id=b.observation_id
		FROM jsonb_populate_recordset(NULL::source_observation_applications,$1) b
		WHERE a.id=b.id AND a.observation_id IS NULL
		AND to_jsonb(a)-'observation_id'=to_jsonb(b)-'observation_id'`, applications)
	require.NoError(t, err)
	require.Positive(t, updated.RowsAffected())
	require.Equal(t, before, retirementContentIdentity(t, pool))

	var restored bool

	err = pool.QueryRow(ctx, `SELECT to_jsonb(o)=$2::jsonb AND to_jsonb(p)=$3::jsonb
		FROM source_observations o JOIN source_observation_payloads p ON p.id=o.payload_id WHERE o.id=$1`, observationID, observation, payload).Scan(&restored)
	require.NoError(t, err)
	require.True(t, restored)
}
