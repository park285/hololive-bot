package joblease

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/sourceobservation"
)

const (
	subjectUCC      = "UC_C"
	videoLiveID     = "video-live-1"
	testGlobalClass = string(sourceobservation.JobClassGlobal)
)

func contentJob(subject string) *JobSpec {
	return &JobSpec{
		JobKey:   "collector:youtubejs:youtubejs_content:" + subject,
		Provider: contract.ProviderYouTubeJS, Class: "SUBJECT",
		CollectionJobKind: "youtubejs_content", SubjectKey: subject, PollInterval: time.Minute,
	}
}

func holodexLiveJob() *JobSpec {
	return &JobSpec{
		JobKey:   "collector:holodex:holodex_live:global",
		Provider: contract.ProviderHolodex, Class: testGlobalClass,
		CollectionJobKind: "holodex_live", SubjectKey: "global:holodex_live", PollInterval: time.Minute,
	}
}

func jobContractFor(t *testing.T, spec *JobSpec) sourceobservation.JobContract {
	t.Helper()

	job, ok := sourceobservation.InitialJobContracts().Definition(sourceobservation.JobID{
		Provider: spec.Provider, Kind: sourceobservation.JobKind(spec.CollectionJobKind),
	})
	if !ok {
		t.Fatalf("missing job contract %s/%s", spec.Provider, spec.CollectionJobKind)
	}

	return job
}

// projectionChange는 API refresh 한 번의 대상 변화다. 필드 dropped는 이어받지 않고, added는 새 generation부터의
// 대상으로 넣는다(재등록·cadence 변경 포함).
type projectionChange struct {
	dropped []leaseTarget
	added   []leaseTarget
}

// transitionProjection은 API refresh처럼 guard를 먼저 잡고 이전 CURRENT를 RETIRED로 바꾼 뒤 새 CURRENT를 만든다.
// 같은 의미로 이어진 대상은 member_since_generation을 그대로 이어받는다. 이전 generation 행의 valid_until은 남긴다.
func transitionProjection(t *testing.T, pool *pgxpool.Pool, previous int64, change projectionChange) int64 {
	t.Helper()

	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if rollbackErr := tx.Rollback(context.WithoutCancel(t.Context())); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			t.Errorf("rollback projection transition: %v", rollbackErr)
		}
	}()

	next := applyProjectionChange(t, tx, previous, change)

	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}

	return next
}

func applyProjectionChange(t *testing.T, tx pgx.Tx, previous int64, change projectionChange) int64 {
	t.Helper()

	ctx := t.Context()

	var guard bool

	if err := tx.QueryRow(ctx, mustTestSQL("lock_projection_guard.sql")).Scan(&guard); err != nil {
		t.Fatalf("lock projection guard: %v", err)
	}

	if _, err := tx.Exec(ctx, mustTestSQL("retire_projection.sql"), previous); err != nil {
		t.Fatalf("retire projection: %v", err)
	}

	var next int64

	if err := tx.QueryRow(ctx, mustTestSQL("insert_projection.sql"), 0).Scan(&next); err != nil {
		t.Fatalf("insert projection: %v", err)
	}

	droppedSubjects := make([]string, len(change.dropped))
	droppedKinds := make([]string, len(change.dropped))

	for i, target := range change.dropped {
		droppedSubjects[i] = target.subject
		droppedKinds[i] = string(target.kind)
	}

	if _, err := tx.Exec(ctx, mustTestSQL("carry_targets.sql"), previous, next, droppedSubjects, droppedKinds); err != nil {
		t.Fatalf("carry targets: %v", err)
	}

	for _, target := range change.added {
		if _, err := tx.Exec(ctx, mustTestSQL("insert_target_since.sql"), next, target.subject, target.kind,
			target.interval.Milliseconds(), target.enabled, next); err != nil {
			t.Fatalf("add target: %v", err)
		}
	}

	return next
}

func contentTargets(subject string) []leaseTarget {
	return []leaseTarget{
		{subject, contract.KindVideoList, time.Minute, true},
		{subject, contract.KindShortsList, time.Minute, true},
	}
}

