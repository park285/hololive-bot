package sourceobservation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	publishkit "github.com/kapu/hololive-youtube-collector/testkit/sourceobservation"
)

type liveRuntimeKeyAuditCounts struct {
	triggerKeys float64
	missing     float64
	extra       float64
}

func liveRuntimeKeyAuditCountsOf(path string) liveRuntimeKeyAuditCounts {
	return liveRuntimeKeyAuditCounts{
		triggerKeys: testutil.ToFloat64(liveRuntimeKeyAuditTriggerKeysTotal.WithLabelValues(path)),
		missing:     testutil.ToFloat64(liveRuntimeKeyAuditMissingTotal.WithLabelValues(path)),
		extra:       testutil.ToFloat64(liveRuntimeKeyAuditExtraTotal.WithLabelValues(path)),
	}
}

func liveRuntimeKeyAuditDelta(path string, step func()) liveRuntimeKeyAuditCounts {
	before := liveRuntimeKeyAuditCountsOf(path)

	step()

	after := liveRuntimeKeyAuditCountsOf(path)

	return liveRuntimeKeyAuditCounts{
		triggerKeys: after.triggerKeys - before.triggerKeys,
		missing:     after.missing - before.missing,
		extra:       after.extra - before.extra,
	}
}

// requireLiveRuntimeKeysCovered는 step의 커밋된 writer 트랜잭션이 path로 비교됐고, 원본 트리거가 refresh한
// key가 있었으며 writer가 그 key를 하나도 빠뜨리지 않았는지 확인한다.
func requireLiveRuntimeKeysCovered(t *testing.T, path string, step func()) {
	t.Helper()

	delta := liveRuntimeKeyAuditDelta(path, step)
	require.Positive(t, delta.triggerKeys, "path %s의 비교가 트리거 갱신 key를 보지 못했습니다", path)
	require.Zero(t, delta.missing, "path %s가 트리거 갱신 key를 빠뜨렸습니다", path)
}

type liveRuntimeRow struct {
	present         bool
	sessionStatus   *string
	availabilitySet bool
}

func loadLiveRuntimeRow(t *testing.T, pool *pgxpool.Pool) liveRuntimeRow {
	t.Helper()

	var row liveRuntimeRow

	err := pool.QueryRow(t.Context(), `SELECT session_status, availability_at IS NOT NULL
		FROM youtube_live_runtime_state WHERE video_id = $1`, testVideoID).Scan(&row.sessionStatus, &row.availabilitySet)
	if errors.Is(err, pgx.ErrNoRows) {
		return row
	}

	require.NoError(t, err)

	row.present = true

	return row
}

func requireLiveRuntimeSession(t *testing.T, pool *pgxpool.Pool, want string) {
	t.Helper()

	row := loadLiveRuntimeRow(t, pool)

	if want == "" {
		require.False(t, row.present, "활성 원본이 없으면 수명 모델 행도 없어야 합니다")

		return
	}

	require.True(t, row.present)
	require.NotNil(t, row.sessionStatus)
	require.Equal(t, want, *row.sessionStatus)
}

// 비교 수단 자체가 누락과 초과를 구분해 세지 않으면 경로별 0은 아무것도 증명하지 못한다.
// 비교 뒤 커밋이 실패한 시도는 재시도에서 다시 비교되므로 세지 않는다.
func TestLiveRuntimeKeyAuditCountsCommittedMissingAndExtraKeys(t *testing.T) {
	for _, tc := range []struct {
		name       string
		failCommit bool
		want       liveRuntimeKeyAuditCounts
	}{
		{name: "committed", want: liveRuntimeKeyAuditCounts{triggerKeys: 2, missing: 1, extra: 1}},
		{name: "commit failed", failCommit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			ctx := t.Context()
			batch := publishAndClaimCommunityPost(ctx, t, pool, claimOptions())

			var err error

			delta := liveRuntimeKeyAuditDelta(string(contract.KindCommunityPage), func() {
				_, err = NewRepository(pool).Finalize(ctx, batch.Claims[0].Claim(batch.ConsumerName), unreportedLiveWrite(tc.failCommit))
			})
			require.Equal(t, tc.want, delta)

			if tc.failCommit {
				require.ErrorContains(t, err, "fk_youtube_live_head_pending_end")
				assertTableCount(t, pool, "youtube_live_runtime_state", 0)

				return
			}

			require.NoError(t, err)
			assertTableCount(t, pool, "youtube_live_runtime_state", 2)
		})
	}
}

