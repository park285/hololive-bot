package targetprojection

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

// BenchmarkProjectionStorageCapacity는 합성 대상의 출입과 not_before 갱신이 만드는 실제 PG
// 저장량을 비교한다. 압축 표현은 실험용 임시 테이블이며 운영 이행 구현이 아니다.
// -benchtime=120x는 5초 refresh 10분에 해당하는 입력 횟수다. 실행 시각을 기다리지 않으므로
// 운영 vacuum 주기와 동시 부하 또는 실시간 10분 성능을 재현했다고 해석하지 않는다.
func BenchmarkProjectionStorageCapacity(b *testing.B) {
	for _, targetCount := range []int{620, 1240} {
		for _, mode := range []string{"membership_changes", "freshness_only", "unchanged_heartbeat"} {
			b.Run(fmt.Sprintf("targets_%d/%s", targetCount, mode), func(b *testing.B) {
				benchmarkProjectionStorageCapacity(b, targetCount, mode)
			})
		}
	}
}

func benchmarkProjectionStorageCapacity(b *testing.B, targetCount int, mode string) {
	b.Helper()

	pool := dbtest.NewPool(b)

	refresher, err := NewRefresher(pool, time.Hour)
	if err != nil {
		b.Fatal(err)
	}

	base := time.Now().UTC()
	targets, reasons := capacityTargets(targetCount)

	if _, err := refresher.Refresh(b.Context(), staticBuilder{targets: targets, reasons: reasons}, base); err != nil {
		b.Fatal(err)
	}

	before := capacityRelationBytes(b, pool)
	initialTuples := projectionTupleVersions(b, pool)

	var startLSN string

	if err := pool.QueryRow(b.Context(), "SELECT pg_current_wal_insert_lsn()::text").Scan(&startLSN); err != nil {
		b.Fatal(err)
	}

	ticks := 0

	for b.Loop() {
		ticks++

		now := base.Add(time.Duration(ticks) * 5 * time.Second)

		if mode != "unchanged_heartbeat" {
			targets[len(targets)-1].NotBefore = now.Add(2 * time.Minute)
		}

		input := staticBuilder{targets: targets, reasons: reasons}

		if mode == "membership_changes" && ticks%2 == 1 {
			input.targets = targets[:len(targets)-1]
			input.reasons = reasons[:len(reasons)-1]
		}

		if _, err := refresher.Refresh(b.Context(), input, now); err != nil {
			b.Fatal(err)
		}
	}

	generations := reportCapacityGrowth(b, pool, before, startLSN, ticks)
	if mode != "membership_changes" && generations != 1 {
		b.Fatalf("%s changed generation count to %d", mode, generations)
	}

	reportProjectionTupleChanges(b, pool, initialTuples, mode)

	archiveBytes := measureCapacityArchive(b, pool)
	expectedTargets := targetCount

	if mode == "membership_changes" && ticks%2 == 1 {
		expectedTargets--
	}

	measureCapacityCompaction(b, pool, archiveBytes, expectedTargets)
}

func reportProjectionTupleChanges(b *testing.B, pool *pgxpool.Pool, initialTuples map[projectionTupleKey]string, mode string) {
	b.Helper()

	// xmin/ctid는 같은 값을 쓴 UPDATE도 드러낸다. 전체 UPDATE 횟수가 아니라 살아 있는 변경 tuple 수다.
	changed := changedProjectionTuples(initialTuples, projectionTupleVersions(b, pool))
	b.ReportMetric(float64(changed["target"]), "target-tuples-changed")
	b.ReportMetric(float64(changed["reason"]), "reason-tuples-changed")

	if mode == "unchanged_heartbeat" && (changed["target"] != 0 || changed["reason"] != 0) {
		b.Fatalf("unchanged heartbeat rewrote tuples: %v", changed)
	}

	if mode == "freshness_only" && (changed["target"] != 1 || changed["reason"] != 0) {
		b.Fatalf("freshness-only rewrote unrelated tuples: %v", changed)
	}
}

func reportCapacityGrowth(b *testing.B, pool *pgxpool.Pool, before int64, startLSN string, ticks int) int64 {
	b.Helper()

	var walBytes float64

	if err := pool.QueryRow(b.Context(), "SELECT pg_wal_lsn_diff(pg_current_wal_insert_lsn(), $1::pg_lsn)::float8", startLSN).Scan(&walBytes); err != nil {
		b.Fatal(err)
	}

	var generations int64

	if err := pool.QueryRow(b.Context(), "SELECT count(*) FROM youtube_collection_projection_generations").Scan(&generations); err != nil {
		b.Fatal(err)
	}

	after := capacityRelationBytes(b, pool)
	b.ReportMetric(float64(generations), "generations")
	b.ReportMetric(float64(after), "relation-B")
	b.ReportMetric(float64(after-before)/float64(ticks), "growth-B/refresh")
	b.ReportMetric(walBytes/float64(ticks), "WAL-B/refresh")

	return generations
}

