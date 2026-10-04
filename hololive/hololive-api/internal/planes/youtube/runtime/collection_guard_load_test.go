package runtime

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/youtube/targetprojection"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

// 2249×운영 4종 + 알림 3종 + 영상 1000종 + 전역 1종 = 허용 최대 10000 target입니다.
// 검토된 UPCOMING 500개와 과거 영수증 15000개는 실제 rosterReader 경로로 제외합니다.
func newCollectionGuardLoad(t *testing.T) (*pgxpool.Pool, *targetprojection.Refresher, targetprojection.PolicyBuilder, [4]*pgxpool.Pool) {
	t.Helper()

	pool := dbtest.NewPool(t)
	_, err := pool.Exec(t.Context(), `
		DELETE FROM alarms;
		UPDATE members SET status='graduated',is_graduated=true;
		INSERT INTO members(slug,channel_id,english_name,org,sync_source,status,is_graduated)
		SELECT 'guard-'||n,CASE WHEN n=1 THEN 'load-channel' ELSE 'guard-channel-'||n END,
		       'Guard '||n,'Hololive','manual','active',false FROM generate_series(1,2249) n;
		INSERT INTO alarms(room_id,user_id,channel_id) VALUES ('guard-room','guard-user','load-channel');
		INSERT INTO youtube_live_sessions(video_id,channel_id,status,title,lifecycle_origin)
		SELECT 'guard-live-'||n,'load-channel','LIVE',repeat('x',500),'observed'
		FROM generate_series(1,1000) n;
		INSERT INTO youtube_live_sessions(video_id,channel_id,status,title,lifecycle_origin)
		SELECT 'load-'||n,'load-channel','UPCOMING',repeat('x',500),'legacy_unknown'
		FROM generate_series(1,500) n;
		INSERT INTO youtube_live_reconciliation_heads(video_id,status)
		SELECT video_id,status FROM youtube_live_sessions;
	`)
	require.NoError(t, err)
	seedCollectionLifecycleReceipts(t, pool)

	_, err = pool.Exec(t.Context(), `ANALYZE members; ANALYZE alarms; ANALYZE youtube_live_sessions;
		ANALYZE youtube_live_reconciliation_heads; ANALYZE youtube_live_review_receipts`)
	require.NoError(t, err)

	refresher, err := targetprojection.NewRefresher(pool, time.Hour)
	require.NoError(t, err)

	builder := targetprojection.PolicyBuilder{Reader: rosterReader{}, Schedules: targetprojection.DefaultPolicySchedules()}
	initial, err := refresher.Refresh(t.Context(), builder, time.Now())
	require.NoError(t, err)
	require.Equal(t, targetprojection.MaxTargetCount, initial.RowCount)

	var readers [4]*pgxpool.Pool

	for ap := range readers {
		config := pool.Config()

		config.MaxConns, config.MinConns = 1, 0
		readers[ap], err = pgxpool.NewWithConfig(t.Context(), config)
		require.NoError(t, err)
		t.Cleanup(readers[ap].Close)
		require.NoError(t, readers[ap].Ping(t.Context()))
	}

	return pool, refresher, builder, readers
}

type collectionGuardSample struct {
	generation int64
	guardTime  time.Duration
	totalTime  time.Duration
	err        error
}

// collector가 호출하는 실제 DB 함수를 통해 공유 guard와 현재 header를 확인합니다.
// 전체 acquisition/provider/consume 지연을 측정하는 함수가 아닙니다.
func readCollectionSharedGuard(ctx context.Context, pool *pgxpool.Pool) (sample collectionGuardSample) {
	started := time.Now()

	defer func() { sample.totalTime = time.Since(started) }()

	tx, err := pool.Begin(ctx)
	if err != nil {
		sample.err = err
		return sample
	}

	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()

		if rollbackErr := tx.Rollback(cleanup); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			sample.err = errors.Join(sample.err, rollbackErr)
		}
	}()

	guardStarted := time.Now()

	sample.err = tx.QueryRow(ctx, `SELECT generation FROM lock_current_youtube_collection_projection()`).Scan(&sample.generation)
	sample.guardTime = time.Since(guardStarted)

	if sample.err != nil {
		return sample
	}

	sample.err = tx.Commit(ctx)

	return sample
}

