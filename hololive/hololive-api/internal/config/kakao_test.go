package config

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settingstest"
)

func TestLoadKakaoConfigDefaultsWhenACLVariablesAreUnset(t *testing.T) {
	settingstest.UnsetEnv(t, kakaoACLEnabledEnv)
	settingstest.UnsetEnv(t, kakaoACLModeEnv)

	config, err := loadKakaoConfig()
	if err != nil {
		t.Fatalf("loadKakaoConfig() error = %v", err)
	}

	if !config.ACLEnabled {
		t.Fatal("loadKakaoConfig() ACLEnabled = false, want true default")
	}

	if config.ACLMode != "whitelist" {
		t.Fatalf("loadKakaoConfig() ACLMode = %q, want whitelist default", config.ACLMode)
	}
}

func TestLoadKakaoConfigHasNoRoomDefault(t *testing.T) {
	settingstest.UnsetEnv(t, "KAKAO_ROOMS")

	config, err := loadKakaoConfig()
	if err != nil {
		t.Fatalf("loadKakaoConfig() error = %v", err)
	}

	if len(config.Rooms) != 0 {
		t.Fatalf("loadKakaoConfig() Rooms = %q, want no default seed", config.Rooms)
	}

	if err := validateKakaoRooms(config.Rooms); err == nil || err.Error() != "KAKAO_ROOMS is required" {
		t.Fatalf("validateKakaoRooms() error = %v, want KAKAO_ROOMS is required", err)
	}
}

func TestLoadKakaoConfigRejectsPresentInvalidACLVariables(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		value     string
		wantError string
	}{
		{name: "blank enabled", key: kakaoACLEnabledEnv, value: " ", wantError: "KAKAO_ACL_ENABLED must not be empty"},
		{name: "invalid enabled", key: kakaoACLEnabledEnv, value: "maybe", wantError: "invalid bool env KAKAO_ACL_ENABLED"},
		{name: "blank mode", key: kakaoACLModeEnv, value: " ", wantError: "invalid KAKAO_ACL_MODE"},
		{name: "invalid mode", key: kakaoACLModeEnv, value: "open", wantError: "invalid KAKAO_ACL_MODE"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(kakaoACLEnabledEnv, "true")
			t.Setenv(kakaoACLModeEnv, "whitelist")
			t.Setenv(tc.key, tc.value)

			config, err := loadKakaoConfig()
			if err == nil {
				t.Fatalf("loadKakaoConfig() = %+v, nil error", config)
			}

			if !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("loadKakaoConfig() error = %v, want %q", err, tc.wantError)
			}
		})
	}
}
