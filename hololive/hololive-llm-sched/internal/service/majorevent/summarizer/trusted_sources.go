package summarizer

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/kapu/hololive-shared/pkg/constants"
)

func isTrustedDiscoveredSource(source string) bool {
	normalized := strings.ToLower(strings.TrimSpace(source))
	if normalized == "" {
		return false
	}

	// bare domain의 auto-prepend 우회를 막기 위해 URL 형식만 검증한다.
	if strings.HasPrefix(normalized, "http://") || strings.HasPrefix(normalized, "https://") {
		if trusted, handled := isTrustedURLSource(normalized); handled {
			return trusted
		}
	}

	return isTrustedTextSource(normalized)
}

func isTrustedURLSource(source string) (trusted bool, handled bool) {
	parsed, err := parseSourceURL(source)
	if err != nil || parsed == nil || parsed.Hostname() == "" {
		return false, false
	}

	host := normalizeHost(parsed.Hostname())
	if host == "" {
		return false, true
	}

	if isTrustedDomainHost(host) {
		return true, true
	}
	if !isSocialHost(host) {
		return false, true
	}

	return isTrustedURLSocialAccount(parsed.Path), true
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

func isTrustedTextSource(source string) bool {
	return isTrustedTextDomainSource(source) || isTrustedTextSocialSource(source)
}

func isTrustedTextDomainSource(source string) bool {
	for _, domain := range constants.MajorEventConfig.TrustedSourceDomains {
		token := normalizeHost(domain)
		if token == "" {
			continue
		}
		if source == "https://"+token || source == "http://"+token {
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

func isTrustedTextSocialToken(source string, token string) bool {
	return source == "@"+token ||
		source == "x.com/"+token ||
		source == "twitter.com/"+token ||
		source == "https://x.com/"+token ||
		source == "https://twitter.com/"+token ||
		source == "http://x.com/"+token ||
		source == "http://twitter.com/"+token
}

func parseSourceURL(raw string) (*url.URL, error) {
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("parse source url: %w", err)
		}
		return parsed, nil
	}
	parsed, err := url.Parse("https://" + raw)
	if err != nil {
		return nil, fmt.Errorf("parse source url with default scheme: %w", err)
	}
	return parsed, nil
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
	trimmed := strings.Trim(strings.TrimSpace(path), "/")
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 {
		return ""
	}
	account := strings.TrimPrefix(parts[0], "@")
	return strings.ToLower(strings.TrimSpace(account))
}
