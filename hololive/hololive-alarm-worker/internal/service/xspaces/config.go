// Package xspaces는 무료 X 웹 API 관측을 기존 알림 발송 원장으로 연결한다.
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

// Target은 X 개설자와 기존 방송 구독 채널 사이의 명시적 연결이다.
type Target struct {
	UserID     string `json:"user_id"`
	ChannelID  string `json:"channel_id"`
	MemberName string `json:"member_name"`
}

// Config는 설정된 경우에만 외부 조회를 활성화한다. 파일에는 인증 값을 넣지 않는다.
type Config struct {
	Targets     []Target `json:"targets"`
	PollSeconds int      `json:"poll_seconds"`
}

// ErrDisabled는 감시 설정을 지정하지 않아 수집이 꺼져 있음을 나타낸다.
var ErrDisabled = errors.New("x spaces collection disabled")

// LoadConfig는 X_SPACES_CONFIG_FILE이 없으면 비활성화하고 잘못된 설정은 거부한다.
func LoadConfig() (result *Config, retErr error) {
	path := os.Getenv("X_SPACES_CONFIG_FILE")
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

// Validate는 계정 범위와 호출 주기를 제한하고 모호한 멤버 매핑을 거부한다.
func (c Config) Validate() error {
	if len(c.Targets) == 0 || len(c.Targets) > 100 || c.PollSeconds < 120 || c.PollSeconds > 3600 {
		return errors.New("x spaces requires 1-100 targets and a 120-3600 second interval")
	}

	seenUsers := make(map[string]bool)
	seenChannels := make(map[string]bool)
	channelPattern := regexp.MustCompile(`^UC[a-zA-Z0-9_-]{22}$`)

	for _, target := range c.Targets {
		payload := domain.XSpaceDispatchPayload{SpaceID: "validation", CreatorID: target.UserID, ChannelID: target.ChannelID, MemberName: target.MemberName, StartedAt: time.Unix(1, 0)}
		if payload.Validate() != nil || !channelPattern.MatchString(target.ChannelID) || seenUsers[target.UserID] || seenChannels[target.ChannelID] {
			return errors.New("invalid or duplicate X spaces target")
		}

		seenUsers[target.UserID], seenChannels[target.ChannelID] = true, true
	}

	return nil
}