// 불변식 1·7: 같은 kind의 다른 subject, 다른 kind, 영상 생존 확인 대상이 바뀌어도 이미 획득한 content 작업의
// renew·snapshot·empty complete가 유지된다. Proof의 획득 generation은 바뀌지 않는다.
func TestUnrelatedTargetChurnKeepsAdmittedContentJob(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	first := seedProjection(t, pool, append(contentTargets(subjectUCA), leaseTarget{subjectUCB, contract.KindVideoList, time.Minute, true}))
	repository := newTestRepository(t, pool)
	spec := contentJob(subjectUCA)
	lease := mustAcquireLease(t, repository, spec, "collector-a")

	second := transitionProjection(t, pool, first, projectionChange{
		dropped: []leaseTarget{{subjectUCB, contract.KindVideoList, time.Minute, true}},
		added: []leaseTarget{
			{subjectUCC, contract.KindVideoList, time.Minute, true},
			{videoLiveID, contract.KindVideoLiveCheck, 2 * time.Minute, true},
		},
	})
	third := transitionProjection(t, pool, second, projectionChange{
		dropped: []leaseTarget{{videoLiveID, contract.KindVideoLiveCheck, 2 * time.Minute, true}},
	})

	if err := lease.Renew(ctx); err != nil {
		t.Fatalf("renew after unrelated churn: %v", err)
	}

	proof := lease.Proof()
	if proof.ProjectionGeneration != first || third <= first {
		t.Fatalf("proof generation = %d, want acquisition generation %d (current %d)", proof.ProjectionGeneration, first, third)
	}

	snapshot, err := repository.LoadTargetSnapshot(ctx, &proof, spec, jobContractFor(t, spec), 10)
	if err != nil {
		t.Fatalf("snapshot after unrelated churn: %v", err)
	}

	for _, kind := range []contract.ObservationKind{contract.KindVideoList, contract.KindShortsList} {
		roster, rosterErr := snapshot.Roster(kind)
		if rosterErr != nil || len(roster) != 1 || roster[0] != subjectUCA {
			t.Fatalf("%s roster = %#v, %v", kind, roster, rosterErr)
		}
	}

	if snapshot.Generation() != first {
		t.Fatalf("snapshot generation = %d, want acquisition generation %d", snapshot.Generation(), first)
	}

	if err := lease.CompleteCurrent(ctx); err != nil {
		t.Fatalf("complete after unrelated churn: %v", err)
	}
}

// 불변식 2·5: 자기 범위의 제거·비활성·cadence 변경·추가는 소유를 잃지 않은 채 superseded로 거절되고,
// 자기 증명으로 fenced superseded release할 수 있다. RETIRED 행의 valid_until이 남아 있어도 허용하지 않는다.
func TestOwnMembershipChangeSupersedesWithoutOwnerLoss(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		initial []leaseTarget
		change  projectionChange
		mutate  string
	}{
		{
			name:    "removed",
			initial: contentTargets(subjectUCA),
			change:  projectionChange{dropped: []leaseTarget{{subjectUCA, contract.KindShortsList, time.Minute, true}}},
		},
		{
			name:    "disabled in place",
			initial: contentTargets(subjectUCA),
			mutate:  `UPDATE youtube_collection_targets SET enabled = FALSE WHERE projection_generation = $1 AND observation_kind = 'shorts_list'`,
		},
		{
			name:    "cadence changed",
			initial: contentTargets(subjectUCA),
			change: projectionChange{
				dropped: []leaseTarget{{subjectUCA, contract.KindShortsList, time.Minute, true}},
				added:   []leaseTarget{{subjectUCA, contract.KindShortsList, 2 * time.Minute, true}},
			},
		},
		{
			name:    "kind added",
			initial: []leaseTarget{{subjectUCA, contract.KindVideoList, time.Minute, true}},
			change:  projectionChange{added: []leaseTarget{{subjectUCA, contract.KindShortsList, time.Minute, true}}},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := t.Context()
			pool := dbtest.NewPool(t)
			first := seedProjection(t, pool, testCase.initial)
			repository := newTestRepository(t, pool)
			spec := contentJob(subjectUCA)
			lease := mustAcquireLease(t, repository, spec, "collector-a")

			current := transitionProjection(t, pool, first, testCase.change)
			if testCase.mutate != "" {
				if _, err := pool.Exec(ctx, testCase.mutate, current); err != nil {
					t.Fatal(err)
				}
			}

			assertSuperseded(t, repository, lease, spec)

			if err := lease.Release(ctx, ReleaseSuperseded); err != nil {
				t.Fatalf("fenced superseded release: %v", err)
			}
		})
	}
}

