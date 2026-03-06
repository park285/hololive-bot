package config

import "time"

// TelemetryConfig: OpenTelemetry 분산 추적 설정
type TelemetryConfig struct {
	Enabled               bool          // 트레이싱 활성화 여부
	MetricsEnabled        bool          // OTel metrics export 활성화 여부 (Prometheus와 병행 가능)
	MetricsExportInterval time.Duration // OTel metrics export 주기
	ServiceName           string        // 서비스 식별자 (ex "hololive-bot")
	ServiceVersion        string        // 서비스 버전 (ex "1.0.0")
	Environment           string        // 배포 환경 (ex "production")
	OTLPEndpoint          string        // OTLP collector 주소 (ex "otel-collector:4317")
	OTLPInsecure          bool          // TLS 없이 연결 (내부망 전용)
	SampleRate            float64       // 샘플링 비율 (0.0 ~ 1.0)
}
