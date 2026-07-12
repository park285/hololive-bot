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
	"crypto/tls"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/park285/shared-go/pkg/logging"
)

func TestProvideMajorEventLLMClient_Disabled(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewTestLoggerWithOutput(&buf)

	client := ProvideMajorEventLLMClient(config.CliproxyConfig{Enabled: false, APIKey: "key"}, nil, logger)
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

	client := ProvideMajorEventLLMClient(config.CliproxyConfig{Enabled: true, APIKey: ""}, nil, logger)
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
	}, nil, logger)
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
	}, nil, logger)
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
	}, nil, logger)
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

	client := ProvideMemberNewsLLMClient(config.CliproxyConfig{Enabled: false}, &config.LLMConfig{}, nil, logger)
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

	client := ProvideMemberNewsLLMClient(config.CliproxyConfig{Enabled: true, APIKey: ""}, &config.LLMConfig{}, nil, logger)
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
		&config.LLMConfig{
			MemberNewsModel: "test-model",
		},
		nil, logger,
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
		&config.LLMConfig{
			MemberNewsModel: "", // 빈값 → Cliproxy.Model fallback
		},
		nil, logger,
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
		&config.LLMConfig{
			MemberNewsModel: "old-model",
		},
		nil, logger,
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
		&config.LLMConfig{
			MemberNewsModel: "new-model",
		},
		nil, logger,
	)
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if strings.Contains(buf.String(), "legacy") {
		t.Error("should not have legacy warning for new env var")
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
		&config.LLMConfig{
			MemberNewsModel:       "test-model",
			MemberNewsTemperature: 0,
		},
		nil, logger,
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
		}, nil, logger)
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
			&config.LLMConfig{
				MemberNewsModel: "", // 빈값 + Cliproxy.Model 빈값 → error 경로
			},
			nil, logger,
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
	t.Setenv("HOLODEX_API_KEY_1", "test-key")
	t.Setenv("YOUTUBE_API_KEY", "test-youtube-key")
	t.Setenv("KAKAO_ROOMS", "test-room")
	t.Setenv("IRIS_WEBHOOK_TOKEN", "test-webhook-token")
	t.Setenv("IRIS_BOT_TOKEN", "test-bot-token")
	t.Setenv("IRIS_BASE_URL", newWorkerProfileEnabledIrisServer(t).URL)
	t.Setenv("IRIS_TRANSPORT", "http1")
	t.Setenv("API_SECRET_KEY", "test-api-key")
	t.Setenv("HOLOLIVE_H3_CERT_FILE", "/run/hololive-bot/certs/hololive-h3.crt")
	t.Setenv("HOLOLIVE_H3_KEY_FILE", "/run/hololive-bot/certs/hololive-h3.key")

	t.Setenv("CLIPROXY_ENABLED", "true")
	t.Setenv("CLIPROXY_API_KEY", "test-api-key")
	t.Setenv("CLIPROXY_BASE_URL", "https://example.com/v1")
	t.Setenv("CLIPROXY_MODEL", "default-model")

	t.Setenv("MEMBER_NEWS_LLM_MODEL", "new-model")

	appConfig, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}

	var buf bytes.Buffer
	logger := logging.NewTestLoggerWithOutput(&buf)
	client := ProvideMemberNewsLLMClient(appConfig.Cliproxy, &appConfig.LLM, nil, logger)
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "new-model") {
		t.Error("expected log with new model name")
	}
}

func newWorkerProfileEnabledIrisServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/diagnostics/runtime" {
			http.NotFound(w, r)

			return
		}

		w.Header().Set("Content-Type", "application/json")

		if _, err := w.Write([]byte(`{
			"workers": {
				"webhook": {
					"webhookPipeline": {
						"profileEnabled": true,
						"profileVersion": 1,
						"profileId": "llm-runtime-test",
						"profileHash": "6370ddeae7dab5d64d74c056fa9cf95b42de71b65c3da4a8d45949ba2bc4ed17",
						"workerProfile": {
							"version": 1,
							"profile_id": "llm-runtime-test",
							"delivery": {
								"lane_workers": 32,
								"lane_queue_capacity": 128,
								"max_global_in_flight": 32,
								"max_per_endpoint_in_flight": 8,
								"max_drain_per_tick": 128,
								"max_attempts": 6,
								"request_timeout_ms": 30000,
								"lane_idle_timeout_ms": 750,
								"breaker_failure_threshold": 5,
								"breaker_cooldown_ms": 30000
							},
							"receive": {
								"workers": 16,
								"queue_size": 1000,
								"enqueue_timeout_ms": 50,
								"handler_timeout_ms": 30000,
								"max_body_bytes": 65536,
								"dedup_ttl_ms": 60000,
								"dedup_timeout_ms": 200
							},
							"bot_pool": {
								"workers": 10,
								"queue_size": 100
							},
							"validation": {
								"min_queue_per_endpoint_multiplier": 4,
								"require_receive_capacity_for_endpoint_burst": true
							}
						}
					}
				}
			}
		}`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	server.TLS = &tls.Config{NextProtos: []string{"http/1.1"}}
	server.StartTLS()
	t.Cleanup(server.Close)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	caFile := filepath.Join(t.TempDir(), "iris-diagnostics-ca.pem")
	if err := os.WriteFile(caFile, certPEM, 0o600); err != nil {
		t.Fatalf("write Iris diagnostics CA failed: %v", err)
	}
	t.Setenv("SSL_CERT_FILE", caFile)

	return server
}

func TestProvideMemberNewsReviewerClient_ConsensusDisabled(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewTestLoggerWithOutput(&buf)

	client := ProvideMemberNewsReviewerClient(
		config.CliproxyConfig{Enabled: true, APIKey: "key", BaseURL: "https://example.com/v1", Model: "m"},
		&config.LLMConfig{MemberNews: config.ConsensusLLMConfig{Enabled: false}},
		nil, logger,
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
		&config.LLMConfig{MemberNews: config.ConsensusLLMConfig{Enabled: true, ReviewerModel: "gpt-4.1-mini"}},
		nil, logger,
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
		&config.LLMConfig{MemberNewsModel: "news-model", MemberNews: config.ConsensusLLMConfig{Enabled: true, ReviewerModel: ""}},
		nil, logger,
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
		&config.LLMConfig{MemberNews: config.ConsensusLLMConfig{Enabled: false}},
		nil, logger,
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
		&config.LLMConfig{MemberNews: config.ConsensusLLMConfig{Enabled: true, AdjudicatorModel: "gpt-4.1"}},
		nil, logger,
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
			&config.LLMConfig{MemberNewsModel: "news-model", MemberNews: config.ConsensusLLMConfig{Enabled: true, AdjudicatorModel: ""}},
			nil, logger,
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
			&config.LLMConfig{MemberNews: config.ConsensusLLMConfig{Enabled: true, AdjudicatorModel: ""}},
			nil, logger,
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
			&config.LLMConfig{MemberNews: config.ConsensusLLMConfig{Enabled: true, AdjudicatorModel: ""}},
			nil, logger,
		)
		if client != nil {
			t.Fatal("expected nil when all models empty")
		}
		if !strings.Contains(buf.String(), "incomplete") {
			t.Error("expected incomplete config warning")
		}
	})
}
