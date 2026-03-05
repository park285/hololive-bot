package app

import "testing"

func TestOutboxConfigFromEnv_DefaultFalse(t *testing.T) {
	t.Setenv("YOUTUBE_OUTBOX_PER_ROOM_MODE", "")

	cfg := outboxConfigFromEnv()
	if cfg.PerRoomMode {
		t.Fatalf("expected PerRoomMode=false by default")
	}
}

func TestOutboxConfigFromEnv_True(t *testing.T) {
	t.Setenv("YOUTUBE_OUTBOX_PER_ROOM_MODE", "true")

	cfg := outboxConfigFromEnv()
	if !cfg.PerRoomMode {
		t.Fatalf("expected PerRoomMode=true when env is true")
	}
}

func TestOutboxConfigFromEnv_InvalidFallback(t *testing.T) {
	t.Setenv("YOUTUBE_OUTBOX_PER_ROOM_MODE", "not-a-bool")

	cfg := outboxConfigFromEnv()
	if cfg.PerRoomMode {
		t.Fatalf("expected PerRoomMode=false on invalid env value")
	}
}
