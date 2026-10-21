package sourceobservation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	publishkit "github.com/kapu/hololive-youtube-collector/testkit/sourceobservation"
)

// requireLiveRuntimeRefreshed는 step의 writer 트랜잭션이 커밋된 뒤 수명 모델이 원본 재계산 오라클과 일치하는지 확인한다.
// 원본 트리거가 없으므로 writer가 바꾼 key를 빠뜨리면 누락·값 변경 행으로 드러난다.
func requireLiveRuntimeRefreshed(t *testing.T, pool *pgxpool.Pool, step func()) {
	t.Helper()

	step()
	require.Equal(t, [3]int64{}, dbtest.LiveRuntimeStateDrift(t, pool), "누락·잉여·값 변경")
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

// Finalize는 writer가 보고한 key로만 갱신하고 갱신은 원본과 같은 커밋 경계를 따른다.
// 보고하지 않은 영상은 오라클이 누락으로 세므로, 경로별 drift 0이 key 수집을 증명한다.
func TestFinalizeRefreshesReportedLiveRuntimeKeys(t *testing.T) {
	for _, tc := range []struct {
		name       string
		reportAll  bool
		failCommit bool
		wantRows   int
		wantDrift  [3]int64
	}{
		{name: "reported", reportAll: true, wantRows: 2},
		{name: "unreported", wantRows: 1, wantDrift: [3]int64{1, 0, 0}},
		{name: "commit failed", reportAll: true, failCommit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			ctx := t.Context()
			batch := publishAndClaimCommunityPost(ctx, t, pool, claimOptions())

			_, err := NewRepository(pool).Finalize(ctx, batch.Claims[0].Claim(batch.ConsumerName), reportedLiveWrite(tc.reportAll, tc.failCommit))
			if tc.failCommit {
				require.ErrorContains(t, err, "fk_youtube_live_head_pending_end")
			} else {
				require.NoError(t, err)
			}

			assertTableCount(t, pool, "youtube_live_runtime_state", tc.wantRows)
			require.Equal(t, tc.wantDrift, dbtest.LiveRuntimeStateDrift(t, pool))
		})
	}
}

// reportedLiveWrite는 두 영상을 쓰고 key를 중복·미작성 영상과 함께 보고한다. 모두 보고하지 않는 경우(reportAll=false)에는 한 영상을 빠뜨린다.
// 커밋 실패 사례는 커밋 때 검사되는 deferred FK를 어기는 head를 함께 써서 갱신 뒤 커밋을 실패시킨다.
func reportedLiveWrite(reportAll, failCommit bool) ReconcileWrite {
	return func(ctx context.Context, tx dbx.Tx, _ *Observation) (ReconcileResult, error) {
		if _, err := tx.Exec(ctx, `INSERT INTO youtube_live_sessions (video_id, channel_id, status)
			VALUES ('refresh-reported', $1, 'LIVE'), ('refresh-other', $1, 'LIVE')`, testChannelID); err != nil {
			return ReconcileResult{}, fmt.Errorf("insert refreshed live sessions: %w", err)
		}

		if failCommit {
			if _, err := tx.Exec(ctx, `INSERT INTO youtube_live_reconciliation_heads
				(video_id, status, end_candidate_kind, end_candidate_observation_id, next_end_check_at)
				VALUES ('refresh-reported', 'LIVE', 'EXPLICIT_END', 999999, now())`); err != nil {
				return ReconcileResult{}, fmt.Errorf("insert dangling candidate head: %w", err)
			}
		}

		keys := []string{"refresh-reported", "refresh-reported", "refresh-unwritten"}

		if reportAll {
			keys = append(keys, "refresh-other")
		}

		return ReconcileResult{LiveRuntimeVideoIDs: keys}, nil
	}
}

