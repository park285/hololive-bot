package joblease

import (
	"context"
	"database/sql"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/dbmigrate"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
)

type candidateTraffic struct {
	rx         atomic.Int64
	tx         atomic.Int64
	headers    atomic.Int64
	candidates atomic.Int64
	oracle     atomic.Int64
}

type candidateTrafficConn struct {
	net.Conn

	traffic *candidateTraffic
}

func (c *candidateTrafficConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.traffic.rx.Add(int64(n))

	return n, err
}

func (c *candidateTrafficConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	c.traffic.tx.Add(int64(n))

	if err != nil {
		return n, fmt.Errorf("write measured candidate connection: %w", err)
	}

	return n, nil
}

func (c *candidateTraffic) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	switch data.SQL {
	case sqlProjectionCurrent, mustTestSQL("projection_260_bd498868a.sql"):
		c.headers.Add(1)
	case sqlCandidates, sqlCandidatesGlobal:
		c.candidates.Add(1)
	case mustTestSQL("candidate_260_bd498868a.sql"):
		c.oracle.Add(1)
	}

	return ctx
}
func (*candidateTraffic) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}
func (c *candidateTraffic) reset() {
	c.rx.Store(0)
	c.tx.Store(0)
	c.headers.Store(0)
	c.candidates.Store(0)
	c.oracle.Store(0)
}

// paired_elapsed는 이전·현재 fleet의 교차 실행 전체이며 개별 SQL 지연이 아닙니다.
func (c *candidateTraffic) report(t *testing.T, phase string, start time.Time) {
	t.Helper()
	t.Logf("phase=%s paired_elapsed=%s wire_rx=%d wire_tx=%d header_reads=%d candidate_reads=%d old_candidate_reads=%d", phase, time.Since(start), c.rx.Load(), c.tx.Load(), c.headers.Load(), c.candidates.Load(), c.oracle.Load())
}

func measuredCandidatePool(t *testing.T, base *pgxpool.Pool) (*pgxpool.Pool, *candidateTraffic) {
	t.Helper()

	traffic := new(candidateTraffic)
	config := base.Config()

	config.MaxConns, config.MinConns = 1, 0

	dial := config.ConnConfig.DialFunc

	config.ConnConfig.DialFunc = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := dial(ctx, network, address)
		if err != nil {
			return nil, err
		}

		return &candidateTrafficConn{Conn: conn, traffic: traffic}, nil
	}
	config.ConnConfig.Tracer = traffic

	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(pool.Close)

	if err := pool.Ping(t.Context()); err != nil {
		t.Fatal(err)
	}

	traffic.reset()

	return pool, traffic
}

// TestProjectionCandidateTraffic는 이전 260 스키마와 현재 261 스키마에서 동일 입력을 비교합니다.
// 이전 SQL 원출처는 bd498868a의 joblease/queries이며 testqueries에 원문 그대로 보관합니다.
// 각 AP는 독립 pool을 사용합니다. 준비·fixture 트래픽은 제외하고 page 손실과 wire 증가를 검출합니다.
// 지연과 EXPLAIN 실행시간은 측정값이며 CPU 사용률 또는 운영 환경 성능을 뜻하지 않습니다.
func TestProjectionCandidateTraffic(t *testing.T) {
	for _, count := range []int{620, 1240, contract.MaxProjectionTargetCount} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			current := newCandidateTrafficFleet(t, dbtest.NewPool(t), count, false)
			legacy := newCandidateTrafficFleet(t, candidateSchema260Pool(t), count, true)

			for _, state := range []string{"new", "not-due", "due", "mixed", "distinct-due", "eligibility"} {
				compareCandidateTrafficState(t, current, legacy, count, state)
			}
		})
	}
}

