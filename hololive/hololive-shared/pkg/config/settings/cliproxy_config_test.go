package settings

import (
	"strings"
	"testing"
)

func TestLoadCliproxyConfigRequiresExplicitBaseURL(t *testing.T) {
	t.Setenv("CLIPROXY_ENABLED", "true")
	t.Setenv("CLIPROXY_API_KEY", "test-key")
	t.Setenv("CLIPROXY_BASE_URL", "")

	cfg := loadCliproxyConfig()
	if cfg.BaseURL != "" {
		t.Fatalf("BaseURL = %q, want empty without explicit CLIPROXY_BASE_URL", cfg.BaseURL)
	}
}

func TestLoadLLMProviderConfigDefaultsToCliproxy(t *testing.T) {
	t.Setenv("LLM_PROVIDER", "")
	t.Setenv("GEMINI_BASE_URL", "")
	t.Setenv("GEMINI_MODEL", "")
	t.Setenv("GEMINI_THINKING_LEVEL", "")

	cfg := buildLLMSchedulerConfig()
	if cfg.LLMProvider != LLMProviderCliproxy {
		t.Fatalf("LLMProvider = %q, want %q", cfg.LLMProvider, LLMProviderCliproxy)
	}

	if cfg.Gemini.BaseURL != "https://generativelanguage.googleapis.com" {
		t.Fatalf("Gemini.BaseURL = %q", cfg.Gemini.BaseURL)
	}

	if cfg.Gemini.Model != "gemini-3.7-flash" || cfg.Gemini.ThinkingLevel != "high" {
		t.Fatalf("Gemini defaults = model %q thinking %q", cfg.Gemini.Model, cfg.Gemini.ThinkingLevel)
	}
}

func TestValidateLLMProviderGemini(t *testing.T) {
	cfg := LLMProviderConfig{
		Name: LLMProviderGemini,
		Gemini: GeminiConfig{
			Enabled:       true,
			BaseURL:       "https://generativelanguage.googleapis.com",
			APIKey:        "test-key",
			Model:         "gemini-3.7-flash",
			ThinkingLevel: "high",
		},
	}

	if err := validateLLMProvider(cfg); err != nil {
		t.Fatalf("validateLLMProvider() error = %v", err)
	}
}

func TestLoadLLMProviderConfigGeminiExplicit(t *testing.T) {
	t.Setenv("LLM_PROVIDER", " GEMINI ")
	t.Setenv("GEMINI_ENABLED", "true")
	t.Setenv("GEMINI_BASE_URL", "https://gemini.example")
	t.Setenv("GEMINI_API_KEY", "test-gemini-key")
	t.Setenv("GEMINI_MODEL", "gemini-3.7-flash")
	t.Setenv("GEMINI_THINKING_LEVEL", "high")

	cfg := buildLLMSchedulerConfig()
	if cfg.LLMProvider != LLMProviderGemini {
		t.Fatalf("LLMProvider = %q, want %q", cfg.LLMProvider, LLMProviderGemini)
	}

	if err := validateLLMProvider(cfg.SelectedLLMProvider()); err != nil {
		t.Fatalf("validateLLMProvider() error = %v", err)
	}
}

func TestValidateLLMProviderRejectsInvalidSelection(t *testing.T) {
	err := validateLLMProvider(LLMProviderConfig{Name: "unknown"})
	if err == nil || !strings.Contains(err.Error(), "LLM_PROVIDER") {
		t.Fatalf("validateLLMProvider() error = %v, want LLM_PROVIDER error", err)
	}
}

func TestValidateLLMProviderRejectsIncompleteGemini(t *testing.T) {
	err := validateLLMProvider(LLMProviderConfig{
		Name: LLMProviderGemini,
		Gemini: GeminiConfig{
			Enabled:       true,
			Model:         "gemini-3.7-flash",
			ThinkingLevel: "high",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("validateLLMProvider() error = %v, want incomplete error", err)
	}
}

func TestValidateLLMProviderRejectsUnsupportedGeminiThinkingLevel(t *testing.T) {
	err := validateLLMProvider(LLMProviderConfig{
		Name: LLMProviderGemini,
		Gemini: GeminiConfig{
			Enabled:       true,
			BaseURL:       "https://generativelanguage.googleapis.com",
			APIKey:        "test-key",
			Model:         "gemini-3.7-flash",
			ThinkingLevel: "xhigh",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "GEMINI_THINKING_LEVEL") {
		t.Fatalf("validateLLMProvider() error = %v, want thinking-level error", err)
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
