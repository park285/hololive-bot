package summarizer

import (
	"slices"
	"strings"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/sourcepolicy"
)

var (
	trustedSourceDomains = []string{
		"hololive.hololivepro.com",
		"hololivepro.com",
		"cover-corp.com",
		"hololive.tv",
		"schedule.hololive.tv",
		"shop.hololivepro.com",
		"hololive-official-cardgame.com",
		"aniplustv.com",
		"aniplus.co.kr",
		"animate.co.jp",
		"lawson.co.jp",
	}
	trustedSocialAccounts = []string{
		"hololivetv",
		"hololive_en",
		"hololive_id",
		"holostarsen",
		"hololive_ocg_en",
		"aniplus_shop",
		"v_square_kr",
		"agf_korea",
	}
)

func isTrustedDiscoveredSource(source string) bool {
	normalized := strings.ToLower(strings.TrimSpace(source))
	if normalized == "" {
		return false
	}

	// URL로 시작한 입력은 파싱 실패도 거절하며 일반 출처 토큰으로 재해석하지 않는다.
	if strings.HasPrefix(normalized, "http:") || strings.HasPrefix(normalized, "https:") {
		return isTrustedURLSource(normalized)
	}

	return isTrustedTextSocialSource(normalized)
}

func isTrustedURLSource(source string) bool {
	parsed, host, err := sourcepolicy.ParseURL(source)
	if err != nil {
		return false
	}

	if isTrustedDomainHost(host) {
		return true
	}

	if !sourcepolicy.IsXHost(host) {
		return false
	}

	return isTrustedSocialAccount(sourcepolicy.XAccountFromPath(parsed.EscapedPath()))
}

func isTrustedSocialAccount(account string) bool {
	if account == "" {
		return false
	}

	for _, trustedAccount := range trustedSocialAccounts {
		if account == strings.ToLower(strings.TrimSpace(trustedAccount)) {
			return true
		}
	}

	return false
}

func isTrustedTextSocialSource(source string) bool {
	for _, account := range trustedSocialAccounts {
		token := strings.ToLower(strings.TrimSpace(account))
		if token == "" {
			continue
		}

		if isTrustedTextSocialToken(source, token) {
			return true
		}
	}

	return false
}

func isTrustedTextSocialToken(source, token string) bool {
	return source == "@"+token || source == "x.com/"+token || source == "twitter.com/"+token
}

func isTrustedDomainHost(host string) bool {
	return slices.ContainsFunc(trustedSourceDomains, func(domain string) bool {
		return sourcepolicy.HostMatchesDomain(host, domain)
	})
}
