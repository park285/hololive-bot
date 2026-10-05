// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package runtime

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/llm"
	"github.com/kapu/hololive-shared/pkg/config/settings"
)

const (
	testProviderKey     = "key"
	testProviderBaseURL = "https://example.com/v1"
)

func newUnsanitizedTestLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, nil))
}

func cliproxyProvider(config settings.CliproxyConfig) settings.LLMProviderConfig {
	return settings.LLMProviderConfig{
		Name:     settings.LLMProviderCliproxy,
		Cliproxy: config,
	}
}

func TestProvideMajorEventLLMClient_Disabled(t *testing.T) {
	var buf bytes.Buffer

	logger := newUnsanitizedTestLogger(&buf)

	client := ProvideMajorEventLLMClient(cliproxyProvider(settings.CliproxyConfig{Enabled: false, APIKey: testProviderKey}), nil, logger)
	if client != nil {
		t.Fatal("expected nil when disabled")
	}

	if !strings.Contains(buf.String(), "disabled") {
		t.Error("expected info log about disabled")
	}
}

func TestProvideMajorEventLLMClient_NoAPIKey(t *testing.T) {
	var buf bytes.Buffer

	logger := newUnsanitizedTestLogger(&buf)

	client := ProvideMajorEventLLMClient(cliproxyProvider(settings.CliproxyConfig{Enabled: true, APIKey: ""}), nil, logger)
	if client != nil {
		t.Fatal("expected nil when API key missing")
	}
}

func TestProvideMajorEventLLMClient_EmptyBaseURL(t *testing.T) {
	var buf bytes.Buffer

	logger := newUnsanitizedTestLogger(&buf)

	client := ProvideMajorEventLLMClient(cliproxyProvider(settings.CliproxyConfig{
		Enabled: true,
		APIKey:  testProviderKey,
		BaseURL: "",
		Model:   "gpt-test",
	}), nil, logger)
	if client != nil {
		t.Fatal("expected nil when baseURL empty")
	}

	if !strings.Contains(buf.String(), "incomplete") {
		t.Error("expected error log about incomplete config")
	}
}

func TestProvideMajorEventLLMClient_EmptyModel(t *testing.T) {
	var buf bytes.Buffer

	logger := newUnsanitizedTestLogger(&buf)

	client := ProvideMajorEventLLMClient(cliproxyProvider(settings.CliproxyConfig{
		Enabled: true,
		APIKey:  testProviderKey,
		BaseURL: testProviderBaseURL,
		Model:   "",
	}), nil, logger)
	if client != nil {
		t.Fatal("expected nil when model empty")
	}
}

func TestProvideMajorEventLLMClient_BlankAPIKeyReturnsNil(t *testing.T) {
	var buf bytes.Buffer

	logger := newUnsanitizedTestLogger(&buf)

	client := ProvideMajorEventLLMClient(cliproxyProvider(settings.CliproxyConfig{
		Enabled: true,
		APIKey:  "   ",
		BaseURL: testProviderBaseURL,
		Model:   "gpt-test",
	}), nil, logger)
	if client != nil {
		t.Fatal("expected nil when generator construction fails")
	}

	if !strings.Contains(buf.String(), "disabled") {
		t.Error("expected info log about disabled provider")
	}
}

func TestProvideMajorEventLLMClient_Success(t *testing.T) {
	var buf bytes.Buffer

	logger := newUnsanitizedTestLogger(&buf)

	client := ProvideMajorEventLLMClient(cliproxyProvider(settings.CliproxyConfig{
		Enabled: true,
		APIKey:  testProviderKey,
		BaseURL: testProviderBaseURL,
		Model:   "gpt-test",
	}), nil, logger)
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	if !strings.Contains(buf.String(), "gpt-test") {
		t.Error("expected log with model name")
	}
}