func assertSuperseded(t *testing.T, repository *Repository, lease *JobLease, spec *JobSpec) {
	t.Helper()

	ctx := t.Context()
	proof := lease.Proof()

	if err := lease.Renew(ctx); !errors.Is(err, ErrTargetDisabled) || errors.Is(err, ErrFenceLost) {
		t.Fatalf("renew error = %v, want membership superseded without owner loss", err)
	}

	if _, err := repository.LoadTargetSnapshot(ctx, &proof, spec, jobContractFor(t, spec), 10); !errors.Is(err, ErrTargetDisabled) {
		t.Fatalf("snapshot error = %v, want membership superseded", err)
	}

	if err := lease.CompleteCurrent(ctx); !errors.Is(err, ErrTargetDisabled) {
		t.Fatalf("complete error = %v, want membership superseded", err)
	}
}

// 불변식 3: 제거 후 같은 속성으로 재등록(ABA)해도 새 generation부터의 대상이므로 과거 lease를 허용하지 않는다.
// 새로 획득한 lease는 재등록 이후 범위로 정상 동작한다.
func TestRemovedAndReaddedTargetDoesNotRevalidateOldLease(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	first := seedProjection(t, pool, contentTargets(subjectUCA))
	repository := newTestRepository(t, pool)
	spec := contentJob(subjectUCA)
	old := mustAcquireLease(t, repository, spec, "collector-a")
	shorts := leaseTarget{subjectUCA, contract.KindShortsList, time.Minute, true}

	second := transitionProjection(t, pool, first, projectionChange{dropped: []leaseTarget{shorts}})
	transitionProjection(t, pool, second, projectionChange{added: []leaseTarget{shorts}})

	assertSuperseded(t, repository, old, spec)

	if err := old.Release(ctx, ReleaseSuperseded); err != nil {
		t.Fatalf("release old lease: %v", err)
	}

	renewed := mustAcquireLease(t, repository, spec, "collector-a")
	if err := renewed.Renew(ctx); err != nil {
		t.Fatalf("renew lease acquired after re-registration: %v", err)
	}

	if err := renewed.CompleteCurrent(ctx); err != nil {
		t.Fatalf("complete lease acquired after re-registration: %v", err)
	}
}

// 불변식 4: owner·epoch·scheduled_for·ACTIVE·만료와 유효 CURRENT 존재 검사는 membership 판정보다 우선해 유지된다.
func TestOwnershipAndProjectionChecksRemainAuthoritative(t *testing.T) {
	t.Run("expired lease is owner loss", testExpiredLeaseIsOwnerLoss)
	t.Run("owner loss outranks stale projection", testOwnerLossOutranksStaleProjection)
	t.Run("legacy lease without recorded scope fails closed", testLegacyLeaseWithoutScopeFailsClosed)
	t.Run("unproven current membership is not admitted", testUnprovenMembershipIsNotAdmitted)
}

func testExpiredLeaseIsOwnerLoss(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, contentTargets(subjectUCA))

	repository := newTestRepository(t, pool)
	spec := contentJob(subjectUCA)
	lease := mustAcquireLease(t, repository, spec, "collector-a")

	if _, err := pool.Exec(ctx, mustTestSQL("expire_lease.sql"), spec.JobKey); err != nil {
		t.Fatal(err)
	}

	if err := lease.Renew(ctx); !errors.Is(err, ErrFenceLost) {
		t.Fatalf("expired renew error = %v", err)
	}

	if err := lease.CompleteCurrent(ctx); !errors.Is(err, ErrFenceLost) {
		t.Fatalf("expired complete error = %v", err)
	}

	takeover := mustAcquireLease(t, repository, spec, "collector-b")

	if err := lease.Release(ctx, ReleaseSuperseded); !errors.Is(err, ErrFenceLost) {
		t.Fatalf("stale release error = %v, must not release another owner", err)
	}

	if err := takeover.Renew(ctx); err != nil {
		t.Fatalf("takeover renew: %v", err)
	}
}

