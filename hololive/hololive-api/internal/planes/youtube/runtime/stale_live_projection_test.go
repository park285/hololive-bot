package runtime

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-api/internal/planes/youtube/targetprojection"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/config/settings/apiplane"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

const (
	staleOpsChannel = "UC_tp_stale_ops"

	// DB 상대 fixture는 기록 뒤 stale 조회 statement까지 흐른 실제 시간만큼 늙는다.
	// 예산 안쪽 경계 fixture는 이 여유만큼 안쪽에 두어 테스트 실행 시간과 무관하게 신선하다.
	freshBoundaryMargin = 10 * time.Second
)

func defaultLiveFreshnessBudget() time.Duration {
	return targetprojection.LiveFreshnessBudget(targetprojection.DefaultPolicySchedules()[contract.KindLiveSnapshot].PollInterval)
}

func seedStaleProjectionMembers(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO members(slug, channel_id, english_name, org, sync_source, status, is_graduated)
		VALUES ('tp-stale-ops', 'UC_tp_stale_ops', 'Stale Ops', 'Hololive', 'manual', 'active', false),
		       ('tp-stale-grad', 'UC_tp_stale_grad', 'Stale Grad', 'Hololive', 'manual', 'graduated', true)
	`); err != nil {
		t.Fatal(err)
	}
}

func TestStaleLiveVideoInputSelectsOperationalLiveWithoutFreshPositive(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	budget := defaultLiveFreshnessBudget()

	seedStaleProjectionMembers(t, pool)

	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions(video_id, channel_id, status, scheduled_start_time)
		VALUES ('tp-fresh', 'UC_tp_stale_ops', 'LIVE', NULL),
		       ('tp-stale', 'UC_tp_stale_ops', 'LIVE', NULL),
		       ('tp-seen-stale', 'UC_tp_stale_ops', 'LIVE', NULL),
		       ('tp-no-head', 'UC_tp_stale_ops', 'LIVE', NULL),
		       ('tp-null-clock', 'UC_tp_stale_ops', 'LIVE', NULL),
		       ('tp-future', 'UC_tp_stale_ops', 'LIVE', NULL),
		       ('tp-future-seen', 'UC_tp_stale_ops', 'LIVE', NULL),
		       ('tp-inside-boundary', 'UC_tp_stale_ops', 'LIVE', NULL),
		       ('tp-past-boundary', 'UC_tp_stale_ops', 'LIVE', NULL),
		       ('tp-upcoming', 'UC_tp_stale_ops', 'UPCOMING', statement_timestamp() - INTERVAL '1 hour'),
		       ('tp-ended', 'UC_tp_stale_ops', 'ENDED', NULL),
		       ('tp-graduated', 'UC_tp_stale_grad', 'LIVE', NULL),
		       ('tp-unrostered', 'UC_tp_stale_none', 'LIVE', NULL)
	`); err != nil {
		t.Fatal(err)
	}

	// 시각은 모두 기록 statement의 DB 시각 기준이다. head 부재(tp-no-head)는 레거시 보강 없이 stale이고,
	// 두 positive 시각 중 하나라도 미래면 신선한 것으로 세지 않는다.
	// 예산 안쪽은 LiveQuery처럼 신선하며, 예산을 1ms 넘긴 positive는 시간이 흐를수록 더 늙으므로 계속 stale이다.
	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_reconciliation_heads(video_id, status, last_live_positive_at, last_live_positive_seen_at)
		SELECT v.video_id, v.status, clock.as_of + v.positive_offset, clock.as_of + v.seen_offset
		FROM (SELECT statement_timestamp() AS as_of) clock
		CROSS JOIN (VALUES
		       ('tp-fresh', 'LIVE', INTERVAL '-30 seconds', INTERVAL '-29 seconds'),
		       ('tp-stale', 'LIVE', INTERVAL '-10 minutes', INTERVAL '-10 minutes'),
		       ('tp-seen-stale', 'LIVE', INTERVAL '-30 seconds', INTERVAL '-10 minutes'),
		       ('tp-null-clock', 'LIVE', NULL, NULL),
		       ('tp-future', 'LIVE', INTERVAL '1 minute', INTERVAL '0'),
		       ('tp-future-seen', 'LIVE', INTERVAL '-30 seconds', INTERVAL '1 minute'),
		       ('tp-inside-boundary', 'LIVE', -($1::bigint - $2::bigint) * INTERVAL '1 millisecond',
		                                      -($1::bigint - $2::bigint) * INTERVAL '1 millisecond'),
		       ('tp-past-boundary', 'LIVE', -($1::bigint + 1) * INTERVAL '1 millisecond', INTERVAL '-30 seconds'),
		       ('tp-upcoming', 'UPCOMING', NULL, NULL),
		       ('tp-ended', 'LIVE', INTERVAL '-10 minutes', INTERVAL '-10 minutes'),
		       ('tp-graduated', 'LIVE', INTERVAL '-10 minutes', INTERVAL '-10 minutes'),
		       ('tp-unrostered', 'LIVE', INTERVAL '-10 minutes', INTERVAL '-10 minutes')
		) AS v(video_id, status, positive_offset, seen_offset)
	`, budget.Milliseconds(), freshBoundaryMargin.Milliseconds()); err != nil {
		t.Fatal(err)
	}

	videos, err := dbx.InPgxTxWithResult(ctx, pool, func(tx dbx.Tx) ([]targetprojection.StaleLiveVideo, error) {
		operational, readErr := rosterReader{}.OperationalChannelIDs(ctx, tx)
		if readErr != nil {
			return nil, readErr
		}

		return rosterReader{}.StaleLiveVideos(ctx, tx, targetprojection.StaleLiveVideoQuery{
			OperationalChannelIDs: operational, FreshnessBudget: budget,
		})
	})
	if err != nil {
		t.Fatal(err)
	}

	var got []string

	for _, video := range videos {
		if !strings.HasPrefix(video.VideoID, "tp-") {
			continue
		}

		if video.ChannelID != staleOpsChannel {
			t.Fatalf("stale video %s channel = %q, want operational roster channel", video.VideoID, video.ChannelID)
		}

		got = append(got, video.VideoID)
	}

	want := []string{"tp-future", "tp-future-seen", "tp-no-head", "tp-null-clock", "tp-past-boundary", "tp-seen-stale", "tp-stale", "tp-upcoming"}
	if !slices.Equal(got, want) {
		t.Fatalf("stale live videos = %v, want %v", got, want)
	}
}

type projectionRefresh func(at time.Time, label string) targetprojection.Result

// newLatencyProjection은 운영 roster 채널의 LIVE 영상 tp-latency와 방금 기록된 positive를 두고,
// 실제 Refresher·PolicyBuilder·rosterReader 경로로 projection을 갱신하는 함수를 돌려준다.
func newLatencyProjection(t *testing.T) (*pgxpool.Pool, projectionRefresh) {
	t.Helper()

	pool := dbtest.NewPool(t)
	ctx := t.Context()

	refresher, err := targetprojection.NewRefresher(pool, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	builder := targetprojection.PolicyBuilder{Reader: rosterReader{}, Schedules: targetprojection.DefaultPolicySchedules()}

	seedStaleProjectionMembers(t, pool)

	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions(video_id, channel_id, status) VALUES ('tp-latency', 'UC_tp_stale_ops', 'LIVE');
		INSERT INTO youtube_live_reconciliation_heads(video_id, status, last_live_positive_at, last_live_positive_seen_at)
		VALUES ('tp-latency', 'LIVE', statement_timestamp(), statement_timestamp())
	`); err != nil {
		t.Fatal(err)
	}

	refresh := func(at time.Time, label string) targetprojection.Result {
		t.Helper()

		started := time.Now()

		result, refreshErr := refresher.Refresh(ctx, builder, at)
		if refreshErr != nil {
			t.Fatalf("%s refresh: %v", label, refreshErr)
		}

		t.Logf("projection transaction: phase=%s refresh_clock=%s generation=%d changed=%t wall=%s",
			label, at.UTC().Format(time.RFC3339Nano), result.Generation, result.Changed, time.Since(started))

		return result
	}

	return pool, refresh
}

