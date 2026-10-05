package settings

import (
	"errors"
	"fmt"
)

// RejectRetiredRuntimeEnv는 Iris egress runtime(hololive-api bot·admin plane, alarm-worker)이 설정 구획을 읽기 전에 거절하는
// 퇴역 env 가드를 차례로 실행한다. 각 가드의 제거 조건과 재검토 기한은 해당 config_*_retired_env.go 주석이 소유한다.
// 런타임 설정 조립은 각 runtime config 패키지가 소유하고, 이 함수는 공통 거절 정책만 제공한다.
func RejectRetiredRuntimeEnv() error {
	if err := RejectRetiredLLMEnv(); err != nil {
		return fmt.Errorf("reject retired LLM env: %w", err)
	}

	if err := rejectRetiredHolodexLiveStatusFallbackEnv(); err != nil {
		return fmt.Errorf("reject retired holodex live-status fallback env: %w", err)
	}

	if err := rejectRetiredScraperFetchEnv(); err != nil {
		return fmt.Errorf("reject retired scraper fetch env: %w", err)
	}

	if err := rejectRetiredScraperConfigEnv(); err != nil {
		return fmt.Errorf("reject retired scraper config env: %w", err)
	}

	if err := rejectRetiredIngestionEnv(); err != nil {
		return fmt.Errorf("reject retired ingestion env: %w", err)
	}

	if err := rejectRetiredIrisEnv(); err != nil {
		return fmt.Errorf("reject retired iris env: %w", err)
	}

	if err := rejectRetiredWebhookEnv(); err != nil {
		return fmt.Errorf("reject retired webhook env: %w", err)
	}

	if err := RejectRetiredOutboxV3HandoffEnv(); err != nil {
		return fmt.Errorf("reject retired outbox v3 handoff env: %w", err)
	}

	if err := rejectRetiredRateLimiterInstanceIDEnv(); err != nil {
		return fmt.Errorf("reject retired rate limiter instance id env: %w", err)
	}

	// SERVICES_* 구획은 admin plane만 읽지만, 퇴역 키 거절은 예전처럼 모든 egress runtime 기동에서 유지한다.
	if err := rejectRetiredServicesEnv(); err != nil {
		return fmt.Errorf("reject retired services env: %w", err)
	}

	return nil
}

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
