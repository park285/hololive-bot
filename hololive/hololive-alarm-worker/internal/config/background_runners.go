package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	sharedenv "github.com/park285/shared-go/core/envutil"

	"github.com/kapu/hololive-shared/pkg/config/envload"
)

const celebrationRunnerEnabledEnv = "CELEBRATION_RUNNER_ENABLED"

type CelebrationRunnerConfig struct {
	Enabled      bool
	CheckHourKST int
	RunInterval  time.Duration
}

type XSpacesConfig struct {
	ConfigFile string
	KeyFile    string
}

func loadCelebrationRunnerConfig() (CelebrationRunnerConfig, error) {
	enabled, err := sharedenv.BoolE(celebrationRunnerEnabledEnv, false)
	if err != nil {
		return CelebrationRunnerConfig{}, fmt.Errorf("celebration runner enabled: %w", err)
	}

	if !enabled {
		return CelebrationRunnerConfig{}, nil
	}

	checkHour, err := hourOfDayEnv("CELEBRATION_CHECK_HOUR_KST", 0)
	if err != nil {
		return CelebrationRunnerConfig{}, fmt.Errorf("celebration runner config: %w", err)
	}

	runInterval, err := envload.RequiredMillisDurationEnv("CELEBRATION_RUN_INTERVAL_MS", time.Hour)
	if err != nil {
		return CelebrationRunnerConfig{}, fmt.Errorf("celebration runner config: %w", err)
	}

	return CelebrationRunnerConfig{Enabled: true, CheckHourKST: checkHour, RunInterval: runInterval}, nil
}

func loadXSpacesConfig() XSpacesConfig {
	return XSpacesConfig{
		ConfigFile: sharedenv.StringRaw("X_SPACES_CONFIG_FILE", ""),
		KeyFile:    sharedenv.StringRaw("X_SPACES_KEY_FILE", ""),
	}
}

func (c XSpacesConfig) validate() error {
	if c.ConfigFile != "" && c.KeyFile == "" {
		return errors.New("X_SPACES_CONFIG_FILE requires X_SPACES_KEY_FILE")
	}

	return nil
}

func hourOfDayEnv(key string, def int) (int, error) {
	raw, found := os.LookupEnv(key)
	if !found {
		return def, nil
	}

	if strings.TrimSpace(raw) == "" {
		return 0, fmt.Errorf("%s must be an hour between 0 and 23", key)
	}

	value, err := sharedenv.IntE(key, def)
	if err != nil {
		return 0, fmt.Errorf("read hour env: %w", err)
	}

	if value < 0 || value > 23 {
		return 0, fmt.Errorf("%s must be an hour between 0 and 23", key)
	}

	return value, nil
}
