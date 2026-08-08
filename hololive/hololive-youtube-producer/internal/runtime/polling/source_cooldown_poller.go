package polling

import (
	"context"
	"errors"
	"log/slog"
	"time"

	providers "github.com/kapu/hololive-shared/pkg/providers"

	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/scheduler"
	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
)

const (
	youtubeSourceRateLimitedCooldown = 15 * time.Minute
	youtubeSourceForbiddenCooldown   = 30 * time.Minute
	youtubeSourceBlockedCooldown     = 30 * time.Minute
)

const defaultSourceCooldownReportTimeout = 2 * time.Second

type sourceCooldownReportingPoller struct {
	inner         scheduler.Poller
	reporter      polling.SourceCooldownReporter
	source        polling.BudgetSource
	logger        *slog.Logger
	reportTimeout time.Duration
}

type sourceCooldownReportingTargetSnapshotPoller struct {
	*sourceCooldownReportingPoller
}

var _ providers.ChannelTargetSnapshotPoller = (*sourceCooldownReportingTargetSnapshotPoller)(nil)

func wrapYouTubeProducerSourceCooldownPollers(
	registrations []providers.ChannelPollerRegistration,
	limiter polling.GlobalBudgetLimiter,
	logger *slog.Logger,
) []providers.ChannelPollerRegistration {
	if registrations == nil {
		registrations = []providers.ChannelPollerRegistration{}
	}
	reporter, ok := limiter.(polling.SourceCooldownReporter)
	if !ok || reporter == nil {
		return registrations
	}
	wrapped := make([]providers.ChannelPollerRegistration, len(registrations))
	copy(wrapped, registrations)
	for i := range wrapped {
		if !registrationUsesSource(&wrapped[i], polling.BudgetSourceYouTubeScraper) {
			continue
		}
		wrapped[i].Poller = newSourceCooldownReportingPoller(wrapped[i].Poller, reporter, logger)
	}
	return wrapped
}

func registrationUsesSource(registration *providers.ChannelPollerRegistration, source polling.BudgetSource) bool {
	if registration == nil {
		return false
	}
	if registration.Poller == nil {
		return false
	}
	return registration.BudgetProfile.SourceUnits[source] > 0 ||
		registration.BudgetProfile.FallbackSourceUnits[source] > 0
}

func newSourceCooldownReportingPoller(
	inner scheduler.Poller,
	reporter polling.SourceCooldownReporter,
	logger *slog.Logger,
) scheduler.Poller {
	if inner == nil || reporter == nil {
		return inner
	}
	reportingPoller := &sourceCooldownReportingPoller{
		inner:         inner,
		reporter:      reporter,
		source:        polling.BudgetSourceYouTubeScraper,
		logger:        logger,
		reportTimeout: defaultSourceCooldownReportTimeout,
	}
	if _, ok := inner.(providers.ChannelTargetSnapshotPoller); ok {
		return &sourceCooldownReportingTargetSnapshotPoller{sourceCooldownReportingPoller: reportingPoller}
	}
	return reportingPoller
}

func (p *sourceCooldownReportingPoller) Poll(ctx context.Context, channelID string) error {
	err := p.inner.Poll(ctx, channelID)
	if err != nil {
		p.reportIfSourceCooldown(ctx, err)
	}
	return err
}

func (p *sourceCooldownReportingPoller) Name() string {
	if p == nil || p.inner == nil {
		return "source_cooldown_reporting"
	}
	return p.inner.Name()
}

func (p *sourceCooldownReportingPoller) SetProxyEnabled(enabled bool) bool {
	proxyPoller, ok := p.inner.(interface {
		SetProxyEnabled(bool) bool
	})
	return ok && proxyPoller.SetProxyEnabled(enabled)
}

func (p *sourceCooldownReportingPoller) ProxyEnabled() bool {
	proxyPoller, ok := p.inner.(interface {
		ProxyEnabled() bool
	})
	return ok && proxyPoller.ProxyEnabled()
}

func (p *sourceCooldownReportingTargetSnapshotPoller) ChannelTargets() []string {
	if p == nil || p.sourceCooldownReportingPoller == nil {
		return nil
	}
	snapshotPoller, ok := p.inner.(providers.ChannelTargetSnapshotPoller)
	if !ok {
		return nil
	}
	return snapshotPoller.ChannelTargets()
}

func (p *sourceCooldownReportingTargetSnapshotPoller) WithChannelTargets(channelIDs []string) (scheduler.Poller, polling.BudgetProfile) {
	if p == nil || p.sourceCooldownReportingPoller == nil {
		return p, polling.BudgetProfile{}
	}
	snapshotPoller, ok := p.inner.(providers.ChannelTargetSnapshotPoller)
	if !ok {
		return p, polling.BudgetProfile{}
	}
	updatedInner, profile := snapshotPoller.WithChannelTargets(channelIDs)
	updated := *p.sourceCooldownReportingPoller
	updated.inner = updatedInner
	return &sourceCooldownReportingTargetSnapshotPoller{sourceCooldownReportingPoller: &updated}, profile
}

func (p *sourceCooldownReportingPoller) reportIfSourceCooldown(ctx context.Context, err error) {
	ttl, reason, ok := youtubeSourceCooldownForError(err)
	if !ok {
		return
	}
	reportCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.reportTimeout)
	defer cancel()
	if reportErr := p.reporter.MarkSourceCooldown(reportCtx, p.source, ttl, reason); reportErr != nil {
		if p.logger != nil {
			p.logger.Warn("youtube_producer_source_cooldown_report_failed",
				slog.String("poller", p.Name()),
				slog.String("source", string(p.source)),
				slog.String("reason", reason),
				slog.Duration("ttl", ttl),
				slog.Any("error", reportErr),
			)
		}
		return
	}
	if p.logger != nil {
		p.logger.Warn("youtube_producer_source_cooldown_reported",
			slog.String("poller", p.Name()),
			slog.String("source", string(p.source)),
			slog.String("reason", reason),
			slog.Duration("ttl", ttl),
		)
	}
}

func youtubeSourceCooldownForError(err error) (time.Duration, string, bool) {
	switch {
	case errors.Is(err, scraper.ErrRateLimited):
		return youtubeSourceRateLimitedCooldown, "youtube_rate_limited", true
	case errors.Is(err, scraper.ErrForbidden):
		return youtubeSourceForbiddenCooldown, "youtube_forbidden", true
	case errors.Is(err, scraper.ErrBlockedResponse):
		return youtubeSourceBlockedCooldown, "youtube_blocked_response", true
	default:
		return 0, "", false
	}
}