func compareCandidateTrafficState(t *testing.T, current, legacy *candidateTrafficFleet, count int, state string) {
	t.Helper()

	for _, fleet := range []*candidateTrafficFleet{legacy, current} {
		seedCandidateTrafficState(t, fleet.base, fleet.generation, state)

		if _, err := fleet.base.Exec(t.Context(), "ANALYZE youtube_collection_targets; ANALYZE youtube_collection_job_leases"); err != nil {
			t.Fatal(err)
		}
	}

	for _, fleet := range []*candidateTrafficFleet{legacy, current} {
		fleet.warm(t)
	}

	started := time.Now()

	for cycle := range 120 {
		if state == "eligibility" {
			legacy.advanceEligibility(t, cycle)
			current.advanceEligibility(t, cycle)
		}

		oldPages := legacy.cycle(t)
		pages := current.cycle(t)

		for ap := range pages {
			if !reflect.DeepEqual(pages[ap], oldPages[ap]) {
				t.Fatalf("상태=%s cycle=%d AP=%d: 현재 page=%+v 이전 page=%+v", state, cycle, ap, pages[ap], oldPages[ap])
			}

			assertCandidateTrafficPage(t, pages[ap], state, cycle, count)
		}
	}

	oldBytes, currentBytes := legacy.report(t, state, started), current.report(t, state, started)
	if currentBytes > oldBytes {
		t.Fatalf("동일 page의 warm wire 회귀: 현재=%d 이전260=%d", currentBytes, oldBytes)
	}

	legacy.explain(t, state)
	current.explain(t, state)
}

type candidateTrafficFleet struct {
	base         *pgxpool.Pool
	pools        [4]*pgxpool.Pool
	traffic      [4]*candidateTraffic
	repositories [4]*Repository
	generation   int64
	legacy       bool
	job          collection.JobContract
}

// 모든 연결을 같은 횟수 예열하며 statement cache와 plan 준비를 측정에서 뺍니다.
func (f *candidateTrafficFleet) warm(t *testing.T) {
	t.Helper()

	for range 2 {
		f.cycle(t)
	}

	for _, traffic := range f.traffic {
		traffic.reset()
	}
}

func newCandidateTrafficFleet(t *testing.T, pool *pgxpool.Pool, count int, legacy bool) *candidateTrafficFleet {
	t.Helper()

	fleet := &candidateTrafficFleet{base: pool, legacy: legacy, job: mustTestJob(t, contract.ProviderYouTubeJS, "community_collect")}

	fleet.generation = seedProjection(t, pool, nil)

	columns, expiry := "", ""

	if legacy {
		columns, expiry = ",valid_until", ",TIMESTAMPTZ '2099-01-01 00:00:00+00'"
	}

	query := `INSERT INTO youtube_collection_targets
		(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled,member_since_generation` + columns + `)
		SELECT $1,'channel:'||lpad(i::text,5,'0'),'community_page',50,60000,true,$1` + expiry + `
		FROM generate_series(1,$2::int) i`
	if _, err := pool.Exec(t.Context(), query, fleet.generation, count); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(t.Context(), `UPDATE youtube_collection_projection_generations SET row_count=$2,valid_until='2099-01-01' WHERE generation=$1`, fleet.generation, count); err != nil {
		t.Fatal(err)
	}

	for ap := range fleet.pools {
		fleet.pools[ap], fleet.traffic[ap] = measuredCandidatePool(t, pool)
		fleet.repositories[ap] = newTestRepository(t, fleet.pools[ap])
	}

	return fleet
}

