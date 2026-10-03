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

	"github.com/kapu/hololive-shared/pkg/config/envload"
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
	if err := rejectStandardOTLPEndpointEnv(); err != nil {
		return TracingConfig{}, fmt.Errorf("reject standard OTLP endpoint env: %w", err)
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

	insecure, err := sharedenv.BoolE(envload.OTLPInsecureEnv, false)
	if err != nil {
		return TracingConfig{}, fmt.Errorf("read bool env: %w", err)
	}

	sampleRate, err := sharedenv.FloatE(envload.OTELSampleRateEnv, defaultOTELSampleRate)
	if err != nil {
		return TracingConfig{}, fmt.Errorf("read float env: %w", err)
	}

	config := TracingConfig{
		Enabled:    enabled,
		Endpoint:   strings.TrimSpace(sharedenv.String(envload.HololiveOTLPGRPCEndpointEnv, "")),
		Insecure:   insecure,
		SampleRate: sampleRate,
	}
	if err := ValidateTracingConfig(config); err != nil {
		return TracingConfig{}, fmt.Errorf("validate tracing config: %w", err)
	}

	return config, nil
}

// rejectStandardOTLPEndpointEnv는 퇴역 가드가 아니라 영구 계약이다(DEC-20260926-hololive-legacy-env-config-retirement).
// OpenTelemetry 표준 endpoint env는 URL 문법을 자동 적용하므로 Hololive runtime은 gRPC host:port 형식의
// HOLOLIVE_OTLP_GRPC_ENDPOINT 하나만 읽고 표준 이름은 받지 않는다(도입 eabc150b9). 표준 이름이라 운영에서 사라질
// 날이 오지 않으므로 제거 조건과 재검토 기한을 두지 않는다. 판정은 OTel 명세가 빈 값을 미설정으로 다루는 것에
// 맞춰 non-empty로 한다(프로젝트 퇴역 키의 존재 기준과 다른 이유).
func rejectStandardOTLPEndpointEnv() error {
	for _, standardEnv := range []string{envload.OTLPEndpointEnv, envload.OTLPTracesEndpointEnv} {
		if strings.TrimSpace(sharedenv.String(standardEnv, "")) != "" {
			return fmt.Errorf("%s is not accepted by Hololive runtimes; use %s", standardEnv, envload.HololiveOTLPGRPCEndpointEnv)
		}
	}

	return nil
}

func tracingEnabledEnv(runtime TracingRuntime, collectorInstanceID string) (string, error) {
	switch runtime {
	case TracingRuntimeHololiveAPI:
		return envload.TracingHololiveAPIEnabledEnv, nil
	case TracingRuntimeAlarmWorker:
		return envload.TracingAlarmWorkerEnabledEnv, nil
	case TracingRuntimeYouTubeCollector:
		out, err := youtubeCollectorTracingEnabledResult(collectorInstanceID)

		return out, err
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
		return envload.TracingYouTubeCollectorEnabledEnv, nil
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
		envload.TracingYouTubeCollectorAEnabledEnv,
		envload.TracingYouTubeCollectorBEnabledEnv,
		envload.TracingYouTubeCollectorCEnabledEnv,
		envload.TracingYouTubeCollectorDEnabledEnv,
		envload.TracingYouTubeCollectorEnabledEnv,
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
		"a": envload.TracingYouTubeCollectorAEnabledEnv,
		"b": envload.TracingYouTubeCollectorBEnabledEnv,
		"c": envload.TracingYouTubeCollectorCEnabledEnv,
		"d": envload.TracingYouTubeCollectorDEnabledEnv,
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
		return fmt.Errorf("%s is required when tracing is enabled", envload.HololiveOTLPGRPCEndpointEnv)
	}

	return nil
}
