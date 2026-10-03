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

package settings

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/envload"
	"github.com/kapu/hololive-shared/pkg/config/settingstest"
)

var tracingEnabledEnvKeys = envload.TracingEnabledEnvKeys()

func clearTracingEnv(t *testing.T) {
	t.Helper()
	settingstest.ClearTracingEnv(t)
}

// 표준 OTel endpoint env 거부는 영구 계약이다. 빈 값은 OTel 명세상 미설정이라 통과한다.
func TestLoadTracingConfigRejectsStandardOTLPEndpoint(t *testing.T) {
	for _, standardEnv := range []string{envload.OTLPEndpointEnv, envload.OTLPTracesEndpointEnv} {
		for _, includeCanonical := range []bool{false, true} {
			clearTracingEnv(t)
			t.Setenv(envload.TracingHololiveAPIEnabledEnv, "true")
			t.Setenv(standardEnv, "otel-collector:4317")

			if includeCanonical {
				t.Setenv(envload.HololiveOTLPGRPCEndpointEnv, "otel-collector:4317")
			}

			_, err := LoadTracingConfig(envload.TracingHololiveAPIEnabledEnv)
			if err == nil || !strings.Contains(err.Error(), standardEnv+" is not accepted by Hololive runtimes") {
				t.Fatalf("LoadTracingConfig() error = %v, want standard endpoint rejection", err)
			}
		}
	}
}

func TestLoadTracingConfigDefaultsDisabled(t *testing.T) {
	clearTracingEnv(t)

	config, err := LoadTracingConfig(envload.TracingHololiveAPIEnabledEnv)
	if err != nil {
		t.Fatalf("LoadTracingConfig() error = %v", err)
	}

	if config.Enabled {
		t.Fatal("TracingConfig.Enabled = true, want false")
	}

	if config.Endpoint != "" {
		t.Fatalf("TracingConfig.Endpoint = %q, want empty", config.Endpoint)
	}

	if config.Insecure {
		t.Fatal("TracingConfig.Insecure = true, want false")
	}

	if config.SampleRate != defaultOTELSampleRate {
		t.Fatalf("TracingConfig.SampleRate = %v, want %v", config.SampleRate, defaultOTELSampleRate)
	}
}

func TestLoadTracingConfigReadsOnlyGivenToggle(t *testing.T) {
	for _, selectedEnv := range []string{envload.TracingHololiveAPIEnabledEnv, envload.TracingAlarmWorkerEnabledEnv} {
		t.Run(selectedEnv, func(t *testing.T) {
			clearTracingEnv(t)

			for _, key := range tracingEnabledEnvKeys {
				t.Setenv(key, "not-a-bool")
			}

			t.Setenv(selectedEnv, "true")
			t.Setenv("OTEL_ENABLED", "true")
			t.Setenv(envload.HololiveOTLPGRPCEndpointEnv, " otel-collector:4317 ")

			config, err := LoadTracingConfig(selectedEnv)
			if err != nil {
				t.Fatalf("LoadTracingConfig() error = %v", err)
			}

			if !config.Enabled {
				t.Fatal("TracingConfig.Enabled = false, want true")
			}

			if config.Endpoint != "otel-collector:4317" {
				t.Fatalf("TracingConfig.Endpoint = %q, want otel-collector:4317", config.Endpoint)
			}
		})
	}
}

// 토글 이름을 빠뜨린 호출자는 tracing을 조용히 끄지 않고 기동에 실패한다.
func TestLoadTracingConfigRejectsMissingToggleName(t *testing.T) {
	clearTracingEnv(t)

	_, err := LoadTracingConfig(" ")
	if err == nil || !strings.Contains(err.Error(), "tracing enabled env is required") {
		t.Fatalf("LoadTracingConfig() error = %v, want missing toggle name error", err)
	}
}

func TestLoadTracingConfigRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name     string
		envKey   string
		envValue string
		wantErr  string
	}{
		{
			name:     "selected enabled toggle",
			envKey:   envload.TracingHololiveAPIEnabledEnv,
			envValue: "not-a-bool",
			wantErr:  envload.TracingHololiveAPIEnabledEnv,
		},
		{
			name:     "insecure toggle",
			envKey:   "OTEL_EXPORTER_OTLP_INSECURE",
			envValue: "not-a-bool",
			wantErr:  "OTEL_EXPORTER_OTLP_INSECURE",
		},
		{
			name:     "sample parse",
			envKey:   envload.OTELSampleRateEnv,
			envValue: "not-a-number",
			wantErr:  envload.OTELSampleRateEnv,
		},
		{
			name:     "negative sample",
			envKey:   envload.OTELSampleRateEnv,
			envValue: "-0.1",
			wantErr:  "between 0 and 1",
		},
		{
			name:     "sample above one",
			envKey:   envload.OTELSampleRateEnv,
			envValue: "1.1",
			wantErr:  "between 0 and 1",
		},
		{
			name:     "non finite sample",
			envKey:   envload.OTELSampleRateEnv,
			envValue: "NaN",
			wantErr:  "between 0 and 1",
		},
		{
			name:     "enabled without endpoint",
			envKey:   envload.TracingHololiveAPIEnabledEnv,
			envValue: "true",
			wantErr:  "HOLOLIVE_OTLP_GRPC_ENDPOINT is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearTracingEnv(t)
			t.Setenv(tt.envKey, tt.envValue)

			_, err := LoadTracingConfig(envload.TracingHololiveAPIEnabledEnv)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("LoadTracingConfig() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
