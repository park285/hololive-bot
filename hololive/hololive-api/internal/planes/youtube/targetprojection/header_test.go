package targetprojection

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

type projectionTupleKey struct {
	table string
	key   string
}

// projectionTupleVersions는 값 비교로 놓치는 no-op UPDATE까지 실제 MVCC tuple identity로 관측한다.
func projectionTupleVersions(tb testing.TB, pool *pgxpool.Pool) map[projectionTupleKey]string {
	tb.Helper()

	rows, err := pool.Query(tb.Context(), `
		SELECT 'header',generation::text,xmin::text||':'||ctid::text FROM youtube_collection_projection_generations
		UNION ALL
		SELECT 'target',jsonb_build_array(projection_generation,subject_key,observation_kind)::text,xmin::text||':'||ctid::text
		FROM youtube_collection_targets
		UNION ALL
		SELECT 'reason',jsonb_build_array(projection_generation,subject_key,observation_kind,reason_kind,reason_key)::text,xmin::text||':'||ctid::text
		FROM youtube_collection_target_reasons`)
	require.NoError(tb, err)

	defer rows.Close()

	versions := make(map[projectionTupleKey]string)

	for rows.Next() {
		var (
			key     projectionTupleKey
			version string
		)

		require.NoError(tb, rows.Scan(&key.table, &key.key, &version))

		versions[key] = version
	}

	require.NoError(tb, rows.Err())

	return versions
}

func changedProjectionTuples(before, after map[projectionTupleKey]string) map[string]int {
	changed := make(map[string]int)

	for key, version := range after {
		if before[key] != version {
			changed[key.table]++
		}
	}

	for key := range before {
		if _, exists := after[key]; !exists {
			changed[key.table]++
		}
	}

	return changed
}

func requireEligibilityVersion(t *testing.T, pool *pgxpool.Pool, generation, want int64) {
	t.Helper()

	var version int64

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT eligibility_version FROM youtube_collection_projection_generations WHERE generation=$1`, generation).Scan(&version))
	require.Equal(t, want, version)
}

func requireValidityRefreshedAt(t *testing.T, pool *pgxpool.Pool, generation int64, want time.Time) {
	t.Helper()

	var refreshedAt time.Time

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT validity_refreshed_at FROM youtube_collection_projection_generations WHERE generation=$1`, generation).Scan(&refreshedAt))
	require.True(t, refreshedAt.Equal(want.Truncate(time.Microsecond)), "refresh clock = %s, want %s", refreshedAt, want)
}

func TestHeartbeatAppliesValidityChangesAcrossRestarts(t *testing.T) {
	for _, count := range []int{0, 1} {
		t.Run(fmt.Sprintf("targets_%d", count), func(t *testing.T) {
			f := newProjectionFixture(t, time.Now().UTC().Truncate(time.Microsecond).Add(-30*time.Second))
			targets, reasons := capacityTargets(count)
			input := staticBuilder{targets: targets, reasons: reasons}
			first := mustRefresh(t, f.refresher, input, f.base, "initial")
			requireValidityRefreshedAt(t, f.pool, first.Generation, f.base)

			for _, step := range []struct {
				name           string
				validity       time.Duration
				offset         time.Duration
				wantRefreshed  time.Duration
				wantExpiration time.Duration
			}{
				{"decrease", MinValidity, time.Second, time.Second, 6 * time.Second},
				{"increase at same time", time.Hour, time.Second, time.Second, time.Hour + time.Second},
				{"decrease at same time", MinValidity, time.Second, time.Second, 6 * time.Second},
				{"increase", time.Hour, 2 * time.Second, 2 * time.Second, time.Hour + 2*time.Second},
				{"reordered same TTL", time.Hour, time.Second, 2 * time.Second, time.Hour + 2*time.Second},
				{"reordered different TTL", MinValidity, time.Second, 2 * time.Second, time.Hour + 2*time.Second},
				{"restart same TTL", time.Hour, 3*time.Second + 900*time.Nanosecond, 3 * time.Second, time.Hour + 3*time.Second},
				{"same PostgreSQL microsecond", MinValidity, 3*time.Second + 100*time.Nanosecond, 3 * time.Second, 8 * time.Second},
			} {
				// 매 호출마다 재생성하여 메모리 상태 없이 DB header만으로 정책과 호출 순서를 보존하는지 확인한다.
				refresher, err := NewRefresher(f.pool, step.validity)
				require.NoError(t, err)

				before := projectionTupleVersions(t, f.pool)
				got := mustRefresh(t, refresher, input, f.base.Add(step.offset), step.name)
				require.False(t, got.Changed, step.name)
				require.Equal(t, first.Generation, got.Generation, step.name)
				require.Equal(t, first.SHA256, got.SHA256, step.name)
				require.Equal(t, map[string]int{"header": 1}, changedProjectionTuples(before, projectionTupleVersions(t, f.pool)), step.name)
				requireEligibilityVersion(t, f.pool, first.Generation, 1)
				requireValidityRefreshedAt(t, f.pool, first.Generation, f.base.Add(step.wantRefreshed))
				assertGenerationValidUntil(t, f.pool, first.Generation, f.base.Add(step.wantExpiration))

				if count > 0 {
					valid := membershipValidity{t: t, pool: f.pool, proof: first.Generation}
					valid.require(step.name, []string{string(targets[0].ObservationKind)}, true, targets[0].SubjectKey, 1, step.wantExpiration > time.Hour)
				}
			}
		})
	}
}