// 실제 최대 입력 Build를 마친 writer가 취소될 때 대기 reader와 만료 header가 회복되는지 확인합니다.
// 채널 장벽은 잠금 상태를 재현하기 위한 것이며 성능 수치에 포함하지 않습니다.
func TestCollectionRealPolicyGuardCancellationAndExpiry(t *testing.T) {
	pool, refresher, builder, readers := newCollectionGuardLoad(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)

	defer cancel()

	entered := make(chan struct{})
	writerCtx, cancelWriter := context.WithCancel(ctx)

	defer cancelWriter()

	writerDone := make(chan error, 1)

	go func() {
		_, refreshErr := refresher.Refresh(writerCtx, collectionPausedPolicy{builder: builder, entered: entered}, time.Now())
		writerDone <- refreshErr
	}()

	select {
	case <-entered:
	case err := <-writerDone:
		t.Fatalf("실제 Build 진입 실패: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	pids, done, cancelReader := startCollectionGuardReaders(ctx, t, readers)

	defer cancelReader()

	waitCollectionGuardReaders(ctx, t, pool, pids[:])

	// 모든 reader statement가 시작된 뒤 만료시킵니다. 잠금 대기 전 statement 시각을
	// 재사용하면 이 header를 유효하다고 오판하므로 실제 DB 함수의 clock 계약을 검증합니다.
	_, err := pool.Exec(ctx, `UPDATE youtube_collection_projection_generations SET valid_until=clock_timestamp() WHERE status='CURRENT'`)
	require.NoError(t, err)

	var before time.Time

	require.NoError(t, pool.QueryRow(ctx, `SELECT valid_until FROM youtube_collection_projection_generations WHERE status='CURRENT'`).Scan(&before))
	cancelReader()

	canceled := awaitCollectionGuardSample(ctx, t, done[0])
	require.ErrorIs(t, canceled.err, context.Canceled)
	cancelWriter()

	select {
	case writerErr := <-writerDone:
		require.ErrorIs(t, writerErr, context.Canceled)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	for ap := 1; ap < len(done); ap++ {
		sample := awaitCollectionGuardSample(ctx, t, done[ap])
		require.ErrorIs(t, sample.err, pgx.ErrNoRows, "만료 CURRENT는 공유 guard 대기 후에도 거부해야 합니다")
	}

	var after time.Time

	require.NoError(t, pool.QueryRow(ctx, `SELECT valid_until FROM youtube_collection_projection_generations WHERE status='CURRENT'`).Scan(&after))
	require.True(t, before.Equal(after), "취소된 Build가 header를 갱신했습니다")

	recovered, err := refresher.Refresh(ctx, builder, time.Now())
	require.NoError(t, err)

	for _, reader := range readers {
		sample := readCollectionSharedGuard(ctx, reader)
		require.NoError(t, sample.err)
		require.Equal(t, recovered.Generation, sample.generation)
	}
}

func startCollectionGuardReaders(ctx context.Context, t *testing.T, readers [4]*pgxpool.Pool) ([4]int32, [4]chan collectionGuardSample, context.CancelFunc) {
	t.Helper()

	var (
		pids [4]int32
		done [4]chan collectionGuardSample
	)

	readerCtx, cancelReader := context.WithCancel(ctx)

	t.Cleanup(cancelReader)

	for ap, reader := range readers {
		require.NoError(t, reader.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pids[ap]))

		done[ap] = make(chan collectionGuardSample, 1)

		go func() {
			readCtx := ctx

			if ap == 0 {
				readCtx = readerCtx
			}

			done[ap] <- readCollectionSharedGuard(readCtx, reader)
		}()
	}

	return pids, done, cancelReader
}

type collectionPausedPolicy struct {
	builder targetprojection.PolicyBuilder
	entered chan struct{}
}

func (b collectionPausedPolicy) Build(ctx context.Context, tx dbx.Tx, now time.Time) ([]targetprojection.TargetSpec, []targetprojection.TargetReason, error) {
	targets, reasons, err := b.builder.Build(ctx, tx, now)
	if err != nil {
		return nil, nil, fmt.Errorf("실제 정책 입력 생성: %w", err)
	}

	close(b.entered)
	<-ctx.Done()

	return targets, reasons, ctx.Err()
}

func waitCollectionGuardReaders(ctx context.Context, t *testing.T, pool *pgxpool.Pool, pids []int32) {
	t.Helper()

	ticker := time.NewTicker(5 * time.Millisecond)

	defer ticker.Stop()

	for {
		var waiting int

		err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE pid=ANY($1::int[]) AND wait_event_type='Lock' AND cardinality(pg_blocking_pids(pid))>0`, pids).Scan(&waiting)
		require.NoError(t, err)

		if waiting == len(pids) {
			return
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("공유 guard reader 대기=%d/%d: %v", waiting, len(pids), ctx.Err())
		}
	}
}

func awaitCollectionGuardSample(ctx context.Context, t *testing.T, done <-chan collectionGuardSample) collectionGuardSample {
	t.Helper()

	select {
	case sample := <-done:
		return sample
	case <-ctx.Done():
		t.Fatal(ctx.Err())

		return collectionGuardSample{err: ctx.Err()}
	}
}

// 고정 30회(공유 reader 120회) 동시 부하의 지연 분포를 기록합니다. 벽시계 임계값은 합격 조건이 아닙니다.
// 측정 범위는 최대 입력의 실제 Refresh와 공유 guard SQL/조회 transaction이며 수집→intent가 아닙니다.
func TestCollectionRealPolicySharedGuardLoad(t *testing.T) {
	_, refresher, builder, readers := newCollectionGuardLoad(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)

	defer cancel()

	var guardTimes, transactionTimes, refreshTimes []time.Duration

	for round := range 32 {
		var (
			samples    [4]collectionGuardSample
			refreshed  targetprojection.Result
			refreshErr error
			elapsed    time.Duration
			wg         sync.WaitGroup
		)

		start := make(chan struct{})
		building := make(chan struct{})

		wg.Go(func() {
			<-start

			started := time.Now()

			refreshed, refreshErr = refresher.Refresh(ctx, collectionObservedPolicy{builder: builder, entered: building}, time.Now())
			elapsed = time.Since(started)
		})

		for ap, reader := range readers {
			wg.Go(func() {
				select {
				case <-building:
					samples[ap] = readCollectionSharedGuard(ctx, reader)
				case <-ctx.Done():
					samples[ap].err = ctx.Err()
				}
			})
		}

		close(start)
		wg.Wait()
		require.NoError(t, refreshErr)
		require.Equal(t, targetprojection.MaxTargetCount, refreshed.RowCount)

		for _, sample := range samples {
			require.NoError(t, sample.err)
			require.Equal(t, refreshed.Generation, sample.generation)

			if round >= 2 {
				guardTimes = append(guardTimes, sample.guardTime)
				transactionTimes = append(transactionTimes, sample.totalTime)
			}
		}

		if round >= 2 {
			refreshTimes = append(refreshTimes, elapsed)
		}
	}

	for name, durations := range map[string][]time.Duration{
		"공유_guard_header_SQL_대기포함":    guardTimes,
		"공유_guard_header_transaction": transactionTimes,
		"실제_policy_roster_Refresh":    refreshTimes,
	} {
		slices.Sort(durations)
		t.Logf("범위=%s 입력_target=%d live=%d 현재검토=%d 과거검토=%d 예열=2 표본=%d p95=%s p99=%s 최대=%s",
			name, targetprojection.MaxTargetCount, targetprojection.MaxInputLiveCheckVideoCount, collectionLifecycleReviewed, collectionLifecycleReviewed*30,
			len(durations), collectionGuardPercentile(durations, 95), collectionGuardPercentile(durations, 99), durations[len(durations)-1])
	}
}

func collectionGuardPercentile(sorted []time.Duration, percent int) time.Duration {
	return sorted[(len(sorted)*percent+99)/100-1]
}

var _ targetprojection.Builder = collectionPausedPolicy{}

// guard 획득 직후 실제 Build가 시작되면 reader를 풀어 입력 조회 전체와 경합시킵니다.
// Reader를 기다리거나 Build 종료를 늦추지 않으므로 인위적인 장벽 지연을 측정하지 않습니다.
type collectionObservedPolicy struct {
	builder targetprojection.PolicyBuilder
	entered chan struct{}
}

func (b collectionObservedPolicy) Build(ctx context.Context, tx dbx.Tx, now time.Time) ([]targetprojection.TargetSpec, []targetprojection.TargetReason, error) {
	close(b.entered)

	targets, reasons, err := b.builder.Build(ctx, tx, now)
	if err != nil {
		return nil, nil, fmt.Errorf("경합 중 실제 정책 입력 생성: %w", err)
	}

	return targets, reasons, nil
}
