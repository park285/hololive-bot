package config

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-api/internal/config/configtest"
	"github.com/kapu/hololive-shared/pkg/config/settingstest"
)

var configurePlanesIntEnvKeys = []string{
	"HOLOLIVE_ADMIN_API_PORT",
	"ADMIN_API_POSTGRES_POOL_MIN_CONNS",
	"ADMIN_API_POSTGRES_POOL_MAX_CONNS",
	"BOT_POSTGRES_POOL_MIN_CONNS",
	"BOT_POSTGRES_POOL_MAX_CONNS",
}

func clearConfigurePlanesIntEnv(t *testing.T) {
	t.Helper()

	for _, key := range configurePlanesIntEnvKeys {
		t.Setenv(key, "")
	}
}

func TestConfigurePlanesRejectsInvalidEnvValues(t *testing.T) {
	for _, key := range configurePlanesIntEnvKeys {
		t.Run(key, func(t *testing.T) {
			clearConfigurePlanesIntEnv(t)
			t.Setenv(key, "four")

			botConfig := &BotPlaneConfig{}
			adminConfig := &AdminPlaneConfig{}

			err := configurePlanes(botConfig, adminConfig)
			if err == nil {
				t.Fatalf("configurePlanes() accepted invalid %s; want a startup error", key)
			}

			if !strings.Contains(err.Error(), key) {
				t.Fatalf("configurePlanes() error = %v, want it to name %s", err, key)
			}
		})
	}
}

func TestLoadRuntimeRejectsInvalidPlaneEnvValues(t *testing.T) {
	for _, key := range []string{
		"HOLOLIVE_ADMIN_API_PORT",
		"ADMIN_API_POSTGRES_POOL_MIN_CONNS",
		"ADMIN_API_POSTGRES_POOL_MAX_CONNS",
		"BOT_POSTGRES_POOL_MIN_CONNS",
		"BOT_POSTGRES_POOL_MAX_CONNS",
	} {
		t.Run(key, func(t *testing.T) {
			settingstest.ClearRuntimeRoleEnv(t)
			configtest.SetRequiredLoadEnv(t)
			clearConfigurePlanesIntEnv(t)
			t.Setenv("SERVER_PORT", "")
			t.Setenv("APP_ENV", "development")
			t.Setenv("ALARM_INTERNAL_URL", "http://127.0.0.1:30007")
			t.Setenv(key, "1.5")

			_, err := LoadRuntime()
			if err == nil {
				t.Fatalf("LoadRuntime() accepted invalid %s; want a startup error", key)
			}

			if !strings.Contains(err.Error(), "configure hololive-api planes") || !strings.Contains(err.Error(), key) {
				t.Fatalf("LoadRuntime() error = %v, want the configure-planes error naming %s", err, key)
			}
		})
	}
}

func TestLoadRuntimeIgnoresGenericPoolKeys(t *testing.T) {
	settingstest.ClearRuntimeRoleEnv(t)
	configtest.SetRequiredLoadEnv(t)
	clearConfigurePlanesIntEnv(t)
	t.Setenv("SERVER_PORT", "")
	t.Setenv("APP_ENV", "development")
	t.Setenv("ALARM_INTERNAL_URL", "http://127.0.0.1:30007")
	t.Setenv("POSTGRES_POOL_MIN_CONNS", "ignored")
	t.Setenv("POSTGRES_POOL_MAX_CONNS", "ignored")
	t.Setenv("BOT_POSTGRES_POOL_MIN_CONNS", "0")

	config, err := LoadRuntime()
	if err != nil {
		t.Fatal(err)
	}

	if config.Bot.Postgres.PoolMinConns != 0 || config.Bot.Postgres.PoolMaxConns != 4 || config.Admin.Postgres.PoolMaxConns != 4 {
		t.Fatal("API plane pools did not use their own keys")
	}
}
