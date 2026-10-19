package runtime

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/youtube/targetprojection"
	"github.com/kapu/hololive-api/internal/youtube/sourceobservation"
)

// 드문 사건의 시리즈가 첫 증가 때 생기면 Prometheus increase()가 그 사건을 0으로 센다.
// 사건이 한 번도 없던 프로세스에서도 알려진 라벨 값이 0으로 노출되는지 확인한다.
func TestRareOutcomeSeriesAreExportedBeforeFirstEvent(t *testing.T) {
	require.Subset(t, gatheredLabelValues(t, "hololive_youtube_plane_consume_total", "result"), []string{
		consumeResultSuccess, consumeResultRetry, consumeResultRetryError, consumeResultClaimLost,
		consumeResultFatal, consumeResultDeadLetter, consumeResultDeadLetterError,
	})

	errorTables := gatheredLabelValues(t, "hololive_youtube_plane_retention_errors_total", "table")
	require.Subset(t, errorTables, sourceobservation.RetentionTables())
	require.Subset(t, errorTables, []string{"source_observations", "youtube_collection_projection_generations", "none"})

	require.Subset(t, gatheredLabelValues(t, "hololive_youtube_plane_retention_deleted_total", "table"),
		[]string{"source_observation_queue", "youtube_collection_job_leases", "youtube_collection_targets"})
	require.ElementsMatch(t, []string{"source", "projection"},
		gatheredLabelValues(t, "hololive_youtube_plane_retention_tick_seconds", "phase"))
	require.Subset(t, gatheredLabelValues(t, "hololive_youtube_plane_db_slot_wait_seconds", "operation"),
		[]string{"claim", "consume", "retry", "projection_refresh", "retention_source"})
}

// 슬롯을 끝내 얻지 못한 작업도 대기 시간이 남아야 포화 원인을 지표로 가를 수 있다.
func TestWithDBRecordsSlotWaitOnFailedAcquireAndHoldAfterRelease(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRuntime(t, fakeClaimer{}, fakeConsumer{})

		r.dbSem = make(chan struct{}, 1)
		r.dbSem <- struct{}{}

		waitCount, waitSum := histogramSample(t, "hololive_youtube_plane_db_slot_wait_seconds", "operation", "probe")
		holdCount, holdSum := histogramSample(t, "hololive_youtube_plane_db_slot_hold_seconds", "operation", "probe")

		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		err := r.withDB(ctx, dbOperationProbe, func(context.Context) error {
			t.Fatal("fn must not run without a slot")

			return nil
		})

		cancel()
		require.ErrorIs(t, err, context.DeadlineExceeded)

		afterWaitCount, afterWaitSum := histogramSample(t, "hololive_youtube_plane_db_slot_wait_seconds", "operation", "probe")
		require.Equal(t, waitCount+1, afterWaitCount)
		require.InDelta(t, waitSum+3, afterWaitSum, 1e-9)

		afterHoldCount, _ := histogramSample(t, "hololive_youtube_plane_db_slot_hold_seconds", "operation", "probe")
		require.Equal(t, holdCount, afterHoldCount, "an unacquired slot must not record hold time")

		<-r.dbSem

		require.NoError(t, r.withDB(t.Context(), dbOperationProbe, func(context.Context) error {
			time.Sleep(2 * time.Second)

			return nil
		}))

		afterHoldCount, afterHoldSum := histogramSample(t, "hololive_youtube_plane_db_slot_hold_seconds", "operation", "probe")
		require.Equal(t, holdCount+1, afterHoldCount)
		require.InDelta(t, holdSum+2, afterHoldSum, 1e-9)
		require.Empty(t, r.dbSem, "slot must be released after fn returns")
	})
}

type blockingRefresher struct{}

func (blockingRefresher) Refresh(ctx context.Context, _ targetprojection.Builder, _ time.Time) (targetprojection.Result, error) {
	<-ctx.Done()

	return targetprojection.Result{}, ctx.Err()
}