func TestLiveRuntimeRefreshLiveSnapshotWriter(t *testing.T) {
	t.Parallel()

	pool, _, consumer, proof := startLivePersist(t)
	ctx := t.Context()

	requireLiveRuntimeRefreshed(t, pool, func() {
		proof = publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof, liveSession(testVideoID, testStatusUpcoming))
	})
	requireLiveRuntimeSession(t, pool, testStatusUpcoming)

	requireLiveRuntimeRefreshed(t, pool, func() {
		proof = publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof, liveSession(testVideoID, testStatusLive))
	})
	requireLiveRuntimeSession(t, pool, testStatusLive)

	// 부재 slot 두 개가 grace 0의 LIVE를 끝내며 head 부재 이력과 세션 종료를 쓴다.
	requireLiveRuntimeRefreshed(t, pool, func() {
		proof = publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof)
	})
	requireLiveRuntimeSession(t, pool, testStatusLive)

	requireLiveRuntimeRefreshed(t, pool, func() {
		publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof)
	})
	requireLiveRuntimeSession(t, pool, "")
}

func TestLiveRuntimeRefreshScheduleWriter(t *testing.T) {
	t.Parallel()

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

		requireLiveRuntimeRefreshed(t, pool, func() {
			_, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(scheduleEnvelope(t, &proof, item)))
			require.NoError(t, err)
			require.NoError(t, consumer.Consume(ctx, liveClaimOptions()))
		})
		requireLiveRuntimeSession(t, pool, testStatusUpcoming)

		proof = advanceLease(ctx, t, pool, &proof, time.Minute)
	}
}

func TestLiveRuntimeRefreshPremiereWriter(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedContentWatermark(t, pool)

	consumer := NewConsumerWithGraces(NewRepository(pool), 0, 0)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindVideoList, testChannelID, "youtubejs_content")
	scheduled := time.Date(2026, time.August, 30, 3, 0, 0, 0, time.UTC)

	requireLiveRuntimeRefreshed(t, pool, func() {
		_, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(premiereVideoListEnvelope(t, &proof, scheduled)))
		require.NoError(t, err)
		require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))
	})
	requireLiveRuntimeSession(t, pool, testStatusUpcoming)
}

func TestLiveRuntimeRefreshVideoLiveCheckWriter(t *testing.T) {
	t.Run("active session", func(t *testing.T) {
		pool, consumer, _, videoProof := startVideoLiveCheck(t)
		ctx := t.Context()

		requireLiveRuntimeRefreshed(t, pool, func() {
			publishLiveCheck(ctx, t, publishkit.NewPublisher(pool), videoLiveCheckEnvelope(t, &videoProof, liveVideoCheck(contract.VideoAvailabilityMembersOnly)))
			consumeLiveChecks(ctx, t, consumer)
		})
		require.True(t, loadLiveRuntimeRow(t, pool).availabilitySet)

		// 검증된 종료는 같은 관측에서 가용성과 세션·head 종료를 함께 쓴다.
		videoProof = advanceLease(ctx, t, pool, &videoProof, time.Minute)

		requireLiveRuntimeRefreshed(t, pool, func() {
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

		// 적용되지 않은 가용성 저장은 원본을 바꾸지 않으므로 갱신 key가 없어도 모델이 그대로 맞아야 한다.
		requireLiveRuntimeRefreshed(t, pool, func() {
			olderID = publishLiveCheck(ctx, t, publishkit.NewPublisher(pool), videoLiveCheckEnvelope(t, &older, liveVideoCheck(contract.VideoAvailabilityMembersOnly)))
			consumeLiveChecks(ctx, t, consumer)
		})
		assertApplicationDecision(t, pool, olderID, videoAvailabilityEntityKind, liveCheckDecisionNotNewer)
	})
}

func TestLiveRuntimeRefreshLiveEndFinalizer(t *testing.T) {
	t.Parallel()

	pool, repo, consumer, proof := startLivePersistGrace(t, time.Hour)
	ctx := t.Context()

	proof = publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof, liveSession(testVideoID, testStatusLive))
	publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof, liveSession(testVideoID, testStatusEnded))
	requireLiveRuntimeSession(t, pool, testStatusLive)

	_, err := pool.Exec(ctx, `UPDATE youtube_live_reconciliation_heads
		SET last_live_positive_seen_at = NOW() - INTERVAL '2 hours', next_end_check_at = NOW()
		WHERE video_id = $1`, testVideoID)
	require.NoError(t, err)

	requireLiveRuntimeRefreshed(t, pool, func() {
		processed, finalizeErr := repo.FinalizeNextDueLiveEnd(ctx, time.Hour)
		require.NoError(t, finalizeErr)
		require.True(t, processed)
	})
	requireLiveRuntimeSession(t, pool, "")
}