// 261을 역변환하지 않고 빈 DB에 260까지 실제 migration을 재생합니다.
// 260까지의 migration은 bd498868a와 동일하며 임시 manifest는 DB 격리에만 사용합니다.
func candidateSchema260Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("이전 schema migration 위치를 확인하지 못했습니다")
	}

	source := filepath.Join(filepath.Dir(file), "../../../../hololive-api/scripts/migrations")

	if configured := os.Getenv("HOLOLIVE_MIGRATIONS_DIR"); configured != "" {
		source = configured
	}

	sourceRoot, err := os.OpenRoot(source)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if closeErr := sourceRoot.Close(); closeErr != nil {
			t.Errorf("원본 migration root 닫기: %v", closeErr)
		}
	})

	entries, err := dbmigrate.Manifest(sourceRoot.FS())
	if err != nil {
		t.Fatal(err)
	}

	cutoff := slices.Index(entries, "260_youtube_video_novelty_evidence.sql")
	if cutoff < 0 {
		t.Fatal("260 migration이 없습니다")
	}

	dir := t.TempDir()

	targetRoot, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if closeErr := targetRoot.Close(); closeErr != nil {
			t.Errorf("이전 migration root 닫기: %v", closeErr)
		}
	})

	var selected strings.Builder

	for i, filename := range entries[:cutoff+1] {
		body, readErr := fs.ReadFile(sourceRoot.FS(), filename)
		if readErr != nil {
			t.Fatal(readErr)
		}

		if writeErr := targetRoot.WriteFile(filename, body, 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}

		_, _ = fmt.Fprintf(&selected, "%03d %s\n", i+1, filename)
	}

	if err := targetRoot.WriteFile(dbmigrate.ManifestName, []byte(selected.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOLOLIVE_MIGRATIONS_DIR", dir)

	return dbtest.NewPool(t)
}

func (f *candidateTrafficFleet) cycle(t *testing.T) [4]CandidatePage {
	t.Helper()

	var (
		pages [4]CandidatePage
		errs  [4]error
		wg    sync.WaitGroup
	)

	start := make(chan struct{})

	for ap := range f.pools {
		wg.Go(func() {
			<-start

			pages[ap], errs[ap] = f.readPage(t.Context(), ap)
		})
	}

	close(start)
	wg.Wait()

	for ap, err := range errs {
		if err != nil {
			t.Fatalf("이전260=%t AP=%d: %v", f.legacy, ap, err)
		}
	}

	return pages
}

func (f *candidateTrafficFleet) readPage(ctx context.Context, ap int) (CandidatePage, error) {
	if !f.legacy {
		generation, err := f.repositories[ap].CurrentProjectionGeneration(ctx)
		if err != nil {
			return CandidatePage{}, err
		}

		if generation != f.generation {
			return CandidatePage{}, fmt.Errorf("현재 generation=%d 예상=%d", generation, f.generation)
		}

		return f.repositories[ap].CandidatesForProjection(ctx, generation, f.job, nil, 10)
	}

	var generation int64

	if err := f.pools[ap].QueryRow(ctx, mustTestSQL("projection_260_bd498868a.sql")).Scan(&generation); err != nil {
		return CandidatePage{}, err
	}

	if generation != f.generation {
		return CandidatePage{}, fmt.Errorf("이전 generation=%d 예상=%d", generation, f.generation)
	}

	rows, err := f.pools[ap].Query(ctx, mustTestSQL("candidate_260_bd498868a.sql"), f.arguments()...)
	if err != nil {
		return CandidatePage{}, err
	}
	defer rows.Close()

	return collectCandidatePage(rows, f.job, 10)
}

func (f *candidateTrafficFleet) arguments() []any {
	return []any{f.generation, []string{string(contract.KindCommunityPage)}, string(contract.ProviderYouTubeJS), "community_collect", []string{}, 10}
}

func (f *candidateTrafficFleet) advanceEligibility(t *testing.T, cycle int) {
	t.Helper()

	// 매 cycle 실제 후보가 출입하므로 eligibility version만 읽고 page를 재사용하는 회귀도 잡습니다.
	_, err := f.base.Exec(t.Context(), `UPDATE youtube_collection_targets SET not_before=CASE WHEN $2::boolean THEN NULL ELSE TIMESTAMPTZ '2099-01-01' END WHERE projection_generation=$1 AND subject_key='channel:00001'`, f.generation, cycle%2 == 0)
	if err != nil {
		t.Fatal(err)
	}

	if !f.legacy {
		if _, err := f.base.Exec(t.Context(), `UPDATE youtube_collection_projection_generations SET eligibility_version=eligibility_version+1 WHERE generation=$1`, f.generation); err != nil {
			t.Fatal(err)
		}
	}
}

func assertCandidateTrafficPage(t *testing.T, page CandidatePage, state string, cycle, count int) {
	t.Helper()

	if state == "not-due" {
		if len(page.Jobs) != 0 || page.Truncated {
			t.Fatalf("미도래 후보가 반환되었습니다: %+v", page)
		}

		return
	}

	if len(page.Jobs) != 10 || !page.Truncated {
		t.Fatalf("page 손실: 상태=%s %+v", state, page)
	}

	for i, job := range page.Jobs {
		if job.PollInterval != time.Minute {
			t.Fatalf("후보 poll interval 변경: %+v", job)
		}

		n := i + 1

		switch state {
		case "eligibility":
			if cycle%2 != 0 {
				n++
			}
		case "mixed":
			n = (i/5)*10 + 5 + i%5
		case "distinct-due":
			n = count - i
		}

		if job.SubjectKey != fmt.Sprintf("channel:%05d", n) {
			t.Fatalf("후보 순서 변경: 상태=%s %+v", state, page)
		}
	}
}

func (f *candidateTrafficFleet) report(t *testing.T, state string, started time.Time) int64 {
	t.Helper()

	var total int64

	for ap, traffic := range f.traffic {
		traffic.report(t, fmt.Sprintf("%s-schema260=%t-AP%d", state, f.legacy, ap), started)

		reads := traffic.candidates.Load()

		if f.legacy {
			reads = traffic.oracle.Load()
		}

		if traffic.headers.Load() != 120 || reads != 120 {
			t.Fatalf("불완전한 동시 cycle: AP%d header=%d candidate=%d", ap, traffic.headers.Load(), reads)
		}

		total += traffic.rx.Load() + traffic.tx.Load()
	}

	return total
}

func (f *candidateTrafficFleet) explain(t *testing.T, state string) {
	t.Helper()

	for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
		f.explainMode(t, state, mode)
	}
}

