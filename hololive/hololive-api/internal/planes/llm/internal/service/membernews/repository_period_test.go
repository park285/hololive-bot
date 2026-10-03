package membernews

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/filter"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/model"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/summarizer"
	dbtest "github.com/kapu/hololive-dbtest"
)

func newPeriodCandidatePool(tb testing.TB) (*Repository, *pgxpool.Pool) {
	tb.Helper()

	pool := dbtest.NewBlankPool(tb)

	_, err := pool.Exec(tb.Context(), `CREATE TABLE major_events (
  id int PRIMARY KEY, type text NOT NULL, title text, description text, members text[],
  pub_date timestamptz, event_start_date date, link text, status text NOT NULL, link_status text);
  CREATE INDEX ON major_events(event_start_date);
  CREATE INDEX ON major_events(status,type,event_start_date)`)
	if err != nil {
		tb.Fatal(err)
	}

	return &Repository{pool: newPGXMemberNewsQuerier(pool)}, pool
}

func TestPeriodCandidateSQLPreservesDatesValuesOrderAndFallback(t *testing.T) {
	repository, pool := newPeriodCandidatePool(t)
	ctx := t.Context()

	_, err := pool.Exec(ctx, `INSERT INTO major_events VALUES
  (1,'news','미코 동률 행사','body',ARRAY['미코'],'2026-10-02T03:00:00Z','2020-01-01','https://hololivepro.com/z','active','unchecked'),
  (2,'news','미코 동률 행사','body',ARRAY['미코'],'2026-10-02T03:00:00Z',NULL,'https://hololivepro.com/a','active',NULL),
  (3,'news','미코 outside pub',NULL,ARRAY['미코'],'2020-01-01','2026-10-02','https://hololivepro.com/3','active','unchecked'),
  (4,'event','미코 outside event',NULL,ARRAY['미코'],'2026-10-02','2020-01-01','https://hololivepro.com/4','active','unchecked'),
  (5,'event','미코 fallback pub',NULL,ARRAY['미코'],'2026-10-02',NULL,'https://hololivepro.com/5','active','unchecked'),
  (6,'news','미코 fallback date',NULL,ARRAY['미코'],NULL,'2026-10-02','https://hololivepro.com/6','active','unchecked'),
  (7,'news','미코 no date',NULL,ARRAY['미코'],NULL,NULL,'https://hololivepro.com/7','active','unchecked'),
  (8,'news','미코 lower edge',NULL,ARRAY['미코'],'2026-09-24T15:00:00Z',NULL,'https://hololivepro.com/8','active','unchecked'),
  (9,'news','미코 below lower',NULL,ARRAY['미코'],'2026-09-24T14:59:59.999999Z',NULL,'https://hololivepro.com/9','active','unchecked'),
  (10,'news','미코 upper edge',NULL,ARRAY['미코'],'2026-10-23T14:59:59.999999Z',NULL,'https://hololivepro.com/10','active','unchecked'),
  (11,'news','미코 above upper',NULL,ARRAY['미코'],'2026-10-23T15:00:00Z',NULL,'https://hololivepro.com/11','active','unchecked'),
  (12,'event','미코 UTC date in KST',NULL,ARRAY['미코'],NULL,'2026-09-25','https://hololivepro.com/12','active','unchecked'),
  (13,'news','미코 blocked',NULL,ARRAY['미코'],'2026-10-02',NULL,'https://hololivepro.com/13','active','blocked'),
  (14,'news','미코 inactive',NULL,ARRAY['미코'],'2026-10-02',NULL,'https://hololivepro.com/14','ended','unchecked'),
  (15,'news',NULL,NULL,NULL,'2026-10-02',NULL,NULL,'active','unchecked')`)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, time.October, 2, 12, 0, 0, 0, time.FixedZone("KST", 9*3600))

	for _, timezone := range []string{"UTC", "Asia/Seoul", "America/New_York"} {
		if _, err := pool.Exec(ctx, "SET TIME ZONE '"+timezone+"'"); err != nil {
			t.Fatal(err)
		}

		for _, period := range []model.Period{model.PeriodWeekly, model.PeriodMonthly, "이번달"} {
			all, err := repository.ListActiveMajorEvents(ctx)
			if err != nil {
				t.Fatal(err)
			}

			selected, err := repository.ListActiveMajorEventsForPeriod(ctx, period, now)
			if err != nil {
				t.Fatal(err)
			}

			want := filter.FilterCandidates(all, period, now, []string{"미코"}, nil, nil)
			got := filter.FilterCandidates(selected, period, now, []string{"미코"}, nil, nil)

			if !reflect.DeepEqual(got, want) {
				t.Fatalf("timezone=%s period=%s values/order differ\ngot=%#v\nwant=%#v", timezone, period, got, want)
			}

			prepared := filter.PrepareCandidates(selected, period, now).Filter([]string{"미코"}, nil, nil)
			if !reflect.DeepEqual(prepared, want) {
				t.Fatalf("timezone=%s period=%s prepared values/order differ", timezone, period)
			}

			if !reflect.DeepEqual(summarizer.BuildDeterministicFallback(period, got), summarizer.BuildDeterministicFallback(period, want)) {
				t.Fatalf("timezone=%s period=%s fallback differs", timezone, period)
			}
		}
	}
}

