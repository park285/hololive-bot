package xspaces

import (
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type Target struct {
	UserID    string `json:"user_id"`
	ChannelID string `json:"channel_id"`
}

type Config struct {
	Targets     []Target `json:"targets"`
	PollSeconds int      `json:"poll_seconds"`
}

var ErrDisabled = errors.New("x spaces collection disabled")

func LoadConfig(path string) (result *Config, retErr error) {
	if path == "" {
		return nil, ErrDisabled
	}

	if !filepath.IsAbs(path) {
		return nil, errors.New("x spaces config path must be absolute")
	}

	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("open X spaces config directory: %w", err)
	}

	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close X spaces config directory: %w", closeErr))
		}
	}()

	file, err := root.Open(filepath.Base(path))
	if err != nil {
		return nil, fmt.Errorf("open X spaces config: %w", err)
	}
	defer file.Close()

	raw, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil || len(raw) > 65536 {
		return nil, errors.New("read X spaces config failed or too large")
	}

	config := Config{PollSeconds: 120}
	if err := jsonv2.Unmarshal(raw, &config, jsonv2.RejectUnknownMembers(true)); err != nil {
		return nil, errors.New("invalid X spaces config JSON")
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &config, nil
}

var targetChannelIDPattern = regexp.MustCompile(`^UC[a-zA-Z0-9_-]{22}$`)

func (c Config) Validate() error {
	if len(c.Targets) == 0 || len(c.Targets) > 100 || c.PollSeconds < 120 || c.PollSeconds > 3600 {
		return errors.New("x spaces requires 1-100 targets and a 120-3600 second interval")
	}

	seenUsers := make(map[string]bool)
	seenChannels := make(map[string]bool)

	for _, target := range c.Targets {
		payload := domain.XSpaceDispatchPayload{SpaceID: "validation", CreatorID: target.UserID, ChannelID: target.ChannelID, StartedAt: time.Unix(1, 0)}
		if payload.Validate() != nil || !targetChannelIDPattern.MatchString(target.ChannelID) || seenUsers[target.UserID] || seenChannels[target.ChannelID] {
			return errors.New("invalid or duplicate X spaces target")
		}

		seenUsers[target.UserID], seenChannels[target.ChannelID] = true, true
	}

	return nil
}
