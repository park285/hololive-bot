package workerapp

import (
	"fmt"
	"log/slog"
	"time"

	envutil "github.com/park285/shared-go/v2/pkg/envutil"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/service/alarm/handoff"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
)

func youtubeOutboxHandoffActivation(appConfig *settings.Config, logger *slog.Logger) (handoff.Mode, bool, error) {
	mode, err := parseYouTubeOutboxHandoffMode()
	if err != nil {
		return mode, false, fmt.Errorf("parse youtube outbox handoff mode: %w", err)
	}

	enabled, err := youtubeOutboxDispatcherEnabled(appConfig, mode, logger)
	if err != nil {
		return mode, false, fmt.Errorf("youtube outbox dispatcher enabled: %w", err)
	}

	return mode, enabled, nil
}

func parseYouTubeOutboxHandoffMode() (handoff.Mode, error) {
	out, err := handoff.ParseMode(envutil.String("YOUTUBE_OUTBOX_V3_HANDOFF_MODE", "off"))
	if err != nil {
		return out, fmt.Errorf("parse mode: %w", err)
	}

	return out, nil
}

func youtubeOutboxDispatcherEnabled(appConfig *settings.Config, mode handoff.Mode, logger *slog.Logger) (bool, error) {
	if appConfig.AlarmWorkerProfile.Loaded.Profile.Workers["youtube_delivery"].Executor.Enabled {
		return true, nil
	}

	if mode != handoff.ModeOff {
		return false, fmt.Errorf("youtube outbox v3 handoff mode %q requires youtube_delivery executor.enabled=true", mode)
	}

	if logger != nil {
		logger.Info("YouTube outbox dispatcher disabled")
	}

	return false, nil
}

func validateYouTubeOutboxHandoffConfig(_ handoff.Mode) error {
	return nil
}

func newYouTubeOutboxDispatcher(
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	sender delivery.MessageSender,
	logger *slog.Logger,
) *youtubedispatch.Dispatcher {
	profile := appConfig.AlarmWorkerProfile.YouTubeDelivery
	worker := appConfig.AlarmWorkerProfile.Loaded.Profile.Workers["youtube_delivery"]
	dispatchConfig := dispatchstate.Config{
		BatchSize: profile.BatchSize, LockTimeout: durationMS(profile.LockTimeoutMS),
		PollInterval: durationMS(profile.PollIntervalMS), MaxRetries: profile.MaxRetries,
		RetryBackoff: durationMS(profile.RetryBackoffMS), CleanupAfter: durationMS(profile.CleanupAfterMS),
		CleanupEnabled: profile.CleanupEnabled, ReviveEnabled: profile.ReviveEnabled,
		ReviveInterval: durationMS(profile.ReviveIntervalMS), ReviveFreshnessWindow: durationMS(profile.ReviveFreshnessWindowMS),
		ClaimFreshnessWindow: durationMS(profile.ClaimFreshnessWindowMS), DeliveryParallelism: worker.Executor.ConfiguredWorkers,
		DeliverySendTimeout: durationMS(profile.DeliverySendTimeoutMS), SubscriberLookupParallelism: profile.SubscriberLookupParallelism,
		AggregateSyncInterval: durationMS(profile.AggregateSyncIntervalMS), TelemetryPollInterval: durationMS(profile.TelemetryPollIntervalMS),
		TelemetryBackfillBatch: profile.TelemetryBackfillBatch, TelemetryFlushBatch: profile.TelemetryFlushBatch,
		TelemetryRetryBackoff: durationMS(profile.TelemetryRetryBackoffMS), TelemetryRetention: durationMS(profile.TelemetryRetentionMS),
	}

	return youtubedispatch.NewDispatcher(
		infra.Postgres.GetPool(),
		infra.Cache,
		sender,
		template.NewRenderer(infra.Postgres.GetPool(), logger),
		logger,
		&dispatchConfig,
	)
}

func durationMS(milliseconds int64) time.Duration {
	return time.Duration(milliseconds) * time.Millisecond
}

func configureYouTubeOutboxDispatcher(
	dispatcher *youtubedispatch.Dispatcher,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
	mode handoff.Mode,
) error {
	if mode == handoff.ModeOff {
		return nil
	}

	publisher := queue.NewPublisher(
		infra.Cache,
		logger,
		queue.WithOutbox(dispatchoutbox.NewPgxRepository(infra.Postgres, logger)),
	)

	if err := dispatcher.ConfigureHandoff(mode, youtubeOutboxDispatchPublisher{publisher: publisher}); err != nil {
		return fmt.Errorf("configure handoff: %w", err)
	}

	return nil
}