func TestPeriodCandidateSQLRetainsNonfiniteScanErrors(t *testing.T) {
	repository, pool := newPeriodCandidatePool(t)
	ctx := t.Context()
	now := time.Date(2026, time.October, 2, 12, 0, 0, 0, time.UTC)

	for _, dates := range []string{"'infinity',NULL", "'-infinity',NULL", "'2020-01-01','infinity'", "'2020-01-01','-infinity'"} {
		if _, err := pool.Exec(ctx, "TRUNCATE major_events"); err != nil {
			t.Fatal(err)
		}

		if _, err := pool.Exec(ctx, "INSERT INTO major_events VALUES (1,'news','미코 invalid','',ARRAY['미코'],"+dates+",'https://hololivepro.com/x','active','unchecked')"); err != nil {
			t.Fatal(err)
		}

		for _, period := range []model.Period{model.PeriodWeekly, model.PeriodMonthly} {
			_, oldErr := repository.ListActiveMajorEvents(ctx)
			_, newErr := repository.ListActiveMajorEventsForPeriod(ctx, period, now)

			if oldErr == nil || newErr == nil || !strings.Contains(oldErr.Error(), "Infinity") || !strings.Contains(newErr.Error(), "Infinity") {
				t.Fatalf("dates=%s full=%v period=%v", dates, oldErr, newErr)
			}
		}
	}
}

type candidateQueryCost struct {
	rows, bytes, final int
	err                error
}

func candidateStringsSize(candidates []model.Candidate) int {
	size := 0

	for i := range candidates {
		size += len(candidates[i].Title) + len(candidates[i].Description) + len(candidates[i].SourceURL)
	}

	return size
}

func candidateQueryIteration(ctx context.Context, repository *Repository, now time.Time, prepared *filter.PreparedCandidates, variant string) candidateQueryCost {
	if prepared != nil {
		return candidateQueryCost{final: len(prepared.Filter([]string{"미코"}, nil, nil))}
	}

	var (
		candidates []model.Candidate
		err        error
	)

	if variant == "full" {
		candidates, err = repository.ListActiveMajorEvents(ctx)
	} else {
		candidates, err = repository.ListActiveMajorEventsForPeriod(ctx, model.PeriodWeekly, now)
	}

	if err != nil {
		return candidateQueryCost{err: err}
	}

	return candidateQueryCost{rows: len(candidates), bytes: candidateStringsSize(candidates), final: len(filter.FilterCandidates(candidates, model.PeriodWeekly, now, []string{"미코"}, nil, nil))}
}

func updateConcurrencyPeak(peak *atomic.Int32, current int32) {
	for {
		prev := peak.Load()
		if current <= prev || peak.CompareAndSwap(prev, current) {
			return
		}
	}
}

func candidateQueryBatch(ctx context.Context, repository *Repository, now time.Time, variant string) (rows, bytes, final int, peak int32, err error) {
	var prepared *filter.PreparedCandidates

	if variant == "snapshot" {
		candidates, queryErr := repository.ListActiveMajorEventsForPeriod(ctx, model.PeriodWeekly, now)
		if queryErr != nil {
			return 0, 0, 0, 0, queryErr
		}

		rows = len(candidates)
		bytes = candidateStringsSize(candidates)
		prepared = filter.PrepareCandidates(candidates, model.PeriodWeekly, now)
	}

	results := make(chan candidateQueryCost, 20)

	var (
		wg                sync.WaitGroup
		firstWave         sync.WaitGroup
		active, maxActive atomic.Int32
	)

	firstWave.Add(5)

	start := make(chan struct{})

	for range 5 {
		wg.Go(func() {
			for iteration := range 4 {
				current := active.Add(1)
				updateConcurrencyPeak(&maxActive, current)

				if iteration == 0 {
					firstWave.Done()
					<-start
				}

				result := candidateQueryIteration(ctx, repository, now, prepared, variant)

				active.Add(-1)

				results <- result
			}
		})
	}

	firstWave.Wait()
	close(start)

	wg.Wait()
	close(results)

	for result := range results {
		if result.err != nil {
			err = result.err
		}

		rows += result.rows
		bytes += result.bytes
		final += result.final
	}

	return rows, bytes, final, maxActive.Load(), err
}

