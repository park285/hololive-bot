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
	"errors"
	"fmt"
	"math"
	"strings"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"

	"github.com/kapu/hololive-shared/pkg/config/settings/internal/load"
)

const defaultOTELSampleRate = 0.1

// TracingRuntime: OTEL enable 토글 환경변수를 고르는 런타임 구분자다.
type TracingRuntime uint8

const (
	TracingRuntimeHololiveAPI TracingRuntime = iota + 1
	TracingRuntimeAlarmWorker
	TracingRuntimeYouTubeCollector
)

type TracingConfig struct {
	Enabled    bool
	Endpoint   string
	Insecure   bool
	SampleRate float64
}

// LoadTracingConfig: collectorInstanceID는 youtube-collector 런타임에서만 쓰인다.
func LoadTracingConfig(runtime TracingRuntime, collectorInstanceID string) (TracingConfig, error) {
	if err := rejectRetiredOTLPEndpointEnv(); err != nil {
		return TracingConfig{}, fmt.Errorf("reject retired OTLP endpoint env: %w", err)
	}

	enabledEnv, err := tracingEnabledEnv(runtime, collectorInstanceID)
	if err != nil {
		return TracingConfig{}, fmt.Errorf("tracing enabled env: %w", err)
	}

	enabled := false

	if enabledEnv != "" {
		enabled, err = sharedenv.BoolE(enabledEnv, false)
		if err != nil {
			return TracingConfig{}, fmt.Errorf("read bool env: %w", err)
		}
	}

	insecure, err := sharedenv.BoolE(load.OTLPInsecureEnv, false)
	if err != nil {
		return TracingConfig{}, fmt.Errorf("read bool env: %w", err)
	}

	sampleRate, err := sharedenv.FloatE(load.OTELSampleRateEnv, defaultOTELSampleRate)
	if err != nil {
		return TracingConfig{}, fmt.Errorf("read float env: %w", err)
	}

	config := TracingConfig{
		Enabled:    enabled,
		Endpoint:   strings.TrimSpace(sharedenv.String(load.HololiveOTLPGRPCEndpointEnv, "")),
		Insecure:   insecure,
		SampleRate: sampleRate,
	}
	if err := ValidateTracingConfig(config); err != nil {
		return TracingConfig{}, fmt.Errorf("validate tracing config: %w", err)
	}

	return config, nil
}

func rejectRetiredOTLPEndpointEnv() error {
	for _, retiredEnv := range []string{load.OTLPEndpointEnv, load.OTLPTracesEndpointEnv} {
		if strings.TrimSpace(sharedenv.String(retiredEnv, "")) != "" {
			return fmt.Errorf("%s is no longer supported; use %s", retiredEnv, load.HololiveOTLPGRPCEndpointEnv)
		}
	}

	return nil
}

func tracingEnabledEnv(runtime TracingRuntime, collectorInstanceID string) (string, error) {
	switch runtime {
	case TracingRuntimeHololiveAPI:
		return load.TracingHololiveAPIEnabledEnv, nil
	case TracingRuntimeAlarmWorker:
		return load.TracingAlarmWorkerEnabledEnv, nil
	case TracingRuntimeYouTubeCollector:
		out, err := youtubeCollectorTracingEnabledResult(collectorInstanceID)

		return out, errors.Join(err)
	default:
		return "", fmt.Errorf("unsupported tracing runtime %d", runtime)
	}
}

func youtubeCollectorTracingEnabledResult(collectorInstanceID string) (string, error) {
	out, err := tracingEnabledEnvForYouTubeCollector(collectorInstanceID)
	if err != nil {
		return out, fmt.Errorf("tracing enabled env for youtube collector: %w", err)
	}

	return out, nil
}

func tracingEnabledEnvForYouTubeCollector(collectorInstanceID string) (string, error) {
	enabledEnv, err := YouTubeCollectorTracingEnabledEnv(collectorInstanceID)
	if err == nil {
		return enabledEnv, nil
	}

	if strings.TrimSpace(collectorInstanceID) == "" {
		return load.TracingYouTubeCollectorEnabledEnv, nil
	}

	disabled, disabledErr := allYouTubeCollectorTracingDisabled()
	if disabledErr != nil {
		return "", fmt.Errorf("all youtube collector tracing disabled: %w", disabledErr)
	}

	if disabled {
		return "", nil
	}

	return "", fmt.Errorf("youtube collector tracing enabled env: %w", err)
}

func allYouTubeCollectorTracingDisabled() (bool, error) {
	for _, key := range []string{
		load.TracingYouTubeCollectorAEnabledEnv,
		load.TracingYouTubeCollectorBEnabledEnv,
		load.TracingYouTubeCollectorCEnabledEnv,
		load.TracingYouTubeCollectorDEnabledEnv,
		load.TracingYouTubeCollectorEnabledEnv,
	} {
		enabled, err := sharedenv.BoolE(key, false)
		if err != nil {
			return false, fmt.Errorf("read bool env: %w", err)
		}

		if enabled {
			return false, nil
		}
	}

	return true, nil
}

// YouTubeCollectorTracingEnabledEnv: instance id에 대응하는 OTEL 토글 환경변수 이름이다.
func YouTubeCollectorTracingEnabledEnv(instanceID string) (string, error) {
	normalized := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(instanceID)), "youtube-collector-")
	enabledEnv, ok := map[string]string{
		"a": load.TracingYouTubeCollectorAEnabledEnv,
		"b": load.TracingYouTubeCollectorBEnabledEnv,
		"c": load.TracingYouTubeCollectorCEnabledEnv,
		"d": load.TracingYouTubeCollectorDEnabledEnv,
	}[normalized]

	if !ok {
		return "", errors.New("YOUTUBE_COLLECTOR_INSTANCE_ID must be one of a, b, c, d, youtube-collector-a, youtube-collector-b, youtube-collector-c, youtube-collector-d")
	}

	return enabledEnv, nil
}

func ValidateTracingConfig(config TracingConfig) error {
	if math.IsNaN(config.SampleRate) || math.IsInf(config.SampleRate, 0) || config.SampleRate < 0 || config.SampleRate > 1 {
		return errors.New("OTEL_SAMPLE_RATE must be between 0 and 1")
	}

	if config.Enabled && strings.TrimSpace(config.Endpoint) == "" {
		return fmt.Errorf("%s is required when tracing is enabled", load.HololiveOTLPGRPCEndpointEnv)
	}

	return nil
}