// unreportedLiveWrite는 두 영상을 쓰고 그중 하나와 쓰지 않은 영상을 key로 보고한다. 커밋 실패 사례에서는 커밋 때
// 검사되는 deferred FK를 어기는 head를 함께 써서 비교가 끝난 뒤 커밋을 실패시킨다.
func unreportedLiveWrite(failCommit bool) ReconcileWrite {
	return func(ctx context.Context, tx dbx.Tx, _ *Observation) (ReconcileResult, error) {
		if _, err := tx.Exec(ctx, `INSERT INTO youtube_live_sessions (video_id, channel_id, status)
			VALUES ('audit-reported', $1, 'LIVE'), ('audit-unreported', $1, 'LIVE')`, testChannelID); err != nil {
			return ReconcileResult{}, fmt.Errorf("insert audited live sessions: %w", err)
		}

		if failCommit {
			if _, err := tx.Exec(ctx, `INSERT INTO youtube_live_reconciliation_heads
				(video_id, status, end_candidate_kind, end_candidate_observation_id, next_end_check_at)
				VALUES ('audit-reported', 'LIVE', 'EXPLICIT_END', 999999, now())`); err != nil {
				return ReconcileResult{}, fmt.Errorf("insert dangling candidate head: %w", err)
			}
		}

		return ReconcileResult{LiveRuntimeVideoIDs: []string{"audit-reported", "audit-reported", "audit-unwritten"}}, nil
	}
}

func TestLiveRuntimeKeyAuditLiveSnapshotWriter(t *testing.T) {
	pool, _, consumer, proof := startLivePersist(t)
	ctx := t.Context()
	path := string(contract.KindLiveSnapshot)

	requireLiveRuntimeKeysCovered(t, path, func() {
		proof = publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof, liveSession(testVideoID, testStatusUpcoming))
	})
	requireLiveRuntimeSession(t, pool, testStatusUpcoming)

	requireLiveRuntimeKeysCovered(t, path, func() {
		proof = publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof, liveSession(testVideoID, testStatusLive))
	})
	requireLiveRuntimeSession(t, pool, testStatusLive)

	// 부재 slot 두 개가 grace 0의 LIVE를 끝내며 head 부재 이력과 세션 종료를 쓴다.
	requireLiveRuntimeKeysCovered(t, path, func() {
		proof = publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof)
	})
	requireLiveRuntimeSession(t, pool, testStatusLive)

	requireLiveRuntimeKeysCovered(t, path, func() {
		publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof)
	})
	requireLiveRuntimeSession(t, pool, "")
}

func TestLiveRuntimeKeyAuditScheduleWriter(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	consumer := NewConsumerWithGraces(NewRepository(pool), 0, 0)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderHololiveOfficial, contract.KindSchedule, "global:hololive-schedule", "official_schedule")
	item := contract.ScheduleItemV1{
		ExternalID: testVideoID, VideoID: testVideoID, ChannelID: testChannelID, Title: "Official",
		ScheduledAt: time.Date(2026, time.August, 14, 9, 0, 0, 0, time.UTC),
	}

	for _, title := range []string{"Official", "Official retitled"} {
		item.Title = title

		requireLiveRuntimeKeysCovered(t, string(contract.KindSchedule), func() {
			_, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(scheduleEnvelope(t, &proof, item)))
			require.NoError(t, err)
			require.NoError(t, consumer.Consume(ctx, liveClaimOptions()))
		})
		requireLiveRuntimeSession(t, pool, testStatusUpcoming)

		proof = advanceLease(ctx, t, pool, &proof, time.Minute)
	}
}

func TestLiveRuntimeKeyAuditPremiereWriter(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedContentWatermark(t, pool)

	consumer := NewConsumerWithGraces(NewRepository(pool), 0, 0)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindVideoList, testChannelID, "youtubejs_content")
	scheduled := time.Date(2026, time.August, 30, 3, 0, 0, 0, time.UTC)

	requireLiveRuntimeKeysCovered(t, string(contract.KindVideoList), func() {
		_, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(premiereVideoListEnvelope(t, &proof, scheduled)))
		require.NoError(t, err)
		require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))
	})
	requireLiveRuntimeSession(t, pool, testStatusUpcoming)
}