func TestProvideMajorEventLLMClient_GeminiNativeProvider(t *testing.T) {
	const (
		sensitiveKey           = "gemini-provider-key"
		testGeminiThinkingHigh = "high"
	)

	var buf bytes.Buffer

	logger := newUnsanitizedTestLogger(&buf)

	client := ProvideMajorEventLLMClient(settings.LLMProviderConfig{
		Name: settings.LLMProviderGemini,
		Gemini: settings.GeminiConfig{
			Enabled:       true,
			APIKey:        sensitiveKey,
			BaseURL:       "https://generativelanguage.googleapis.com",
			Model:         "gemini-3.7-flash",
			ThinkingLevel: testGeminiThinkingHigh,
		},
	}, nil, logger)
	if client == nil {
		t.Fatal("expected non-nil Gemini client")
	}

	if _, ok := client.(*llm.GeminiClient); !ok {
		t.Fatalf("client type = %T, want *llm.GeminiClient", client)
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "provider=gemini") || !strings.Contains(logOutput, "reasoning_level=high") {
		t.Fatalf("provider log = %s", logOutput)
	}

	if strings.Contains(logOutput, sensitiveKey) {
		t.Fatal("provider log leaked Gemini API key")
	}
}

func TestProvideMemberNewsLLMClient_Disabled(t *testing.T) {
	var buf bytes.Buffer

	logger := newUnsanitizedTestLogger(&buf)

	client, err := ProvideMemberNewsLLMClient(cliproxyProvider(settings.CliproxyConfig{Enabled: false}), &settings.LLMConfig{}, nil, logger)
	if client != nil || !isLLMFeatureDisabled(err) {
		t.Fatalf("got (%v, %v), want nil client with disabled sentinel", client, err)
	}

	if !strings.Contains(buf.String(), "disabled") {
		t.Error("expected info log about disabled")
	}
}

func TestProvideMemberNewsLLMClient_NoAPIKey(t *testing.T) {
	var buf bytes.Buffer

	logger := newUnsanitizedTestLogger(&buf)

	client, err := ProvideMemberNewsLLMClient(cliproxyProvider(settings.CliproxyConfig{Enabled: true, APIKey: ""}), &settings.LLMConfig{}, nil, logger)
	if client != nil || !isLLMFeatureDisabled(err) {
		t.Fatalf("got (%v, %v), want nil client with disabled sentinel", client, err)
	}

	if !strings.Contains(buf.String(), "disabled") {
		t.Error("expected info log about disabled")
	}
}

func TestProvideMemberNewsLLMClient_EmptyBaseURL(t *testing.T) {
	var buf bytes.Buffer

	logger := newUnsanitizedTestLogger(&buf)

	client, err := ProvideMemberNewsLLMClient(
		cliproxyProvider(settings.CliproxyConfig{
			Enabled: true,
			APIKey:  testProviderKey,
			BaseURL: "",
		}),
		&settings.LLMConfig{
			MemberNewsModel: "test-model",
		},
		nil, logger,
	)
	// provider가 켜져 있는데 설정이 불완전하면 기동 실패 대상 오류다(stack audit B5).
	if client != nil || err == nil || isLLMFeatureDisabled(err) {
		t.Fatalf("got (%v, %v), want initialization error", client, err)
	}

	if !strings.Contains(err.Error(), "incomplete") {
		t.Errorf("error = %v, want incomplete config", err)
	}
}

