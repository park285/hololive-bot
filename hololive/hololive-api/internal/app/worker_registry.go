package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	botruntime "github.com/kapu/hololive-api/internal/planes/bot/runtime"
	youtuberuntime "github.com/kapu/hololive-api/internal/planes/youtube/runtime"
	"github.com/kapu/hololive-shared/pkg/config/settings"
)

func installAPIWorkerRegistry(ctx context.Context, config *settings.HololiveAPIConfig, bot *botruntime.BotRuntime, youtube *youtuberuntime.Runtime) error {
	if err := validateAPIWorkerRegistryInputs(config, bot); err != nil {
		return fmt.Errorf("validate API worker registry inputs: %w", err)
	}

	loaded := config.Bot.APIWorkerProfile.Loaded
	checker := workercontract.NewProfileFileChecker(loaded, time.Now())
	registry := workercontract.NewRegistry(loaded, checker)

	for _, registration := range bot.WorkerRegistrations() {
		if err := registry.Register(registration); err != nil {
			return fmt.Errorf("register %s worker: %w", registration.WorkerID, err)
		}
	}

	if err := registerYouTubeWorker(registry, youtube); err != nil {
		return fmt.Errorf("register youtube worker: %w", err)
	}

	if err := registry.Seal(); err != nil {
		return fmt.Errorf("seal API worker registry: %w", err)
	}

	bot.InstallWorkerRegistry(ctx, registry, checker)

	return nil
}

func validateAPIWorkerRegistryInputs(config *settings.HololiveAPIConfig, bot *botruntime.BotRuntime) error {
	if config == nil || config.Bot == nil || config.Bot.APIWorkerProfile == nil {
		return errors.New("install API worker registry: worker profile is required")
	}

	if bot == nil {
		return errors.New("install API worker registry: bot runtime is required")
	}

	return nil
}

func registerYouTubeWorker(registry *workercontract.Registry, youtube *youtuberuntime.Runtime) error {
	if youtube != nil {
		registration := youtube.WorkerRegistration()
		if err := registry.Register(registration); err != nil {
			return fmt.Errorf("register %s worker: %w", registration.WorkerID, err)
		}

		return nil
	}

	code := workercontract.QueueNotSampled
	registration := workercontract.Registration{
		WorkerID:          "source_observation",
		Runtime:           workercontract.RuntimeGo,
		QueueBackend:      workercontract.QueuePostgres,
		QueueScope:        workercontract.QueueScopeShared,
		SettingsValidated: true,
		QueueSnapshot: func() workercontract.QueueSnapshot {
			return workercontract.QueueSnapshot{Status: workercontract.QueueSnapshotUnavailable, ErrorCode: &code}
		},
	}

	if err := registry.Register(registration); err != nil {
		return fmt.Errorf("register unavailable source_observation worker: %w", err)
	}

	return nil
}
