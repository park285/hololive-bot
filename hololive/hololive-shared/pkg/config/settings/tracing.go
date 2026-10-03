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

type TracingConfig struct {
	Enabled    bool
	Endpoint   string
	Insecure   bool
	SampleRate float64
}

// LoadTracingConfig: enabledEnv는 호출 런타임이 고른 OTEL enable 토글 환경변수 이름이다.
// 런타임별 토글 선택(API·alarm 고정 이름, collector AP slot)은 각 런타임 설정 소유자가 정한다.
func LoadTracingConfig(enabledEnv string) (TracingConfig, error) {
	if err := RejectStandardOTLPEndpointEnv(); err != nil {
		return TracingConfig{}, fmt.Errorf("reject standard OTLP endpoint env: %w", err)
	}

	if strings.TrimSpace(enabledEnv) == "" {
		return TracingConfig{}, errors.New("tracing enabled env is required")
	}

	enabled, err := sharedenv.BoolE(enabledEnv, false)
	if err != nil {
		return TracingConfig{}, fmt.Errorf("read bool env: %w", err)
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

// RejectStandardOTLPEndpointEnv는 퇴역 가드가 아니라 영구 계약이다(DEC-20260926-hololive-legacy-env-config-retirement).
// OpenTelemetry 표준 endpoint env는 URL 문법을 자동 적용하므로 Hololive runtime은 gRPC host:port 형식의
// HOLOLIVE_OTLP_GRPC_ENDPOINT 하나만 읽고 표준 이름은 받지 않는다(도입 eabc150b9). 표준 이름이라 운영에서 사라질
// 날이 오지 않으므로 제거 조건과 재검토 기한을 두지 않는다. 판정은 OTel 명세가 빈 값을 미설정으로 다루는 것에
// 맞춰 non-empty로 한다(프로젝트 퇴역 키의 존재 기준과 다른 이유).
// LoadTracingConfig가 먼저 호출하며, 토글 이름을 고르기 전에 실패해야 하는 런타임은 직접 먼저 호출한다.
func RejectStandardOTLPEndpointEnv() error {
	for _, standardEnv := range []string{envload.OTLPEndpointEnv, envload.OTLPTracesEndpointEnv} {
		if strings.TrimSpace(sharedenv.String(standardEnv, "")) != "" {
			return fmt.Errorf("%s is not accepted by Hololive runtimes; use %s", standardEnv, envload.HololiveOTLPGRPCEndpointEnv)
		}
	}

	return nil
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
