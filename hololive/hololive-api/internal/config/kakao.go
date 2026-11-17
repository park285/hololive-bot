package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	sharedenv "github.com/park285/shared-go/core/envutil"

	"github.com/kapu/hololive-shared/pkg/config/envload"
)

const (
	kakaoACLEnabledEnv = "KAKAO_ACL_ENABLED"
	kakaoACLModeEnv    = "KAKAO_ACL_MODE"
)

// KakaoConfig는 room ACL 초기 seed다. 실행 중 방 목록 변경과 허용 판정은 hololive-api acl.Service가 소유한다.
type KakaoConfig struct {
	Rooms      []string
	ACLEnabled bool
	ACLMode    string // "whitelist" 또는 "blacklist"
}

func loadKakaoConfig() (KakaoConfig, error) {
	enabled, err := loadKakaoACLEnabled()
	if err != nil {
		return KakaoConfig{}, fmt.Errorf("load kakao ACL enabled: %w", err)
	}

	mode, err := loadKakaoACLMode()
	if err != nil {
		return KakaoConfig{}, fmt.Errorf("load kakao ACL mode: %w", err)
	}

	// KAKAO_ROOMS는 ACL 초기 seed이며 정규 signed i64 chatID 목록이 필수다.
	return KakaoConfig{
		Rooms:      envload.CommaSeparated(sharedenv.String("KAKAO_ROOMS", "")),
		ACLEnabled: enabled,
		ACLMode:    mode,
	}, nil
}

func loadKakaoACLEnabled() (bool, error) {
	const key = kakaoACLEnabledEnv

	raw, found := os.LookupEnv(key)

	if !found {
		return true, nil
	}

	if strings.TrimSpace(raw) == "" {
		return false, fmt.Errorf("%s must not be empty", key)
	}

	enabled, err := sharedenv.BoolE(key, true)
	if err != nil {
		return false, fmt.Errorf("read bool env: %w", err)
	}

	return enabled, nil
}

func loadKakaoACLMode() (string, error) {
	const key = kakaoACLModeEnv

	raw, found := os.LookupEnv(key)

	if !found {
		return "whitelist", nil
	}

	mode := strings.ToLower(strings.TrimSpace(raw))
	switch mode {
	case "whitelist", "blacklist":
		return mode, nil
	default:
		return "", fmt.Errorf("invalid %s: %q", key, raw)
	}
}

func validateKakaoRooms(rooms []string) error {
	if len(rooms) == 0 {
		return errors.New("KAKAO_ROOMS is required")
	}

	return nil
}
