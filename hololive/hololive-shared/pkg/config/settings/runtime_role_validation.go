package settings

import (
	"errors"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/config/settings/internal/load"
)

// proactive notification egress 소유를 거부하는 bot runtime config 로더다.
func LoadBotRuntime() (*Config, error) {
	out, err := LoadConfig((*Config).ValidateBotRuntime, LoadOptions{
		Section:        loadAPIWorkerProfile,
		TracingRuntime: TracingRuntimeHololiveAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("load config validated: %w", err)
	}

	return out, nil
}

func (c *Config) ValidateBotRuntime() error {
	if err := c.validateWithRequired(c.validateRequiredConfig); err != nil {
		return fmt.Errorf("validate with required: %w", err)
	}

	if c.APIWorkerProfile == nil {
		return errors.New("bot runtime requires Stack Worker Profile v1")
	}

	if err := load.ValidateNoNotificationEgressOwnership(load.RuntimeBot); err != nil {
		return fmt.Errorf("validate no notification egress ownership: %w", err)
	}

	return nil
}
