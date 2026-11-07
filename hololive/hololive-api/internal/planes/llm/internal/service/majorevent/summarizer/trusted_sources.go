package summarizer

import (
	"net/url"
	"strings"

	"github.com/kapu/hololive-shared/pkg/constants"
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
	parsed, err := url.Parse(source)
	if err != nil || parsed.Hostname() == "" {
		return false
	}

	host := normalizeHost(parsed.Hostname())
	if host == "" {
		return false
	}

	if isTrustedDomainHost(host) {
		return true
	}

	if !isSocialHost(host) {
		return false
	}

	return isTrustedURLSocialAccount(parsed.EscapedPath())
}

func isTrustedURLSocialAccount(path string) bool {
	account := extractSocialAccount(path)
	if account == "" {
		return false
	}

	for _, trustedAccount := range constants.MajorEventConfig.TrustedSocialAccounts {
		if account == strings.ToLower(strings.TrimSpace(trustedAccount)) {
			return true
		}
	}

	return false
}

func isTrustedTextSocialSource(source string) bool {
	for _, account := range constants.MajorEventConfig.TrustedSocialAccounts {
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

func normalizeHost(host string) string {
	normalized := strings.ToLower(strings.TrimSpace(host))

	normalized = strings.TrimPrefix(normalized, "www.")

	return normalized
}

func isTrustedDomainHost(host string) bool {
	for _, domain := range constants.MajorEventConfig.TrustedSourceDomains {
		token := normalizeHost(domain)
		if token == "" {
			continue
		}

		if host == token || strings.HasSuffix(host, "."+token) {
			return true
		}
	}

	return false
}

func isSocialHost(host string) bool {
	return host == "x.com" || host == "twitter.com"
}

func extractSocialAccount(path string) string {
	trimmed, ok := strings.CutPrefix(path, "/")
	if !ok || trimmed == "" || strings.Contains(trimmed, "//") {
		return ""
	}

	// 브라우저가 제거하는 dot segment와 경계가 달라지는 인코딩·역슬래시는 정규화하지 않고 거절한다.
	for segment := range strings.SplitSeq(trimmed, "/") {
		decoded, err := url.PathUnescape(segment)
		if err != nil || decoded == "." || decoded == ".." ||
			strings.ContainsAny(decoded, "/\\\t\r\n\x00") || strings.TrimSpace(decoded) != decoded {
			return ""
		}
	}

	first, _, _ := strings.Cut(trimmed, "/")

	account, err := url.PathUnescape(first)
	if err != nil {
		return ""
	}

	return strings.ToLower(strings.TrimPrefix(account, "@"))
}
