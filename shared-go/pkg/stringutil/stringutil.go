// Package stringutil: 문자열 및 슬라이스 유틸리티
package stringutil

import (
	"slices"
	"strings"
)

// TruncateString: 주어진 문자열을 최대 길이(Rune 기준)로 자르고, 초과 시 "..."을 붙여 반환합니다.
func TruncateString(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

// ContainsString: 문자열 슬라이스에 특정 문자열이 포함되어 있는지 확인합니다.
func ContainsString(slice []string, item string) bool {
	return slices.Contains(slice, item)
}

// TrimSpace: 문자열 양쪽 끝의 공백을 제거합니다.
func TrimSpace(s string) string {
	return strings.TrimSpace(s)
}

// Normalize: 문자열을 소문자로 변환하고 양쪽 공백을 제거합니다.
func Normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// StripLeadingHeader: 텍스트 앞부분의 헤더 문자열을 제거합니다.
// 여러 개행 패턴을 시도하여 가장 적절한 방식으로 제거합니다.
func StripLeadingHeader(text, header string) string {
	if TrimSpace(text) == "" || TrimSpace(header) == "" {
		return text
	}
	candidates := []string{
		header + "\r\n\r\n",
		header + "\n\n",
		header + "\r\n",
		header + "\n",
		header,
	}
	for _, candidate := range candidates {
		if after, ok := strings.CutPrefix(text, candidate); ok {
			return after
		}
	}
	return text
}

// NormalizeKey: 검색 키 생성을 위해 특수문자, 공백 등을 제거하여 문자열을 정규화합니다.
// Unicode를 올바르게 처리하며, 다양한 특수문자(공백, 하이픈, 언더스코어 등)를 제거합니다.
func NormalizeKey(s string) string {
	s = Normalize(s)
	if s == "" {
		return ""
	}

	var builder strings.Builder
	for _, r := range s {
		switch r {
		case ' ', '-', '_', '.', '!', '☆', '・', '\u2018', '\u2019', '\'', 'ー', '—':
			continue
		default:
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

// Slugify: URL 등에 사용하기 적합하도록 문자열을 변환합니다.
// 공백은 하이픈(-)으로 변환하고, 특정 특수문자를 제거합니다.
func Slugify(s string) string {
	s = Normalize(s)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "'", "")
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, "!", "")
	return s
}
