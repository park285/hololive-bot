package runtime

import (
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-api/internal/planes/youtube/targetprojection"
	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

const (
	liveCheckOpsChannel = "UC_tp_stale_ops"

	// DB 상대 fixture는 기록 뒤 조회 statement까지 흐른 실제 시간만큼 늙는다.
	// 예산 안쪽 경계 fixture는 이 여유만큼 안쪽에 두어 테스트 실행 시간과 무관하게 신선하다.
	freshBoundaryMargin = 10 * time.Second

	// 영상별 not_before 분류: 받아들인 신선도 근거 없음, 다음 확인까지 대기, 지금 확인 가능.
	notBeforeNone     = "none"
	notBeforeSleeping = "sleeping"
	notBeforeEligible = "eligible"
)

func defaultLiveFreshnessBudget() time.Duration {
	return targetprojection.LiveFreshnessBudget(targetprojection.DefaultPolicySchedules()[contract.KindLiveSnapshot].PollInterval)
}

func seedLiveCheckProjectionMembers(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO members(slug, channel_id, english_name, org, sync_source, status, is_graduated)
		VALUES ('tp-stale-ops', 'UC_tp_stale_ops', 'Stale Ops', 'Hololive', 'manual', 'active', false),
		       ('tp-stale-grad', 'UC_tp_stale_grad', 'Stale Grad', 'Hololive', 'manual', 'graduated', true)
	`); err != nil {
		t.Fatal(err)
	}
}

// seedLiveCheckVideoFixtures는 운영·졸업·roster 밖 채널의 LIVE/UPCOMING/ENDED 영상과 DB 시각 기준 positive·UNKNOWN 근거를 둔다.
func seedLiveCheckVideoFixtures(t *testing.T, pool *pgxpool.Pool, budget time.Duration) {
	t.Helper()

	ctx := t.Context()

	seedLiveCheckProjectionMembers(t, pool)

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
		       ('tp-upcoming-unknown', 'UC_tp_stale_ops', 'UPCOMING', statement_timestamp() - INTERVAL '1 hour'),
		       ('tp-upcoming-later', 'UC_tp_stale_ops', 'UPCOMING', statement_timestamp() + INTERVAL '1 hour'),
		       ('tp-ended', 'UC_tp_stale_ops', 'ENDED', NULL),
		       ('tp-graduated', 'UC_tp_stale_grad', 'LIVE', NULL),
		       ('tp-unrostered', 'UC_tp_stale_none', 'LIVE', NULL)
	`); err != nil {
		t.Fatal(err)
	}

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
		) AS v(video_id, status, positive_offset, seen_offset);
	`, budget.Milliseconds(), freshBoundaryMargin.Milliseconds()); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_video_availability
		(video_id,channel_id,provider,identity_confirmed,availability,method,unknown_reason,evidence_sha256,
		 scheduled_for,effective_at,observed_at,received_at)
		VALUES ('tp-upcoming-unknown','UC_tp_stale_ops','youtubejs',false,'UNKNOWN','unknown','identity_missing',repeat('a',64),
		        statement_timestamp()-INTERVAL '1 minute',statement_timestamp()-INTERVAL '1 minute',statement_timestamp()-INTERVAL '1 minute',statement_timestamp())
	`); err != nil {
		t.Fatal(err)
	}
}

// classifyLiveCheckVideos는 운영 roster 채널만 담겼는지 확인하고 영상별 not_before를 queried 기준으로 분류한다.
func classifyLiveCheckVideos(t *testing.T, videos []targetprojection.LiveCheckVideo, queried time.Time) map[string]string {
	t.Helper()

	got := make(map[string]string, len(videos))

	for _, video := range videos {
		if video.ChannelID != liveCheckOpsChannel {
			t.Fatalf("live check video %s channel = %q, want operational roster channel", video.VideoID, video.ChannelID)
		}

		switch {
		case video.NotBefore.IsZero():
			got[video.VideoID] = notBeforeNone
		case video.NotBefore.After(queried):
			got[video.VideoID] = notBeforeSleeping
		default:
			got[video.VideoID] = notBeforeEligible
		}
	}

	return got
}