func seedBenchmarkCandidates(b *testing.B, pool *pgxpool.Pool, total, percent int) {
	b.Helper()

	if _, err := pool.Exec(b.Context(), "TRUNCATE major_events"); err != nil {
		b.Fatal(err)
	}

	_, err := pool.Exec(b.Context(), `INSERT INTO major_events SELECT i,'news','미코 행사 '||i,repeat('미코 공식 기사 ',64),ARRAY['미코'],CASE WHEN i%100<$2 THEN '2026-10-02T03:00:00Z'::timestamptz ELSE '2020-01-01T00:00:00Z'::timestamptz END,NULL,'https://hololivepro.com/'||i,'active','unchecked' FROM generate_series(1,$1) i`, total, percent)
	if err != nil {
		b.Fatal(err)
	}

	if _, err := pool.Exec(b.Context(), "ANALYZE major_events"); err != nil {
		b.Fatal(err)
	}
}

func benchmarkCandidateQueryVariant(b *testing.B, repository *Repository, pool *pgxpool.Pool, now time.Time, total, percent int, variant string) {
	b.Helper()

	if _, _, final, _, err := candidateQueryBatch(b.Context(), repository, now, variant); err != nil || final != total*percent/100*20 {
		b.Fatalf("warmup count=%d error=%v", final, err)
	}

	b.ReportAllocs()

	var (
		rows, bytes int
		peak        int32
	)

	waitBefore := pool.Stat().EmptyAcquireCount()

	for b.Loop() {
		var (
			final int
			err   error
		)

		rows, bytes, final, peak, err = candidateQueryBatch(b.Context(), repository, now, variant)

		if err != nil || final != total*percent/100*20 {
			b.Fatalf("count=%d error=%v", final, err)
		}
	}

	b.ReportMetric(float64(rows), "rows/op")
	b.ReportMetric(float64(bytes), "string-bytes/op")
	b.ReportMetric(float64(peak), "parallel-peak")
	b.ReportMetric(float64(pool.Stat().EmptyAcquireCount()-waitBefore)/float64(b.N), "pool-waits/op")
}

// 합성 후보만 사용한다. LLM/운영 처리량을 측정하지 않고 실제 병렬5·선택률·pool 대기를 함께 기록한다.
func BenchmarkMemberNewsCandidateQueries(b *testing.B) {
	repository, pool := newPeriodCandidatePool(b)
	now := time.Date(2026, time.October, 2, 12, 0, 0, 0, time.FixedZone("KST", 9*3600))

	b.Logf("synthetic=true rooms=20 parallelism=5 gomaxprocs=%d pool_max=%d indexes=existing_only", runtime.GOMAXPROCS(0), pool.Config().MaxConns)

	for _, total := range []int{1000, 10000} {
		for _, percent := range []int{1, 10, 100} {
			seedBenchmarkCandidates(b, pool, total, percent)

			assertMatrixCandidateEquivalence(b, repository, now)

			for _, variant := range []string{"full", "period", "snapshot"} {
				b.Run(fmt.Sprintf("rows=%d/select=%d/%s", total, percent, variant), func(b *testing.B) {
					benchmarkCandidateQueryVariant(b, repository, pool, now, total, percent, variant)
				})
			}
		}
	}
}

func assertMatrixCandidateEquivalence(tb testing.TB, repository *Repository, now time.Time) {
	tb.Helper()

	all, err := repository.ListActiveMajorEvents(tb.Context())
	if err != nil {
		tb.Fatal(err)
	}

	selected, err := repository.ListActiveMajorEventsForPeriod(tb.Context(), model.PeriodWeekly, now)
	if err != nil {
		tb.Fatal(err)
	}

	want := filter.FilterCandidates(all, model.PeriodWeekly, now, []string{"미코"}, nil, nil)
	got := filter.FilterCandidates(selected, model.PeriodWeekly, now, []string{"미코"}, nil, nil)
	prepared := filter.PrepareCandidates(selected, model.PeriodWeekly, now).Filter([]string{"미코"}, nil, nil)

	if !reflect.DeepEqual(want, got) || !reflect.DeepEqual(want, prepared) {
		tb.Fatal("matrix full/period/prepared full values or order differ")
	}
}