func (f *candidateTrafficFleet) explainMode(t *testing.T, state, mode string) {
	t.Helper()

	query := sqlCandidates

	if f.legacy {
		query = mustTestSQL("candidate_260_bd498868a.sql")
	}

	tx, err := f.base.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
		defer cancel()

		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("EXPLAIN transaction rollback: %v", err)
		}
	}()

	if _, err := tx.Exec(t.Context(), "SET LOCAL plan_cache_mode = "+mode); err != nil {
		t.Fatal(err)
	}

	if _, err := tx.Exec(t.Context(), "PREPARE candidate_traffic_plan(bigint,text[],text,text,text[],integer) AS "+query); err != nil {
		t.Fatal(err)
	}

	// EXECUTE의 인수는 고정 fixture 값과 DB가 발급한 정수 generation뿐입니다.
	explain := fmt.Sprintf(`EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) EXECUTE candidate_traffic_plan(%d,ARRAY['community_page']::text[],'youtubejs','community_collect',ARRAY[]::text[],10)`, f.generation)

	var raw []byte

	for range 2 {
		if err := tx.QueryRow(t.Context(), explain).Scan(&raw); err != nil {
			t.Fatal(err)
		}
	}

	requireCandidatePlanMode(t, tx, state, f.legacy, mode)

	if _, err := tx.Exec(t.Context(), "DEALLOCATE candidate_traffic_plan"); err != nil {
		t.Fatal(err)
	}

	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}

	reportCandidatePlan(t, state, f.legacy, mode, raw)
}

func requireCandidatePlanMode(t *testing.T, tx pgx.Tx, state string, legacy bool, mode string) {
	t.Helper()

	var generic, custom int64

	if err := tx.QueryRow(t.Context(), `SELECT generic_plans,custom_plans FROM pg_prepared_statements WHERE name='candidate_traffic_plan'`).Scan(&generic, &custom); err != nil {
		t.Fatal(err)
	}

	if (mode == "force_generic_plan" && (generic != 2 || custom != 0)) || (mode == "force_custom_plan" && (custom != 2 || generic != 0)) {
		t.Fatalf("실제 prepared plan 불일치: mode=%s generic=%d custom=%d", mode, generic, custom)
	}

	t.Logf("schema260=%t 상태=%s plan=%s generic_plans=%d custom_plans=%d", legacy, state, mode, generic, custom)
}

func reportCandidatePlan(t *testing.T, state string, legacy bool, mode string, raw []byte) {
	t.Helper()

	var plans []struct {
		ExecutionMS float64 `json:"Execution Time"`
		Plan        struct {
			Hits       int64 `json:"Shared Hit Blocks"`
			Reads      int64 `json:"Shared Read Blocks"`
			TempReads  int64 `json:"Temp Read Blocks"`
			TempWrites int64 `json:"Temp Written Blocks"`
		} `json:"Plan"`
	}

	if err := jsonv2.Unmarshal(raw, &plans); err != nil {
		t.Fatal(err)
	}

	if len(plans) != 1 {
		t.Fatalf("EXPLAIN 결과 수=%d", len(plans))
	}

	t.Logf("종류=community_collect 상태=%s schema260=%t plan=%s 예열=1 실행_ms=%.3f shared_hit=%d shared_read=%d temp_read=%d temp_write=%d", state, legacy, mode, plans[0].ExecutionMS, plans[0].Plan.Hits, plans[0].Plan.Reads, plans[0].Plan.TempReads, plans[0].Plan.TempWrites)
}

