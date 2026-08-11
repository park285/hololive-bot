package polling

import (
	"context"
	"errors"
	"testing"
	"time"

	providers "github.com/kapu/hololive-shared/pkg/providers"

	youtubeadmission "github.com/kapu/hololive-shared/pkg/service/youtube/admission"
	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/scheduler"
	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/pollers"
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
	source polling.BudgetSource
	ttl    time.Duration
	reason string
	calls  int
	err    error
}

func (r *sourceCooldownTestReporter) MarkSourceCooldown(ctx context.Context, source polling.BudgetSource, ttl time.Duration, reason string) error {
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

func (l *sourceCooldownTestLimiter) TryReserve(context.Context, *polling.BudgetJob, polling.BudgetProfile, time.Duration) (reservation polling.BudgetReservation, decision polling.BudgetDecision, err error) {
	return nil, polling.BudgetDecision{Allowed: true}, nil
}

func TestSourceCooldownReportingPollerReportsOnlySourceLevelYouTubeErrors(t *testing.T) {
	reporter := &sourceCooldownTestReporter{}
	wrapped := newSourceCooldownReportingPoller(
		sourceCooldownTestPoller{err: scraper.ErrRateLimited},
		reporter,
		nil,
	)

	err := wrapped.Poll(context.Background(), "UC_TEST")
	require.ErrorIs(t, err, scraper.ErrRateLimited)
	require.Equal(t, 1, reporter.calls)
	require.Equal(t, polling.BudgetSourceYouTubeScraper, reporter.source)
	require.Equal(t, "youtube_rate_limited", reporter.reason)
	require.Greater(t, reporter.ttl, time.Duration(0))

	reporter.calls = 0
	wrapped = newSourceCooldownReportingPoller(
		sourceCooldownTestPoller{err: youtubeadmission.ErrDeferred},
		reporter,
		nil,
	)
	require.Error(t, wrapped.Poll(context.Background(), "UC_TEST"))
	require.Equal(t, 0, reporter.calls)

	reporter.calls = 0
	wrapped = newSourceCooldownReportingPoller(
		sourceCooldownTestPoller{err: errors.New("parser drift")},
		reporter,
		nil,
	)
	require.Error(t, wrapped.Poll(context.Background(), "UC_TEST"))
	require.Equal(t, 0, reporter.calls)
}

func TestWrapSourceCooldownPollersIncludesLiveBatchFallbackScraperSource(t *testing.T) {
	limiter := &sourceCooldownTestLimiter{}
	registration := providers.NewChannelPollerRegistration(
		sourceCooldownTestPoller{name: "live_batch", err: scraper.ErrBlockedResponse},
		scheduler.PriorityHigh,
		time.Minute,
	).
		WithChannelIDs([]string{providers.SyntheticGlobalPollerChannelID}).
		WithBudgetProfile(holodexLiveBatchBudgetProfile(30, polling.BudgetBurstPrimary, polling.BudgetPriorityHigh))

	wrapped := wrapYouTubeProducerSourceCooldownPollers([]providers.ChannelPollerRegistration{registration}, limiter, nil)
	err := wrapped[0].Poller.Poll(context.Background(), providers.SyntheticGlobalPollerChannelID)

	require.ErrorIs(t, err, scraper.ErrBlockedResponse)
	require.Equal(t, 1, limiter.calls)
	require.Equal(t, polling.BudgetSourceYouTubeScraper, limiter.source)
	require.Equal(t, "youtube_blocked_response", limiter.reason)
}

func TestWrapSourceCooldownPollersPreservesTargetSnapshotContract(t *testing.T) {
	limiter := &sourceCooldownTestLimiter{}
	base := pollers.NewLivePollerWithStatusProvider(nil, nil, nil)
	batch := newLiveBatchPoller(
		"live_batch",
		base,
		[]string{"UC_OLD"},
		polling.BudgetBurstPrimary,
		polling.BudgetPriorityHigh,
	)
	registration := providers.NewChannelPollerRegistration(batch, scheduler.PriorityHigh, time.Minute).
		WithChannelIDs([]string{providers.SyntheticGlobalPollerChannelID}).
		WithBudgetProfile(batch.budgetProfile())

	wrapped := wrapYouTubeProducerSourceCooldownPollers([]providers.ChannelPollerRegistration{registration}, limiter, nil)
	snapshot, ok := wrapped[0].Poller.(providers.ChannelTargetSnapshotPoller)
	require.True(t, ok)
	require.Equal(t, []string{"UC_OLD"}, snapshot.ChannelTargets())

	updatedPoller, profile := snapshot.WithChannelTargets([]string{"UC_NEW_A", "UC_NEW_B"})
	updated, ok := updatedPoller.(providers.ChannelTargetSnapshotPoller)
	require.True(t, ok)
	require.Equal(t, []string{"UC_OLD"}, snapshot.ChannelTargets())
	require.Equal(t, []string{"UC_NEW_A", "UC_NEW_B"}, updated.ChannelTargets())
	require.Equal(t, 2.0, profile.SourceUnits[polling.BudgetSourcePostgresWrite])
}

func TestSourceCooldownReportingPollerBoundsReportContext(t *testing.T) {
	reporter := &sourceCooldownTestReporter{err: context.DeadlineExceeded}
	wrapped := newSourceCooldownReportingPoller(
		sourceCooldownTestPoller{err: scraper.ErrForbidden},
		reporter,
		nil,
	)
	reportingPoller, ok := wrapped.(*sourceCooldownReportingPoller)
	require.True(t, ok, "wrapped poller must report source cooldowns")
	reportingPoller.reportTimeout = 10 * time.Millisecond

	startedAt := time.Now()
	err := reportingPoller.Poll(context.Background(), "UC_TEST")

	require.ErrorIs(t, err, scraper.ErrForbidden)
	require.Equal(t, 1, reporter.calls)
	require.Less(t, time.Since(startedAt), time.Second)
}
