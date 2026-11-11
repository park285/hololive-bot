package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"

	"github.com/kapu/hololive-shared/pkg/config/envload"
)

const (
	celebrationRunnerEnabledEnv    = "CELEBRATION_RUNNER_ENABLED"
	birthdayStreamRunnerEnabledEnv = "BIRTHDAY_STREAM_RUNNER_ENABLED"
)

// CelebrationRunnerConfig는 CELEBRATION_RUNNER_ENABLED와 켜진 runner의 확인 시각·주기다.
// 꺼져 있으면 CELEBRATION_CHECK_HOUR_KST·CELEBRATION_RUN_INTERVAL_MS를 읽지 않고 나머지 필드는 0이다.
type CelebrationRunnerConfig struct {
	Enabled      bool
	CheckHourKST int
	RunInterval  time.Duration
}

// BirthdayStreamRunnerConfig는 BIRTHDAY_STREAM_RUNNER_ENABLED와 켜진 runner의 평가 주기·세션 신선도 창이다.
// 꺼져 있으면 BIRTHDAY_STREAM_*_MS를 읽지 않고 나머지 필드는 0이다.
type BirthdayStreamRunnerConfig struct {
	Enabled          bool
	RunInterval      time.Duration
	SessionFreshness time.Duration
}

// XSpacesConfig는 X_SPACES_CONFIG_FILE·X_SPACES_KEY_FILE 경로다. ConfigFile이 비어 있으면 수집이 꺼진다.
// 파일 내용 검증은 runner 조립이 한다.
type XSpacesConfig struct {
	ConfigFile string
	KeyFile    string
}

func loadCelebrationRunnerConfig() (CelebrationRunnerConfig, error) {
	// 잘못된 스위치 값을 "꺼짐"으로 읽고 넘어가지 않고 기동 오류로 드러낸다.
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

func loadBirthdayStreamRunnerConfig() (BirthdayStreamRunnerConfig, error) {
	enabled, err := sharedenv.BoolE(birthdayStreamRunnerEnabledEnv, false)
	if err != nil {
		return BirthdayStreamRunnerConfig{}, fmt.Errorf("birthday stream runner enabled: %w", err)
	}

	if !enabled {
		return BirthdayStreamRunnerConfig{}, nil
	}

	runInterval, err := envload.RequiredMillisDurationEnv("BIRTHDAY_STREAM_POLL_INTERVAL_MS", 30*time.Minute)
	if err != nil {
		return BirthdayStreamRunnerConfig{}, fmt.Errorf("birthday stream runner config: %w", err)
	}

	sessionFreshness, err := envload.RequiredMillisDurationEnv("BIRTHDAY_STREAM_SESSION_FRESHNESS_MS", 30*time.Minute)
	if err != nil {
		return BirthdayStreamRunnerConfig{}, fmt.Errorf("birthday stream runner config: %w", err)
	}

	return BirthdayStreamRunnerConfig{Enabled: true, RunInterval: runInterval, SessionFreshness: sessionFreshness}, nil
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

// hourOfDayEnv는 key의 값을 0부터 23까지의 시(hour)로 읽는다. 키가 없으면 def를 쓴다.
// 키가 있는데 비어 있거나 공백뿐이면, 또는 정수가 아니거나 범위 밖이면 기본값으로 바꾸지 않고 설정 오류를 돌려준다.
// 빈 값 규칙은 envload.RequiredPositiveIntEnv와 같고, 오류에는 값을 넣지 않는다.
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
