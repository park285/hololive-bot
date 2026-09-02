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

	"github.com/kapu/hololive-shared/pkg/config/settings/internal/load"
	"github.com/kapu/hololive-shared/pkg/config/settings/internal/settingstest"
)

var tracingEnabledEnvKeys = load.TracingEnabledEnvKeys()

func clearTracingEnv(t *testing.T) {
	t.Helper()
	settingstest.ClearTracingEnv(t)
}

func TestLoadTracingConfigRejectsRetiredStandardEndpoint(t *testing.T) {
	for _, retiredEnv := range []string{load.OTLPEndpointEnv, load.OTLPTracesEndpointEnv} {
		for _, includeCanonical := range []bool{false, true} {
			clearTracingEnv(t)
			t.Setenv(load.TracingHololiveAPIEnabledEnv, "true")
			t.Setenv(retiredEnv, "otel-collector:4317")

			if includeCanonical {
				t.Setenv(load.HololiveOTLPGRPCEndpointEnv, "otel-collector:4317")
			}

			_, err := LoadTracingConfig(TracingRuntimeHololiveAPI, "")
			if err == nil || !strings.Contains(err.Error(), retiredEnv+" is no longer supported") {
				t.Fatalf("LoadTracingConfig() error = %v, want retired standard endpoint rejection", err)
			}
		}
	}
}

func TestLoadTracingConfigDefaultsDisabled(t *testing.T) {
	clearTracingEnv(t)

	config, err := LoadTracingConfig(TracingRuntimeHololiveAPI, "")
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

func TestLoadTracingConfigSelectsOnlyRuntimeToggle(t *testing.T) {
	tests := []struct {
		name                string
		runtime             TracingRuntime
		collectorInstanceID string
		selectedEnv         string
	}{
		{name: "hololive api", runtime: TracingRuntimeHololiveAPI, selectedEnv: load.TracingHololiveAPIEnabledEnv},
		{name: "alarm worker", runtime: TracingRuntimeAlarmWorker, selectedEnv: load.TracingAlarmWorkerEnabledEnv},
		{name: "youtube collector a", runtime: TracingRuntimeYouTubeCollector, collectorInstanceID: "a", selectedEnv: load.TracingYouTubeCollectorAEnabledEnv},
		{name: "youtube collector b", runtime: TracingRuntimeYouTubeCollector, collectorInstanceID: "b", selectedEnv: load.TracingYouTubeCollectorBEnabledEnv},
		{name: "youtube collector c", runtime: TracingRuntimeYouTubeCollector, collectorInstanceID: "c", selectedEnv: load.TracingYouTubeCollectorCEnabledEnv},
		{name: "youtube collector d", runtime: TracingRuntimeYouTubeCollector, collectorInstanceID: "d", selectedEnv: load.TracingYouTubeCollectorDEnabledEnv},
		{name: "youtube collector default", runtime: TracingRuntimeYouTubeCollector, selectedEnv: load.TracingYouTubeCollectorEnabledEnv},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearTracingEnv(t)

			for _, key := range tracingEnabledEnvKeys {
				t.Setenv(key, "not-a-bool")
			}

			t.Setenv(tt.selectedEnv, "true")
			t.Setenv("OTEL_ENABLED", "true")
			t.Setenv(load.HololiveOTLPGRPCEndpointEnv, " otel-collector:4317 ")

			config, err := LoadTracingConfig(tt.runtime, tt.collectorInstanceID)
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

func TestLoadTracingConfigRejectsUnknownCollectorInstance(t *testing.T) {
	clearTracingEnv(t)

	for _, key := range tracingEnabledEnvKeys {
		t.Setenv(key, "true")
	}

	t.Setenv("OTEL_ENABLED", "true")

	_, err := LoadTracingConfig(TracingRuntimeYouTubeCollector, "unknown")
	if err == nil || !strings.Contains(err.Error(), "YOUTUBE_COLLECTOR_INSTANCE_ID") {
		t.Fatalf("LoadTracingConfig() error = %v, want instance ID validation error", err)
	}
}

func TestLoadTracingConfigAllowsDisabledUnknownCollectorInstance(t *testing.T) {
	tests := []struct {
		name       string
		instanceID string
		setFlags   bool
	}{
		{name: "empty instance and unset flags"},
		{name: "unknown instance and false flags", instanceID: "youtube-collector-legacy", setFlags: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearTracingEnv(t)

			if tt.setFlags {
				for _, key := range tracingEnabledEnvKeys[2:] {
					t.Setenv(key, "false")
				}
			}

			config, err := LoadTracingConfig(TracingRuntimeYouTubeCollector, tt.instanceID)
			if err != nil {
				t.Fatalf("LoadTracingConfig() error = %v, want nil", err)
			}

			if config.Enabled {
				t.Fatal("TracingConfig.Enabled = true, want false")
			}
		})
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
			envKey:   load.TracingHololiveAPIEnabledEnv,
			envValue: "not-a-bool",
			wantErr:  load.TracingHololiveAPIEnabledEnv,
		},
		{
			name:     "insecure toggle",
			envKey:   "OTEL_EXPORTER_OTLP_INSECURE",
			envValue: "not-a-bool",
			wantErr:  "OTEL_EXPORTER_OTLP_INSECURE",
		},
		{
			name:     "sample parse",
			envKey:   load.OTELSampleRateEnv,
			envValue: "not-a-number",
			wantErr:  load.OTELSampleRateEnv,
		},
		{
			name:     "negative sample",
			envKey:   load.OTELSampleRateEnv,
			envValue: "-0.1",
			wantErr:  "between 0 and 1",
		},
		{
			name:     "sample above one",
			envKey:   load.OTELSampleRateEnv,
			envValue: "1.1",
			wantErr:  "between 0 and 1",
		},
		{
			name:     "non finite sample",
			envKey:   load.OTELSampleRateEnv,
			envValue: "NaN",
			wantErr:  "between 0 and 1",
		},
		{
			name:     "enabled without endpoint",
			envKey:   load.TracingHololiveAPIEnabledEnv,
			envValue: "true",
			wantErr:  "HOLOLIVE_OTLP_GRPC_ENDPOINT is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearTracingEnv(t)
			t.Setenv(tt.envKey, tt.envValue)

			_, err := LoadTracingConfig(TracingRuntimeHololiveAPI, "")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("LoadTracingConfig() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