func TestHeartbeatEstablishesUnknownValidityClock(t *testing.T) {
	f := newProjectionFixture(t, projectionNow)
	targets, reasons := capacityTargets(1)
	input := staticBuilder{targets: targets, reasons: reasons}
	first := mustRefresh(t, f.refresher, input, f.base, "initial")
	_, err := f.pool.Exec(t.Context(), `UPDATE youtube_collection_projection_generations SET validity_refreshed_at=NULL WHERE generation=$1`, first.Generation)
	require.NoError(t, err)

	refresher, err := NewRefresher(f.pool, MinValidity)
	require.NoError(t, err)

	got := mustRefresh(t, refresher, input, f.base.Add(time.Second), "first heartbeat after migration")
	require.Equal(t, first.Generation, got.Generation)
	requireValidityRefreshedAt(t, f.pool, first.Generation, f.base.Add(time.Second))
	assertGenerationValidUntil(t, f.pool, first.Generation, f.base.Add(time.Second+MinValidity))
}

func TestHeartbeatWritesOnlyHeaderAndVersionsChangedEligibility(t *testing.T) {
	f := newProjectionFixture(t, time.Now().UTC().Truncate(time.Microsecond))
	targets, reasons := capacityTargets(3)
	input := staticBuilder{targets: targets, reasons: reasons}
	first := mustRefresh(t, f.refresher, input, f.base, "initial")
	initialMember := loadMembership(t, f.pool, first.Generation, targets[0])
	requireEligibilityVersion(t, f.pool, first.Generation, 1)

	before := projectionTupleVersions(t, f.pool)
	heartbeat := mustRefresh(t, f.refresher, input, f.base.Add(time.Minute), "unchanged heartbeat")
	require.False(t, heartbeat.Changed)
	require.Equal(t, first.Generation, heartbeat.Generation)

	changed := changedProjectionTuples(before, projectionTupleVersions(t, f.pool))
	require.Equal(t, map[string]int{"header": 1}, changed, "unchanged values must not hide target/reason tuple writes")
	requireEligibilityVersion(t, f.pool, first.Generation, 1)
	assertGenerationValidUntil(t, f.pool, first.Generation, f.base.Add(61*time.Minute))

	// guard를 뒤늦게 얻은 호출의 과거 시각으로 기존 header 유효기간을 줄이지 않는다.
	mustRefresh(t, f.refresher, input, f.base.Add(30*time.Second), "reordered heartbeat clock")
	assertGenerationValidUntil(t, f.pool, first.Generation, f.base.Add(61*time.Minute))

	before = projectionTupleVersions(t, f.pool)
	targets[0].NotBefore = f.base.Add(3 * time.Minute)
	targets[1].NotBefore = f.base.Add(4 * time.Minute)

	freshness := mustRefresh(t, f.refresher, input, f.base.Add(2*time.Minute), "two eligibility changes")
	require.Equal(t, first.Generation, freshness.Generation)
	require.Equal(t, first.SHA256, freshness.SHA256)
	requireEligibilityVersion(t, f.pool, first.Generation, 2)
	require.Equal(t, map[string]int{"header": 1, "target": 2}, changedProjectionTuples(before, projectionTupleVersions(t, f.pool)))

	member := loadMembership(t, f.pool, first.Generation, targets[0])
	require.Equal(t, initialMember.memberSince, member.memberSince)
	require.Equal(t, initialMember.createdAt, member.createdAt)

	before = projectionTupleVersions(t, f.pool)
	mustRefresh(t, f.refresher, input, f.base.Add(3*time.Minute), "same eligibility")
	requireEligibilityVersion(t, f.pool, first.Generation, 2)
	require.Equal(t, map[string]int{"header": 1}, changedProjectionTuples(before, projectionTupleVersions(t, f.pool)))

	targets[0].NotBefore = time.Time{}

	mustRefresh(t, f.refresher, input, f.base.Add(4*time.Minute), "eligibility becomes NULL")
	requireEligibilityVersion(t, f.pool, first.Generation, 3)
	require.False(t, loadMembership(t, f.pool, first.Generation, targets[0]).notBefore.Valid)

	reasons[0].ReasonKey = "different-reason"

	mustRefresh(t, f.refresher, input, f.base.Add(5*time.Minute), "reason only")
	requireEligibilityVersion(t, f.pool, first.Generation, 3)

	targets[0].Priority++

	structural := mustRefresh(t, f.refresher, input, f.base.Add(6*time.Minute), "structural")
	require.True(t, structural.Changed)
	requireEligibilityVersion(t, f.pool, structural.Generation, 1)
	requireValidityRefreshedAt(t, f.pool, structural.Generation, f.base.Add(6*time.Minute))
}

