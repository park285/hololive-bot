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
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/logging"
)

func TestProvideMajorEventLLMClient_Disabled(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewTestLoggerWithOutput(&buf)

	client := ProvideMajorEventLLMClient(config.CliproxyConfig{Enabled: false, APIKey: "key"}, logger)
	if client != nil {
		t.Fatal("expected nil when disabled")
	}
	if !strings.Contains(buf.String(), "disabled") {
		t.Error("expected info log about disabled")
	}
}

func TestProvideMajorEventLLMClient_NoAPIKey(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewTestLoggerWithOutput(&buf)

	client := ProvideMajorEventLLMClient(config.CliproxyConfig{Enabled: true, APIKey: ""}, logger)
	if client != nil {
		t.Fatal("expected nil when API key missing")
	}
}

func TestProvideMajorEventLLMClient_EmptyBaseURL(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewTestLoggerWithOutput(&buf)

	client := ProvideMajorEventLLMClient(config.CliproxyConfig{
		Enabled: true,
		APIKey:  "key",
		BaseURL: "",
		Model:   "gpt-test",
	}, logger)
	if client != nil {
		t.Fatal("expected nil when baseURL empty")
	}
	if !strings.Contains(buf.String(), "incomplete") {
		t.Error("expected error log about incomplete config")
	}
}

func TestProvideMajorEventLLMClient_EmptyModel(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewTestLoggerWithOutput(&buf)

	client := ProvideMajorEventLLMClient(config.CliproxyConfig{
		Enabled: true,
		APIKey:  "key",
		BaseURL: "https://example.com/v1",
		Model:   "",
	}, logger)
	if client != nil {
		t.Fatal("expected nil when model empty")
	}
}

func TestProvideMajorEventLLMClient_Success(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewTestLoggerWithOutput(&buf)

	client := ProvideMajorEventLLMClient(config.CliproxyConfig{
		Enabled: true,
		APIKey:  "key",
		BaseURL: "https://example.com/v1",
		Model:   "gpt-test",
	}, logger)
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if !strings.Contains(buf.String(), "gpt-test") {
		t.Error("expected log with model name")
	}
}

func TestProvideMemberNewsLLMClient_Disabled(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewTestLoggerWithOutput(&buf)

	client := ProvideMemberNewsLLMClient(config.CliproxyConfig{Enabled: false}, config.LLMConfig{}, logger)
	if client != nil {
		t.Fatal("expected nil when disabled")
	}
	if !strings.Contains(buf.String(), "disabled") {
		t.Error("expected info log about disabled")
	}
}

func TestProvideMemberNewsLLMClient_NoAPIKey(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewTestLoggerWithOutput(&buf)

	client := ProvideMemberNewsLLMClient(config.CliproxyConfig{Enabled: true, APIKey: ""}, config.LLMConfig{}, logger)
	if client != nil {
		t.Fatal("expected nil when API key missing")
	}
	if !strings.Contains(buf.String(), "disabled") {
		t.Error("expected info log about disabled")
	}
}

func TestProvideMemberNewsLLMClient_EmptyBaseURL(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewTestLoggerWithOutput(&buf)

	client := ProvideMemberNewsLLMClient(
		config.CliproxyConfig{
			Enabled: true,
			APIKey:  "key",
			BaseURL: "",
		},
		config.LLMConfig{
			MemberNewsModel: "test-model",
		},
		logger,
	)
	if client != nil {
		t.Fatal("expected nil when baseURL empty")
	}
	if !strings.Contains(buf.String(), "incomplete") {
		t.Error("expected error log about incomplete config")
	}
}

func TestProvideMemberNewsLLMClient_ModelFallback(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewTestLoggerWithOutput(&buf)

	client := ProvideMemberNewsLLMClient(
		config.CliproxyConfig{
			Enabled: true,
			APIKey:  "key",
			BaseURL: "https://example.com/v1",
			Model:   "default-model",
		},
		config.LLMConfig{
			MemberNewsModel: "", // 빈값 → Cliproxy.Model fallback
		},
		logger,
	)
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if !strings.Contains(buf.String(), "default-model") {
		t.Error("expected log with fallback model name")
	}
}

