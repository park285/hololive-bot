package middleware

import (
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestShouldSkipPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string
		exactSkip  map[string]bool
		prefixSkip []string
		suffixSkip []string
		want       bool
	}{
		{
			name:      "정확히 일치 → true",
			path:      "/health",
			exactSkip: map[string]bool{"/health": true},
			want:      true,
		},
		{
			name:       "prefix 일치 → true",
			path:       "/api/holo/ws/connect",
			prefixSkip: []string{"/api/holo/ws"},
			want:        true,
		},
		{
			name:       "suffix 일치 → true",
			path:       "/api/stream",
			suffixSkip: []string{"/stream"},
			want:        true,
		},
		{
			name:       "어떤 패턴에도 불일치 → false",
			path:       "/api/data",
			exactSkip:  map[string]bool{"/health": true},
			prefixSkip: []string{"/metrics"},
			suffixSkip: []string{"/stream"},
			want:        false,
		},
		{
			name:       "빈 맵/슬라이스 → false",
			path:       "/anything",
			exactSkip:  map[string]bool{},
			prefixSkip: []string{},
			suffixSkip: []string{},
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := shouldSkipPath(tt.path, tt.exactSkip, tt.prefixSkip, tt.suffixSkip)
			if got != tt.want {
				t.Fatalf("shouldSkipPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestTruncateUA(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ua   string
		want string
	}{
		{
			name: "짧은 UA → 그대로 반환",
			ua:   "Mozilla/5.0",
			want: "Mozilla/5.0",
		},
		{
			name: "정확히 80자 → 그대로 반환",
			ua:   strings.Repeat("a", 80),
			want: strings.Repeat("a", 80),
		},
		{
			name: "81자 초과 → 80자 잘라 '...' 추가",
			ua:   strings.Repeat("b", 100),
			want: strings.Repeat("b", 80) + "...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := truncateUA(tt.ua)
			if got != tt.want {
				t.Fatalf("truncateUA(%q) = %q, want %q", tt.ua, got, tt.want)
			}
		})
	}
}

func TestLogDebugf_NoPanic(t *testing.T) {
	t.Parallel()

	// 패닉 없이 실행되는지만 검증 (스모크 테스트)
	logger := slog.Default()
	ctx := context.Background()

	// 패닉 발생 시 테스트가 실패하므로 별도 assertion 불필요
	LogDebugf(ctx, logger, "테스트 메시지", slog.String("key", "value"))
}
