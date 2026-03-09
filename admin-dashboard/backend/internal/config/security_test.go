package config

import (
	"log/slog"
	"os"
	"testing"
)

func TestParseSecurityMode_ValidModes(t *testing.T) {
	tests := []struct {
		input    string
		expected SecurityMode
	}{
		{"enforce", SecurityModeEnforce},
		{"ENFORCE", SecurityModeEnforce},
		{"Enforce", SecurityModeEnforce},
		{"monitor", SecurityModeMonitor},
		{"MONITOR", SecurityModeMonitor},
		{"off", SecurityModeOff},
		{"OFF", SecurityModeOff},
		{" enforce ", SecurityModeEnforce}, // 공백 처리
		{" monitor\n", SecurityModeMonitor},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseSecurityMode(tt.input)
			if got != tt.expected {
				t.Errorf("ParseSecurityMode(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestParseSecurityMode_DefaultsToEnforce(t *testing.T) {
	// 알 수 없는 값은 보안 원칙(fail-closed)에 따라 enforce
	tests := []string{
		"",
		"invalid",
		"enabled",
		"disabled",
		"true",
		"false",
		"123",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			got := ParseSecurityMode(input)
			if got != SecurityModeEnforce {
				t.Errorf("ParseSecurityMode(%q) = %q, want %q (default)", input, got, SecurityModeEnforce)
			}
		})
	}
}

func TestNormalizeOrigin(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"https://example.com", "https://example.com"},
		{"https://example.com/", "https://example.com"},
		{" https://example.com ", "https://example.com"},
		{" https://example.com/ ", "https://example.com"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeOrigin(tt.input)
			if got != tt.expected {
				t.Errorf("normalizeOrigin(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestIsLocalhostOrigin(t *testing.T) {
	localhostOrigins := []string{
		"http://localhost:5173",
		"http://localhost",
		"https://LOCALHOST:3000",
		"http://127.0.0.1:8080",
		"http://127.0.0.1",
		"http://[::1]:8080",
	}

	for _, origin := range localhostOrigins {
		if !isLocalhostOrigin(origin) {
			t.Errorf("isLocalhostOrigin(%q) = false, want true", origin)
		}
	}

	nonLocalhostOrigins := []string{
		"https://admin.capu.blog",
		"https://example.com",
		"http://192.168.1.1:8080",
	}

	for _, origin := range nonLocalhostOrigins {
		if isLocalhostOrigin(origin) {
			t.Errorf("isLocalhostOrigin(%q) = true, want false", origin)
		}
	}
}

func TestParseAllowedOrigins_FromEnvVar(t *testing.T) {
	// 환경변수 설정
	os.Setenv("ALLOWED_ORIGINS", "https://admin.capu.blog, https://example.com ")
	defer os.Unsetenv("ALLOWED_ORIGINS")

	origins, fallback := parseAllowedOrigins("development", false, nil)

	if fallback {
		t.Error("parseAllowedOrigins() returned fallback=true, want false")
	}

	expected := []string{"https://admin.capu.blog", "https://example.com"}
	if len(origins) != len(expected) {
		t.Fatalf("parseAllowedOrigins() returned %d origins, want %d", len(origins), len(expected))
	}

	for i, origin := range origins {
		if origin != expected[i] {
			t.Errorf("origins[%d] = %q, want %q", i, origin, expected[i])
		}
	}
}

func TestParseAllowedOrigins_FallbackWhenNotSet(t *testing.T) {
	os.Unsetenv("ALLOWED_ORIGINS")

	// development 환경에서는 localhost 포함
	origins, fallback := parseAllowedOrigins("development", false, nil)

	if !fallback {
		t.Error("parseAllowedOrigins() returned fallback=false, want true")
	}

	if len(origins) != 2 {
		t.Fatalf("parseAllowedOrigins() returned %d origins, want 2", len(origins))
	}
}

func TestParseAllowedOrigins_RejectsLocalhostInProd(t *testing.T) {
	os.Setenv("ALLOWED_ORIGINS", "https://admin.capu.blog,http://localhost:5173")
	defer os.Unsetenv("ALLOWED_ORIGINS")

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	origins, fallback := parseAllowedOrigins("production", false, logger)

	if fallback {
		t.Error("parseAllowedOrigins() returned fallback=true, want false")
	}

	// production에서 localhost는 필터링됨
	if len(origins) != 1 {
		t.Fatalf("parseAllowedOrigins() returned %d origins, want 1 (localhost filtered)", len(origins))
	}

	if origins[0] != "https://admin.capu.blog" {
		t.Errorf("origins[0] = %q, want %q", origins[0], "https://admin.capu.blog")
	}
}

func TestParseAllowedOrigins_AllowsLocalhostInProdWithFlag(t *testing.T) {
	os.Setenv("ALLOWED_ORIGINS", "https://admin.capu.blog,http://localhost:5173")
	defer os.Unsetenv("ALLOWED_ORIGINS")

	// ALLOW_LOCALHOST_IN_PROD=true
	origins, _ := parseAllowedOrigins("production", true, nil)

	if len(origins) != 2 {
		t.Fatalf("parseAllowedOrigins() with allowLocalhostInProd=true returned %d origins, want 2", len(origins))
	}
}

func TestSecurityConfig_AllowedOriginsMap(t *testing.T) {
	cfg := &SecurityConfig{
		AllowedOrigins: []string{"https://a.com", "https://b.com"},
	}

	m := cfg.AllowedOriginsMap()

	if len(m) != 2 {
		t.Fatalf("AllowedOriginsMap() returned map with %d entries, want 2", len(m))
	}

	if _, ok := m["https://a.com"]; !ok {
		t.Error("AllowedOriginsMap() missing https://a.com")
	}

	if _, ok := m["https://b.com"]; !ok {
		t.Error("AllowedOriginsMap() missing https://b.com")
	}
}

func TestLoadSecurityConfig_DefaultValues(t *testing.T) {
	// 모든 환경변수 초기화
	os.Unsetenv("ALLOWED_ORIGINS")
	os.Unsetenv("ALLOW_LOCALHOST_IN_PROD")
	os.Unsetenv("CSRF_MODE")
	os.Unsetenv("WS_ORIGIN_MODE")
	os.Unsetenv("STREAM_LIMIT_MODE")
	os.Unsetenv("GLOBAL_STREAM_LIMIT")
	os.Unsetenv("PER_SESSION_STREAM_LIMIT")

	cfg := LoadSecurityConfig("development", nil)

	// 기본값 검증
	if cfg.CSRFMode != SecurityModeEnforce {
		t.Errorf("CSRFMode = %q, want %q", cfg.CSRFMode, SecurityModeEnforce)
	}

	if cfg.WSOriginMode != SecurityModeEnforce {
		t.Errorf("WSOriginMode = %q, want %q", cfg.WSOriginMode, SecurityModeEnforce)
	}

	if cfg.GlobalStreamLimit != 10 {
		t.Errorf("GlobalStreamLimit = %d, want 10", cfg.GlobalStreamLimit)
	}

	if cfg.PerSessionStreamLimit != 2 {
		t.Errorf("PerSessionStreamLimit = %d, want 2", cfg.PerSessionStreamLimit)
	}

	if !cfg.UsingOriginFallback() {
		t.Error("UsingOriginFallback() = false, want true (env not set)")
	}
}

func TestLoadSecurityConfig_CustomValues(t *testing.T) {
	os.Setenv("CSRF_MODE", "monitor")
	os.Setenv("WS_ORIGIN_MODE", "off")
	os.Setenv("STREAM_LIMIT_MODE", "monitor")
	os.Setenv("GLOBAL_STREAM_LIMIT", "20")
	os.Setenv("PER_SESSION_STREAM_LIMIT", "5")
	os.Setenv("ALLOWED_ORIGINS", "https://test.com")
	defer func() {
		os.Unsetenv("CSRF_MODE")
		os.Unsetenv("WS_ORIGIN_MODE")
		os.Unsetenv("STREAM_LIMIT_MODE")
		os.Unsetenv("GLOBAL_STREAM_LIMIT")
		os.Unsetenv("PER_SESSION_STREAM_LIMIT")
		os.Unsetenv("ALLOWED_ORIGINS")
	}()

	cfg := LoadSecurityConfig("development", nil)

	if cfg.CSRFMode != SecurityModeMonitor {
		t.Errorf("CSRFMode = %q, want %q", cfg.CSRFMode, SecurityModeMonitor)
	}

	if cfg.WSOriginMode != SecurityModeOff {
		t.Errorf("WSOriginMode = %q, want %q", cfg.WSOriginMode, SecurityModeOff)
	}

	if cfg.StreamLimitMode != SecurityModeMonitor {
		t.Errorf("StreamLimitMode = %q, want %q", cfg.StreamLimitMode, SecurityModeMonitor)
	}

	if cfg.GlobalStreamLimit != 20 {
		t.Errorf("GlobalStreamLimit = %d, want 20", cfg.GlobalStreamLimit)
	}

	if cfg.PerSessionStreamLimit != 5 {
		t.Errorf("PerSessionStreamLimit = %d, want 5", cfg.PerSessionStreamLimit)
	}

	if cfg.UsingOriginFallback() {
		t.Error("UsingOriginFallback() = true, want false")
	}
}
