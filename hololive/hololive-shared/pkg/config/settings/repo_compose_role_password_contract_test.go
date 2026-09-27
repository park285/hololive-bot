package settings

import (
	"regexp"
	"strings"
	"testing"
)

// 역할별 DB 비밀번호는 전용 키 하나만 쓴다. 비어 있을 때 admin DB_PASSWORD나 다른 역할 비밀번호로 내려가는
// compose 치환 폴백을 다시 넣지 않도록 고정한다(stack audit 2026-09-26, T18에서 운영 env 키 확인).
func TestRepoComposeRolePasswordsHaveNoFallbackChain(t *testing.T) {
	compose := readRepoFile(t, "deploy/compose/docker-compose.prod.yml")

	fallback := regexp.MustCompile(`\$\{HOLOLIVE_(DB|MIGRATOR|SCRAPER)_PASSWORD:-\$\{`)
	if match := fallback.FindString(compose); match != "" {
		t.Fatalf("role password keeps a fallback chain: %q", match)
	}

	for _, required := range []string{
		"POSTGRES_PASSWORD: ${HOLOLIVE_DB_PASSWORD:?HOLOLIVE_DB_PASSWORD is required}",
		"POSTGRES_PASSWORD: ${HOLOLIVE_SCRAPER_PASSWORD:?HOLOLIVE_SCRAPER_PASSWORD is required}",
		"PGPASSWORD: ${HOLOLIVE_MIGRATOR_PASSWORD:-}",
	} {
		if !strings.Contains(compose, required) {
			t.Errorf("compose must render %q", required)
		}
	}
}