// setLatencyPositiveAge는 tp-latency의 두 positive 시각을 기록 statement의 DB 시각보다 age만큼 과거로 둔다.
func setLatencyPositiveAge(t *testing.T, pool *pgxpool.Pool, age time.Duration) {
	t.Helper()

	var positiveAt time.Time

	if err := pool.QueryRow(t.Context(), `
		UPDATE youtube_live_reconciliation_heads
		SET last_live_positive_at = statement_timestamp() - $1::bigint * INTERVAL '1 millisecond',
		    last_live_positive_seen_at = statement_timestamp() - $1::bigint * INTERVAL '1 millisecond'
		WHERE video_id = 'tp-latency'
		RETURNING last_live_positive_at
	`, age.Milliseconds()).Scan(&positiveAt); err != nil {
		t.Fatal(err)
	}

	t.Logf("positive fixture: age_at_write=%s db_positive_at=%s", age, positiveAt.UTC().Format(time.RFC3339Nano))
}

// TestProjectionRefreshTracksStaleLiveVideoTransitions는 DB 상대 positive 나이로 projection 전이를 검증한다.
// 예산 안쪽 positive는 대상이 아니고, 예산과 refresh 간격을 넘긴 첫 refresh가 후보를 만들며,
// 신선한 positive나 종료 이후 다음 refresh가 제거한다.
func TestProjectionRefreshTracksStaleLiveVideoTransitions(t *testing.T) {
	pool, refresh := newLatencyProjection(t)
	interval := apiplane.DefaultYouTubePlaneConfig().TargetProjection.Interval
	budget := defaultLiveFreshnessBudget()

	refresh(time.Now(), "fresh positive")

	if currentVideoTargetReason(t, pool) != "" {
		t.Fatal("fresh positive produced a video live check target")
	}

	setLatencyPositiveAge(t, pool, budget-freshBoundaryMargin)
	refresh(time.Now(), "inside freshness boundary")

	if currentVideoTargetReason(t, pool) != "" {
		t.Fatal("positive inside the freshness boundary produced a video live check target")
	}

	staleAge := budget + interval
	setLatencyPositiveAge(t, pool, staleAge)

	stale := refresh(time.Now(), "first refresh after stale boundary")
	if !stale.Changed || currentVideoTargetReason(t, pool) != "stale_live_session/"+staleOpsChannel {
		t.Fatalf("first refresh after stale boundary did not activate the video target: %+v", stale)
	}

	t.Logf("stale candidate: positive_age=%s budget=%s over_budget=%s refresh_interval=%s",
		staleAge, budget, staleAge-budget, interval)

	verifyProjectionRecovery(t, pool, refresh, budget+interval)
}

