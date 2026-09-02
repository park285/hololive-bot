package load

// OTEL 관련 환경변수 이름이다. 이 이름들은 core tracing 로더와 plane 런타임 테스트가 함께 보므로 여기서 소유한다.
const (
	HololiveOTLPGRPCEndpointEnv = "HOLOLIVE_OTLP_GRPC_ENDPOINT"
	OTLPEndpointEnv             = "OTEL_EXPORTER_OTLP_ENDPOINT"
	OTLPTracesEndpointEnv       = "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"
	OTLPInsecureEnv             = "OTEL_EXPORTER_OTLP_INSECURE"
	OTELSampleRateEnv           = "OTEL_SAMPLE_RATE"
)

const (
	TracingHololiveAPIEnabledEnv       = "OTEL_HOLOLIVE_API_ENABLED"
	TracingAlarmWorkerEnabledEnv       = "OTEL_HOLOLIVE_ALARM_WORKER_ENABLED"
	TracingYouTubeCollectorAEnabledEnv = "OTEL_YOUTUBE_COLLECTOR_A_ENABLED"
	TracingYouTubeCollectorBEnabledEnv = "OTEL_YOUTUBE_COLLECTOR_B_ENABLED"
	TracingYouTubeCollectorCEnabledEnv = "OTEL_YOUTUBE_COLLECTOR_C_ENABLED"
	TracingYouTubeCollectorDEnabledEnv = "OTEL_YOUTUBE_COLLECTOR_D_ENABLED"
	TracingYouTubeCollectorEnabledEnv  = "OTEL_YOUTUBE_COLLECTOR_ENABLED"
)

// TracingEnabledEnvKeys: 런타임별 OTEL 토글 환경변수 전체 목록이다.
func TracingEnabledEnvKeys() []string {
	return []string{
		TracingHololiveAPIEnabledEnv,
		TracingAlarmWorkerEnabledEnv,
		TracingYouTubeCollectorAEnabledEnv,
		TracingYouTubeCollectorBEnabledEnv,
		TracingYouTubeCollectorCEnabledEnv,
		TracingYouTubeCollectorDEnabledEnv,
		TracingYouTubeCollectorEnabledEnv,
	}
}