// TestLiveCheckVideoInputKeepsStructuralMembershipWithNotBefore는 신선도가 membership을 바꾸지 않고
// not_before로만 표현되는지 확인한다. 미래·NULL 근거 시각은 받아들이지 않고, UNKNOWN 확인도
// UPCOMING의 다음 확인만 미룰 뿐 대상에서 빼지 않는다.
func TestLiveCheckVideoInputKeepsStructuralMembershipWithNotBefore(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	budget := defaultLiveFreshnessBudget()

	seedLiveCheckVideoFixtures(t, pool, budget)

	var (
		videos  []targetprojection.LiveCheckVideo
		queried time.Time
	)

	err := dbx.InPgxTx(ctx, pool, func(tx dbx.Tx) error {
		operational, readErr := rosterReader{}.OperationalChannelIDs(ctx, tx)
		if readErr != nil {
			return readErr
		}

		videos, readErr = rosterReader{}.LiveCheckVideos(ctx, tx, targetprojection.LiveCheckVideoQuery{
			OperationalChannelIDs: operational, FreshnessBudget: budget,
		})
		if readErr != nil {
			return readErr
		}

		return tx.QueryRow(ctx, `SELECT statement_timestamp()`).Scan(&queried)
	})
	if err != nil {
		t.Fatal(err)
	}

	got := classifyLiveCheckVideos(t, videos, queried)
	want := map[string]string{
		"tp-fresh": notBeforeSleeping, "tp-inside-boundary": notBeforeSleeping, "tp-upcoming-unknown": notBeforeSleeping,
		"tp-stale": notBeforeEligible, "tp-seen-stale": notBeforeEligible, "tp-past-boundary": notBeforeEligible,
		"tp-future": notBeforeNone, "tp-future-seen": notBeforeNone, "tp-no-head": notBeforeNone,
		"tp-null-clock": notBeforeNone, "tp-upcoming": notBeforeNone,
	}

	if len(got) != len(want) {
		t.Fatalf("live check membership = %v, want %v", got, want)
	}

	for id, class := range want {
		if got[id] != class {
			t.Fatalf("live check video %s not_before class = %q, want %q (all=%v)", id, got[id], class, got)
		}
	}

	ids := make([]string, 0, len(videos))
	for _, video := range videos {
		ids = append(ids, video.VideoID)
	}

	if !slices.IsSorted(ids) {
		t.Fatalf("live check videos are not in canonical order: %v", ids)
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

	seedLiveCheckProjectionMembers(t, pool)

	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions(video_id, channel_id, status) VALUES ('tp-latency', 'UC_tp_stale_ops', 'LIVE');
		INSERT INTO youtube_live_reconciliation_heads(video_id, status, last_live_positive_at, last_live_positive_seen_at)
		VALUES ('tp-latency', 'LIVE', statement_timestamp(), statement_timestamp())
	`); err != nil {
		t.Fatal(err)
	}

	refresh := func(at time.Time, label string) targetprojection.Result {
		t.Helper()

		result, refreshErr := refresher.Refresh(ctx, builder, at)
		if refreshErr != nil {
			t.Fatalf("%s refresh: %v", label, refreshErr)
		}

		return result
	}

	return pool, refresh
}

// setLatencyPositiveAge는 tp-latency의 두 positive 시각을 기록 statement의 DB 시각보다 age만큼 과거로 둔다.
func setLatencyPositiveAge(t *testing.T, pool *pgxpool.Pool, age time.Duration) {
	t.Helper()

	if _, err := pool.Exec(t.Context(), `
		UPDATE youtube_live_reconciliation_heads
		SET last_live_positive_at = statement_timestamp() - $1::bigint * INTERVAL '1 millisecond',
		    last_live_positive_seen_at = statement_timestamp() - $1::bigint * INTERVAL '1 millisecond'
		WHERE video_id = 'tp-latency'
	`, age.Milliseconds()); err != nil {
		t.Fatal(err)
	}
}

// TestProjectionRefreshKeepsLiveVideoMembershipAcrossFreshness는 positive 신선도의 출입이
// generation·membership을 바꾸지 않고 not_before만 갱신하며, 실제 종료만 대상을 빼는지 확인한다.
func TestProjectionRefreshKeepsLiveVideoMembershipAcrossFreshness(t *testing.T) {
	pool, refresh := newLatencyProjection(t)
	budget := defaultLiveFreshnessBudget()

	first := refresh(time.Now(), "fresh positive")

	fresh := currentVideoTarget(t, pool)
	if !fresh.heldSince(first.Generation) || !fresh.sleeping || fresh.reason != "live_session_check/"+liveCheckOpsChannel {
		t.Fatalf("fresh positive target = %+v, generation %d", fresh, first.Generation)
	}

	setLatencyPositiveAge(t, pool, budget+time.Minute)

	stale := refresh(time.Now(), "stale positive")
	if stale.Changed || stale.Generation != first.Generation {
		t.Fatalf("freshness expiry rotated the projection: first=%+v stale=%+v", first, stale)
	}

	staleTarget := currentVideoTarget(t, pool)
	if !staleTarget.heldSince(first.Generation) || staleTarget.sleeping || staleTarget.notBefore.IsZero() {
		t.Fatalf("stale positive target = %+v", staleTarget)
	}

	setLatencyPositiveAge(t, pool, 0)

	recovered := refresh(time.Now(), "fresh positive recovery")
	if recovered.Changed || recovered.Generation != first.Generation || !currentVideoTarget(t, pool).sleeping {
		t.Fatalf("fresh positive recovery = %+v target=%+v", recovered, currentVideoTarget(t, pool))
	}

	endLatencySession(t, pool)

	ended := refresh(time.Now(), "after end")
	if !ended.Changed || currentVideoTarget(t, pool).found {
		t.Fatalf("next refresh after end kept the video target: %+v", ended)
	}

	// 같은 채널의 다른 target은 영상 대상 제거와 무관하므로 membership 시작 generation을 유지한다.
	if memberSince := liveSnapshotMemberSince(t, pool, ended.Generation); memberSince != first.Generation {
		t.Fatalf("unrelated live_snapshot member_since = %d, want %d", memberSince, first.Generation)
	}
}

// endLatencySession은 tp-latency 방송을 DB 시각으로 종료한다.
func endLatencySession(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	if _, err := pool.Exec(t.Context(), `
		UPDATE youtube_live_sessions SET status = 'ENDED', ended_at = statement_timestamp() WHERE video_id = 'tp-latency'
	`); err != nil {
		t.Fatal(err)
	}
}

// liveSnapshotMemberSince는 generation의 운영 채널 live_snapshot target membership 시작 generation을 돌려준다.
func liveSnapshotMemberSince(t *testing.T, pool *pgxpool.Pool, generation int64) int64 {
	t.Helper()

	var memberSince int64

	if err := pool.QueryRow(t.Context(), `
		SELECT member_since_generation FROM youtube_collection_targets
		WHERE projection_generation = $1 AND subject_key = $2 AND observation_kind = 'live_snapshot'
	`, generation, liveCheckOpsChannel).Scan(&memberSince); err != nil {
		t.Fatal(err)
	}

	return memberSince
}

// TestProjectionRefreshJudgesPositiveCommittedAfterRefreshClock는 refresh 시각을 잡은 뒤 DB에 커밋된
// 신선한 positive가 조회 statement 시각 기준으로 받아들여져 다음 확인을 미루는지 실제 Refresh 경로로 확인한다.
// 캡처한 refresh 시각으로 판정하면 새 positive는 미래로 보여 근거에서 빠진다.
func TestProjectionRefreshJudgesPositiveCommittedAfterRefreshClock(t *testing.T) {
	pool, refresh := newLatencyProjection(t)
	ctx := t.Context()

	setLatencyPositiveAge(t, pool, defaultLiveFreshnessBudget()+time.Minute)

	var captured time.Time

	if err := pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&captured); err != nil {
		t.Fatal(err)
	}

	var afterCapture bool

	if err := pool.QueryRow(ctx, `
		UPDATE youtube_live_reconciliation_heads
		SET last_live_positive_at = statement_timestamp(), last_live_positive_seen_at = statement_timestamp()
		WHERE video_id = 'tp-latency'
		RETURNING last_live_positive_at > $1::timestamptz
	`, captured).Scan(&afterCapture); err != nil {
		t.Fatal(err)
	}

	if !afterCapture {
		t.Fatal("positive was not committed after the refresh clock")
	}

	refresh(captured, "positive committed after refresh clock")

	if target := currentVideoTarget(t, pool); !target.found || !target.sleeping {
		t.Fatalf("positive committed after refresh clock was not accepted as freshness: %+v", target)
	}
}

type liveCheckTargetState struct {
	found       bool
	reason      string
	memberSince int64
	notBefore   time.Time
	sleeping    bool
}

// heldSince는 target이 존재하고 generation에서 membership을 시작했는지 확인한다.
func (s liveCheckTargetState) heldSince(generation int64) bool {
	return s.found && s.memberSince == generation
}

// currentVideoTarget은 현재 generation의 tp-latency 영상 확인 target 상태를 DB 시각 기준으로 돌려준다.
func currentVideoTarget(t *testing.T, pool *pgxpool.Pool) liveCheckTargetState {
	t.Helper()

	var (
		state     liveCheckTargetState
		notBefore pgtype.Timestamptz
	)

	rows, err := pool.Query(t.Context(), `
		SELECT r.reason_kind || '/' || r.reason_key, t.member_since_generation, t.not_before,
		       COALESCE(t.not_before > statement_timestamp(), false)
		FROM youtube_collection_targets t
		JOIN youtube_collection_projection_generations g ON g.generation = t.projection_generation
		JOIN youtube_collection_target_reasons r
		  ON r.projection_generation = t.projection_generation
		 AND r.subject_key = t.subject_key AND r.observation_kind = t.observation_kind
		WHERE g.status = 'CURRENT' AND t.subject_key = 'tp-latency' AND t.observation_kind = 'video_live_check'
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		if state.found {
			t.Fatal("tp-latency has more than one video live check reason")
		}

		if err := rows.Scan(&state.reason, &state.memberSince, &notBefore, &state.sleeping); err != nil {
			t.Fatal(err)
		}

		state.found = true
		state.notBefore = notBefore.Time
	}

	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	return state
}
