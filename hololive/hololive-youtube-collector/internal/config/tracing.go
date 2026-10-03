package config

import (
	"errors"
	"fmt"
	"strings"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"

	"github.com/kapu/hololive-shared/pkg/config/envload"
	"github.com/kapu/hololive-shared/pkg/config/settings"
)

// loadTracingConfig: AP slot(a/b/c/d)에 대응하는 OTEL 토글을 골라 공통 tracing loader로 읽는다.
// 표준 OTLP endpoint env 거부가 instance ID 오류보다 먼저 보이도록 slot 해석 전에 확인한다.
func loadTracingConfig(instanceID string) (settings.TracingConfig, error) {
	if err := settings.RejectStandardOTLPEndpointEnv(); err != nil {
		return settings.TracingConfig{}, fmt.Errorf("reject standard OTLP endpoint env: %w", err)
	}

	enabledEnv, err := resolveTracingEnabledEnv(instanceID)
	if err != nil {
		return settings.TracingConfig{}, fmt.Errorf("tracing enabled env: %w", err)
	}

	config, err := settings.LoadTracingConfig(enabledEnv)
	if err != nil {
		return settings.TracingConfig{}, fmt.Errorf("load shared tracing config: %w", err)
	}

	return config, nil
}

// resolveTracingEnabledEnv: 빈 instance ID는 기본 토글을 쓴다. 알 수 없는 instance ID는 모든 collector 토글이
// false일 때만 받아들이며, 그때는 false로 확인된 기본 토글을 돌려줘 tracing이 꺼진 채로 읽힌다.
func resolveTracingEnabledEnv(instanceID string) (string, error) {
	enabledEnv, err := tracingEnabledEnvForInstance(instanceID)
	if err == nil {
		return enabledEnv, nil
	}

	if strings.TrimSpace(instanceID) == "" {
		return envload.TracingYouTubeCollectorEnabledEnv, nil
	}

	disabled, disabledErr := allTracingTogglesDisabled()
	if disabledErr != nil {
		return "", fmt.Errorf("all youtube collector tracing disabled: %w", disabledErr)
	}

	if disabled {
		return envload.TracingYouTubeCollectorEnabledEnv, nil
	}

	return "", fmt.Errorf("youtube collector tracing enabled env: %w", err)
}

func allTracingTogglesDisabled() (bool, error) {
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

// tracingEnabledEnvForInstance: instance ID에 대응하는 AP slot OTEL 토글 환경변수 이름이다.
func tracingEnabledEnvForInstance(instanceID string) (string, error) {
	switch strings.TrimPrefix(strings.ToLower(strings.TrimSpace(instanceID)), "youtube-collector-") {
	case "a":
		return envload.TracingYouTubeCollectorAEnabledEnv, nil
	case "b":
		return envload.TracingYouTubeCollectorBEnabledEnv, nil
	case "c":
		return envload.TracingYouTubeCollectorCEnabledEnv, nil
	case "d":
		return envload.TracingYouTubeCollectorDEnabledEnv, nil
	default:
		return "", errors.New("YOUTUBE_COLLECTOR_INSTANCE_ID must be one of a, b, c, d, youtube-collector-a, youtube-collector-b, youtube-collector-c, youtube-collector-d")
	}
}
