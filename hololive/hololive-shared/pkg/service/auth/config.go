package auth

import "time"

// Config: 인증 서비스 동작 파라미터
type Config struct {
	// SessionTTL: 세션 토큰 유효 기간 (기본 7일)
	SessionTTL time.Duration
	// ResetTokenTTL: 비밀번호 재설정 토큰 유효 기간
	ResetTokenTTL time.Duration

	// LoginRateLimitPerMinute: IP 기준 로그인 요청 제한 (분당)
	LoginRateLimitPerMinute int64
	// PasswordResetRequestRateLimitPerMinute: IP 기준 비밀번호 재설정 요청 제한 (분당)
	PasswordResetRequestRateLimitPerMinute int64

	// LoginFailLimit: 이메일 기준 연속 실패 허용 횟수
	LoginFailLimit int64
	// LoginFailWindow: 실패 카운트 집계 윈도우
	LoginFailWindow time.Duration
	// LoginLockDuration: 계정 잠금 지속 시간
	LoginLockDuration time.Duration

	// UserSessionsTTL: 유저별 세션 인덱스(Set) TTL
	UserSessionsTTL time.Duration

	// AutoPrepareSchema: 런타임에서 인증 관련 테이블 생성 여부
	AutoPrepareSchema bool
}

func DefaultConfig() Config {
	sessionTTL := 7 * 24 * time.Hour
	return Config{
		SessionTTL:                             sessionTTL,
		ResetTokenTTL:                          60 * time.Minute,
		LoginRateLimitPerMinute:                30,
		PasswordResetRequestRateLimitPerMinute: 10,
		LoginFailLimit:                         5,
		LoginFailWindow:                        15 * time.Minute,
		LoginLockDuration:                      15 * time.Minute,
		UserSessionsTTL:                        sessionTTL + (24 * time.Hour),
		AutoPrepareSchema:                      true,
	}
}
