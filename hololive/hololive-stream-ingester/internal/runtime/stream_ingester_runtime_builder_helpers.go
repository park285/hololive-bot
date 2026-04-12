// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package runtime

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/park285/iris-client-go/iris"
)

func buildStreamIngesterYouTubeComponents(
	scraperCfg config.ScraperConfig,
	postgresService database.Client,
	notificationChannelIDs []string,
	statsChannelIDs []string,
	scraperClient *scraper.Client,
	cacheService cache.Client,
	irisClient iris.Sender,
	templateRenderer *template.Renderer,
	routeDecider poller.NotificationRouteDecider,
	logger *slog.Logger,
) (*poller.Scheduler, *outbox.Dispatcher, []providers.ChannelPollerRegistration, error) {
	pollerRegistrations := buildStreamIngesterChannelPollerRegistrationsWithClient(
		postgresService,
		scraperCfg,
		scraperClient,
		routeDecider,
		notificationChannelIDs,
		statsChannelIDs,
	)
	if err := validateExplicitPollerRegistrations(pollerRegistrations); err != nil {
		return nil, nil, nil, err
	}
	logCombinedYouTubeScraperBudget(scraperCfg, pollerRegistrations, logger)

	scraperScheduler := providers.ProvideScraperScheduler(
		nil,
		logger,
		providers.WithChannelPollerRegistrations(pollerRegistrations),
		providers.WithSchedulerWorkerCount(scraperCfg.WorkerCountOrDefault()),
	)

	outboxDispatcher := outbox.NewDispatcher(
		postgresService.GetGormDB(),
		cacheService,
		delivery.NewIrisMessageSender(irisClient),
		templateRenderer,
		logger,
		outbox.DefaultConfig(),
	)

	return scraperScheduler, outboxDispatcher, pollerRegistrations, nil
}

func buildSharedYouTubeScraperClient(
	scraperCfg config.ScraperConfig,
	cacheService cache.Client,
	sharedRL *scraper.RateLimiter,
) *scraper.Client {
	proxyConfig := scraper.ProxyConfig{
		Enabled: scraperCfg.ProxyEnabled,
		URL:     scraperCfg.ProxyURL,
	}

	return scraper.NewClient(
		scraper.WithProxy(proxyConfig),
		scraper.WithRateLimiter(sharedRL),
		scraper.WithStateStore(cacheService),
	)
}

func buildPendingPublishedAtResolver(
	scraperCfg config.ScraperConfig,
	postgresService database.Client,
	scraperClient *scraper.Client,
	routeDecider poller.NotificationRouteDecider,
	logger *slog.Logger,
) *poller.PendingPublishedAtResolver {
	if postgresService == nil || scraperClient == nil {
		return nil
	}
	resolverCfg := effectivePublishedAtResolverConfig(scraperCfg)
	if !resolverCfg.Enabled {
		return nil
	}

	resolver := poller.NewPendingPublishedAtResolverWithControls(
		postgresService.GetGormDB(),
		scraperClient,
		routeDecider,
		resolverCfg.Interval,
		resolverCfg.BatchSize,
		resolverCfg.MaxResolvePerRun,
		resolverCfg.MaxRunDuration,
		resolverCfg.ResolveTimeout,
		resolverCfg.MinDetectedAge,
		resolverCfg.FailureBackoffTTL,
		logger,
	)
	if logger != nil {
		logger.Info("published_at_resolver_configured",
			slog.Duration("interval", resolverCfg.Interval),
			slog.Int("batch_size", resolverCfg.BatchSize),
			slog.Int("max_resolve_per_run", resolverCfg.MaxResolvePerRun),
			slog.Duration("max_run_duration", resolverCfg.MaxRunDuration),
			slog.Duration("resolve_timeout", resolverCfg.ResolveTimeout),
			slog.Duration("min_detected_age", resolverCfg.MinDetectedAge),
			slog.Duration("failure_backoff_ttl", resolverCfg.FailureBackoffTTL),
			slog.Float64("estimated_max_rpm", estimatedPublishedAtResolverMaxRPM(resolverCfg)),
		)
	}
	return resolver
}

func effectivePublishedAtResolverConfig(scraperCfg config.ScraperConfig) config.ScraperPublishedAtResolverConfig {
	resolverCfg := scraperCfg.PublishedAtResolver
	if !resolverCfg.Enabled {
		return resolverCfg
	}

	defaults := config.DefaultScraperPublishedAtResolverConfig()
	if resolverCfg.Interval <= 0 {
		resolverCfg.Interval = defaults.Interval
	}
	if resolverCfg.BatchSize <= 0 {
		resolverCfg.BatchSize = defaults.BatchSize
	}
	if resolverCfg.MaxResolvePerRun <= 0 {
		resolverCfg.MaxResolvePerRun = defaults.MaxResolvePerRun
	}
	if resolverCfg.MaxRunDuration <= 0 {
		resolverCfg.MaxRunDuration = defaults.MaxRunDuration
	}
	if resolverCfg.ResolveTimeout <= 0 {
		resolverCfg.ResolveTimeout = defaults.ResolveTimeout
	}
	if resolverCfg.MinDetectedAge <= 0 {
		resolverCfg.MinDetectedAge = defaults.MinDetectedAge
	}
	if resolverCfg.FailureBackoffTTL <= 0 {
		resolverCfg.FailureBackoffTTL = defaults.FailureBackoffTTL
	}
	return resolverCfg
}

