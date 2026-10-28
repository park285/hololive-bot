package settings

import (
	"strings"
	"testing"
)

// 숫자·bool env의 잘못된 값은 기본값으로 바뀌지 않고 기동 실패로 드러나야 한다.
// 각 행은 공통 형식 검사가 다시 쓰는 공유 parser 하나를 대표하며,
// 해당 구획을 보관하지 않는 runtime에서도 같은 거절이 유지된다.
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

func TestLoadIrisConfigRejectsInvalidTimeouts(t *testing.T) {
	for _, value := range []string{"10s", "99999999999999999"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("IRIS_HTTP_TIMEOUT_SECONDS", value)

			if _, err := LoadIrisConfig(); err == nil || !strings.Contains(err.Error(), "IRIS_HTTP_TIMEOUT_SECONDS") {
				t.Fatalf("LoadIrisConfig() error = %v, want IRIS_HTTP_TIMEOUT_SECONDS rejection", err)
			}
		})
	}
}

// 공통 구획 여러 곳의 잘못된 숫자 env는 기본값으로 바뀌지 않고, 한 번의 기동 실패가 잘못된 키를 모두 보인다.
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
