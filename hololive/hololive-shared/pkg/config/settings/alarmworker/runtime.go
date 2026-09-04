package alarmworker

import (
	"errors"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/config/settings/internal/load"
)

// RuntimeConfig: core Config에 alarm-worker 전용 구획을 더한 런타임 설정이다.
type RuntimeConfig struct {
	*settings.Config

	DispatchRetention DispatchRetentionConfig
}

func LoadRuntime() (*RuntimeConfig, error) {
	var retention DispatchRetentionConfig

	// worker profile과 dispatch retention은 alarm-worker만 읽으므로 core 로더 대신 여기서 채운다.
	loadSections := func(config *settings.Config) error {
		if err := rejectRetiredNotificationEgressEnv(); err != nil {
			return fmt.Errorf("reject retired notification egress env: %w", err)
		}

		profile, err := LoadWorkerProfile()
		if err != nil {
			return fmt.Errorf("load alarm worker profile: %w", err)
		}

		retention, err = loadDispatchRetentionConfig()
		if err != nil {
			return fmt.Errorf("load alarm dispatch retention config: %w", err)
		}

		config.AlarmWorkerProfile = profile

		return nil
	}

	core, err := settings.LoadConfig(validateRuntime, settings.LoadOptions{
		Section:        loadSections,
		TracingRuntime: settings.TracingRuntimeAlarmWorker,
	})
	if err != nil {
		return nil, fmt.Errorf("load config validated: %w", err)
	}

	return &RuntimeConfig{Config: core, DispatchRetention: retention}, nil
}

func validateRuntime(config *settings.Config) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	if config.AlarmWorkerProfile == nil {
		return errors.New("alarm-worker runtime requires Stack Worker Profile v1")
	}

	if err := validateOwnership(config.Environment); err != nil {
		return fmt.Errorf("validate alarm worker ownership: %w", err)
	}

	if !load.IsProduction(config.Environment) {
		return nil
	}

	if err := validateProductionExecutors(config.AlarmWorkerProfile); err != nil {
		return fmt.Errorf("validate production alarm executors: %w", err)
	}

	return nil
}