func verifyProjectionRecovery(t *testing.T, pool *pgxpool.Pool, refresh projectionRefresh, staleAge time.Duration) {
	t.Helper()

	setLatencyPositiveAge(t, pool, 0)

	if refreshed := refresh(time.Now(), "fresh positive recovery"); !refreshed.Changed ||
		currentVideoTargetReason(t, pool) != "" {
		t.Fatalf("fresh positive did not stop the video target: %+v", refreshed)
	}

	setLatencyPositiveAge(t, pool, staleAge)

	if refreshed := refresh(time.Now(), "stale re-entry"); !refreshed.Changed ||
		currentVideoTargetReason(t, pool) == "" {
		t.Fatalf("stale re-entry did not recreate the video target: %+v", refreshed)
	}

	var endedAt time.Time

	if err := pool.QueryRow(t.Context(), `
		UPDATE youtube_live_sessions SET status = 'ENDED', ended_at = statement_timestamp() WHERE video_id = 'tp-latency'
		RETURNING ended_at
	`).Scan(&endedAt); err != nil {
		t.Fatal(err)
	}

	ended := refresh(time.Now(), "after end")
	if !ended.Changed || currentVideoTargetReason(t, pool) != "" {
		t.Fatalf("next refresh after end kept the video target: %+v", ended)
	}

	t.Logf("ended target: session_ended=%s removed_by_generation=%d",
		endedAt.UTC().Format(time.RFC3339Nano), ended.Generation)
}

