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
)

var tracingEnabledEnvKeys = []string{
	tracingHololiveAPIEnabledEnv,
	tracingAlarmWorkerEnabledEnv,
	tracingYouTubeCollectorAEnabledEnv,
	tracingYouTubeCollectorBEnabledEnv,
	tracingYouTubeCollectorCEnabledEnv,
	tracingYouTubeCollectorDEnabledEnv,
	tracingYouTubeCollectorEnabledEnv,
}

func clearTracingEnv(t *testing.T) {
	t.Helper()

	for _, key := range append([]string{
		otlpEndpointEnv,
		otlpTracesEndpointEnv,
		"HOLOLIVE_OTLP_GRPC_ENDPOINT",
		"OTEL_EXPORTER_OTLP_INSECURE",
		otelSampleRateEnv,
		"OTEL_ENABLED",
		"OTEL_SERVICE_NAME",
		"OTEL_SERVICE_VERSION",
	}, tracingEnabledEnvKeys...) {
		t.Setenv(key, "")
	}
}

func TestLoadTracingConfigRejectsRetiredStandardEndpoint(t *testing.T) {
	for _, retiredEnv := range []string{otlpEndpointEnv, otlpTracesEndpointEnv} {
		for _, includeCanonical := range []bool{false, true} {
			clearTracingEnv(t)
			t.Setenv(tracingHololiveAPIEnabledEnv, "true")
			t.Setenv(retiredEnv, "otel-collector:4317")

			if includeCanonical {
				t.Setenv(hololiveOTLPGRPCEndpointEnv, "otel-collector:4317")
			}

			_, err := loadTracingConfig(tracingRuntimeHololiveAPI, "")
			if err == nil || !strings.Contains(err.Error(), retiredEnv+" is no longer supported") {
				t.Fatalf("loadTracingConfig() error = %v, want retired standard endpoint rejection", err)
			}
		}
	}
}