func TestPeriodCandidateSQLRetainsMemberArrayScanErrors(t *testing.T) {
	repository, pool := newPeriodCandidatePool(t)
	ctx := t.Context()
	now := time.Date(2026, time.October, 2, 12, 0, 0, 0, time.UTC)

	for _, array := range []string{"ARRAY['미코',NULL]", "ARRAY[['미코','아쿠아'],[NULL,'스이세이']]", "'[0:1]={미코,NULL}'::text[]"} {
		if _, err := pool.Exec(ctx, "TRUNCATE major_events"); err != nil {
			t.Fatal(err)
		}

		_, err := pool.Exec(ctx, "INSERT INTO major_events VALUES (1,'news','미코 invalid','',"+array+",'2020-01-01',NULL,'https://hololivepro.com/x','active','unchecked')")
		if err != nil {
			t.Fatal(err)
		}

		for _, period := range []model.Period{model.PeriodWeekly, model.PeriodMonthly} {
			_, fullErr := repository.ListActiveMajorEvents(ctx)
			_, periodErr := repository.ListActiveMajorEventsForPeriod(ctx, period, now)

			if fullErr == nil || periodErr == nil || fullErr.Error() != periodErr.Error() {
				t.Fatalf("array=%s period=%s full=%v period query=%v", array, period, fullErr, periodErr)
			}
		}
	}
}

func TestPeriodCandidateSQLPreservesMultidimensionalAndBoundedMemberArrays(t *testing.T) {
	repository, pool := newPeriodCandidatePool(t)
	ctx := t.Context()

	_, err := pool.Exec(ctx, `INSERT INTO major_events VALUES
  (1,'news','미코 multi','',ARRAY[['미코','아쿠아'],['스이세이','소라']],'2026-10-02',NULL,'https://hololivepro.com/1','active','unchecked'),
  (2,'news','미코 lower bound','', '[0:1]={미코,아쿠아}'::text[],'2026-10-02',NULL,'https://hololivepro.com/2','active','unchecked'),
  (3,'news','미코 outside multi','',ARRAY[['미코','아쿠아'],['스이세이','소라']],'2020-01-01',NULL,'https://hololivepro.com/3','active','unchecked'),
  (4,'news','미코 null array','',NULL,'2026-10-02',NULL,'https://hololivepro.com/4','active','unchecked'),
  (5,'news','미코 empty array','',ARRAY[]::text[],'2026-10-02',NULL,'https://hololivepro.com/5','active','unchecked')`)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, time.October, 2, 12, 0, 0, 0, time.UTC)
	assertMatrixCandidateEquivalence(t, repository, now)
}

func TestPeriodCandidateSQLPreservesFullFiniteDateRangeAndPriority(t *testing.T) {
	repository, pool := newPeriodCandidatePool(t)
	ctx := t.Context()

	_, err := pool.Exec(ctx, `INSERT INTO major_events VALUES
  (1,'event','미코 far future','',ARRAY['미코'],NULL,'500000-01-01','https://hololivepro.com/1','active','unchecked'),
  (2,'event','미코 maximum date','',ARRAY['미코'],NULL,'5874897-12-31','https://hololivepro.com/2','active','unchecked'),
  (3,'event','미코 minimum date','',ARRAY['미코'],NULL,'4713-01-01 BC','https://hololivepro.com/3','active','unchecked'),
  (4,'news','미코 primary pub wide secondary','',ARRAY['미코'],'2026-10-02','500000-01-01','https://hololivepro.com/4','active','unchecked'),
  (5,'news','미코 fallback wide date','',ARRAY['미코'],NULL,'500000-01-01','https://hololivepro.com/5','active','unchecked'),
  (6,'event','미코 primary wide date','',ARRAY['미코'],'2026-10-02','500000-01-01','https://hololivepro.com/6','active','unchecked'),
  (7,'event','미코 fallback pub','',ARRAY['미코'],'2026-10-02',NULL,'https://hololivepro.com/7','active','unchecked')`)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, time.October, 2, 12, 0, 0, 0, time.UTC)

	for _, period := range []model.Period{model.PeriodWeekly, model.PeriodMonthly} {
		all, err := repository.ListActiveMajorEvents(ctx)
		if err != nil {
			t.Fatalf("full finite date scan: %v", err)
		}

		selected, err := repository.ListActiveMajorEventsForPeriod(ctx, period, now)
		if err != nil {
			t.Fatalf("period finite date scan: %v", err)
		}

		want := filter.FilterCandidates(all, period, now, []string{"미코"}, nil, nil)
		got := filter.FilterCandidates(selected, period, now, []string{"미코"}, nil, nil)

		if !reflect.DeepEqual(want, got) {
			t.Fatalf("period=%s full values/order differ", period)
		}
	}
}
