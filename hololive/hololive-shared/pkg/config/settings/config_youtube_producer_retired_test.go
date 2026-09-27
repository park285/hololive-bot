package settings

import "testing"

// 아래 두 테스트는 퇴역 가드의 fail-closed 계약을 고정한다. 제거 조건과 삭제 리비전은
// config_youtube_producer_retired.go 상단 주석이 소유하며, 가드를 삭제하는 리비전에서 두 테스트도 함께 삭제한다.
// TestLoadYouTubeConfigAcceptsRequestIntervalOverride는 후속 env 계약이므로 남긴다.
func TestRejectRetiredYouTubeProducerEnv(t *testing.T) {
	// 존재 기준이다. 빈 값으로 남은 키도 거절해야 퇴역 키가 남은 호스트를 드러낸다.
	for _, key := range retiredYouTubeProducerEnvKeys {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "")

			if err := rejectRetiredYouTubeProducerEnv(); err == nil {
				t.Fatalf("retired producer env %s must fail closed even when empty", key)
			}
		})
	}
}

func TestLoadYouTubeConfigRejectsRetiredProducerEnv(t *testing.T) {
	t.Setenv("YOUTUBE_PRODUCER_REQUEST_INTERVAL_SECONDS", "2")

	if _, err := loadYouTubeConfig(); err == nil {
		t.Fatal("loadYouTubeConfig must reject retired producer env")
	}
}

func TestLoadYouTubeConfigAcceptsRequestIntervalOverride(t *testing.T) {
	t.Setenv("YOUTUBE_REQUEST_INTERVAL_SECONDS", "5")

	cfg, err := loadYouTubeConfig()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.RequestInterval.Seconds() != 5 {
		t.Fatalf("RequestInterval = %s, want 5s", cfg.RequestInterval)
	}
}
