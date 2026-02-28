package httpx

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ParseTrustedProxies는 신뢰 프록시 목록(IP/CIDR)을 netip.Prefix로 변환합니다.
func ParseTrustedProxies(values []string) ([]netip.Prefix, error) {
	if len(values) == 0 {
		return nil, nil
	}

	result := make([]netip.Prefix, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}

		if strings.Contains(value, "/") {
			prefix, err := netip.ParsePrefix(value)
			if err != nil {
				return nil, err
			}
			result = append(result, prefix.Masked())
			continue
		}

		addr, err := netip.ParseAddr(value)
		if err != nil {
			return nil, err
		}
		bits := 32
		if addr.Is6() {
			bits = 128
		}
		result = append(result, netip.PrefixFrom(addr, bits).Masked())
	}

	return result, nil
}

// RateLimitIdentity는 API key > trusted proxy 기반 client IP > remote addr 우선순위로 식별자를 생성합니다.
func RateLimitIdentity(r *http.Request, apiKey string, trustedProxies []netip.Prefix) string {
	if key := strings.TrimSpace(apiKey); key != "" {
		return "key:" + RateLimitKeyHash(key)
	}
	if r == nil {
		return "ip:unknown"
	}

	clientIP := selectClientIP(r, trustedProxies)
	if clientIP == "" {
		return "ip:unknown"
	}
	return "ip:" + clientIP
}

// RateLimitKeyHash는 rate-limit 키/로그 식별용 짧은 SHA-256 해시를 반환합니다.
func RateLimitKeyHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	encoded := hex.EncodeToString(sum[:])
	if len(encoded) <= 16 {
		return encoded
	}
	return encoded[:16]
}

func selectClientIP(r *http.Request, trustedProxies []netip.Prefix) string {
	remoteIP, ok := parseIPCandidate(r.RemoteAddr)
	if !ok {
		return ""
	}

	// X-Forwarded-For/X-Real-IP는 trusted proxy 경로에서만 사용합니다.
	if isTrustedProxy(remoteIP, trustedProxies) {
		if forwarded := firstForwardedFor(r.Header.Get("X-Forwarded-For")); forwarded != "" {
			return forwarded
		}
		if realIP, ok := parseIPCandidate(r.Header.Get("X-Real-IP")); ok {
			return realIP
		}
	}

	return remoteIP
}

func isTrustedProxy(remoteIP string, trustedProxies []netip.Prefix) bool {
	if len(trustedProxies) == 0 {
		return false
	}

	addr, err := netip.ParseAddr(remoteIP)
	if err != nil {
		return false
	}

	for _, prefix := range trustedProxies {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func firstForwardedFor(headerValue string) string {
	raw := strings.TrimSpace(headerValue)
	if raw == "" {
		return ""
	}

	parts := strings.SplitSeq(raw, ",")
	for part := range parts {
		if ip, ok := parseIPCandidate(part); ok {
			return ip
		}
	}
	return ""
}

func parseIPCandidate(value string) (string, bool) {
	raw := strings.TrimSpace(strings.Trim(value, `"`))
	if raw == "" {
		return "", false
	}

	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	} else {
		raw = strings.TrimPrefix(raw, "[")
		raw = strings.TrimSuffix(raw, "]")
	}

	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return "", false
	}
	return addr.String(), true
}
