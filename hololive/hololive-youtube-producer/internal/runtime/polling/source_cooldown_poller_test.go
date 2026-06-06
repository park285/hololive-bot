package polling

import (
	"context"
	"errors"
	"testing"
	"time"

	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/stretchr/testify/require"
)

type sourceCooldownTestPoller struct {
	name string
	err  error
}

func (p sourceCooldownTestPoller) Poll(context.Context, string) error { return p.err }
func (p sourceCooldownTestPoller) Name() string {
	if p.name == "" {
		return "test"
	}
	return p.name
}

type sourceCooldownTestReporter struct {
	source poller.BudgetSource
	ttl    time.Duration
	reason string
	calls  int
	err    error
}

func (r *sourceCooldownTestReporter) MarkSourceCooldown(ctx context.Context, source poller.BudgetSource, ttl time.Duration, reason string) error {
	r.calls++
	r.source = source
	r.ttl = ttl
	r.reason = reason
	if r.err != nil {
		<-ctx.Done()
		return ctx.Err()
	}
	return r.err
}

type sourceCooldownTestLimiter struct {
	sourceCooldownTestReporter
}

func (l *sourceCooldownTestLimiter) TryReserve(context.Context, poller.BudgetJob, poller.BudgetProfile, time.Duration) (poller.BudgetReservation, poller.BudgetDecision, error) {
	return nil, poller.BudgetDecision{Allowed: true}, nil
}

func TestSourceCooldownReportingPollerReportsOnlySourceLevelYouTubeErrors(t *testing.T) {
	reporter := &sourceCooldownTestReporter{}
	wrapped := newSourceCooldownReportingPoller(
		sourceCooldownTestPoller{err: scraper.ErrRateLimited},
		reporter,
		poller.BudgetSourceYouTubeScraper,
		nil,
	)

	err := wrapped.Poll(context.Background(), "UC_TEST")
	require.ErrorIs(t, err, scraper.ErrRateLimited)
	require.Equal(t, 1, reporter.calls)
	require.Equal(t, poller.BudgetSourceYouTubeScraper, reporter.source)
	require.Equal(t, "youtube_rate_limited", reporter.reason)
	require.Greater(t, reporter.ttl, time.Duration(0))

	reporter.calls = 0
	wrapped = newSourceCooldownReportingPoller(
		sourceCooldownTestPoller{err: errors.New("parser drift")},
		reporter,
		poller.BudgetSourceYouTubeScraper,
		nil,
	)
	require.Error(t, wrapped.Poll(context.Background(), "UC_TEST"))
	require.Equal(t, 0, reporter.calls)
}

func TestWrapSourceCooldownPollersIncludesLiveBatchFallbackScraperSource(t *testing.T) {
	limiter := &sourceCooldownTestLimiter{}
	registration := providers.NewChannelPollerRegistration(
		sourceCooldownTestPoller{name: "live_batch", err: scraper.ErrBlockedResponse},
		poller.PriorityHigh,
		time.Minute,
	).
		WithChannelIDs([]string{providers.SyntheticGlobalPollerChannelID}).
		WithBudgetProfile(holodexLiveBatchBudgetProfile(30, poller.BudgetBurstPrimary, poller.BudgetPriorityHigh))

	wrapped := wrapYouTubeProducerSourceCooldownPollers([]providers.ChannelPollerRegistration{registration}, limiter, nil)
	err := wrapped[0].Poller.Poll(context.Background(), providers.SyntheticGlobalPollerChannelID)

	require.ErrorIs(t, err, scraper.ErrBlockedResponse)
	require.Equal(t, 1, limiter.calls)
	require.Equal(t, poller.BudgetSourceYouTubeScraper, limiter.source)
	require.Equal(t, "youtube_blocked_response", limiter.reason)
}

func TestSourceCooldownReportingPollerBoundsReportContext(t *testing.T) {
	reporter := &sourceCooldownTestReporter{err: context.DeadlineExceeded}
	wrapped := newSourceCooldownReportingPoller(
		sourceCooldownTestPoller{err: scraper.ErrForbidden},
		reporter,
		poller.BudgetSourceYouTubeScraper,
		nil,
	)
	wrapped.(*sourceCooldownReportingPoller).reportTimeout = 10 * time.Millisecond

	startedAt := time.Now()
	err := wrapped.Poll(context.Background(), "UC_TEST")

	require.ErrorIs(t, err, scraper.ErrForbidden)
	require.Equal(t, 1, reporter.calls)
	require.Less(t, time.Since(startedAt), time.Second)
}
