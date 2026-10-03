package targetprojection

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

type membershipRow struct {
	memberSince int64
	createdAt   time.Time
	notBefore   pgtype.Timestamptz
}

// projectionFixture는 실제 Refresher로 기준 시각에서 offset만큼 지난 refresh를 반복하는 테스트 묶음이다.
type projectionFixture struct {
	t         *testing.T
	pool      *pgxpool.Pool
	refresher *Refresher
	base      time.Time
}

func newProjectionFixture(t *testing.T, base time.Time) *projectionFixture {
	t.Helper()

	pool := dbtest.NewPool(t)

	refresher, err := NewRefresher(pool, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	return &projectionFixture{t: t, pool: pool, refresher: refresher, base: base}
}

// refresh는 기준 시각에서 offset만큼 지난 시각에 targets만으로 projection을 갱신한다.
func (f *projectionFixture) refresh(offset time.Duration, label string, targets ...TargetSpec) Result {
	f.t.Helper()

	return mustRefresh(f.t, f.refresher, staticBuilder{targets: targets}, f.base.Add(offset), label)
}

// requireMemberSince는 generation의 target membership이 want generation에서 시작했는지 확인하고 row를 돌려준다.
func (f *projectionFixture) requireMemberSince(label string, generation int64, target TargetSpec, want int64) membershipRow {
	f.t.Helper()

	row := loadMembership(f.t, f.pool, generation, target)
	if row.memberSince != want {
		f.t.Fatalf("%s member_since = %d, want %d (row %+v)", label, row.memberSince, want, row)
	}

	return row
}

// TestRefreshCarriesMembershipOnlyForUnchangedTargets는 무관한 target 변경과 신선도 변화가 기존 target의
// membership 시작 generation·created_at을 유지하고, 자신의 scheduling 변경·재등록(ABA)만 새로 시작하는지 확인한다.
func TestRefreshCarriesMembershipOnlyForUnchangedTargets(t *testing.T) {
	f := newProjectionFixture(t, projectionNow)

	community := TargetSpec{SubjectKey: subjectChannelA, ObservationKind: contract.KindCommunityPage, Priority: 40, PollInterval: 2 * time.Minute, Enabled: true}
	videos := TargetSpec{SubjectKey: "channel:b", ObservationKind: contract.KindVideoList, Priority: 50, PollInterval: 5 * time.Minute, Enabled: true}
	video := TargetSpec{SubjectKey: "vid-1", ObservationKind: contract.KindVideoLiveCheck, Priority: 20, PollInterval: 2 * time.Minute, Enabled: true, NotBefore: projectionNow.Add(time.Minute)}
	added := TargetSpec{SubjectKey: "vid-2", ObservationKind: contract.KindVideoLiveCheck, Priority: 20, PollInterval: 2 * time.Minute, Enabled: true}

	first := f.refresh(0, "initial", community, videos, video)
	initialCommunity := f.requireMemberSince("initial community", first.Generation, community, first.Generation)

	// not_before만 바뀌면 generation과 membership은 그대로이고 값만 갱신된다.
	video.NotBefore = projectionNow.Add(3 * time.Minute)

	freshness := f.refresh(time.Minute, "freshness only", community, videos, video)
	if freshness.Changed || freshness.Generation != first.Generation {
		t.Fatalf("freshness-only refresh rotated generation: %+v", freshness)
	}

	if got := loadMembership(t, f.pool, first.Generation, video); !got.notBefore.Valid || !got.notBefore.Time.Equal(video.NotBefore) {
		t.Fatalf("freshness-only not_before = %+v, want %s", got.notBefore, video.NotBefore)
	}

	// 무관한 영상 target 추가는 새 generation을 만들지만 기존 row의 membership과 논리적 시작 시각을 보존한다.
	second := f.refresh(2*time.Minute, "unrelated add", community, videos, video, added)
	f.assertUnrelatedAddCarriesMembership(first.Generation, second, initialCommunity, community, video, added)

	// 자신의 cadence 변경은 새 membership이다.
	videos.PollInterval = 10 * time.Minute

	third := f.refresh(3*time.Minute, "own cadence change", community, videos, video, added)
	f.requireMemberSince("cadence change", third.Generation, videos, third.Generation)
	f.requireMemberSince("cadence change of another target community", third.Generation, community, first.Generation)

	// 해제 뒤 재등록(ABA)은 과거 membership을 되살리지 않는다.
	removed := f.refresh(4*time.Minute, "remove community", videos, video, added)
	readded := f.refresh(5*time.Minute, "re-add community", community, videos, video, added)

	if !removed.Changed || !readded.Changed {
		t.Fatalf("remove/re-add did not rotate generations: %+v %+v", removed, readded)
	}

	if got := f.requireMemberSince("re-added community", readded.Generation, community, readded.Generation); !got.createdAt.After(initialCommunity.createdAt) {
		t.Fatalf("re-added community membership = %+v, want created_at after %s", got, initialCommunity.createdAt)
	}
}

// assertUnrelatedAddCarriesMembership은 무관한 target 추가가 generation을 바꾸면서 기존 row의 membership·created_at·
// not_before를 보존하고, 추가된 target만 그 generation에서 NULL not_before로 membership을 시작하는지 확인한다.
func (f *projectionFixture) assertUnrelatedAddCarriesMembership(
	firstGeneration int64, second Result, initialCommunity membershipRow, community, video, added TargetSpec,
) {
	f.t.Helper()

	if !second.Changed || second.Generation == firstGeneration {
		f.t.Fatalf("unrelated add did not rotate generation: %+v", second)
	}

	carried := f.requireMemberSince("unrelated add community", second.Generation, community, firstGeneration)
	if !carried.createdAt.Equal(initialCommunity.createdAt) {
		f.t.Fatalf("unrelated add reset community membership: %+v, initial %+v", carried, initialCommunity)
	}

	if got := f.requireMemberSince("unrelated add video", second.Generation, video, firstGeneration); !got.notBefore.Time.Equal(video.NotBefore) {
		f.t.Fatalf("unrelated add reset video membership: %+v", got)
	}

	if got := f.requireMemberSince("new target", second.Generation, added, second.Generation); got.notBefore.Valid {
		f.t.Fatalf("new target membership = %+v, want NULL not_before", got)
	}
}

// TestRefreshFailsClosedWithoutProjectionGuard는 guard row가 없으면 CURRENT를 바꾸지 않고 실패하는지 확인한다.
func TestRefreshFailsClosedWithoutProjectionGuard(t *testing.T) {
	pool := dbtest.NewPool(t)

	refresher, err := NewRefresher(pool, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	target := TargetSpec{SubjectKey: subjectChannelA, ObservationKind: contract.KindCommunityPage, Priority: 40, PollInterval: 2 * time.Minute, Enabled: true}
	current := mustRefresh(t, refresher, staticBuilder{targets: []TargetSpec{target}}, projectionNow, "initial")

	if _, err := pool.Exec(t.Context(), `DELETE FROM youtube_collection_projection_guard`); err != nil {
		t.Fatal(err)
	}

	target.PollInterval = 3 * time.Minute

	if _, err := refresher.Refresh(t.Context(), staticBuilder{targets: []TargetSpec{target}}, projectionNow.Add(time.Minute)); err == nil {
		t.Fatal("refresh without projection guard succeeded")
	}

	assertGenerationStatus(t, pool, current.Generation, "CURRENT")
}

// membershipValidity는 collector가 호출하는 단일 유효성 함수를 proof generation으로 부른다.
type membershipValidity struct {
	t     *testing.T
	pool  *pgxpool.Pool
	proof int64
}

func (v *membershipValidity) require(label string, kinds []string, exact bool, subject string, count int32, want bool) {
	v.t.Helper()

	var got bool

	if err := v.pool.QueryRow(v.t.Context(), `SELECT public.youtube_collection_membership_valid($1, $2, $3, $4, $5)`,
		kinds, exact, subject, count, v.proof).Scan(&got); err != nil {
		v.t.Fatal(err)
	}

	if got != want {
		v.t.Fatalf("%s: membership valid = %t, want %t", label, got, want)
	}
}

// TestMembershipValidityFollowsJobScope는 collector가 호출하는 단일 유효성 함수가 무관한 변경에는 유지되고
// 자신의 범위 제거·disable·cadence 변경·ABA·유효 CURRENT 부재에는 거부되는지 실제 refresh 경로로 확인한다.
func TestMembershipValidityFollowsJobScope(t *testing.T) {
	// 유효성 함수는 DB statement 시각으로 validity를 보므로 실제 시각으로 refresh한다.
	f := newProjectionFixture(t, time.Now())

	videos := TargetSpec{SubjectKey: subjectChannelA, ObservationKind: contract.KindVideoList, Priority: 50, PollInterval: 5 * time.Minute, Enabled: true}
	shorts := TargetSpec{SubjectKey: subjectChannelA, ObservationKind: contract.KindShortsList, Priority: 50, PollInterval: 5 * time.Minute, Enabled: true}
	other := TargetSpec{SubjectKey: "vid-1", ObservationKind: contract.KindVideoLiveCheck, Priority: 20, PollInterval: 2 * time.Minute, Enabled: true}
	unrelated := TargetSpec{SubjectKey: "vid-2", ObservationKind: contract.KindVideoLiveCheck, Priority: 20, PollInterval: 2 * time.Minute, Enabled: true}

	acquired := f.refresh(0, "acquire", videos, shorts, other)
	contentKinds := []string{string(contract.KindShortsList), string(contract.KindVideoList)}
	videoKinds := []string{string(contract.KindVideoLiveCheck)}
	valid := membershipValidity{t: t, pool: f.pool, proof: acquired.Generation}

	valid.require("acquired content", contentKinds, true, subjectChannelA, 2, true)
	valid.require("acquired global video scope", videoKinds, false, "global", 1, true)
	valid.require("empty scope fails closed", nil, true, subjectChannelA, 0, false)
	valid.require("count drift", contentKinds, true, subjectChannelA, 1, false)

	// 다른 영상 target 추가: 같은 kind의 다른 subject는 exact content job에 무관하지만 global 범위에는 변경이다.
	f.refresh(time.Second, "unrelated add", videos, shorts, other, unrelated)
	valid.require("content after unrelated add", contentKinds, true, subjectChannelA, 2, true)
	valid.require("exact video after unrelated add", videoKinds, true, "vid-1", 1, true)
	valid.require("global video scope after add", videoKinds, false, "global", 1, false)

	// 자신의 cadence 변경은 같은 수여도 새 membership이라 거부된다.
	shorts.PollInterval = 10 * time.Minute
	f.refresh(2*time.Second, "own cadence", videos, shorts, other, unrelated)
	valid.require("content after own cadence change", contentKinds, true, subjectChannelA, 2, false)

	// 자신의 disable은 범위 수 감소로 거부된다.
	other.Enabled = false
	f.refresh(3*time.Second, "disable", videos, shorts, other, unrelated)
	valid.require("video after disable", videoKinds, true, "vid-1", 1, false)

	// 다시 enable한 generation의 proof도 해제 뒤 같은 scheduling 재등록(ABA)을 받아들이지 않는다.
	other.Enabled = true
	valid.proof = f.refresh(4*time.Second, "re-enable", videos, shorts, other, unrelated).Generation

	valid.require("video after re-enable", videoKinds, true, "vid-1", 1, true)

	f.refresh(5*time.Second, "remove", videos, shorts, unrelated)

	latest := f.refresh(6*time.Second, "re-add", videos, shorts, other, unrelated)

	valid.require("video after ABA", videoKinds, true, "vid-1", 1, false)

	// 유효한 CURRENT가 없으면 연속 membership인 proof도 유효하지 않다.
	valid.proof = latest.Generation

	valid.require("unrelated exact video at latest generation", videoKinds, true, "vid-2", 1, true)

	if _, err := f.pool.Exec(t.Context(), `UPDATE youtube_collection_projection_generations SET valid_until = now() - interval '1 second' WHERE status = 'CURRENT'`); err != nil {
		t.Fatal(err)
	}

	valid.require("unrelated exact video without valid CURRENT", videoKinds, true, "vid-2", 1, false)
}

func loadMembership(t *testing.T, pool *pgxpool.Pool, generation int64, target TargetSpec) membershipRow {
	t.Helper()

	var row membershipRow

	if err := pool.QueryRow(t.Context(), `
		SELECT member_since_generation, created_at, not_before
		FROM youtube_collection_targets
		WHERE projection_generation = $1 AND subject_key = $2 AND observation_kind = $3
	`, generation, target.SubjectKey, string(target.ObservationKind)).Scan(&row.memberSince, &row.createdAt, &row.notBefore); err != nil {
		t.Fatalf("load membership %s/%s generation %d: %v", target.SubjectKey, target.ObservationKind, generation, err)
	}

	return row
}