func testOwnerLossOutranksStaleProjection(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	generation := seedProjection(t, pool, contentTargets(subjectUCA))
	repository := newTestRepository(t, pool)
	spec := contentJob(subjectUCA)
	lease := mustAcquireLease(t, repository, spec, "collector-a")

	if _, err := pool.Exec(ctx, mustTestSQL("expire_projection.sql"), generation); err != nil {
		t.Fatal(err)
	}

	if err := lease.Renew(ctx); !errors.Is(err, ErrProjectionStale) {
		t.Fatalf("stale projection renew error = %v", err)
	}

	if _, err := pool.Exec(ctx, mustTestSQL("expire_lease.sql"), spec.JobKey); err != nil {
		t.Fatal(err)
	}

	if err := lease.CompleteCurrent(ctx); !errors.Is(err, ErrFenceLost) {
		t.Fatalf("expired lease on stale projection complete error = %v", err)
	}
}

func testLegacyLeaseWithoutScopeFailsClosed(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, contentTargets(subjectUCA))

	repository := newTestRepository(t, pool)
	spec := contentJob(subjectUCA)
	lease := mustAcquireLease(t, repository, spec, "collector-a")

	if _, err := pool.Exec(ctx, mustTestSQL("clear_lease_membership.sql"), spec.JobKey); err != nil {
		t.Fatal(err)
	}

	assertSuperseded(t, repository, lease, spec)
}

func testUnprovenMembershipIsNotAdmitted(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	generation := seedProjection(t, pool, contentTargets(subjectUCA))

	if _, err := pool.Exec(ctx, mustTestSQL("null_target_membership.sql"), generation); err != nil {
		t.Fatal(err)
	}

	if _, err := newTestRepository(t, pool).Acquire(ctx, contentJob(subjectUCA), "collector-a"); !errors.Is(err, ErrInvalidJob) {
		t.Fatalf("acquire with NULL member_since_generation error = %v", err)
	}
}

// 불변식 6: retention이 lease 행을 지우고 다시 만들면 fence_epoch가 1부터 재시작한다. 과거 증명은 같은 owner·epoch여도
// scheduled_for 등 증명 전체가 달라 renew·complete·release가 모두 소유 손실로 거절되며, 유효성은 lease 이력이 아니라
// 재사용되지 않는 projection generation에 묶인다.
func TestLeaseRecreatedAfterRetentionDoesNotReviveOldProof(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, contentTargets(subjectUCA))

	repository := newTestRepository(t, pool)
	spec := contentJob(subjectUCA)
	old := mustAcquireLease(t, repository, spec, "collector-a")

	if _, err := pool.Exec(ctx, mustTestSQL("delete_lease.sql"), spec.JobKey); err != nil {
		t.Fatal(err)
	}

	recreated := mustAcquireLease(t, repository, spec, "collector-a")
	if recreated.Proof().FenceEpoch != old.Proof().FenceEpoch || recreated.Proof() == old.Proof() {
		t.Fatalf("recreated proof = %#v, old = %#v; want reused epoch with a distinct proof", recreated.Proof(), old.Proof())
	}

	if err := old.Renew(ctx); !errors.Is(err, ErrFenceLost) {
		t.Fatalf("old proof renew error = %v", err)
	}

	if err := old.CompleteCurrent(ctx); !errors.Is(err, ErrFenceLost) {
		t.Fatalf("old proof complete error = %v", err)
	}

	if err := old.Release(ctx, ReleaseSuperseded); !errors.Is(err, ErrFenceLost) {
		t.Fatalf("old proof release error = %v", err)
	}

	if err := recreated.Renew(ctx); err != nil {
		t.Fatalf("recreated renew: %v", err)
	}
}