func estimatedPublishedAtResolverMaxRPM(cfg config.ScraperPublishedAtResolverConfig) float64 {
	if cfg.Interval <= 0 || cfg.MaxResolvePerRun <= 0 {
		return 0
	}
	return float64(cfg.MaxResolvePerRun) * 60 / cfg.Interval.Seconds()
}

func logCombinedYouTubeScraperBudget(
	scraperCfg config.ScraperConfig,
	registrations []providers.ChannelPollerRegistration,
	logger *slog.Logger,
) {
	if logger == nil {
		return
	}

	pollerRPM := estimateResolvedPollerRPM(registrations)
	pollerRetryAmplifiedRPM := estimatedPollerWorstCaseRPM(registrations)
	resolverCfg := effectivePublishedAtResolverConfig(scraperCfg)
	resolverRPM := 0.0
	resolverRetryAmplifiedRPM := 0.0
	if resolverCfg.Enabled {
		resolverRPM = estimatedPublishedAtResolverMaxRPM(resolverCfg)
		resolverRetryAmplifiedRPM = estimatedPublishedAtResolverWorstCaseRPM(resolverCfg)
	}
	combinedRPM := pollerRPM + resolverRPM
	combinedRetryAmplifiedRPM := pollerRetryAmplifiedRPM + resolverRetryAmplifiedRPM
	budgetRPM := 60.0 / constants.YouTubeScraperRateLimitConfig.RequestInterval.Seconds()

	logger.Info("youtube_scraper_combined_budget_summary",
		slog.Float64("expected_poller_rpm", pollerRPM),
		slog.Float64("expected_poller_retry_amplified_rpm_max", pollerRetryAmplifiedRPM),
		slog.Float64("expected_resolver_rpm", resolverRPM),
		slog.Float64("expected_resolver_retry_amplified_rpm_max", resolverRetryAmplifiedRPM),
		slog.Float64("expected_combined_rpm", combinedRPM),
		slog.Float64("expected_combined_retry_amplified_rpm_max", combinedRetryAmplifiedRPM),
		slog.Float64("budget_rpm", budgetRPM),
	)
	if combinedRPM > budgetRPM {
		logger.Warn("youtube_scraper_combined_budget_exceeds_rate_limit",
			slog.Float64("expected_poller_rpm", pollerRPM),
			slog.Float64("expected_resolver_rpm", resolverRPM),
			slog.Float64("expected_combined_rpm", combinedRPM),
			slog.Float64("budget_rpm", budgetRPM),
		)
	}
	if combinedRetryAmplifiedRPM > budgetRPM {
		logger.Warn("youtube_scraper_retry_amplified_budget_exceeds_rate_limit",
			slog.Float64("expected_poller_rpm", pollerRPM),
			slog.Float64("expected_poller_retry_amplified_rpm_max", pollerRetryAmplifiedRPM),
			slog.Float64("expected_resolver_retry_amplified_rpm_max", resolverRetryAmplifiedRPM),
			slog.Float64("expected_combined_retry_amplified_rpm_max", combinedRetryAmplifiedRPM),
			slog.Float64("budget_rpm", budgetRPM),
		)
	}
}

func estimatedPollerWorstCaseRPM(registrations []providers.ChannelPollerRegistration) float64 {
	return estimateResolvedPollerRPM(registrations) * float64(scraper.FetchPageMaxAttempts)
}

func estimatedPublishedAtResolverWorstCaseRPM(cfg config.ScraperPublishedAtResolverConfig) float64 {
	return estimatedPublishedAtResolverMaxRPM(cfg) * float64(scraper.FetchPageMaxAttempts)
}

func estimateResolvedPollerRPM(registrations []providers.ChannelPollerRegistration) float64 {
	var rpm float64
	for _, registration := range registrations {
		if registration.Poller == nil || registration.Interval <= 0 {
			continue
		}
		channelCount := len(mergeUniqueChannelIDs(registration.ChannelIDs))
		if channelCount == 0 {
			continue
		}
		rpm += float64(channelCount) * (60.0 / registration.Interval.Seconds())
	}
	return rpm
}

func validateExplicitPollerRegistrations(registrations []providers.ChannelPollerRegistration) error {
	missing := make([]string, 0)
	for _, registration := range registrations {
		if registration.Poller == nil || registration.Interval <= 0 {
			continue
		}
		if registration.HasExplicitChannelIDs {
			continue
		}
		missing = append(missing, registration.Poller.Name())
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf(
		"stream-ingester poller registrations require explicit channel IDs: %s",
		strings.Join(missing, ", "),
	)
}
