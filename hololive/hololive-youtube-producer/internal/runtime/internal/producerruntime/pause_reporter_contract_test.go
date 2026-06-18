package producerruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/readiness"
	"github.com/stretchr/testify/require"
)

func captureWarnRecords(t *testing.T, fn func()) []map[string]any {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(previous)
	fn()

	var records []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var payload map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &payload))
		records = append(records, payload)
	}
	return records
}

func TestPauseReporterLeaseWarnContract(t *testing.T) {
	state := readiness.New("youtube-producer", readiness.Features{YouTubeEnabled: true, ActiveActiveEnabled: true})
	state.MarkRunning()
	claimer := newReadinessReportingJobClaimer(&readinessClaimStub{
		status: poller.JobClaimStatus{Result: poller.JobClaimUnavailable},
		err:    fmt.Errorf("valkey unavailable"),
	}, state)

	records := captureWarnRecords(t, func() {
		for range 3 {
			_, _, err := claimer.TryClaim(context.Background(), "videos", "channel-1", time.Minute, time.Minute)
			require.Error(t, err)
		}
	})

	require.Len(t, records, 1)
	rec := records[0]
	require.Equal(t, "active_active_paused", rec["msg"])
	require.Equal(t, "WARN", rec["level"])
	require.Equal(t, "valkey_unavailable_active_active_fail_closed", rec["reason"])
	require.Equal(t, "videos", rec["poller"])
	require.Equal(t, "valkey unavailable", rec["error"])
}

func TestPauseReporterLeaseWarnOmitsErrorWhenNil(t *testing.T) {
	state := readiness.New("youtube-producer", readiness.Features{YouTubeEnabled: true, ActiveActiveEnabled: true})
	state.MarkRunning()
	claimer := newReadinessReportingJobClaimer(&readinessClaimStub{
		status: poller.JobClaimStatus{Result: poller.JobClaimUnavailable},
	}, state)

	records := captureWarnRecords(t, func() {
		_, _, err := claimer.TryClaim(context.Background(), "videos", "channel-1", time.Minute, time.Minute)
		require.NoError(t, err)
	})

	require.Len(t, records, 1)
	rec := records[0]
	require.Equal(t, "active_active_paused", rec["msg"])
	require.Equal(t, "valkey_unavailable_active_active_fail_closed", rec["reason"])
	require.Equal(t, "videos", rec["poller"])
	_, hasError := rec["error"]
	require.False(t, hasError)
}

func TestPauseReporterLeaseResetsAfterAvailable(t *testing.T) {
	state := readiness.New("youtube-producer", readiness.Features{YouTubeEnabled: true, ActiveActiveEnabled: true})
	state.MarkRunning()
	stub := readinessClaimStub{
		status: poller.JobClaimStatus{Result: poller.JobClaimUnavailable},
		err:    fmt.Errorf("valkey unavailable"),
	}
	available := readinessClaimStub{status: poller.JobClaimStatus{Result: poller.JobClaimAcquired}}

	c := readinessReportingJobClaimer{inner: &stub, readiness: state, pauseLogger: &pauseTransitionLogger{}}
	cAvail := readinessReportingJobClaimer{inner: &available, readiness: state, pauseLogger: c.pauseLogger}

	records := captureWarnRecords(t, func() {
		_, _, err := c.TryClaim(context.Background(), "videos", "channel-1", time.Minute, time.Minute)
		require.Error(t, err)
		_, _, err = c.TryClaim(context.Background(), "videos", "channel-1", time.Minute, time.Minute)
		require.Error(t, err)
		_, _, err = cAvail.TryClaim(context.Background(), "videos", "channel-1", time.Minute, time.Minute)
		require.NoError(t, err)
		_, _, err = c.TryClaim(context.Background(), "videos", "channel-1", time.Minute, time.Minute)
		require.Error(t, err)
	})

	require.Len(t, records, 2)
	for _, rec := range records {
		require.Equal(t, "active_active_paused", rec["msg"])
		require.Equal(t, "valkey_unavailable_active_active_fail_closed", rec["reason"])
	}
}

func TestPauseReporterBudgetWarnContract(t *testing.T) {
	state := readiness.New("youtube-producer", readiness.Features{YouTubeEnabled: true, GlobalBudgetEnabled: true})
	state.MarkRunning()
	stub := &readinessBudgetLimiterStub{err: fmt.Errorf("valkey unavailable")}
	limiter := newReadinessReportingBudgetLimiter(stub, state)

	records := captureWarnRecords(t, func() {
		for range 3 {
			_, _, err := limiter.TryReserve(context.Background(), &poller.BudgetJob{PollerName: "videos"}, readinessBudgetProfile(poller.BudgetSourceYouTubeScraper), time.Minute)
			require.Error(t, err)
		}
	})

	require.Len(t, records, 1)
	rec := records[0]
	require.Equal(t, "global_budget_paused", rec["msg"])
	require.Equal(t, "WARN", rec["level"])
	require.Equal(t, "valkey_unavailable_global_budget_fail_closed", rec["reason"])
	require.Equal(t, "videos", rec["poller"])
	require.Equal(t, "valkey unavailable", rec["error"])
}

func TestPauseReporterBudgetResetsAfterAvailable(t *testing.T) {
	state := readiness.New("youtube-producer", readiness.Features{YouTubeEnabled: true, GlobalBudgetEnabled: true})
	state.MarkRunning()
	stub := &readinessBudgetLimiterStub{err: fmt.Errorf("valkey unavailable")}
	limiter := newReadinessReportingBudgetLimiter(stub, state)

	records := captureWarnRecords(t, func() {
		_, _, err := limiter.TryReserve(context.Background(), &poller.BudgetJob{PollerName: "videos"}, readinessBudgetProfile(poller.BudgetSourceYouTubeScraper), time.Minute)
		require.Error(t, err)
		_, _, err = limiter.TryReserve(context.Background(), &poller.BudgetJob{PollerName: "videos"}, readinessBudgetProfile(poller.BudgetSourceYouTubeScraper), time.Minute)
		require.Error(t, err)
		stub.err = nil
		_, _, err = limiter.TryReserve(context.Background(), &poller.BudgetJob{PollerName: "videos"}, readinessBudgetProfile(poller.BudgetSourceYouTubeScraper), time.Minute)
		require.NoError(t, err)
		stub.err = fmt.Errorf("valkey unavailable again")
		_, _, err = limiter.TryReserve(context.Background(), &poller.BudgetJob{PollerName: "videos"}, readinessBudgetProfile(poller.BudgetSourceYouTubeScraper), time.Minute)
		require.Error(t, err)
	})

	require.Len(t, records, 2)
	require.Equal(t, "global_budget_paused", records[0]["msg"])
	require.Equal(t, "valkey unavailable", records[0]["error"])
	require.Equal(t, "global_budget_paused", records[1]["msg"])
	require.Equal(t, "valkey unavailable again", records[1]["error"])
}