// TestProjectionRefreshKeepsPositiveCommittedAfterRefreshClock는 refresh 시각을 잡은 뒤 DB에 커밋된
// 신선한 positive가 stale 영상 target을 만들지 않음을 실제 Refresh 경로로 확인한다.
// 기존 positive는 이미 stale이라 새 positive가 없으면 target이 된다. 캡처한 refresh 시각으로
// 판정하면 새 positive는 그 시각보다 미래로 보여 target이 되므로, 조회 statement 시각 판정만 통과한다.
func TestProjectionRefreshKeepsPositiveCommittedAfterRefreshClock(t *testing.T) {
	pool, refresh := newLatencyProjection(t)
	ctx := t.Context()

	setLatencyPositiveAge(t, pool, defaultLiveFreshnessBudget()+apiplane.DefaultYouTubePlaneConfig().TargetProjection.Interval)

	// 호출자의 refresh 시각을 같은 DB 시계로 잡아 호스트 간 시계 차이 없이 커밋 순서만 남긴다.
	var captured time.Time

	if err := pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&captured); err != nil {
		t.Fatal(err)
	}

	var (
		committed    time.Time
		afterCapture bool
	)

	if err := pool.QueryRow(ctx, `
		UPDATE youtube_live_reconciliation_heads
		SET last_live_positive_at = statement_timestamp(), last_live_positive_seen_at = statement_timestamp()
		WHERE video_id = 'tp-latency'
		RETURNING last_live_positive_at, last_live_positive_at > $1::timestamptz
	`, captured).Scan(&committed, &afterCapture); err != nil {
		t.Fatal(err)
	}

	if !afterCapture {
		t.Fatalf("positive %s was not committed after refresh clock %s", committed, captured)
	}

	result := refresh(captured, "positive committed after refresh clock")

	if reason := currentVideoTargetReason(t, pool); reason != "" {
		t.Fatalf("positive committed after refresh clock produced a stale video target %q: %+v", reason, result)
	}

	t.Logf("refresh clock race: captured=%s positive_committed=%s committed_after_capture=%s video_target=none generation=%d",
		captured.UTC().Format(time.RFC3339Nano), committed.UTC().Format(time.RFC3339Nano), committed.Sub(captured), result.Generation)
}

// currentVideoTargetReason은 현재 generation의 영상 확인 target reason을 "kind/key"로 돌려주며, 없으면 빈 문자열이다.
func currentVideoTargetReason(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()

	const videoID = "tp-latency"

	var reasons []string

	rows, err := pool.Query(t.Context(), `
		SELECT r.reason_kind || '/' || r.reason_key
		FROM youtube_collection_targets t
		JOIN youtube_collection_projection_generations g ON g.generation = t.projection_generation
		JOIN youtube_collection_target_reasons r
		  ON r.projection_generation = t.projection_generation
		 AND r.subject_key = t.subject_key AND r.observation_kind = t.observation_kind
		WHERE g.status = 'CURRENT' AND t.subject_key = $1 AND t.observation_kind = 'video_live_check'
	`, videoID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var reason string

		if err := rows.Scan(&reason); err != nil {
			t.Fatal(err)
		}

		reasons = append(reasons, reason)
	}

	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	if len(reasons) > 1 {
		t.Fatalf("video %s has %d reasons: %v", videoID, len(reasons), reasons)
	}

	if len(reasons) == 0 {
		return ""
	}

	return reasons[0]
}
