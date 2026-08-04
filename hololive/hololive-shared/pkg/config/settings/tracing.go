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
	tracingHololiveAPIEnabledEnv      = "OTEL_HOLOLIVE_API_ENABLED"
	tracingAlarmWorkerEnabledEnv      = "OTEL_HOLOLIVE_ALARM_WORKER_ENABLED"
	tracingYouTubeProducerAEnabledEnv = "OTEL_YOUTUBE_PRODUCER_A_ENABLED"
	tracingYouTubeProducerBEnabledEnv = "OTEL_YOUTUBE_PRODUCER_B_ENABLED"
	tracingYouTubeProducerCEnabledEnv = "OTEL_YOUTUBE_PRODUCER_C_ENABLED"
	tracingYouTubeProducerDEnabledEnv = "OTEL_YOUTUBE_PRODUCER_D_ENABLED"
)

type tracingRuntime uint8

const (
	tracingRuntimeHololiveAPI tracingRuntime = iota + 1
	tracingRuntimeAlarmWorker
	tracingRuntimeYouTubeProducer
)

type TracingConfig struct {
	Enabled    bool
	Endpoint   string
	Insecure   bool
	SampleRate float64
}

func loadTracingConfig(runtime tracingRuntime, producerInstanceID string) (TracingConfig, error) {
	enabledEnv, err := tracingEnabledEnv(runtime, producerInstanceID)
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
		Endpoint:   strings.TrimSpace(sharedenv.String("OTEL_EXPORTER_OTLP_ENDPOINT", "")),
		Insecure:   insecure,
		SampleRate: sampleRate,
	}
	if err := validateTracingConfig(config); err != nil {
		return TracingConfig{}, err
	}
	return config, nil
}

func tracingEnabledEnv(runtime tracingRuntime, producerInstanceID string) (string, error) {
	switch runtime {
	case tracingRuntimeHololiveAPI:
		return tracingHololiveAPIEnabledEnv, nil
	case tracingRuntimeAlarmWorker:
		return tracingAlarmWorkerEnabledEnv, nil
	case tracingRuntimeYouTubeProducer:
		return tracingEnabledEnvForYouTubeProducer(producerInstanceID)
	default:
		return "", fmt.Errorf("unsupported tracing runtime %d", runtime)
	}
}

func tracingEnabledEnvForYouTubeProducer(producerInstanceID string) (string, error) {
	enabledEnv, err := youtubeProducerTracingEnabledEnv(producerInstanceID)
	if err == nil {
		return enabledEnv, nil
	}
	disabled, disabledErr := allYouTubeProducerTracingDisabled()
	if disabledErr != nil {
		return "", disabledErr
	}
	if disabled {
		return "", nil
	}
	return "", err
}

func allYouTubeProducerTracingDisabled() (bool, error) {
	for _, key := range []string{
		tracingYouTubeProducerAEnabledEnv,
		tracingYouTubeProducerBEnabledEnv,
		tracingYouTubeProducerCEnabledEnv,
		tracingYouTubeProducerDEnabledEnv,
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

func youtubeProducerTracingEnabledEnv(instanceID string) (string, error) {
	normalized := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(instanceID)), "youtube-producer-")
	enabledEnv, ok := map[string]string{
		"a": tracingYouTubeProducerAEnabledEnv,
		"b": tracingYouTubeProducerBEnabledEnv,
		"c": tracingYouTubeProducerCEnabledEnv,
		"d": tracingYouTubeProducerDEnabledEnv,
	}[normalized]
	if !ok {
		return "", fmt.Errorf("YOUTUBE_PRODUCER_INSTANCE_ID must be one of a, b, c, d, youtube-producer-a, youtube-producer-b, youtube-producer-c, youtube-producer-d")
	}
	return enabledEnv, nil
}

func validateTracingConfig(config TracingConfig) error {
	if math.IsNaN(config.SampleRate) || math.IsInf(config.SampleRate, 0) || config.SampleRate < 0 || config.SampleRate > 1 {
		return fmt.Errorf("OTEL_SAMPLE_RATE must be between 0 and 1")
	}
	if config.Enabled && strings.TrimSpace(config.Endpoint) == "" {
		return fmt.Errorf("OTEL_EXPORTER_OTLP_ENDPOINT is required when tracing is enabled")
	}
	return nil
}
