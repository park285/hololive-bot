package settings

import "errors"

// ValidateRuntimeEnvSyntax는 Iris egress runtime이 공통으로 받는 env 구획의 값 형식을 한 번에 검사한다.
// 각 runtime config는 자기가 소비하는 값만 보관하지만, 소비하지 않는 공통 구획의 잘못된 숫자·bool 값도 예전처럼
// 기동 실패로 드러나야 하므로 공유 parser를 그대로 다시 써서 값은 버리고 오류만 합쳐 돌려준다.
// 범위·필수 검증은 runtime이 소비하는 구획에 대해서만 각 Validate* 정책으로 따로 수행한다.
func ValidateRuntimeEnvSyntax() error {
	_, serverErr := LoadServerConfig()
	_, holodexErr := LoadHolodexConfig()
	_, valkeyErr := LoadValkeyConfig()
	_, postgresErr := LoadPostgresConfig()
	_, notificationErr := LoadNotificationConfig()
	_, loggingErr := LoadLoggingConfig()
	_, botErr := LoadBotConfig()
	_, cliproxyErr := LoadCliproxyConfig()
	_, llmErr := LoadLLMConfig()
	_, exaErr := LoadExaConfig()
	_, officialScheduleErr := LoadOfficialScheduleRuntimeConfig()
	// CORS_ENFORCE 기본값은 형식 검사 결과에 영향을 주지 않는다.
	_, corsErr := LoadCORSConfig(false)
	_, ingestionErr := LoadIngestionConfig()

	return errors.Join(
		serverErr, holodexErr, valkeyErr, postgresErr, notificationErr, loggingErr, botErr,
		cliproxyErr, llmErr, exaErr, officialScheduleErr, corsErr, ingestionErr,
	)
}