func TestProvideMemberNewsLLMClient_DeprecatedModel(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewTestLoggerWithOutput(&buf)

	client := ProvideMemberNewsLLMClient(
		config.CliproxyConfig{
			Enabled: true,
			APIKey:  "key",
			BaseURL: "https://example.com/v1",
			Model:   "default-model",
		},
		config.LLMConfig{
			MemberNewsModel: "old-model",
		},
		logger,
	)
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestProvideMemberNewsLLMClient_NewModel_NoDeprecationWarn(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewTestLoggerWithOutput(&buf)

	client := ProvideMemberNewsLLMClient(
		config.CliproxyConfig{
			Enabled: true,
			APIKey:  "key",
			BaseURL: "https://example.com/v1",
			Model:   "default-model",
		},
		config.LLMConfig{
			MemberNewsModel: "new-model",
		},
		logger,
	)
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if strings.Contains(buf.String(), "deprecated") {
		t.Error("should not have deprecation warning for new env var")
	}
	if !strings.Contains(buf.String(), "new-model") {
		t.Error("expected log with model name")
	}
}

func TestProvideMemberNewsLLMClient_TemperatureZero_LogShowsNotApplied(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewTestLoggerWithOutput(&buf)

	client := ProvideMemberNewsLLMClient(
		config.CliproxyConfig{
			Enabled: true,
			APIKey:  "key",
			BaseURL: "https://example.com/v1",
			Model:   "default-model",
		},
		config.LLMConfig{
			MemberNewsModel:       "test-model",
			MemberNewsTemperature: 0,
		},
		logger,
	)
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	logOutput := buf.String()
	if !strings.Contains(logOutput, "temperature_applied=false") {
		t.Error("expected temperature_applied=false when temperature is 0")
	}
}

func TestProviderLogs_NoRawURLInErrorPath(t *testing.T) {
	sensitiveURL := "https://secret-proxy.internal.example.com/v1"
	sensitiveKey := "test-cliproxy-key-redacted"

	t.Run("MajorEvent error path", func(t *testing.T) {
		var buf bytes.Buffer
		logger := logging.NewTestLoggerWithOutput(&buf)

		ProvideMajorEventLLMClient(config.CliproxyConfig{
			Enabled: true,
			APIKey:  sensitiveKey,
			BaseURL: sensitiveURL,
			Model:   "", // 빈값 → error 경로
		}, logger)
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
		logger := logging.NewTestLoggerWithOutput(&buf)

		ProvideMemberNewsLLMClient(
			config.CliproxyConfig{
				Enabled: true,
				APIKey:  sensitiveKey,
				BaseURL: sensitiveURL,
				Model:   "",
			},
			config.LLMConfig{
				MemberNewsModel: "", // 빈값 + Cliproxy.Model 빈값 → error 경로
			},
			logger,
		)
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
	// config.Load() 필수 env vars
	t.Setenv("HOLODEX_API_KEY_1", "test-key")
	t.Setenv("YOUTUBE_API_KEY", "test-youtube-key")
	t.Setenv("KAKAO_ROOMS", "test-room")
	t.Setenv("IRIS_WEBHOOK_TOKEN", "test-webhook-token")
	t.Setenv("IRIS_BOT_TOKEN", "test-bot-token")
	t.Setenv("IRIS_BASE_URL_FILE", "/tmp/iris_base_url")
	t.Setenv("API_SECRET_KEY", "test-api-key")
	t.Setenv("HOLOLIVE_H3_CERT_FILE", "/run/hololive-bot/certs/hololive-h3.crt")
	t.Setenv("HOLOLIVE_H3_KEY_FILE", "/run/hololive-bot/certs/hololive-h3.key")

	// Cliproxy 활성화
	t.Setenv("CLIPROXY_ENABLED", "true")
	t.Setenv("CLIPROXY_API_KEY", "test-api-key")
	t.Setenv("CLIPROXY_BASE_URL", "https://example.com/v1")
	t.Setenv("CLIPROXY_MODEL", "default-model")

	// 최신 env만 설정
	t.Setenv("MEMBER_NEWS_LLM_MODEL", "new-model")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}

	var buf bytes.Buffer
	logger := logging.NewTestLoggerWithOutput(&buf)
	client := ProvideMemberNewsLLMClient(cfg.Cliproxy, cfg.LLM, logger)
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
	logger := logging.NewTestLoggerWithOutput(&buf)

	client := ProvideMemberNewsReviewerClient(
		config.CliproxyConfig{Enabled: true, APIKey: "key", BaseURL: "https://example.com/v1", Model: "m"},
		config.LLMConfig{MemberNews: config.ConsensusLLMConfig{Enabled: false}},
		logger,
	)
	if client != nil {
		t.Fatal("expected nil when consensus disabled")
	}
}

func TestProvideMemberNewsReviewerClient_Enabled(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewTestLoggerWithOutput(&buf)

	client := ProvideMemberNewsReviewerClient(
		config.CliproxyConfig{Enabled: true, APIKey: "key", BaseURL: "https://example.com/v1", Model: "default"},
		config.LLMConfig{MemberNews: config.ConsensusLLMConfig{Enabled: true, ReviewerModel: "gpt-4.1-mini"}},
		logger,
	)
	if client == nil {
		t.Fatal("expected non-nil reviewer client")
	}
	if !strings.Contains(buf.String(), "gpt-4.1-mini") {
		t.Error("expected log with reviewer model name")
	}
}

func TestProvideMemberNewsReviewerClient_ModelFallback(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewTestLoggerWithOutput(&buf)

	client := ProvideMemberNewsReviewerClient(
		config.CliproxyConfig{Enabled: true, APIKey: "key", BaseURL: "https://example.com/v1", Model: "cliproxy-default"},
		config.LLMConfig{MemberNewsModel: "news-model", MemberNews: config.ConsensusLLMConfig{Enabled: true, ReviewerModel: ""}},
		logger,
	)
	if client == nil {
		t.Fatal("expected non-nil reviewer client with model fallback")
	}
	if !strings.Contains(buf.String(), "news-model") {
		t.Error("expected reviewer to fall back to MemberNewsModel")
	}
}

func TestProvideMemberNewsAdjudicatorClient_ConsensusDisabled(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewTestLoggerWithOutput(&buf)

	client := ProvideMemberNewsAdjudicatorClient(
		config.CliproxyConfig{Enabled: true, APIKey: "key", BaseURL: "https://example.com/v1", Model: "m"},
		config.LLMConfig{MemberNews: config.ConsensusLLMConfig{Enabled: false}},
		logger,
	)
	if client != nil {
		t.Fatal("expected nil when consensus disabled")
	}
}

func TestProvideMemberNewsAdjudicatorClient_Enabled(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewTestLoggerWithOutput(&buf)

	client := ProvideMemberNewsAdjudicatorClient(
		config.CliproxyConfig{Enabled: true, APIKey: "key", BaseURL: "https://example.com/v1", Model: "default"},
		config.LLMConfig{MemberNews: config.ConsensusLLMConfig{Enabled: true, AdjudicatorModel: "gpt-4.1"}},
		logger,
	)
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
		logger := logging.NewTestLoggerWithOutput(&buf)

		client := ProvideMemberNewsAdjudicatorClient(
			config.CliproxyConfig{Enabled: true, APIKey: "key", BaseURL: "https://example.com/v1", Model: "cliproxy-default"},
			config.LLMConfig{MemberNewsModel: "news-model", MemberNews: config.ConsensusLLMConfig{Enabled: true, AdjudicatorModel: ""}},
			logger,
		)
		if client == nil {
			t.Fatal("expected non-nil adjudicator client with MemberNewsModel fallback")
		}
		if !strings.Contains(buf.String(), "news-model") {
			t.Error("expected adjudicator to fall back to MemberNewsModel")
		}
	})

	t.Run("falls back to Cliproxy.Model", func(t *testing.T) {
		var buf bytes.Buffer
		logger := logging.NewTestLoggerWithOutput(&buf)

		client := ProvideMemberNewsAdjudicatorClient(
			config.CliproxyConfig{Enabled: true, APIKey: "key", BaseURL: "https://example.com/v1", Model: "cliproxy-default"},
			config.LLMConfig{MemberNews: config.ConsensusLLMConfig{Enabled: true, AdjudicatorModel: ""}},
			logger,
		)
		if client == nil {
			t.Fatal("expected non-nil adjudicator client with Cliproxy.Model fallback")
		}
		if !strings.Contains(buf.String(), "cliproxy-default") {
			t.Error("expected adjudicator to fall back to Cliproxy.Model")
		}
	})

	t.Run("all empty returns nil", func(t *testing.T) {
		var buf bytes.Buffer
		logger := logging.NewTestLoggerWithOutput(&buf)

		client := ProvideMemberNewsAdjudicatorClient(
			config.CliproxyConfig{Enabled: true, APIKey: "key", BaseURL: "https://example.com/v1", Model: ""},
			config.LLMConfig{MemberNews: config.ConsensusLLMConfig{Enabled: true, AdjudicatorModel: ""}},
			logger,
		)
		if client != nil {
			t.Fatal("expected nil when all models empty")
		}
		if !strings.Contains(buf.String(), "incomplete") {
			t.Error("expected incomplete config warning")
		}
	})
}