func seedCandidateTrafficState(t *testing.T, pool *pgxpool.Pool, generation int64, state string) {
	t.Helper()

	var query string

	switch state {
	case "new":
		return
	case "not-due":
		// 완료한 lease의 불변식 next_due_at = scheduled_for + poll_interval을 지킨다. 실효 due는 둘 중 이른 값이다.
		if _, err := pool.Exec(t.Context(), `INSERT INTO youtube_collection_job_leases (job_key,provider,job_class,collection_job_kind,subject_key,projection_generation,poll_interval_ms,scheduled_for,next_due_at)
            SELECT 'collector:youtubejs:community_collect:'||subject_key,'youtubejs','SUBJECT','community_collect',subject_key,$1,60000,TIMESTAMPTZ '2099-01-01'-INTERVAL '1 minute',TIMESTAMPTZ '2099-01-01'
            FROM youtube_collection_targets WHERE projection_generation=$1`, generation); err != nil {
			t.Fatal(err)
		}

		return
	case "due":
		query = `UPDATE youtube_collection_job_leases SET next_due_at=TIMESTAMPTZ '2000-01-01'`
	case "mixed":
		query = `UPDATE youtube_collection_job_leases SET next_due_at=CASE WHEN right(subject_key,1) IN ('0','1','2','3','4') THEN TIMESTAMPTZ '2099-01-01' ELSE TIMESTAMPTZ '2000-01-01' END`
	case "distinct-due":
		query = `UPDATE youtube_collection_job_leases SET next_due_at=TIMESTAMPTZ '2000-01-01'-(right(subject_key,5)::integer*INTERVAL '1 millisecond')`
	case "eligibility":
		query = `UPDATE youtube_collection_job_leases SET next_due_at=TIMESTAMPTZ '2000-01-01'`
	}

	if _, err := pool.Exec(t.Context(), query); err != nil {
		t.Fatal(err)
	}
}

func oracleCandidateSubjects(t *testing.T, pool *pgxpool.Pool, generation int64, excluded []string, limit int) []string {
	t.Helper()

	if excluded == nil {
		excluded = []string{}
	}

	rows, err := pool.Query(t.Context(), mustTestSQL("candidate_order_oracle.sql"), generation, []string{string(contract.KindCommunityPage)}, string(contract.ProviderYouTubeJS), "community_collect", excluded, limit)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var subjects []string

	for rows.Next() {
		var (
			current      bool
			subject      sql.NullString
			minMS, maxMS sql.NullInt64
		)

		if err := rows.Scan(&current, &subject, &minMS, &maxMS); err != nil {
			t.Fatal(err)
		}

		if !current {
			t.Fatal("oracle projection stale")
		}

		if subject.Valid {
			subjects = append(subjects, subject.String)
		}
	}

	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	return subjects
}

func TestCandidateOrderMatchesRelationalOracle(t *testing.T) {
	pool := dbtest.NewPool(t)
	generation := seedCandidateScale(t, pool, 620)

	if _, err := pool.Exec(t.Context(), `INSERT INTO youtube_collection_job_leases (job_key, provider, job_class, collection_job_kind, subject_key, projection_generation, poll_interval_ms, scheduled_for, next_due_at)
		SELECT 'collector:youtubejs:community_collect:' || subject_key, 'youtubejs', 'SUBJECT', 'community_collect', subject_key, $1, 60000,
		CASE WHEN right(subject_key,1) IN ('0','1') THEN clock_timestamp()+INTERVAL '59 minutes' ELSE clock_timestamp()-INTERVAL '61 minutes' END,
		CASE WHEN right(subject_key,1) IN ('0','1') THEN clock_timestamp()+INTERVAL '1 hour' ELSE clock_timestamp()-INTERVAL '1 hour' END
		FROM youtube_collection_targets WHERE projection_generation=$1 AND right(subject_key,1) NOT IN ('8','9')`, generation); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(t.Context(), `UPDATE youtube_collection_targets SET priority=CASE WHEN right(subject_key,1)='9' THEN 80 ELSE 20 END,
		not_before=CASE WHEN right(subject_key,1)='8' THEN clock_timestamp()+INTERVAL '1 hour' ELSE NULL END WHERE projection_generation=$1`, generation); err != nil {
		t.Fatal(err)
	}

	repository := newTestRepository(t, pool)
	excluded := []string{"collector:youtubejs:community_collect:channel:00009"}
	page := candidatePage(t, repository, contract.ProviderYouTubeJS, "community_collect", excluded, 10)
	oracle := oracleCandidateSubjects(t, pool, generation, excluded, 10)
	got := make([]string, len(page.Jobs))

	for i, job := range page.Jobs {
		got[i] = job.SubjectKey
	}

	if page.Truncated != (len(oracle) > 10) || !slices.Equal(got, oracle[:min(len(oracle), 10)]) {
		t.Fatalf("local=%v truncated=%v oracle=%v", got, page.Truncated, oracle)
	}
}
