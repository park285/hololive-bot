package settings

import (
	"strings"
	"testing"
)

func TestLoadBotConfigAggregatesFoldAndOtherEnvErrors(t *testing.T) {
	for _, key := range []string{"BOT_SEE_MORE_FOLD", "BOT_MARKDOWN_REPLIES", "BOT_CALENDAR_ENTRY_CACHE_TTL_SECONDS"} {
		t.Setenv(key, "invalid")
	}

	_, err := LoadBotConfig()
	if err == nil {
		t.Fatal("invalid settings accepted")
	}

	for _, key := range []string{"BOT_SEE_MORE_FOLD", "BOT_MARKDOWN_REPLIES", "BOT_CALENDAR_ENTRY_CACHE_TTL_SECONDS"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error %v does not include %s", err, key)
		}
	}
}

func TestPostgresPoolSyntaxBelongsToPoolLoader(t *testing.T) {
	t.Setenv("POSTGRES_POOL_MAX_CONNS", "invalid")
	t.Setenv("POSTGRES_POOL_MIN_CONNS", "invalid")

	if _, err := LoadPostgresEndpointConfig(); err != nil {
		t.Fatalf("endpoint loader consumed pool input: %v", err)
	}

	if _, err := LoadPostgresConfig(); err == nil {
		t.Fatal("worker pool loader accepted malformed pool input")
	}
}
