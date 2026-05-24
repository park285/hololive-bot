package pollers

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal"
)

func TestObserveCommunityShortsDetectionBatch_ZeroCount(t *testing.T) {
	metrics := polling.NewMetrics()
	before := testutil.ToFloat64(metrics.CommunityShortsDetectedPostsTotal.WithLabelValues(string(domain.AlarmTypeCommunity)))

	observeCommunityShortsDetectionBatch(context.Background(), "UC_TEST", domain.AlarmTypeCommunity, 0, time.Now(), metrics)

	after := testutil.ToFloat64(metrics.CommunityShortsDetectedPostsTotal.WithLabelValues(string(domain.AlarmTypeCommunity)))
	assert.Equal(t, before, after, "detectedCount=0이면 메트릭을 업데이트하지 않는다")
}

func TestObserveCommunityShortsDetectionBatch_NegativeCount(t *testing.T) {
	metrics := polling.NewMetrics()
	before := testutil.ToFloat64(metrics.CommunityShortsDetectedPostsTotal.WithLabelValues(string(domain.AlarmTypeCommunity)))

	observeCommunityShortsDetectionBatch(context.Background(), "UC_TEST", domain.AlarmTypeCommunity, -1, time.Now(), metrics)

	after := testutil.ToFloat64(metrics.CommunityShortsDetectedPostsTotal.WithLabelValues(string(domain.AlarmTypeCommunity)))
	assert.Equal(t, before, after, "detectedCount<0이면 메트릭을 업데이트하지 않는다")
}

func TestObserveCommunityShortsDetectionBatch_PositiveCount(t *testing.T) {
	metrics := polling.NewMetrics()
	before := testutil.ToFloat64(metrics.CommunityShortsDetectedPostsTotal.WithLabelValues(string(domain.AlarmTypeCommunity)))

	detectedAt := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	observeCommunityShortsDetectionBatch(context.Background(), "UC_TEST", domain.AlarmTypeCommunity, 3, detectedAt, metrics)

	after := testutil.ToFloat64(metrics.CommunityShortsDetectedPostsTotal.WithLabelValues(string(domain.AlarmTypeCommunity)))
	assert.Equal(t, float64(3), after-before)
}

func TestObserveCommunityShortsDetectionBatch_LogsStructuredEntry(t *testing.T) {
	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(prev)

	metrics := polling.NewMetrics()
	detectedAt := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	observeCommunityShortsDetectionBatch(context.Background(), "UC_LOG", domain.AlarmTypeShorts, 2, detectedAt, metrics)

	logStr := logBuf.String()
	require.NotEmpty(t, logStr)
	assert.Contains(t, logStr, "UC_LOG")
	assert.Contains(t, logStr, string(domain.AlarmTypeShorts))
}

func TestObserveCommunityShortsDetectionBatch_ShortAlarmType(t *testing.T) {
	metrics := polling.NewMetrics()
	before := testutil.ToFloat64(metrics.CommunityShortsDetectedPostsTotal.WithLabelValues(string(domain.AlarmTypeShorts)))

	observeCommunityShortsDetectionBatch(context.Background(), "UC_TEST", domain.AlarmTypeShorts, 5, time.Now(), metrics)

	after := testutil.ToFloat64(metrics.CommunityShortsDetectedPostsTotal.WithLabelValues(string(domain.AlarmTypeShorts)))
	assert.Equal(t, float64(5), after-before)
}