// not_before는 신규 후보·acquire에만 적용된다. 이미 획득한 작업의 renew·snapshot·complete는 영향을 받지 않는다.
func TestNotBeforeChangesOnlyGateNewAdmission(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	generation := seedProjection(t, pool, []leaseTarget{{videoLiveID, contract.KindVideoLiveCheck, time.Minute, true}})
	repository := newTestRepository(t, pool)
	spec := &JobSpec{
		JobKey:   "collector:youtubejs:youtubejs_video_live:" + videoLiveID,
		Provider: contract.ProviderYouTubeJS, Class: "SUBJECT",
		CollectionJobKind: "youtubejs_video_live", SubjectKey: videoLiveID, PollInterval: time.Minute,
	}
	lease := mustAcquireLease(t, repository, spec, "collector-a")

	if _, err := pool.Exec(ctx, mustTestSQL("set_not_before.sql"), generation, time.Hour.Milliseconds()); err != nil {
		t.Fatal(err)
	}

	proof := lease.Proof()
	if err := lease.Renew(ctx); err != nil {
		t.Fatalf("renew after future not_before: %v", err)
	}

	if _, err := repository.LoadTargetSnapshot(ctx, &proof, spec, jobContractFor(t, spec), 10); err != nil {
		t.Fatalf("snapshot after future not_before: %v", err)
	}

	if err := lease.CompleteCurrent(ctx); err != nil {
		t.Fatalf("complete after future not_before: %v", err)
	}

	if _, err := pool.Exec(ctx, mustTestSQL("make_lease_overdue.sql"), spec.JobKey); err != nil {
		t.Fatal(err)
	}

	if jobs := candidateJobs(t, repository, contract.ProviderYouTubeJS, "youtubejs_video_live", 10); len(jobs) != 0 {
		t.Fatalf("candidates before not_before = %+v", jobs)
	}

	if _, err := repository.Acquire(ctx, spec, "collector-b"); !errors.Is(err, ErrNotAcquired) {
		t.Fatalf("acquire before not_before error = %v", err)
	}

	if _, err := pool.Exec(ctx, mustTestSQL("set_not_before.sql"), generation, -time.Second.Milliseconds()); err != nil {
		t.Fatal(err)
	}

	if jobs := candidateJobs(t, repository, contract.ProviderYouTubeJS, "youtubejs_video_live", 10); len(jobs) != 1 {
		t.Fatalf("candidates after not_before = %+v", jobs)
	}

	mustAcquireLease(t, repository, spec, "collector-b")
}

// API 전환 중 guard를 기다린 snapshot·acquire는 잠금 이후 새 snapshot에서 평가되어 사라진 generation 때문에
// 거절되지 않는다(false stale 없음, 재시도 없음).
func TestGuardWaitersEvaluateFreshSnapshotAfterTransition(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	first := seedProjection(t, pool, append(contentTargets(subjectUCA), contentTargets(subjectUCB)...))
	repository := newTestRepository(t, pool)
	spec := contentJob(subjectUCA)
	lease := mustAcquireLease(t, repository, spec, "collector-a")
	proof := lease.Proof()
	other := contentJob(subjectUCB)
	job := jobContractFor(t, spec)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			t.Errorf("rollback projection transition: %v", rollbackErr)
		}
	}()

	second := applyProjectionChange(t, tx, first, projectionChange{
		added: []leaseTarget{{subjectUCC, contract.KindVideoList, time.Minute, true}},
	})

	type snapshotResult struct {
		snapshot TargetSnapshot
		err      error
	}

	snapshots := make(chan snapshotResult, 1)
	acquired := make(chan error, 1)

	go func() {
		snapshot, loadErr := repository.LoadTargetSnapshot(ctx, &proof, spec, job, 10)
		snapshots <- snapshotResult{snapshot: snapshot, err: loadErr}
	}()

	go func() {
		otherLease, acquireErr := repository.Acquire(ctx, other, "collector-b")
		if acquireErr == nil && otherLease.Proof().ProjectionGeneration != second {
			acquireErr = errors.New("guard waiter acquired an old generation")
		}

		acquired <- acquireErr
	}()

	select {
	case got := <-snapshots:
		t.Fatalf("snapshot did not wait for the projection guard: %+v", got)
	case got := <-acquired:
		t.Fatalf("acquire did not wait for the projection guard: %v", got)
	case <-time.After(200 * time.Millisecond):
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	got := <-snapshots
	if got.err != nil || got.snapshot.Generation() != first {
		t.Fatalf("guard waiter snapshot = generation %d, %v", got.snapshot.Generation(), got.err)
	}

	if err := <-acquired; err != nil {
		t.Fatalf("guard waiter acquire: %v", err)
	}
}

