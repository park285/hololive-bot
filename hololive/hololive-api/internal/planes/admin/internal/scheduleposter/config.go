// Package scheduleposter는 X의 멤버 주간 일정 포스터를 후보로 수집하고, 관리자가 확정한 포스터를 한국어 이미지로 번역한다.
package scheduleposter

import (
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

const (
	defaultPollSeconds = 3600
	defaultLocation    = "global"
	defaultModel       = "gemini-nano-banana-2.1"
	maxConfigBytes     = 64 << 10
)

var ErrDisabled = errors.New("schedule poster collection disabled")

var (
	xUserIDPattern   = regexp.MustCompile(`^[1-9]\d{0,19}$`)
	channelIDPattern = regexp.MustCompile(`^UC[a-zA-Z0-9_-]{22}$`)
	projectPattern   = regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`)
	locationPattern  = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)
	modelPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,79}$`)
)

type Target struct {
	XUserID   string `json:"x_user_id"`
	ChannelID string `json:"channel_id"`
}

// TranslationConfig의 인증은 GOOGLE_APPLICATION_CREDENTIALS(ADC)로 따로 공급한다. 파일에는 비밀을 넣지 않는다.
type TranslationConfig struct {
	Project  string `json:"project"`
	Location string `json:"location"`
	Model    string `json:"model"`
}

type Config struct {
	PollSeconds int               `json:"poll_seconds"`
	Targets     []Target          `json:"targets"`
	Translation TranslationConfig `json:"translation"`
}

func (c Config) PollInterval() time.Duration {
	return time.Duration(c.PollSeconds) * time.Second
}

// LoadConfig는 SCHEDULE_POSTER_CONFIG_FILE 경로가 비어 있으면 ErrDisabled를 돌려준다.
func LoadConfig(path string) (result *Config, retErr error) {
	if path == "" {
		return nil, ErrDisabled
	}

	if !filepath.IsAbs(path) {
		return nil, errors.New("schedule poster config path must be absolute")
	}

	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("open schedule poster config directory: %w", err)
	}

	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close schedule poster config directory: %w", closeErr))
		}
	}()

	file, err := root.Open(filepath.Base(path))
	if err != nil {
		return nil, fmt.Errorf("open schedule poster config: %w", err)
	}

	defer file.Close()

	raw, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil || len(raw) > maxConfigBytes {
		return nil, errors.New("read schedule poster config failed or too large")
	}

	config := Config{
		PollSeconds: defaultPollSeconds,
		Translation: TranslationConfig{Location: defaultLocation, Model: defaultModel},
	}
	if err := jsonv2.Unmarshal(raw, &config, jsonv2.RejectUnknownMembers(true)); err != nil {
		return nil, errors.New("invalid schedule poster config JSON")
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &config, nil
}

func (c Config) Validate() error {
	if len(c.Targets) == 0 || len(c.Targets) > 20 || c.PollSeconds < 600 || c.PollSeconds > 86400 {
		return errors.New("schedule poster requires 1-20 targets and a 600-86400 second interval")
	}

	seenUsers := make(map[string]bool, len(c.Targets))
	for _, target := range c.Targets {
		if !xUserIDPattern.MatchString(target.XUserID) || !channelIDPattern.MatchString(target.ChannelID) || seenUsers[target.XUserID] {
			return errors.New("invalid or duplicate schedule poster target")
		}

		seenUsers[target.XUserID] = true
	}

	translation := c.Translation
	if !projectPattern.MatchString(translation.Project) || !locationPattern.MatchString(translation.Location) ||
		!modelPattern.MatchString(translation.Model) {
		return errors.New("schedule poster translation requires project, location and model")
	}

	return nil
}
