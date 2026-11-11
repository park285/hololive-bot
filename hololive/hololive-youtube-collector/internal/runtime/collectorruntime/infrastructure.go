package collectorruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	databaseproviders "github.com/kapu/hololive-shared/pkg/providers/database"
	"github.com/kapu/hololive-shared/pkg/service/database"
	collectorconfig "github.com/kapu/hololive-youtube-collector/internal/config"
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

func initInfrastructure(ctx context.Context, appConfig *collectorconfig.RuntimeConfig, logger *slog.Logger, waitForDatabase bool) (*collectorInfrastructure, error) {
	if appConfig == nil {
		return nil, errors.New("build collector infra: config is nil")
	}

	databaseResources, cleanupDB, err := provideCollectorDatabase(ctx, appConfig, logger, waitForDatabase)
	if err != nil {
		return nil, fmt.Errorf("build collector infra: %w", err)
	}

	collector := appConfig.Collector

	helper, rpc, err := startYouTubeJSHelper(ctx, &collector, youtubejs.NewRateLimiter(collector.RequestInterval))
	if err != nil {
		cleanupDB()

		return nil, err
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

	rpc.EnableMetrics(nil, collector.RequestInterval)
	helper.EnableMetrics(nil)

	return collectorInfra, nil
}

// 실패한 pool은 provider가 닫고 반환하므로 재시도 사이에 이전 연결이나 통계 등록이 남지 않는다.
func provideCollectorDatabase(
	ctx context.Context,
	appConfig *collectorconfig.RuntimeConfig,
	logger *slog.Logger,
	waitForDatabase bool,
) (*databaseproviders.DatabaseResources, func(), error) {
	if waitForDatabase {
		if deadline, bounded := ctx.Deadline(); !bounded || time.Until(deadline) > 90*time.Second {
			return nil, nil, errors.New("database startup wait requires a deadline of at most 90s")
		}
	}

	var timer *time.Timer

	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}

		resources, cleanup, err := databaseproviders.ProvideDatabaseResources(ctx, &appConfig.Postgres, databaseproviders.RuntimePool{Name: "collector", ReservedConns: 1}, logger)
		if err == nil {
			return resources, cleanup, nil
		}

		if !waitForDatabase || ctx.Err() != nil || !errors.Is(err, context.DeadlineExceeded) {
			return nil, nil, fmt.Errorf("initialize collector database: %w", err)
		}

		logger.LogAttrs(ctx, slog.LevelWarn, "collector_database_startup_wait", slog.String("reason", "connection_deadline"))

		if timer == nil {
			timer = time.NewTimer(5 * time.Second)
		} else {
			timer.Reset(5 * time.Second)
		}

		select {
		case <-ctx.Done():
			return nil, nil, errors.Join(err, ctx.Err())
		case <-timer.C:
		}
	}
}

func (i *collectorInfrastructure) buildProviderClients(
	appConfig *collectorconfig.RuntimeConfig,
	collector *collectorconfig.Config,
) error {
	maxBody := int64(collector.MaxSuccessResponseBytes)

	holodexHTTP, err := providerhttp.NewProviderHTTPClient(providerTransportConfig(
		contract.ProviderHolodex,
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
		contract.ProviderHololiveOfficial,
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

func providerTransportConfig(
	provider contract.Provider,
	requestTimeout time.Duration,
	maxConns int,
) providerhttp.ProviderTransportConfig {
	idle := min(maxConns, 8)
	headerTimeout := 10 * time.Second

	if requestTimeout > 0 && requestTimeout < headerTimeout {
		headerTimeout = requestTimeout
	}

	return providerhttp.ProviderTransportConfig{
		MetricsClient:         string(provider),
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
	collector *collectorconfig.Config,
	limiter *youtubejs.RateLimiter,
) (*youtubejs.Helper, *youtubejs.RPC, error) {
	helper, rpc, err := youtubejs.Start(ctx, &youtubejs.Config{
		NodePath:          collector.YouTubeJSNodePath,
		ScriptPath:        collector.YouTubeJSScriptPath,
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
