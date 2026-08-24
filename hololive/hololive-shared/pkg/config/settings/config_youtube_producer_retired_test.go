package settings

import "testing"

func TestRejectRetiredYouTubeProducerEnv(t *testing.T) {
	t.Setenv("YOUTUBE_PRODUCER_REQUEST_INTERVAL_SECONDS", "2")

	if err := rejectRetiredYouTubeProducerEnv(); err == nil {
		t.Fatal("retired producer env must fail closed")
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
