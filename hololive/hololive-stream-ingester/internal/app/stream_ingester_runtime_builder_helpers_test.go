package app

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
)

func TestOutboxConfigFromEnv_UsesDefaultConfig(t *testing.T) {
	got := outboxConfigFromEnv()
	want := outbox.DefaultConfig()
	if got != want {
		t.Fatalf("outboxConfigFromEnv() = %#v, want %#v", got, want)
	}
}
