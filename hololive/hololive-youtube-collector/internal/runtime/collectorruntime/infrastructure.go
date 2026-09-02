package collectorruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	collectorconfig "github.com/kapu/hololive-shared/pkg/config/settings/collector"
	"github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/holodexcollector"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/officialcollector"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/providerhttp"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
)

type collectorInfrastructure struct {
	postgres     database.Client
	youtubejs    *youtubejs.Helper
	youtubejsRPC *youtubejs.RPC
	holodex      *holodexcollector.Client
	official     *officialcollector.Client
	cleanupDB    func()
	closeOnce    sync.Once
	closeErr     error
}

func initInfrastructure(ctx context.Context, appConfig *collectorconfig.RuntimeConfig, logger *slog.Logger) (*collectorInfrastructure, error) {
	if appConfig == nil {
		return nil, errors.New("build collector infra: config is nil")
	}

	databaseResources, cleanupDB, err := providers.ProvideDatabaseResources(ctx, &appConfig.Postgres, logger)
	if err != nil {
		return nil, fmt.Errorf("build collector infra: %w", err)
	}

	collector := appConfig.Collector

	helper, rpc, err := startYouTubeJSHelper(ctx, &appConfig.Proxy, &collector, ratelimiter.New(collector.RequestInterval))
	if err != nil {
		cleanupDB()

		return nil, fmt.Errorf("start youtube JS helper: %w", err)
	}

	collectorInfra := &collectorInfrastructure{
		postgres:     databaseResources.Service,
		youtubejs:    helper,
		youtubejsRPC: rpc,
		cleanupDB:    cleanupDB,
	}
	if err := collectorInfra.buildProviderClients(appConfig, &collector); err != nil {
		return nil, errors.Join(err, collectorInfra.Close(ctx))
	}

	return collectorInfra, nil
}

func (i *collectorInfrastructure) buildProviderClients(
	appConfig *collectorconfig.RuntimeConfig,
	collector *collectorconfig.Config,
) error {
	maxBody := int64(collector.MaxSuccessResponseBytes)

	holodexHTTP, err := providerhttp.NewProviderHTTPClient(providerTransportConfig(
		appConfig.Holodex.Transport.Timeout,
		collector.HolodexMaxInflight,
	))
	if err != nil {
		return fmt.Errorf("build holodex HTTP client: %w", err)
	}

	holodex, err := holodexcollector.NewClient(holodexHTTP, appConfig.Holodex.BaseURL, appConfig.Holodex.APIKey, maxBody)
	if err != nil {
		return errors.Join(fmt.Errorf("build holodex collector client: %w", err), holodexHTTP.Close())
	}

	i.holodex = holodex

	officialHTTP, err := providerhttp.NewProviderHTTPClient(providerTransportConfig(
		appConfig.OfficialSchedule.Transport.Timeout,
		collector.OfficialMaxInflight,
	))
	if err != nil {
		return fmt.Errorf("build official HTTP client: %w", err)
	}

	official, err := officialcollector.NewClient(officialHTTP, appConfig.OfficialSchedule.BaseURL, maxBody)
	if err != nil {
		return errors.Join(fmt.Errorf("build official schedule collector client: %w", err), officialHTTP.Close())
	}

	i.official = official

	return nil
}

func providerTransportConfig(requestTimeout time.Duration, maxConns int) providerhttp.ProviderTransportConfig {
	idle := min(maxConns, 8)
	headerTimeout := 10 * time.Second

	if requestTimeout > 0 && requestTimeout < headerTimeout {
		headerTimeout = requestTimeout
	}

	return providerhttp.ProviderTransportConfig{
		RequestTimeout:        requestTimeout,
		DialTimeout:           5 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: headerTimeout,
		IdleConnTimeout:       30 * time.Second,
		MaxConnsPerHost:       maxConns,
		MaxIdleConnsPerHost:   idle,
	}
}

func (i *collectorInfrastructure) Close(ctx context.Context) error {
	if i == nil {
		return nil
	}

	i.closeOnce.Do(func() {
		i.closeErr = i.closeResources(ctx)
	})

	return i.closeErr
}

func (i *collectorInfrastructure) closeResources(ctx context.Context) error {
	var errs []error

	if i.official != nil {
		errs = append(errs, i.official.Close())
	}

	if i.holodex != nil {
		errs = append(errs, i.holodex.Close())
	}

	if i.youtubejs != nil {
		errs = append(errs, i.youtubejs.Close(ctx))
	}

	if i.cleanupDB != nil {
		i.cleanupDB()
	}

	return errors.Join(errs...)
}

func startYouTubeJSHelper(
	ctx context.Context,
	proxy *collectorconfig.ProxyConfig,
	collector *collectorconfig.Config,
	limiter *ratelimiter.RateLimiter,
) (*youtubejs.Helper, *youtubejs.RPC, error) {
	proxyConfig := collectorconfig.ProxyConfig{}

	if proxy != nil {
		proxyConfig = *proxy
	}

	helper, rpc, err := youtubejs.Start(ctx, &youtubejs.Config{
		Proxy: youtubejs.ProxyConfig{
			Enabled: proxyConfig.Enabled,
			URL:     proxyConfig.URL,
		},
		StartupTimeout:    collector.YouTubeJSStartupTimeout,
		RequestTimeout:    collector.YouTubeJSRequestTimeout,
		HealthTimeout:     collector.HelperHealthTimeout,
		ShutdownTimeout:   collector.YouTubeJSShutdownTimeout,
		RequestBodyLimit:  youtubejs.DefaultRequestBodyLimit,
		ResponseBodyLimit: int64(collector.MaxSuccessResponseBytes),
		MaxInflight:       collector.YouTubeJSMaxInflight,
		Limiter:           limiter,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("start youtube.js helper: %w", err)
	}

	return helper, rpc, nil
}
