package app

import (
	"context"
	"fmt"
	"time"

	botruntime "github.com/kapu/hololive-api/internal/planes/bot/runtime"
	youtuberuntime "github.com/kapu/hololive-api/internal/planes/youtube/runtime"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/park285/shared-go/pkg/workercontract"
)

func installAPIWorkerRegistry(ctx context.Context, config *settings.HololiveAPIConfig, bot *botruntime.BotRuntime, youtube *youtuberuntime.Runtime) error {
	if config == nil || config.Bot == nil || config.Bot.APIWorkerProfile == nil {
		return fmt.Errorf("install API worker registry: worker profile is required")
	}
	if bot == nil {
		return fmt.Errorf("install API worker registry: bot runtime is required")
	}
	loaded := config.Bot.APIWorkerProfile.Loaded
	checker := workercontract.NewProfileFileChecker(loaded, time.Now())
	registry := workercontract.NewRegistry(loaded, checker)
	for _, registration := range bot.WorkerRegistrations() {
		if err := registry.Register(registration); err != nil {
			return err
		}
	}
	if youtube != nil {
		if err := registry.Register(youtube.WorkerRegistration()); err != nil {
			return err
		}
	} else {
		code := workercontract.QueueNotSampled
		if err := registry.Register(workercontract.Registration{
			WorkerID:          "source_observation",
			Runtime:           workercontract.RuntimeGo,
			QueueBackend:      workercontract.QueuePostgres,
			QueueScope:        workercontract.QueueScopeShared,
			SettingsValidated: true,
			QueueSnapshot: func() workercontract.QueueSnapshot {
				return workercontract.QueueSnapshot{Status: workercontract.QueueSnapshotUnavailable, ErrorCode: &code}
			},
		}); err != nil {
			return err
		}
	}
	if err := registry.Seal(); err != nil {
		return err
	}
	bot.InstallWorkerRegistry(ctx, registry, checker)
	return nil
}
