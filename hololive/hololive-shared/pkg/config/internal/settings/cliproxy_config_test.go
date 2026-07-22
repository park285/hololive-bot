package settings

import "testing"

func TestLoadCliproxyConfigRequiresExplicitBaseURL(t *testing.T) {
	t.Setenv("CLIPROXY_ENABLED", "true")
	t.Setenv("CLIPROXY_API_KEY", "test-key")
	t.Setenv("CLIPROXY_BASE_URL", "")

	cfg := loadCliproxyConfig()
	if cfg.BaseURL != "" {
		t.Fatalf("BaseURL = %q, want empty without explicit CLIPROXY_BASE_URL", cfg.BaseURL)
	}
}

func TestLoadCliproxyConfigUsesExplicitBaseURL(t *testing.T) {
	const endpoint = "https://cliproxy.example/v1"
	t.Setenv("CLIPROXY_BASE_URL", endpoint)

	cfg := loadCliproxyConfig()
	if cfg.BaseURL != endpoint {
		t.Fatalf("BaseURL = %q, want %q", cfg.BaseURL, endpoint)
	}
}