func TestFailedRefreshNeverRenewsHeaderOrEligibility(t *testing.T) {
	f := newProjectionFixture(t, time.Now().UTC().Truncate(time.Microsecond))
	targets, reasons := capacityTargets(1)
	input := staticBuilder{targets: targets, reasons: reasons}
	first := mustRefresh(t, f.refresher, input, f.base, "initial")
	before := projectionTupleVersions(t, f.pool)

	for _, failed := range []staticBuilder{
		{err: errors.New("input unavailable")},
		{targets: []TargetSpec{{SubjectKey: "invalid"}}},
	} {
		_, err := f.refresher.Refresh(t.Context(), failed, f.base.Add(time.Minute))
		require.Error(t, err)
		require.Equal(t, before, projectionTupleVersions(t, f.pool))
	}

	// reason INSERT 실패는 이미 실행한 target eligibility/header 갱신도 함께 rollback해야 한다.
	_, err := f.pool.Exec(t.Context(), `
		CREATE FUNCTION reject_projection_reason_test() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'injected reason write failure'; END; $$;
		CREATE TRIGGER reject_projection_reason_test BEFORE INSERT ON youtube_collection_target_reasons
		FOR EACH ROW EXECUTE FUNCTION reject_projection_reason_test();`)
	require.NoError(t, err)

	targets[0].NotBefore = f.base.Add(3 * time.Minute)
	reasons[0].ReasonKey = "replacement"

	shortRefresher, err := NewRefresher(f.pool, MinValidity)
	require.NoError(t, err)

	_, err = shortRefresher.Refresh(t.Context(), input, f.base.Add(2*time.Minute))
	require.ErrorContains(t, err, "injected reason write failure")
	require.Equal(t, before, projectionTupleVersions(t, f.pool))
	requireEligibilityVersion(t, f.pool, first.Generation, 1)
	assertGenerationValidUntil(t, f.pool, first.Generation, f.base.Add(time.Hour))
	requireValidityRefreshedAt(t, f.pool, first.Generation, f.base)
}