func TestProvideMemberNewsLLMClient_ModelFallback(t *testing.T) {
	var buf bytes.Buffer

	logger := newUnsanitizedTestLogger(&buf)

	client, err := ProvideMemberNewsLLMClient(
		cliproxyProvider(settings.CliproxyConfig{
			Enabled: true,
			APIKey:  testProviderKey,
			BaseURL: testProviderBaseURL,
			Model:   "default-model",
		}),
		&settings.LLMConfig{
			MemberNewsModel: "", // 빈값 → Cliproxy.Model fallback
		},
		nil, logger,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client == nil {
		t.Fatal("expected non-nil client")
	}

	if !strings.Contains(buf.String(), "default-model") {
		t.Error("expected log with fallback model name")
	}
}

func TestProvideMemberNewsLLMClient_LogsConfiguredModel(t *testing.T) {
	var buf bytes.Buffer

	logger := newUnsanitizedTestLogger(&buf)

	client, err := ProvideMemberNewsLLMClient(
		cliproxyProvider(settings.CliproxyConfig{
			Enabled: true,
			APIKey:  testProviderKey,
			BaseURL: testProviderBaseURL,
			Model:   "default-model",
		}),
		&settings.LLMConfig{
			MemberNewsModel: "new-model",
		},
		nil, logger,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client == nil {
		t.Fatal("expected non-nil client")
	}

	if !strings.Contains(buf.String(), "new-model") {
		t.Error("expected log with model name")
	}
}

func TestProvideMemberNewsLLMClient_TemperatureZero_LogShowsNotApplied(t *testing.T) {
	var buf bytes.Buffer

	logger := newUnsanitizedTestLogger(&buf)

	client, err := ProvideMemberNewsLLMClient(
		cliproxyProvider(settings.CliproxyConfig{
			Enabled: true,
			APIKey:  testProviderKey,
			BaseURL: testProviderBaseURL,
			Model:   "default-model",
		}),
		&settings.LLMConfig{
			MemberNewsModel:       "test-model",
			MemberNewsTemperature: 0,
		},
		nil, logger,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client == nil {
		t.Fatal("expected non-nil client")
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "temperature_applied=false") {
		t.Error("expected temperature_applied=false when temperature is 0")
	}
}

func TestAppendSupportedTemperature(t *testing.T) {
	tests := []struct {
		name        string
		model       string
		temperature float64
		wantApplied bool
	}{
		{name: "OpenAI model", model: "gpt-5.4", temperature: 0.1, wantApplied: true},
		{name: "Gemini API model", model: "gemini-3.7-flash", temperature: 0.1},
		{name: "Gemini Antigravity tier", model: "gemini-3.7-flash-high", temperature: 0.1},
		{name: "disabled temperature", model: "gpt-5.4", temperature: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts, applied := appendSupportedTemperature(nil, test.model, test.temperature)
			if applied != test.wantApplied {
				t.Fatalf("appendSupportedTemperature() applied = %t, want %t", applied, test.wantApplied)
			}

			var config llm.Options

			for _, opt := range opts {
				opt(&config)
			}

			if test.wantApplied && (config.Temperature == nil || *config.Temperature != test.temperature) {
				t.Fatalf("temperature = %v, want %v", config.Temperature, test.temperature)
			}

			if !test.wantApplied && config.Temperature != nil {
				t.Fatalf("temperature = %v, want nil", *config.Temperature)
			}
		})
	}
}

func TestProviderLogs_NoRawURLInErrorPath(t *testing.T) {
	sensitiveURL := "https://secret-proxy.internal.example.com/v1"
	sensitiveKey := "test-cliproxy-key-redacted"

	t.Run("MajorEvent error path", func(t *testing.T) {
		var buf bytes.Buffer

		logger := newUnsanitizedTestLogger(&buf)

		ProvideMajorEventLLMClient(cliproxyProvider(settings.CliproxyConfig{
			Enabled: true,
			APIKey:  sensitiveKey,
			BaseURL: sensitiveURL,
			Model:   "", // 빈값 → error 경로
		}), nil, logger)

		logOutput := buf.String()
		if strings.Contains(logOutput, sensitiveURL) {
			t.Error("error log must not contain raw baseURL")
		}

		if strings.Contains(logOutput, sensitiveKey) {
			t.Error("error log must not contain API key")
		}
	})

	t.Run("MemberNews error path", func(t *testing.T) {
		var buf bytes.Buffer

		logger := newUnsanitizedTestLogger(&buf)

		_, err := ProvideMemberNewsLLMClient(
			cliproxyProvider(settings.CliproxyConfig{
				Enabled: true,
				APIKey:  sensitiveKey,
				BaseURL: sensitiveURL,
				Model:   "",
			}),
			&settings.LLMConfig{
				MemberNewsModel: "", // 빈값 + Cliproxy.Model 빈값 → error 경로
			},
			nil, logger,
		)
		if err == nil {
			t.Fatal("expected initialization error")
		}

		// 초기화 오류는 기동 실패 로그로 그대로 나가므로 오류 문구에도 원문 URL과 key가 없어야 한다.
		if strings.Contains(err.Error(), sensitiveURL) || strings.Contains(err.Error(), sensitiveKey) {
			t.Errorf("initialization error leaks raw baseURL or API key: %v", err)
		}

		logOutput := buf.String()
		if strings.Contains(logOutput, sensitiveURL) {
			t.Error("error log must not contain raw baseURL")
		}

		if strings.Contains(logOutput, sensitiveKey) {
			t.Error("error log must not contain API key")
		}
	})
}

func TestProvideMemberNewsLLMClient_NewEnvEndToEnd(t *testing.T) {
	t.Setenv("API_SECRET_KEY", "test-api-key")
	t.Setenv("HOLOLIVE_H3_CERT_FILE", "/run/hololive-bot/certs/hololive-h3.crt")
	t.Setenv("HOLOLIVE_H3_KEY_FILE", "/run/hololive-bot/certs/hololive-h3.key")

	t.Setenv("CLIPROXY_ENABLED", "true")
	t.Setenv("CLIPROXY_API_KEY", "test-api-key")
	t.Setenv("CLIPROXY_BASE_URL", testProviderBaseURL)
	t.Setenv("CLIPROXY_MODEL", "default-model")

	t.Setenv("MEMBER_NEWS_LLM_MODEL", "new-model")

	appConfig, err := apiconfig.LoadLLMSchedulerRuntime()
	if err != nil {
		t.Fatalf("apiconfig.LoadLLMSchedulerRuntime() error = %v", err)
	}

	var buf bytes.Buffer

	logger := newUnsanitizedTestLogger(&buf)

	client, err := ProvideMemberNewsLLMClient(cliproxyProvider(appConfig.Cliproxy), &appConfig.LLM, nil, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client == nil {
		t.Fatal("expected non-nil client")
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "new-model") {
		t.Error("expected log with new model name")
	}
}

func TestProvideMemberNewsReviewerClient_ConsensusDisabled(t *testing.T) {
	var buf bytes.Buffer

	logger := newUnsanitizedTestLogger(&buf)

	client, err := ProvideMemberNewsReviewerClient(
		cliproxyProvider(settings.CliproxyConfig{Enabled: true, APIKey: testProviderKey, BaseURL: testProviderBaseURL, Model: "m"}),
		&settings.LLMConfig{MemberNews: settings.ConsensusLLMConfig{Enabled: false}},
		nil, logger,
	)
	if client != nil || !isLLMFeatureDisabled(err) {
		t.Fatalf("got (%v, %v), want nil client with disabled sentinel", client, err)
	}
}

func TestProvideMemberNewsReviewerClient_Enabled(t *testing.T) {
	var buf bytes.Buffer

	logger := newUnsanitizedTestLogger(&buf)

	client, err := ProvideMemberNewsReviewerClient(
		cliproxyProvider(settings.CliproxyConfig{Enabled: true, APIKey: testProviderKey, BaseURL: testProviderBaseURL, Model: "default"}),
		&settings.LLMConfig{MemberNews: settings.ConsensusLLMConfig{Enabled: true, ReviewerModel: "gpt-4.1-mini"}},
		nil, logger,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client == nil {
		t.Fatal("expected non-nil reviewer client")
	}

	if !strings.Contains(buf.String(), "gpt-4.1-mini") {
		t.Error("expected log with reviewer model name")
	}
}

func TestProvideMemberNewsReviewerClient_ModelFallback(t *testing.T) {
	var buf bytes.Buffer

	logger := newUnsanitizedTestLogger(&buf)

	client, err := ProvideMemberNewsReviewerClient(
		cliproxyProvider(settings.CliproxyConfig{Enabled: true, APIKey: testProviderKey, BaseURL: testProviderBaseURL, Model: "cliproxy-default"}),
		&settings.LLMConfig{MemberNewsModel: "news-model", MemberNews: settings.ConsensusLLMConfig{Enabled: true, ReviewerModel: ""}},
		nil, logger,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client == nil {
		t.Fatal("expected non-nil reviewer client with model fallback")
	}

	if !strings.Contains(buf.String(), "news-model") {
		t.Error("expected reviewer to fall back to MemberNewsModel")
	}
}

func TestProvideMemberNewsAdjudicatorClient_ConsensusDisabled(t *testing.T) {
	var buf bytes.Buffer

	logger := newUnsanitizedTestLogger(&buf)

	client, err := ProvideMemberNewsAdjudicatorClient(
		cliproxyProvider(settings.CliproxyConfig{Enabled: true, APIKey: testProviderKey, BaseURL: testProviderBaseURL, Model: "m"}),
		&settings.LLMConfig{MemberNews: settings.ConsensusLLMConfig{Enabled: false}},
		nil, logger,
	)
	if client != nil || !isLLMFeatureDisabled(err) {
		t.Fatalf("got (%v, %v), want nil client with disabled sentinel", client, err)
	}
}

func TestProvideMemberNewsAdjudicatorClient_Enabled(t *testing.T) {
	var buf bytes.Buffer

	logger := newUnsanitizedTestLogger(&buf)

	client, err := ProvideMemberNewsAdjudicatorClient(
		cliproxyProvider(settings.CliproxyConfig{Enabled: true, APIKey: testProviderKey, BaseURL: testProviderBaseURL, Model: "default"}),
		&settings.LLMConfig{MemberNews: settings.ConsensusLLMConfig{Enabled: true, AdjudicatorModel: "gpt-4.1"}},
		nil, logger,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client == nil {
		t.Fatal("expected non-nil adjudicator client")
	}

	if !strings.Contains(buf.String(), "gpt-4.1") {
		t.Error("expected log with adjudicator model name")
	}
}

func TestProvideMemberNewsAdjudicatorClient_ModelFallbackChain(t *testing.T) {
	t.Run("falls back to MemberNewsModel", func(t *testing.T) {
		var buf bytes.Buffer

		logger := newUnsanitizedTestLogger(&buf)

		client, err := ProvideMemberNewsAdjudicatorClient(
			cliproxyProvider(settings.CliproxyConfig{Enabled: true, APIKey: testProviderKey, BaseURL: testProviderBaseURL, Model: "cliproxy-default"}),
			&settings.LLMConfig{MemberNewsModel: "news-model", MemberNews: settings.ConsensusLLMConfig{Enabled: true, AdjudicatorModel: ""}},
			nil, logger,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if client == nil {
			t.Fatal("expected non-nil adjudicator client with MemberNewsModel fallback")
		}

		if !strings.Contains(buf.String(), "news-model") {
			t.Error("expected adjudicator to fall back to MemberNewsModel")
		}
	})

	t.Run("falls back to Cliproxy.Model", func(t *testing.T) {
		var buf bytes.Buffer

		logger := newUnsanitizedTestLogger(&buf)

		client, err := ProvideMemberNewsAdjudicatorClient(
			cliproxyProvider(settings.CliproxyConfig{Enabled: true, APIKey: testProviderKey, BaseURL: testProviderBaseURL, Model: "cliproxy-default"}),
			&settings.LLMConfig{MemberNews: settings.ConsensusLLMConfig{Enabled: true, AdjudicatorModel: ""}},
			nil, logger,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if client == nil {
			t.Fatal("expected non-nil adjudicator client with Cliproxy.Model fallback")
		}

		if !strings.Contains(buf.String(), "cliproxy-default") {
			t.Error("expected adjudicator to fall back to Cliproxy.Model")
		}
	})

	t.Run("all empty returns nil", func(t *testing.T) {
		var buf bytes.Buffer

		logger := newUnsanitizedTestLogger(&buf)

		client, err := ProvideMemberNewsAdjudicatorClient(
			cliproxyProvider(settings.CliproxyConfig{Enabled: true, APIKey: testProviderKey, BaseURL: testProviderBaseURL, Model: ""}),
			&settings.LLMConfig{MemberNews: settings.ConsensusLLMConfig{Enabled: true, AdjudicatorModel: ""}},
			nil, logger,
		)
		if client != nil || err == nil || isLLMFeatureDisabled(err) {
			t.Fatalf("got (%v, %v), want initialization error", client, err)
		}

		if !strings.Contains(err.Error(), "incomplete") {
			t.Errorf("error = %v, want incomplete config", err)
		}
	})
}
