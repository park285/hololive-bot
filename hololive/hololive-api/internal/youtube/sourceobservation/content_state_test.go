package sourceobservation

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/content"
	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	publishkit "github.com/kapu/hololive-youtube-collector/testkit/sourceobservation"
)

type contentLoadStep struct {
	label        string
	at           time.Time
	completeness contract.Completeness
	videoIDs     []string
}

// publishConsumeContentAt은 lease slot을 임의 시각으로 옮겨 관측을 발행하고 바로 소비한다.
// 관측의 received_at을 slot 시각에 맞춰 도착 순서와 무관하게 grace 판정이 관측 시각을 따르게 한다.
func publishConsumeContentAt(
	ctx context.Context,
	t *testing.T,
	pool *pgxpool.Pool,
	repo *Repository,
	base *contract.LeaseProof,
	epoch int64,
	step *contentLoadStep,
) {
	t.Helper()

	proof := *base

	proof.FenceEpoch = epoch
	proof.ScheduledFor = step.at

	if _, err := pool.Exec(ctx, `
		UPDATE youtube_collection_job_leases
		SET slot_state = 'ACTIVE', owner_instance = $2, lease_expires_at = NOW() + INTERVAL '1 hour',
		    retry_not_before = NULL, fence_epoch = $3, scheduled_for = $4, next_due_at = $4
		WHERE job_key = $1
	`, proof.JobKey, proof.OwnerInstance, proof.FenceEpoch, proof.ScheduledFor); err != nil {
		t.Fatalf("move lease to %s: %v", step.label, err)
	}

	published, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(videoListEnvelope(t, &proof, step.completeness, step.videoIDs...)))
	if err != nil || len(published.Results) != 1 {
		t.Fatalf("publish %s: result=%#v err=%v", step.label, published, err)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE source_observations SET received_at = scheduled_for + INTERVAL '1 second' WHERE id = $1
	`, published.Results[0].ObservationID); err != nil {
		t.Fatalf("align received_at for %s: %v", step.label, err)
	}

	if err := NewConsumerWithAbsenceGrace(repo, 0).Consume(ctx, contentClaimOptions()); err != nil {
		t.Fatalf("consume %s: %v", step.label, err)
	}
}

// contentConvergenceSnapshot은 reducer 순열 수렴 계약(reduce_helpers_test.go snapshotDecision)과 같은 투영을 DB에서 읽는다.
func contentConvergenceSnapshot(ctx context.Context, t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()

	rows, err := pool.Query(ctx, `
		SELECT video_id, missing_since_effective_at IS NOT NULL, withdrawn_at IS NOT NULL, consecutive_absence_slots
		FROM youtube_content_evidence_clocks
		ORDER BY video_id
	`)
	require.NoError(t, err)

	defer rows.Close()

	var parts []string

	for rows.Next() {
		var (
			videoID            string
			missing, withdrawn bool
			slots              int
		)

		require.NoError(t, rows.Scan(&videoID, &missing, &withdrawn, &slots))

		parts = append(parts, fmt.Sprintf("%s|missing=%t|withdrawn=%t|slots=%d", videoID, missing, withdrawn, slots))
	}

	require.NoError(t, rows.Err())

	return strings.Join(parts, ";")
}

func contentLoadPermutations(steps []contentLoadStep) [][]contentLoadStep {
	if len(steps) == 0 {
		return [][]contentLoadStep{{}}
	}

	var result [][]contentLoadStep

	for i := range steps {
		rest := slices.Concat(steps[:i:i], steps[i+1:])

		for _, perm := range contentLoadPermutations(rest) {
			result = append(result, append([]contentLoadStep{steps[i]}, perm...))
		}
	}

	return result
}

func contentStepLabels(steps []contentLoadStep) []string {
	labels := make([]string, len(steps))
	for i := range steps {
		labels[i] = steps[i].label
	}

	return labels
}

// 로더는 현재 slot과 이번 관측보다 늦은 slot, 관측·clock 보유 영상만 적재한다. 늦게 도착한 positive가
// 먼저 저장된 부재 slot을 scheduled_for 순으로 재적용해야 하므로 모든 도착 순서가 같은 상태로 수렴해야 한다.
func TestContentLoaderScopePermutationsConverge(t *testing.T) {
	base := time.Date(2026, time.August, 14, 1, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		steps []contentLoadStep
		want  string
	}{
		{
			name: "positive_then_two_complete_negatives",
			steps: []contentLoadStep{
				{label: "P1", at: base, completeness: contract.CompletenessComplete, videoIDs: []string{"vid-a"}},
				{label: "N2", at: base.Add(time.Hour), completeness: contract.CompletenessComplete},
				{label: "N3", at: base.Add(2 * time.Hour), completeness: contract.CompletenessComplete},
			},
			want: "vid-a|missing=true|withdrawn=true|slots=2",
		},
		{
			name: "late_positive_clears_only_the_newer_video",
			steps: []contentLoadStep{
				{label: "P1", at: base, completeness: contract.CompletenessComplete, videoIDs: []string{"vid-a", "vid-b"}},
				{label: "N2", at: base.Add(time.Hour), completeness: contract.CompletenessComplete},
				{label: "P4", at: base.Add(3 * time.Hour), completeness: contract.CompletenessComplete, videoIDs: []string{"vid-b"}},
			},
			want: "vid-a|missing=true|withdrawn=true|slots=2;vid-b|missing=false|withdrawn=false|slots=0",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, order := range contentLoadPermutations(tc.steps) {
				ctx := t.Context()
				pool := dbtest.NewPool(t)
				seedContentWatermark(t, pool)

				repo := NewRepository(pool)
				proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindVideoList, testChannelID, "youtubejs_content")

				for i := range order {
					publishConsumeContentAt(ctx, t, pool, repo, &proof, proof.FenceEpoch+int64(i)+1, &order[i])
				}

				if got := contentConvergenceSnapshot(ctx, t, pool); got != tc.want {
					t.Fatalf("order %v diverged\n got %s\nwant %s", contentStepLabels(order), got, tc.want)
				}
			}
		})
	}
}

// heap 삽입 순서와 index 순서에 기대지 않고, 늦은 positive가 부재 slot을 시간순으로 재적용해야 한다.
func TestContentLoaderReplaysAbsenceSlotsInScheduledOrder(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	base := time.Date(2026, time.August, 14, 1, 0, 0, 0, time.UTC)
	first, second := base.Add(time.Hour), base.Add(2*time.Hour)
	coverage := contract.ChannelListCoverageV1{ChannelID: testChannelID, MaxResults: 10, Exhausted: true}
	rawCoverage, err := contract.MarshalPayloadV1(coverage)
	require.NoError(t, err)

	// 두 번째 부재를 먼저 저장해 순서 없는 sequential scan은 역순을 반환하게 한다.
	for _, at := range []time.Time{second, first} {
		_, err = pool.Exec(ctx, `
			INSERT INTO youtube_content_absence_slots (
				channel_id, observation_kind, scheduled_for, evidence_sha256, effective_at, received_at, scope_sha256, coverage
			) VALUES ($1, 'video_list', $2, $3, $2, $2, $3, $4)
		`, testChannelID, at, strings.Repeat("ee", 32), rawCoverage)
		require.NoError(t, err)
	}

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, tx.Rollback(context.WithoutCancel(ctx)))
	})

	_, err = tx.Exec(ctx, `SET LOCAL enable_indexscan = off; SET LOCAL enable_indexonlyscan = off; SET LOCAL enable_bitmapscan = off`)
	require.NoError(t, err)

	state := content.State{ChannelID: testChannelID, Kind: contract.KindVideoList}
	evidence := content.Evidence{
		Kind:         contract.KindVideoList,
		ScheduledFor: base,
		EffectiveAt:  base,
		ReceivedAt:   base,
		Completeness: contract.CompletenessPartial,
		Videos:       []content.Entity{{VideoID: testVideoID, ChannelID: testChannelID, Title: "late positive"}},
		Coverage:     content.CoverageValue{Videos: &coverage},
	}
	require.NoError(t, loadContentAbsenceSlots(ctx, tx, &state, &evidence))

	decision, err := content.Reduce(state, evidence, time.Hour)
	require.NoError(t, err)
	require.Len(t, decision.Clocks, 1)

	clock := decision.Clocks[0]
	require.Equal(t, testVideoID, clock.VideoID)
	require.NotNil(t, clock.FirstAbsenceScheduledFor)
	require.NotNil(t, clock.SecondAbsenceScheduledFor)
	require.NotNil(t, clock.Clock.MissingSinceEffectiveAt)
	require.NotNil(t, clock.Clock.LastNegativeEffectiveAt)
	require.Equal(t, first, clock.FirstAbsenceScheduledFor.UTC())
	require.Equal(t, second, clock.SecondAbsenceScheduledFor.UTC())
	require.Equal(t, first, clock.Clock.MissingSinceEffectiveAt.UTC())
	require.Equal(t, second, clock.Clock.LastNegativeEffectiveAt.UTC())
	require.Equal(t, 2, clock.ConsecutiveAbsenceSlots)
	require.NotNil(t, clock.WithdrawnAt)
	require.Equal(t, second, clock.WithdrawnAt.UTC())
}

// 부재 판정에 쓰이지 않는 이력(clock 없는 레거시 영상, 이번 관측보다 이른 slot)은 적재하거나 잠그지 않는다.
// FOR UPDATE는 커밋 뒤에도 튜플 xmax에 잠금 트랜잭션을 남기므로 xmax=0이 잠금 부재의 증거다.
// 방문 행 plan 테스트는 두지 않는다. PK(channel_id, observation_kind, scheduled_for)에 effective_at이 없어
// 0038은 PK prefix로 채널·종류의 slot 이력을 모두 읽은 뒤 조건으로 거르므로 heap 방문은 여전히 이력에 비례한다.
// 0038 변경이 줄이는 것은 잠금·적재 범위이므로 그 범위를 xmax로 고정한다.
func TestContentLoaderLeavesUnusedHistoryUnlocked(t *testing.T) {
	pool, _, consumer, proof := startContentPersist(t)
	ctx := t.Context()

	_, err := pool.Exec(ctx, `
		INSERT INTO youtube_videos (video_id, channel_id, title, first_seen_at, last_seen_at)
		SELECT 'legacy-' || n, $1, 'legacy-' || n, TIMESTAMPTZ '2026-07-01 00:00:00Z', TIMESTAMPTZ '2026-07-01 00:00:00Z'
		FROM generate_series(1, 20) AS n
	`, testChannelID)
	require.NoError(t, err)

	coverage, err := contract.MarshalPayloadV1(contract.ChannelListCoverageV1{ChannelID: testChannelID, MaxResults: 10, Exhausted: true})
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO youtube_content_absence_slots (
			channel_id, observation_kind, scheduled_for, evidence_sha256, effective_at, received_at, scope_sha256, coverage
		)
		SELECT $1, 'video_list', at, $2, at, at, $2, $3
		FROM generate_series(TIMESTAMPTZ '2026-08-01 00:00:00Z', TIMESTAMPTZ '2026-08-01 19:00:00Z', INTERVAL '1 hour') AS at
	`, testChannelID, strings.Repeat("ee", 32), coverage)
	require.NoError(t, err)

	publishConsumeVideos(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof, contract.CompletenessComplete, testVideoID)

	var lockedVideos, lockedSlots, slots int

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(video_id) FILTER (WHERE xmax::text <> '0')
		FROM youtube_videos
		WHERE video_id LIKE 'legacy-%'
	`).Scan(&lockedVideos))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(scheduled_for) FILTER (WHERE xmax::text <> '0'), count(scheduled_for)
		FROM youtube_content_absence_slots
		WHERE scheduled_for < TIMESTAMPTZ '2026-08-14 00:00:00Z'
	`).Scan(&lockedSlots, &slots))

	require.Zero(t, lockedVideos, "legacy videos without clocks must not be locked")
	require.Equal(t, 20, slots)
	require.Zero(t, lockedSlots, "absence slots older than the observation must not be locked")
	assertContentMissing(t, pool, testVideoID, false)
}