func TestLiveRuntimeKeyAuditVideoLiveCheckWriter(t *testing.T) {
	path := string(contract.KindVideoLiveCheck)

	t.Run("active session", func(t *testing.T) {
		pool, consumer, _, videoProof := startVideoLiveCheck(t)
		ctx := t.Context()

		requireLiveRuntimeKeysCovered(t, path, func() {
			publishLiveCheck(ctx, t, publishkit.NewPublisher(pool), videoLiveCheckEnvelope(t, &videoProof, liveVideoCheck(contract.VideoAvailabilityMembersOnly)))
			consumeLiveChecks(ctx, t, consumer)
		})
		require.True(t, loadLiveRuntimeRow(t, pool).availabilitySet)

		// 검증된 종료는 같은 관측에서 가용성과 세션·head 종료를 함께 쓴다.
		videoProof = advanceLease(ctx, t, pool, &videoProof, time.Minute)

		requireLiveRuntimeKeysCovered(t, path, func() {
			publishLiveCheck(ctx, t, publishkit.NewPublisher(pool), videoLiveCheckEnvelope(t, &videoProof, endedVideoCheck(videoProof.ScheduledFor)))
			consumeLiveChecks(ctx, t, consumer)
		})
		requireLiveRuntimeSession(t, pool, "")
	})

	t.Run("availability not newer", func(t *testing.T) {
		pool, consumer, _, videoProof := startVideoLiveCheck(t)
		ctx := t.Context()
		newer := advanceLease(ctx, t, pool, &videoProof, 2*time.Minute)

		publishLiveCheck(ctx, t, publishkit.NewPublisher(pool), videoLiveCheckEnvelope(t, &newer, liveVideoCheck(contract.VideoAvailabilityPublic)))
		consumeLiveChecks(ctx, t, consumer)

		older := advanceLease(ctx, t, pool, &newer, -time.Minute)

		var olderID int64

		// 적용되지 않은 가용성 저장은 원본을 바꾸지 않으므로 트리거도 writer도 key를 내지 않는다.
		delta := liveRuntimeKeyAuditDelta(path, func() {
			olderID = publishLiveCheck(ctx, t, publishkit.NewPublisher(pool), videoLiveCheckEnvelope(t, &older, liveVideoCheck(contract.VideoAvailabilityMembersOnly)))
			consumeLiveChecks(ctx, t, consumer)
		})
		require.Equal(t, liveRuntimeKeyAuditCounts{}, delta)
		assertApplicationDecision(t, pool, olderID, videoAvailabilityEntityKind, liveCheckDecisionNotNewer)
	})

	t.Run("ended time finalization", func(t *testing.T) {
		h := startFinalization(t, nil)
		slot := h.proof.ScheduledFor
		start, end := slot.Add(-2*time.Hour-time.Minute), slot.Add(-22*time.Minute)

		requireLiveRuntimeKeysCovered(t, path, func() {
			h.observe(t, archiveVideoCheck(start, end))
		})

		got := loadFinalizationRow(t, h.pool)
		require.Equal(t, providerTimesSource, got.timesSource)
		require.True(t, sameTime(got.startedAt, start) && sameTime(got.endedAt, end))
	})
}

func TestLiveRuntimeKeyAuditLiveEndFinalizer(t *testing.T) {
	pool, repo, consumer, proof := startLivePersistGrace(t, time.Hour)
	ctx := t.Context()

	proof = publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof, liveSession(testVideoID, testStatusLive))
	publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof, liveSession(testVideoID, testStatusEnded))
	requireLiveRuntimeSession(t, pool, testStatusLive)

	_, err := pool.Exec(ctx, `UPDATE youtube_live_reconciliation_heads
		SET last_live_positive_seen_at = NOW() - INTERVAL '2 hours', next_end_check_at = NOW()
		WHERE video_id = $1`, testVideoID)
	require.NoError(t, err)

	requireLiveRuntimeKeysCovered(t, liveEndFinalizerKeyAuditPath, func() {
		processed, finalizeErr := repo.FinalizeNextDueLiveEnd(ctx, time.Hour)
		require.NoError(t, finalizeErr)
		require.True(t, processed)
	})
	requireLiveRuntimeSession(t, pool, "")
}
