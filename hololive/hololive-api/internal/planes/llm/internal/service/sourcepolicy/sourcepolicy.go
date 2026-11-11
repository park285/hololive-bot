// Package sourcepolicy는 major event와 member news가 함께 쓰는 출처 URL·X 계정 경로 해석 규칙을 소유한다.
// 신뢰할 도메인과 계정 목록은 각 서비스가 정한다.
package sourcepolicy

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// x.com 첫 경로가 이 값이면 계정이 아니라 서비스 화면이다.
var reservedXPaths = []string{"home", "explore", "i", "search"}

// ParseURL은 http·https 출처 URL을 해석하고 비교용으로 정규화한 host를 함께 돌려준다.
func ParseURL(rawURL string) (*url.URL, string, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, "", errors.New("source url is empty")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, "", fmt.Errorf("parse source url: %w", err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, "", fmt.Errorf("unsupported source url scheme: %s", parsed.Scheme)
	}

	host := normalizeHost(parsed.Hostname())
	if host == "" {
		return nil, "", errors.New("source host is empty")
	}

	return parsed, host, nil
}

// HostMatchesDomain은 정규화된 host가 domain 자신이거나 그 하위 도메인이면 참이다. 빈 domain은 어떤 host와도 맞지 않는다.
func HostMatchesDomain(host, domain string) bool {
	token := normalizeHost(domain)

	return token != "" && (host == token || strings.HasSuffix(host, "."+token))
}

func IsXHost(host string) bool {
	return host == "x.com" || host == "twitter.com"
}

// PathSegments는 브라우저가 제거하는 dot segment나 경계가 달라지는 인코딩·역슬래시가 있으면
// 정규화하지 않고 거절한다. 앞 segment만 보고 판정하면 뒤의 ..가 다른 계정으로 이동시킬 수 있다.
// 끝의 / 하나는 segment로 세지 않는다.
func PathSegments(escapedPath string) ([]string, bool) {
	trimmed, ok := strings.CutPrefix(escapedPath, "/")
	if !ok || trimmed == "" || strings.Contains(trimmed, "//") {
		return nil, false
	}

	segments := strings.Split(strings.TrimSuffix(trimmed, "/"), "/")
	for i, segment := range segments {
		decoded, err := url.PathUnescape(segment)
		if err != nil || decoded == "" || decoded == "." || decoded == ".." ||
			strings.ContainsAny(decoded, "/\\\t\r\n\x00") || strings.TrimSpace(decoded) != decoded {
			return nil, false
		}

		segments[i] = decoded
	}

	return segments, true
}

// XAccountFromPath는 x.com·twitter.com 경로의 첫 segment를 @를 뗀 소문자 계정 이름으로 돌려준다.
// 경로가 모호하거나 서비스 화면이면 빈 문자열이다.
func XAccountFromPath(escapedPath string) string {
	segments, ok := PathSegments(escapedPath)
	if !ok {
		return ""
	}

	account := strings.TrimPrefix(strings.ToLower(segments[0]), "@")
	if account == "" || slices.Contains(reservedXPaths, account) {
		return ""
	}

	return account
}

func normalizeHost(host string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(host)), "www.")
}