func capacityTargets(count int) ([]TargetSpec, []TargetReason) {
	targets := make([]TargetSpec, count)
	reasons := make([]TargetReason, count)
	kinds := []contract.ObservationKind{
		contract.KindCommunityPage, contract.KindVideoList, contract.KindShortsList,
		contract.KindLiveSnapshot, contract.KindChannelLiveCheck, contract.KindChannelProfile,
		contract.KindChannelPhoto, contract.KindVideoLiveCheck,
	}

	for i := range count {
		digest := sha256.Sum256(fmt.Appendf(nil, "capacity-subject-%d", i))
		subject := fmt.Sprintf("UC%x", digest[:11])

		targets[i] = TargetSpec{
			SubjectKey: subject, ObservationKind: kinds[i%len(kinds)],
			Priority: 50, PollInterval: 2 * time.Minute, Enabled: true,
		}
		reasons[i] = TargetReason{
			SubjectKey: subject, ObservationKind: targets[i].ObservationKind,
			ReasonKind: "operational_roster", ReasonKey: subject,
		}
	}

	return targets, reasons
}

func capacityRelationBytes(b *testing.B, pool *pgxpool.Pool) int64 {
	b.Helper()

	var size int64

	if err := pool.QueryRow(b.Context(), `
		SELECT pg_total_relation_size('youtube_collection_projection_generations')
		     + pg_total_relation_size('youtube_collection_targets')
		     + pg_total_relation_size('youtube_collection_target_reasons')`).Scan(&size); err != nil {
		b.Fatal(err)
	}

	return size
}

func measureCapacityArchive(b *testing.B, pool *pgxpool.Pool) int64 {
	b.Helper()

	conn, err := pool.Acquire(b.Context())
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Release()

	for _, statement := range []string{
		"CREATE TEMP TABLE capacity_projection_archive (generation bigint PRIMARY KEY, snapshot jsonb NOT NULL)",
		"ALTER TABLE capacity_projection_archive ALTER COLUMN snapshot SET COMPRESSION lz4",
		`INSERT INTO capacity_projection_archive
		 SELECT generation.generation, jsonb_build_object(
		   'targets', (SELECT jsonb_agg(to_jsonb(target) ORDER BY subject_key, observation_kind)
		               FROM youtube_collection_targets target
		               WHERE target.projection_generation=generation.generation),
		   'reasons', (SELECT jsonb_agg(to_jsonb(reason) ORDER BY subject_key, observation_kind, reason_kind, reason_key)
		               FROM youtube_collection_target_reasons reason
		               WHERE reason.projection_generation=generation.generation))
		 FROM youtube_collection_projection_generations generation WHERE status='RETIRED'`,
	} {
		if _, err := conn.Exec(b.Context(), statement); err != nil {
			b.Fatal(err)
		}
	}

	var archiveBytes, generations int64

	if err := conn.QueryRow(b.Context(), `SELECT pg_total_relation_size('pg_temp.capacity_projection_archive'), count(*) FROM capacity_projection_archive`).Scan(&archiveBytes, &generations); err != nil {
		b.Fatal(err)
	}

	var equal bool

	if err := conn.QueryRow(b.Context(), `
		SELECT coalesce(bool_and(
		  snapshot->'targets' = (SELECT jsonb_agg(to_jsonb(target) ORDER BY subject_key, observation_kind)
		                        FROM youtube_collection_targets target WHERE target.projection_generation=archive.generation)
		  AND snapshot->'reasons' = (SELECT jsonb_agg(to_jsonb(reason) ORDER BY subject_key, observation_kind, reason_kind, reason_key)
		                            FROM youtube_collection_target_reasons reason WHERE reason.projection_generation=archive.generation)
		), true) FROM capacity_projection_archive archive`).Scan(&equal); err != nil {
		b.Fatal(err)
	}

	if !equal {
		b.Fatal("stored snapshot changed target or reason values")
	}

	b.ReportMetric(float64(archiveBytes), "archive-relation-B")

	if generations > 0 {
		b.ReportMetric(float64(archiveBytes)/float64(generations), "archive-B/generation")
	}

	return archiveBytes
}

// measureCapacityCompaction은 원문 일치가 확인된 합성 RETIRED 행만 관계형 표현에서 지우고
// 같은 격리 DB에서 물리 재작성한다. 운영 lease 경쟁·cutover·복구를 검증하는 구현은 아니다.
func measureCapacityCompaction(b *testing.B, pool *pgxpool.Pool, archiveBytes int64, targetCount int) {
	b.Helper()

	if _, err := pool.Exec(b.Context(), `DELETE FROM youtube_collection_targets target
		USING youtube_collection_projection_generations generation
		WHERE generation.generation=target.projection_generation AND generation.status='RETIRED'`); err != nil {
		b.Fatal(err)
	}

	for _, table := range []string{
		"youtube_collection_target_reasons",
		"youtube_collection_targets",
		"youtube_collection_projection_generations",
	} {
		if _, err := pool.Exec(b.Context(), "VACUUM FULL "+table); err != nil {
			b.Fatal(err)
		}
	}

	var currentTargets int

	if err := pool.QueryRow(b.Context(), `SELECT count(*) FROM youtube_collection_targets target
		JOIN youtube_collection_projection_generations generation ON generation.generation=target.projection_generation
		WHERE generation.status='CURRENT'`).Scan(&currentTargets); err != nil {
		b.Fatal(err)
	}

	if currentTargets != targetCount {
		b.Fatalf("compaction kept %d current targets, want %d", currentTargets, targetCount)
	}

	b.ReportMetric(float64(capacityRelationBytes(b, pool)+archiveBytes), "compacted-with-archive-B")
}
