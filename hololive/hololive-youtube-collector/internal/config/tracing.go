package config

import (
	"errors"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/config/envload"
	"github.com/kapu/hololive-shared/pkg/config/settings"
)

// loadTracingConfig: AP slot(a/b/c/d)에 대응하는 OTEL 토글을 골라 공통 tracing loader로 읽는다.
// 표준 OTLP endpoint env 거부가 instance ID 오류보다 먼저 보이도록 slot 해석 전에 확인한다.
func loadTracingConfig(instanceID string) (settings.TracingConfig, error) {
	if err := settings.RejectStandardOTLPEndpointEnv(); err != nil {
		return settings.TracingConfig{}, fmt.Errorf("reject standard OTLP endpoint env: %w", err)
	}

	enabledEnv, err := tracingEnabledEnvForInstance(instanceID)
	if err != nil {
		return settings.TracingConfig{}, fmt.Errorf("tracing enabled env: %w", err)
	}

	config, err := settings.LoadTracingConfig(enabledEnv)
	if err != nil {
		return settings.TracingConfig{}, fmt.Errorf("load shared tracing config: %w", err)
	}

	return config, nil
}

// youtubeCollectorInstanceTracingEnv는 PROJECT_MAP이 닫은 collector slot 네 개와 slot별 OTEL 토글을 잇는다.
// 이 표 밖의 instance ID는 lease owner 표시와 tracing slot 정책을 우회하므로 기동 오류다.
var youtubeCollectorInstanceTracingEnv = map[string]string{
	"youtube-collector-a": envload.TracingYouTubeCollectorAEnabledEnv,
	"youtube-collector-b": envload.TracingYouTubeCollectorBEnabledEnv,
	"youtube-collector-c": envload.TracingYouTubeCollectorCEnabledEnv,
	"youtube-collector-d": envload.TracingYouTubeCollectorDEnabledEnv,
}

// tracingEnabledEnvForInstance: instance ID에 대응하는 AP slot OTEL 토글 환경변수 이름이다.
func tracingEnabledEnvForInstance(instanceID string) (string, error) {
	enabledEnv, ok := youtubeCollectorInstanceTracingEnv[instanceID]
	if !ok {
		return "", errors.New("YOUTUBE_COLLECTOR_INSTANCE_ID must be one of youtube-collector-a, youtube-collector-b, youtube-collector-c, youtube-collector-d")
	}

	return enabledEnv, nil
}
