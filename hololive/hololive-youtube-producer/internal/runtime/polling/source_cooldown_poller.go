package polling

import (
	"context"
	"errors"
	"log/slog"
	"time"

	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

const (
	youtubeSourceRateLimitedCooldown = 15 * time.Minute
	youtubeSourceForbiddenCooldown   = 30 * time.Minute
	youtubeSourceBlockedCooldown     = 30 * time.Minute
)

const defaultSourceCooldownReportTimeout = 2 * time.Second

type sourceCooldownReportingPoller struct {
	inner         poller.Poller
	reporter      poller.SourceCooldownReporter
	source        poller.BudgetSource
	logger        *slog.Logger
	reportTimeout time.Duration
}

func wrapYouTubeProducerSourceCooldownPollers(
	registrations []providers.ChannelPollerRegistration,
	limiter poller.GlobalBudgetLimiter,
	logger *slog.Logger,
) []providers.ChannelPollerRegistration {
	reporter, ok := limiter.(poller.SourceCooldownReporter)
	if !ok || reporter == nil {
		return registrations
	}
	wrapped := make([]providers.ChannelPollerRegistration, len(registrations))
	copy(wrapped, registrations)
	for i := range wrapped {
		if !registrationUsesSource(wrapped[i], poller.BudgetSourceYouTubeScraper) {
			continue
		}
		wrapped[i].Poller = newSourceCooldownReportingPoller(wrapped[i].Poller, reporter, poller.BudgetSourceYouTubeScraper, logger)
	}
	return wrapped
}

func registrationUsesSource(registration providers.ChannelPollerRegistration, source poller.BudgetSource) bool {
	if registration.Poller == nil {
		return false
	}
	return registration.BudgetProfile.SourceUnits[source] > 0 ||
		registration.BudgetProfile.FallbackSourceUnits[source] > 0
}

func newSourceCooldownReportingPoller(
	inner poller.Poller,
	reporter poller.SourceCooldownReporter,
	source poller.BudgetSource,
	logger *slog.Logger,
) poller.Poller {
	if inner == nil || reporter == nil || source == "" {
		return inner
	}
	return &sourceCooldownReportingPoller{
		inner:         inner,
		reporter:      reporter,
		source:        source,
		logger:        logger,
		reportTimeout: defaultSourceCooldownReportTimeout,
	}
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
