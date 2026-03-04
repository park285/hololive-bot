package app

import (
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/cache"
)

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Server: ServerConfig{Port: 30020},
		Iris: IrisConfig{
			BaseURL:  "http://localhost:3000",
			BotToken: "token",
		},
		Valkey: cache.Config{
			Host: "localhost",
			Port: 6379,
		},
		Dispatch: DispatchConfig{
			QueueKey:         "alarm:dispatch:queue",
			MaxBatch:         50,
			ReconnectBackoff: time.Second,
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestConfigValidate_RequiresBotToken(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Server: ServerConfig{Port: 30020},
		Iris: IrisConfig{
			BaseURL: "http://localhost:3000",
		},
		Valkey: cache.Config{
			Host: "localhost",
			Port: 6379,
		},
		Dispatch: DispatchConfig{
			QueueKey:         "alarm:dispatch:queue",
			MaxBatch:         50,
			ReconnectBackoff: time.Second,
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error, got nil")
	}
}