type projectionBuilderFunc func(context.Context, dbx.Tx, time.Time) ([]TargetSpec, []TargetReason, error)

func (build projectionBuilderFunc) Build(ctx context.Context, tx dbx.Tx, now time.Time) ([]TargetSpec, []TargetReason, error) {
	return build(ctx, tx, now)
}

func TestProjectionGuardSerializesBuildBeforeInputReads(t *testing.T) {
	f := newProjectionFixture(t, time.Now().UTC().Truncate(time.Microsecond))
	_, err := f.pool.Exec(t.Context(), `CREATE TABLE projection_input_test(priority smallint NOT NULL); INSERT INTO projection_input_test VALUES(40)`)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	entered := make(chan struct{})
	release := make(chan struct{})
	secondBuilt := make(chan struct{}, 1)
	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)
	target := TargetSpec{SubjectKey: subjectChannelA, ObservationKind: contract.KindCommunityPage, Priority: 60, PollInterval: time.Minute, Enabled: true}

	firstBuilder := blockedProjectionInputBuilder(target, entered, release)
	secondBuilder := readingProjectionInputBuilder(target, secondBuilt)

	go func() {
		_, err := f.refresher.Refresh(ctx, firstBuilder, f.base.Add(time.Minute))
		firstDone <- err
	}()

	select {
	case <-entered:
	case err := <-firstDone:
		t.Fatalf("first writer did not enter Build: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	go func() {
		_, err := f.refresher.Refresh(ctx, secondBuilder, f.base)
		secondDone <- err
	}()

	select {
	case <-secondBuilt:
		t.Fatal("overlapping writer read stale input before acquiring the projection guard")
	case err := <-secondDone:
		t.Fatalf("overlapping writer unexpectedly returned: %v", err)
	case <-time.After(300 * time.Millisecond):
	}

	close(release)
	require.NoError(t, <-firstDone)
	require.NoError(t, <-secondDone)

	assertSerializedProjectionInput(t, f.pool, f.base.Add(61*time.Minute))
}

func blockedProjectionInputBuilder(target TargetSpec, entered chan<- struct{}, release <-chan struct{}) projectionBuilderFunc {
	return func(ctx context.Context, tx dbx.Tx, _ time.Time) ([]TargetSpec, []TargetReason, error) {
		if _, err := tx.Exec(ctx, `UPDATE projection_input_test SET priority=60`); err != nil {
			return nil, nil, fmt.Errorf("update writer input: %w", err)
		}

		close(entered)

		select {
		case <-release:
			return []TargetSpec{target}, nil, nil
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}
}

func readingProjectionInputBuilder(target TargetSpec, built chan<- struct{}) projectionBuilderFunc {
	return func(ctx context.Context, tx dbx.Tx, _ time.Time) ([]TargetSpec, []TargetReason, error) {
		latest := target
		if err := tx.QueryRow(ctx, `SELECT priority FROM projection_input_test`).Scan(&latest.Priority); err != nil {
			return nil, nil, fmt.Errorf("read writer input: %w", err)
		}

		built <- struct{}{}

		return []TargetSpec{latest}, nil, nil
	}
}

func assertSerializedProjectionInput(t *testing.T, pool *pgxpool.Pool, validUntil time.Time) {
	t.Helper()

	var (
		priority   int16
		generation int64
	)

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT g.generation,t.priority
		FROM youtube_collection_projection_generations g JOIN youtube_collection_targets t ON t.projection_generation=g.generation
		WHERE g.status='CURRENT'`).Scan(&generation, &priority))
	require.Equal(t, int16(60), priority, "later Build must read the committed input, not overwrite it with an earlier snapshot")
	requireEligibilityVersion(t, pool, generation, 1)
	assertGenerationValidUntil(t, pool, generation, validUntil)
}