// 로더가 clock 없는 레거시 shorts를 싣지 않아도 initialized watermark는 기준 목록으로 유지돼 새 shorts를 알린다
// (빈 PARTIAL watermark를 되돌리던 reducer 분기는 stack-audit 2026-09-26 T11에서 삭제).
func TestContentLoaderKeepsShortsBaselineWithLegacyCatalogOnly(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)

	_, err := pool.Exec(ctx, `
		INSERT INTO youtube_videos (video_id, channel_id, title, is_short, first_seen_at, last_seen_at)
		SELECT 'legacy-' || n, $1, 'legacy-' || n, TRUE, TIMESTAMPTZ '2026-07-01 00:00:00Z', TIMESTAMPTZ '2026-07-01 00:00:00Z'
		FROM generate_series(1, 3) AS n
	`, testChannelID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO youtube_content_watermarks (channel_id, watermark_type, initialized) VALUES ($1, 'SHORT', TRUE)`, testChannelID)
	require.NoError(t, err)

	repo := NewRepository(pool)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindShortsList, testChannelID, "youtubejs_content")
	publishShortWindow(t, repo, &proof, "new")
	require.NoError(t, NewConsumerWithAbsenceGrace(repo, 0).Consume(ctx, contentClaimOptions()))
	assertShortWindowOutboxes(t, pool, "new")
}
