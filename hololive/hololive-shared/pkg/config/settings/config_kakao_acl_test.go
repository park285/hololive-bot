package settings

import (
	"os"
	"strings"
	"testing"
)

func TestLoadKakaoConfigDefaultsWhenACLVariablesAreUnset(t *testing.T) {
	unsetEnvForTest(t, kakaoACLEnabledEnv)
	unsetEnvForTest(t, kakaoACLModeEnv)

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

func TestKakaoConfig_SetACLEnabled(t *testing.T) {
	t.Parallel()

	c := &KakaoConfig{
		ACLEnabled: false,
		ACLMode:    "whitelist",
		Rooms:      []string{"room1"},
	}

	if c.ACLEnabled {
		t.Fatal("ACLEnabled should start as false")
	}

	c.SetACLEnabled(true)

	enabled, _, _ := c.SnapshotACL()
	if !enabled {
		t.Fatal("ACLEnabled should be true after SetACLEnabled(true)")
	}

	c.SetACLEnabled(false)

	enabled, _, _ = c.SnapshotACL()
	if enabled {
		t.Fatal("ACLEnabled should be false after SetACLEnabled(false)")
	}
}

func TestKakaoConfig_IsRoomAllowed_EmptyChatIDDenied(t *testing.T) {
	t.Parallel()

	c := &KakaoConfig{
		ACLEnabled: true,
		ACLMode:    "whitelist",
		Rooms:      []string{"room1"},
	}

	if c.IsRoomAllowed("name", "") {
		t.Fatal("empty chatID should be denied when ACL is enabled")
	}
}

func TestKakaoConfig_IsRoomAllowed_BlacklistMode(t *testing.T) {
	t.Parallel()

	c := &KakaoConfig{
		ACLEnabled: true,
		ACLMode:    "blacklist",
		Rooms:      []string{"blocked-room"},
	}

	if c.IsRoomAllowed("name", "blocked-room") {
		t.Fatal("blocked room should be denied in blacklist mode")
	}

	if !c.IsRoomAllowed("name", "other-room") {
		t.Fatal("non-blocked room should be allowed in blacklist mode")
	}
}

func TestKakaoConfig_AddRoom_EmptyRoomRejected(t *testing.T) {
	t.Parallel()

	c := &KakaoConfig{Rooms: []string{}}
	if c.AddRoom("") {
		t.Fatal("AddRoom(\"\") should return false")
	}

	if c.AddRoom("  ") {
		t.Fatal("AddRoom(\"  \") should return false")
	}
}

func TestKakaoConfig_RemoveRoom_EmptyRoomRejected(t *testing.T) {
	t.Parallel()

	c := &KakaoConfig{Rooms: []string{"a"}}
	if c.RemoveRoom("") {
		t.Fatal("RemoveRoom(\"\") should return false")
	}
}

func TestKakaoConfig_RemoveRoom_NotFound(t *testing.T) {
	t.Parallel()

	c := &KakaoConfig{Rooms: []string{"a", "b"}}
	if c.RemoveRoom("c") {
		t.Fatal("RemoveRoom(\"c\") should return false when not in list")
	}

	if len(c.Rooms) != 2 {
		t.Fatalf("Rooms length = %d after RemoveRoom miss, want 2", len(c.Rooms))
	}
}