// 멈춘 projection refresh는 슬롯을 무기한 붙들지 않고 TransactionTimeout 뒤 오류로 드러나야 한다.
func TestRefreshProjectionReleasesSlotAfterTransactionTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRuntime(t, fakeClaimer{}, fakeConsumer{})

		r.refresher = blockingRefresher{}
		r.dbSem = make(chan struct{}, 1)

		started := time.Now()

		err := r.refreshProjection(t.Context())

		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, r.Config.TransactionTimeout, time.Since(started))
		require.True(t, r.Degraded())
		require.Empty(t, r.dbSem, "timed out refresh must release its DB slot")
		require.True(t, retryableObservationError(err), "projection loop keeps running after a timed out refresh")
	})
}

// retention tick 히스토그램은 슬롯 획득 뒤 실행분만 재고, 슬롯을 얻지 못한 phase는 오류로만 센다.
func TestRetentionTickExcludesSlotWaitAndSkipsUnacquiredPhase(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRuntime(t, fakeClaimer{}, fakeConsumer{})

		r.dbSem = make(chan struct{}, 1)
		r.retainer = fakeRetainer{tick: func(context.Context, sourceobservation.RetentionConfig, time.Time) (sourceobservation.RetentionResult, error) {
			time.Sleep(time.Second)

			return sourceobservation.RetentionResult{}, nil
		}}

		tickCount, tickSum := histogramSample(t, "hololive_youtube_plane_retention_tick_seconds", "phase", "source")
		_, waitSum := histogramSample(t, "hololive_youtube_plane_db_slot_wait_seconds", "operation", "retention_source")

		r.dbSem <- struct{}{}

		done := make(chan error, 1)

		go func() { done <- r.retainSource(t.Context()) }()

		time.Sleep(2 * time.Second)
		<-r.dbSem
		require.NoError(t, <-done)

		afterTickCount, afterTickSum := histogramSample(t, "hololive_youtube_plane_retention_tick_seconds", "phase", "source")
		require.Equal(t, tickCount+1, afterTickCount)
		require.InDelta(t, tickSum+1, afterTickSum, 1e-9, "tick time must exclude the 2s slot wait")

		_, afterWaitSum := histogramSample(t, "hololive_youtube_plane_db_slot_wait_seconds", "operation", "retention_source")
		require.InDelta(t, waitSum+2, afterWaitSum, 1e-9)

		unknownErrors := youtubeRetentionErrorsTotal.WithLabelValues("none")
		beforeErrors := testutil.ToFloat64(unknownErrors)

		r.dbSem <- struct{}{}

		require.ErrorIs(t, r.retainSource(t.Context()), context.DeadlineExceeded)

		finalTickCount, _ := histogramSample(t, "hololive_youtube_plane_retention_tick_seconds", "phase", "source")
		require.Equal(t, afterTickCount, finalTickCount, "a phase that never acquired a slot has no tick time")
		require.InDelta(t, beforeErrors+1, testutil.ToFloat64(unknownErrors), 0)
	})
}

// gatheredSeries는 기본 registry가 실제로 노출하는 시리즈 하나의 라벨과 히스토그램 표본이다.
type gatheredSeries struct {
	labels map[string]string
	count  uint64
	sum    float64
}

func gatherSeries(t *testing.T, name string) []gatheredSeries {
	t.Helper()

	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	for _, family := range families {
		if family.GetName() != name {
			continue
		}

		series := make([]gatheredSeries, 0, len(family.GetMetric()))

		for _, metric := range family.GetMetric() {
			labels := make(map[string]string, len(metric.GetLabel()))
			for _, pair := range metric.GetLabel() {
				labels[pair.GetName()] = pair.GetValue()
			}

			series = append(series, gatheredSeries{
				labels: labels,
				count:  metric.GetHistogram().GetSampleCount(),
				sum:    metric.GetHistogram().GetSampleSum(),
			})
		}

		return series
	}

	t.Fatalf("metric family %s is not exported", name)

	return nil
}

func gatheredLabelValues(t *testing.T, name, label string) []string {
	t.Helper()

	var values []string

	for _, series := range gatherSeries(t, name) {
		if value, ok := series.labels[label]; ok {
			values = append(values, value)
		}
	}

	return values
}

func histogramSample(t *testing.T, name, label, value string) (uint64, float64) {
	t.Helper()

	for _, series := range gatherSeries(t, name) {
		if series.labels[label] == value {
			return series.count, series.sum
		}
	}

	t.Fatalf("metric %s{%s=%q} is not exported", name, label, value)

	return 0, 0
}
