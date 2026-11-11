package settings

import (
	"os"
	"strings"
	"testing"
)

func TestLoadKakaoConfigDefaultsWhenACLVariablesAreUnset(t *testing.T) {
	unsetEnvForTest(t, kakaoACLEnabledEnv)
	unsetEnvForTest(t, kakaoACLModeEnv)

	config, err := LoadKakaoConfig()
	if err != nil {
		t.Fatalf("LoadKakaoConfig() error = %v", err)
	}

	if !config.ACLEnabled {
		t.Fatal("LoadKakaoConfig() ACLEnabled = false, want true default")
	}

	if config.ACLMode != "whitelist" {
		t.Fatalf("LoadKakaoConfig() ACLMode = %q, want whitelist default", config.ACLMode)
	}
}

// KAKAO_ROOMS는 ACL 첫 초기화 seed다. 방 이름 기본값은 chatID 단일 식별에서 어떤 방과도 매칭되지 않는
// 죽은 whitelist 행이 되므로, 값이 없으면 채우지 않고 필수값 검증이 기동 실패로 드러낸다.
func TestLoadKakaoConfigHasNoRoomDefault(t *testing.T) {
	unsetEnvForTest(t, "KAKAO_ROOMS")

	config, err := LoadKakaoConfig()
	if err != nil {
		t.Fatalf("LoadKakaoConfig() error = %v", err)
	}

	if len(config.Rooms) != 0 {
		t.Fatalf("LoadKakaoConfig() Rooms = %q, want no default seed", config.Rooms)
	}

	if err := ValidateKakaoRooms(config.Rooms); err == nil || err.Error() != "KAKAO_ROOMS is required" {
		t.Fatalf("ValidateKakaoRooms() error = %v, want KAKAO_ROOMS is required", err)
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

			config, err := LoadKakaoConfig()
			if err == nil {
				t.Fatalf("LoadKakaoConfig() = %+v, nil error", config)
			}

			if !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("LoadKakaoConfig() error = %v, want %q", err, tc.wantError)
			}
		})
	}
}

func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()

	// 같은 값으로 t.Setenv를 부르는 것은 원래 값 복원 cleanup을 등록하기 위한 것이고, 실제 unset은 그다음 줄이 한다.
	if value, found := os.LookupEnv(key); found {
		t.Setenv(key, value)
	}

	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("os.Unsetenv(%q) error = %v", key, err)
	}
}