// 전역 CURRENT_PROJECTION 작업은 자기 roster kind 전체가 범위다. 다른 kind 변경은 무관하고, 같은 kind의 subject 추가는
// roster를 바꾸므로 superseded다.
func TestGlobalProjectionJobScopeCoversRosterKinds(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	first := seedProjection(t, pool, []leaseTarget{
		{subjectUCA, contract.KindLiveSnapshot, time.Minute, true},
		{subjectUCB, contract.KindLiveSnapshot, time.Minute, true},
	})
	repository := newTestRepository(t, pool)
	spec := holodexLiveJob()
	lease := mustAcquireLease(t, repository, spec, "collector-a")

	second := transitionProjection(t, pool, first, projectionChange{
		added: []leaseTarget{{subjectUCA, contract.KindVideoList, time.Minute, true}},
	})

	if err := lease.Renew(ctx); err != nil {
		t.Fatalf("global renew after unrelated kind change: %v", err)
	}

	transitionProjection(t, pool, second, projectionChange{
		added: []leaseTarget{{subjectUCC, contract.KindLiveSnapshot, time.Minute, true}},
	})
	assertSuperseded(t, repository, lease, spec)
}

// 소유는 유지됐지만 membership이 무효가 된 renew는 callback을 취소·join한 뒤에만 superseded로 fenced release한다.
func TestSupersededRenewReleasesOnlyAfterCallbackJoin(t *testing.T) {
	config := testConfig()

	config.RenewInterval = 10 * time.Millisecond

	repository := &Repository{config: config}
	lease := &orderedReleaseLease{renewErr: ErrTargetDisabled}
	result := repository.Run(t.Context(), lease, func(ctx context.Context, _ contract.LeaseProof) error {
		<-ctx.Done()
		lease.callbackDone.Store(true)

		return ctx.Err()
	})

	if result.Outcome != LeaseRunReleasedAfterSuperseded || !errors.Is(result.Err, ErrTargetDisabled) || errors.Is(result.Err, ErrFenceLost) {
		t.Fatalf("run result = %#v", result)
	}

	if lease.releaseCalls.Load() != 1 || lease.lastRelease != ReleaseSuperseded || !lease.releasedAfterJoin.Load() {
		t.Fatalf("release calls = %d reason = %q after join = %t", lease.releaseCalls.Load(), lease.lastRelease, lease.releasedAfterJoin.Load())
	}
}

func TestSupersededRenewDoesNotReleaseWhenCallbackJoinTimesOut(t *testing.T) {
	config := testConfig()

	config.RenewInterval = 5 * time.Millisecond
	config.CleanupTimeout = 20 * time.Millisecond

	repository := &Repository{config: config}
	lease := &fakeLease{renewErr: ErrProjectionStale}
	releaseRunner := make(chan struct{})
	result := repository.Run(t.Context(), lease, func(context.Context, contract.LeaseProof) error {
		<-releaseRunner

		return nil
	})

	close(releaseRunner)

	if result.Outcome != LeaseRunCleanupTimedOut || !errors.Is(result.Err, ErrProjectionStale) {
		t.Fatalf("run result = %#v", result)
	}

	if lease.releaseCalls.Load() != 0 {
		t.Fatalf("release calls = %d, want none before callback join", lease.releaseCalls.Load())
	}
}

type orderedReleaseLease struct {
	fakeLease

	callbackDone      atomic.Bool
	releasedAfterJoin atomic.Bool
}

func (l *orderedReleaseLease) Release(ctx context.Context, reason ReleaseReason) error {
	l.releasedAfterJoin.Store(l.callbackDone.Load())

	return l.fakeLease.Release(ctx, reason)
}
