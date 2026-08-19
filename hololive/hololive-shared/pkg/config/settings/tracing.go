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
	"fmt"
	"math"
	"strings"

	sharedenv "github.com/park285/shared-go/pkg/envutil"
)

const defaultOTELSampleRate = 0.1

const (
	hololiveOTLPGRPCEndpointEnv = "HOLOLIVE_OTLP_GRPC_ENDPOINT"
	otlpEndpointEnv             = "OTEL_EXPORTER_OTLP_ENDPOINT"
	otlpTracesEndpointEnv       = "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"
)

const (
	tracingHololiveAPIEnabledEnv       = "OTEL_HOLOLIVE_API_ENABLED"
	tracingAlarmWorkerEnabledEnv       = "OTEL_HOLOLIVE_ALARM_WORKER_ENABLED"
	tracingYouTubeCollectorAEnabledEnv = "OTEL_YOUTUBE_COLLECTOR_A_ENABLED"
	tracingYouTubeCollectorBEnabledEnv = "OTEL_YOUTUBE_COLLECTOR_B_ENABLED"
	tracingYouTubeCollectorCEnabledEnv = "OTEL_YOUTUBE_COLLECTOR_C_ENABLED"
	tracingYouTubeCollectorDEnabledEnv = "OTEL_YOUTUBE_COLLECTOR_D_ENABLED"
	tracingYouTubeCollectorEnabledEnv  = "OTEL_YOUTUBE_COLLECTOR_ENABLED"
)

type tracingRuntime uint8

const (
	tracingRuntimeHololiveAPI tracingRuntime = iota + 1
	tracingRuntimeAlarmWorker
	tracingRuntimeYouTubeCollector
)

type TracingConfig struct {
	Enabled    bool
	Endpoint   string
	Insecure   bool
	SampleRate float64
}

func loadTracingConfig(runtime tracingRuntime, collectorInstanceID string) (TracingConfig, error) {
	if err := rejectRetiredOTLPEndpointEnv(); err != nil {
		return TracingConfig{}, err
	}

	enabledEnv, err := tracingEnabledEnv(runtime, collectorInstanceID)
	if err != nil {
		return TracingConfig{}, err
	}

	enabled := false
	if enabledEnv != "" {
		enabled, err = sharedenv.BoolE(enabledEnv, false)
		if err != nil {
			return TracingConfig{}, err
		}
	}
	insecure, err := sharedenv.BoolE("OTEL_EXPORTER_OTLP_INSECURE", false)
	if err != nil {
		return TracingConfig{}, err
	}
	sampleRate, err := sharedenv.FloatE("OTEL_SAMPLE_RATE", defaultOTELSampleRate)
	if err != nil {
		return TracingConfig{}, err
	}

	config := TracingConfig{
		Enabled:    enabled,
		Endpoint:   strings.TrimSpace(sharedenv.String(hololiveOTLPGRPCEndpointEnv, "")),
		Insecure:   insecure,
		SampleRate: sampleRate,
	}
	if err := validateTracingConfig(config); err != nil {
		return TracingConfig{}, err
	}
	return config, nil
}

func rejectRetiredOTLPEndpointEnv() error {
	for _, retiredEnv := range []string{otlpEndpointEnv, otlpTracesEndpointEnv} {
		if strings.TrimSpace(sharedenv.String(retiredEnv, "")) != "" {
			return fmt.Errorf("%s is no longer supported; use %s", retiredEnv, hololiveOTLPGRPCEndpointEnv)
		}
	}
	return nil
}

func tracingEnabledEnv(runtime tracingRuntime, collectorInstanceID string) (string, error) {
	switch runtime {
	case tracingRuntimeHololiveAPI:
		return tracingHololiveAPIEnabledEnv, nil
	case tracingRuntimeAlarmWorker:
		return tracingAlarmWorkerEnabledEnv, nil
	case tracingRuntimeYouTubeCollector:
		return tracingEnabledEnvForYouTubeCollector(collectorInstanceID)
	default:
		return "", fmt.Errorf("unsupported tracing runtime %d", runtime)
	}
}

func tracingEnabledEnvForYouTubeCollector(collectorInstanceID string) (string, error) {
	enabledEnv, err := youtubeCollectorTracingEnabledEnv(collectorInstanceID)
	if err == nil {
		return enabledEnv, nil
	}
	if strings.TrimSpace(collectorInstanceID) == "" {
		return tracingYouTubeCollectorEnabledEnv, nil
	}
	disabled, disabledErr := allYouTubeCollectorTracingDisabled()
	if disabledErr != nil {
		return "", disabledErr
	}
	if disabled {
		return "", nil
	}
	return "", err
}

func allYouTubeCollectorTracingDisabled() (bool, error) {
	for _, key := range []string{
		tracingYouTubeCollectorAEnabledEnv,
		tracingYouTubeCollectorBEnabledEnv,
		tracingYouTubeCollectorCEnabledEnv,
		tracingYouTubeCollectorDEnabledEnv,
		tracingYouTubeCollectorEnabledEnv,
	} {
		enabled, err := sharedenv.BoolE(key, false)
		if err != nil {
			return false, err
		}
		if enabled {
			return false, nil
		}
	}
	return true, nil
}

func youtubeCollectorTracingEnabledEnv(instanceID string) (string, error) {
	normalized := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(instanceID)), "youtube-collector-")
	enabledEnv, ok := map[string]string{
		"a": tracingYouTubeCollectorAEnabledEnv,
		"b": tracingYouTubeCollectorBEnabledEnv,
		"c": tracingYouTubeCollectorCEnabledEnv,
		"d": tracingYouTubeCollectorDEnabledEnv,
	}[normalized]
	if !ok {
		return "", fmt.Errorf("YOUTUBE_COLLECTOR_INSTANCE_ID must be one of a, b, c, d, youtube-collector-a, youtube-collector-b, youtube-collector-c, youtube-collector-d")
	}
	return enabledEnv, nil
}

func validateTracingConfig(config TracingConfig) error {
	if math.IsNaN(config.SampleRate) || math.IsInf(config.SampleRate, 0) || config.SampleRate < 0 || config.SampleRate > 1 {
		return fmt.Errorf("OTEL_SAMPLE_RATE must be between 0 and 1")
	}
	if config.Enabled && strings.TrimSpace(config.Endpoint) == "" {
		return fmt.Errorf("%s is required when tracing is enabled", hololiveOTLPGRPCEndpointEnv)
	}
	return nil
}
