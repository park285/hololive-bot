package settings

import "testing"

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