func TestLoadTracingConfigDefaultsDisabled(t *testing.T) {
	clearTracingEnv(t)

	config, err := loadTracingConfig(tracingRuntimeHololiveAPI, "")
	if err != nil {
		t.Fatalf("loadTracingConfig() error = %v", err)
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
		runtime             tracingRuntime
		collectorInstanceID string
		selectedEnv         string
	}{
		{name: "hololive api", runtime: tracingRuntimeHololiveAPI, selectedEnv: tracingHololiveAPIEnabledEnv},
		{name: "alarm worker", runtime: tracingRuntimeAlarmWorker, selectedEnv: tracingAlarmWorkerEnabledEnv},
		{name: "youtube collector a", runtime: tracingRuntimeYouTubeCollector, collectorInstanceID: "a", selectedEnv: tracingYouTubeCollectorAEnabledEnv},
		{name: "youtube collector b", runtime: tracingRuntimeYouTubeCollector, collectorInstanceID: "b", selectedEnv: tracingYouTubeCollectorBEnabledEnv},
		{name: "youtube collector c", runtime: tracingRuntimeYouTubeCollector, collectorInstanceID: "c", selectedEnv: tracingYouTubeCollectorCEnabledEnv},
		{name: "youtube collector d", runtime: tracingRuntimeYouTubeCollector, collectorInstanceID: "d", selectedEnv: tracingYouTubeCollectorDEnabledEnv},
		{name: "youtube collector default", runtime: tracingRuntimeYouTubeCollector, selectedEnv: tracingYouTubeCollectorEnabledEnv},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearTracingEnv(t)

			for _, key := range tracingEnabledEnvKeys {
				t.Setenv(key, "not-a-bool")
			}

			t.Setenv(tt.selectedEnv, "true")
			t.Setenv("OTEL_ENABLED", "true")
			t.Setenv(hololiveOTLPGRPCEndpointEnv, " otel-collector:4317 ")

			config, err := loadTracingConfig(tt.runtime, tt.collectorInstanceID)
			if err != nil {
				t.Fatalf("loadTracingConfig() error = %v", err)
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

	_, err := loadTracingConfig(tracingRuntimeYouTubeCollector, "unknown")
	if err == nil || !strings.Contains(err.Error(), "YOUTUBE_COLLECTOR_INSTANCE_ID") {
		t.Fatalf("loadTracingConfig() error = %v, want instance ID validation error", err)
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

			config, err := loadTracingConfig(tracingRuntimeYouTubeCollector, tt.instanceID)
			if err != nil {
				t.Fatalf("loadTracingConfig() error = %v, want nil", err)
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
			envKey:   tracingHololiveAPIEnabledEnv,
			envValue: "not-a-bool",
			wantErr:  tracingHololiveAPIEnabledEnv,
		},
		{
			name:     "insecure toggle",
			envKey:   "OTEL_EXPORTER_OTLP_INSECURE",
			envValue: "not-a-bool",
			wantErr:  "OTEL_EXPORTER_OTLP_INSECURE",
		},
		{
			name:     "sample parse",
			envKey:   otelSampleRateEnv,
			envValue: "not-a-number",
			wantErr:  otelSampleRateEnv,
		},
		{
			name:     "negative sample",
			envKey:   otelSampleRateEnv,
			envValue: "-0.1",
			wantErr:  "between 0 and 1",
		},
		{
			name:     "sample above one",
			envKey:   otelSampleRateEnv,
			envValue: "1.1",
			wantErr:  "between 0 and 1",
		},
		{
			name:     "non finite sample",
			envKey:   otelSampleRateEnv,
			envValue: "NaN",
			wantErr:  "between 0 and 1",
		},
		{
			name:     "enabled without endpoint",
			envKey:   tracingHololiveAPIEnabledEnv,
			envValue: "true",
			wantErr:  "HOLOLIVE_OTLP_GRPC_ENDPOINT is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearTracingEnv(t)
			t.Setenv(tt.envKey, tt.envValue)

			_, err := loadTracingConfig(tracingRuntimeHololiveAPI, "")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("loadTracingConfig() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadHololiveAPIRuntimeSelectsHololiveAPIToggle(t *testing.T) {
	clearRuntimeRoleEnv(t)
	clearTracingEnv(t)
	setRequiredLoadEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("ALARM_INTERNAL_URL", "http://127.0.0.1:30007")
	t.Setenv(hololiveOTLPGRPCEndpointEnv, "otel-collector:4317")
	t.Setenv(tracingHololiveAPIEnabledEnv, "true")
	t.Setenv(tracingAlarmWorkerEnabledEnv, "not-a-bool")

	for _, key := range tracingEnabledEnvKeys[2:] {
		t.Setenv(key, "not-a-bool")
	}

	config, err := LoadHololiveAPIRuntime()
	if err != nil {
		t.Fatalf("LoadHololiveAPIRuntime() error = %v", err)
	}

	if !config.Tracing.Enabled || config.Tracing != config.Bot.Tracing || config.Tracing != config.Admin.Tracing {
		t.Fatalf("HololiveAPIConfig tracing = %#v, bot = %#v, admin = %#v", config.Tracing, config.Bot.Tracing, config.Admin.Tracing)
	}
}

func TestLoadAlarmWorkerRuntimeSelectsAlarmWorkerToggle(t *testing.T) {
	clearRuntimeRoleEnv(t)
	clearTracingEnv(t)
	setRequiredLoadEnv(t)
	useStackWorkerProfileFixture(t, "stack-worker-profile-alarm-worker.json")
	t.Setenv("APP_ENV", "development")
	t.Setenv(hololiveOTLPGRPCEndpointEnv, "otel-collector:4317")
	t.Setenv(tracingHololiveAPIEnabledEnv, "not-a-bool")
	t.Setenv(tracingAlarmWorkerEnabledEnv, "true")

	for _, key := range tracingEnabledEnvKeys[2:] {
		t.Setenv(key, "not-a-bool")
	}

	config, err := LoadAlarmWorkerRuntime()
	if err != nil {
		t.Fatalf("LoadAlarmWorkerRuntime() error = %v", err)
	}

	if !config.Tracing.Enabled {
		t.Fatal("TracingConfig.Enabled = false, want true")
	}
}

func TestLoadYouTubeCollectorRuntimeSelectsInstanceToggle(t *testing.T) {
	tests := []struct {
		instanceID  string
		selectedEnv string
	}{
		{instanceID: "youtube-collector-a", selectedEnv: tracingYouTubeCollectorAEnabledEnv},
		{instanceID: "youtube-collector-b", selectedEnv: tracingYouTubeCollectorBEnabledEnv},
		{instanceID: collectorInstanceC, selectedEnv: tracingYouTubeCollectorCEnabledEnv},
		{instanceID: "youtube-collector-d", selectedEnv: tracingYouTubeCollectorDEnabledEnv},
	}

	for _, tt := range tests {
		t.Run(tt.instanceID, func(t *testing.T) {
			clearIrisAndRoomEnv(t)
			clearTracingEnv(t)
			t.Setenv("APP_ENV", "development")
			t.Setenv("API_SECRET_KEY", "dummy-secret")
			setRuntimeH3ServerEnv(t)
			t.Setenv("POSTGRES_USER", "hololive_scraper")
			t.Setenv("YOUTUBE_COLLECTOR_INSTANCE_ID", tt.instanceID)
			t.Setenv("YOUTUBE_COLLECTOR_RUNTIME_ALLOWED", "true")
			t.Setenv("PHOTO_SYNC_ENABLED", "false")
			useStackWorkerProfileFixture(t, "stack-worker-profile-youtube-collector.json")
			t.Setenv("HOLODEX_API_KEY", "dummy-holodex")
			t.Setenv(hololiveOTLPGRPCEndpointEnv, "otel-collector:4317")
			t.Setenv(tracingHololiveAPIEnabledEnv, "not-a-bool")
			t.Setenv(tracingAlarmWorkerEnabledEnv, "not-a-bool")

			for _, key := range tracingEnabledEnvKeys[2:] {
				t.Setenv(key, "not-a-bool")
			}

			t.Setenv(tt.selectedEnv, "true")

			config, err := LoadYouTubeCollectorRuntime()
			if err != nil {
				t.Fatalf("LoadYouTubeCollectorRuntime() error = %v", err)
			}

			if !config.Tracing.Enabled {
				t.Fatal("TracingConfig.Enabled = false, want true")
			}
		})
	}
}
