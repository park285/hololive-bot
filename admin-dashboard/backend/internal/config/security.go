// Package config: 보안 설정 관리
package config

import (
	"log/slog"
	"os"
	"strings"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/envutil"
)

// SecurityMode: Feature Flag 3상태 모드
// - enforce: 위반 시 차단
// - monitor: 위반 시 로그만 남기고 허용
// - off: 검증 건너뛰기 (개발용)
type SecurityMode string

const (
	SecurityModeEnforce SecurityMode = "enforce"
	SecurityModeMonitor SecurityMode = "monitor"
	SecurityModeOff     SecurityMode = "off"
)

// ParseSecurityMode: 문자열을 SecurityMode로 변환
// 알 수 없는 값은 기본값 enforce로 처리 (보안 원칙: fail-closed)
func ParseSecurityMode(s string) SecurityMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "enforce":
		return SecurityModeEnforce
	case "monitor":
		return SecurityModeMonitor
	case "off":
		return SecurityModeOff
	default:
		return SecurityModeEnforce
	}
}

// SecurityConfig: 보안 관련 환경변수 설정
type SecurityConfig struct {
	// Origin 설정
	AllowedOrigins       []string // ALLOWED_ORIGINS (comma-separated)
	AllowLocalhostInProd bool     // ALLOW_LOCALHOST_IN_PROD (기본: false)

	// Feature Flag 모드
	CSRFMode        SecurityMode // CSRF_MODE (기본: enforce)
	WSOriginMode    SecurityMode // WS_ORIGIN_MODE (기본: enforce)
	StreamLimitMode SecurityMode // STREAM_LIMIT_MODE (기본: enforce)

	// 스트림 제한
	GlobalStreamLimit     int64 // GLOBAL_STREAM_LIMIT (기본: 10)
	PerSessionStreamLimit int   // PER_SESSION_STREAM_LIMIT (기본: 2)

	// fallback 사용 여부 (환경변수 미설정 시)
	usingOriginFallback bool
}

// LoadSecurityConfig: 환경 변수에서 보안 설정 로드
//
// 설계 원칙:
// 1. 환경변수 미설정 시 fallback 값 사용 + 경고 로그
// 2. 프로덕션에서 localhost 포함 시 경고 + 무시
// 3. 모드 파싱 실패 시 기본값 enforce (fail-closed)
func LoadSecurityConfig(env string, logger *slog.Logger) *SecurityConfig {
	cfg := &SecurityConfig{
		// 기본값: 모드는 1차 배포를 위해 monitor로 (계획상)
		// 하지만 코드 기본값은 enforce (환경변수로 monitor 설정)
		CSRFMode:        ParseSecurityMode(envutil.String("CSRF_MODE", "enforce")),
		WSOriginMode:    ParseSecurityMode(envutil.String("WS_ORIGIN_MODE", "enforce")),
		StreamLimitMode: ParseSecurityMode(envutil.String("STREAM_LIMIT_MODE", "enforce")),

		GlobalStreamLimit:     envutil.Int64("GLOBAL_STREAM_LIMIT", 10),
		PerSessionStreamLimit: envutil.Int("PER_SESSION_STREAM_LIMIT", 2),

		AllowLocalhostInProd: envutil.Bool("ALLOW_LOCALHOST_IN_PROD", false),
	}

	// Origin 설정 파싱
	cfg.AllowedOrigins, cfg.usingOriginFallback = parseAllowedOrigins(env, cfg.AllowLocalhostInProd, logger)

	// 부팅 시 현재 모드 로그 출력
	if logger != nil {
		logger.Info("security_config_loaded",
			slog.String("csrf_mode", string(cfg.CSRFMode)),
			slog.String("ws_origin_mode", string(cfg.WSOriginMode)),
			slog.String("stream_limit_mode", string(cfg.StreamLimitMode)),
			slog.Int64("global_stream_limit", cfg.GlobalStreamLimit),
			slog.Int("per_session_stream_limit", cfg.PerSessionStreamLimit),
			slog.Bool("using_origin_fallback", cfg.usingOriginFallback),
			slog.Int("allowed_origins_count", len(cfg.AllowedOrigins)),
		)
	}

	return cfg
}

// parseAllowedOrigins: ALLOWED_ORIGINS 환경변수 파싱
//
// 반환값: (origins, usingFallback)
// - 환경변수 미설정 시 fallback 목록 반환 + usingFallback=true
// - 프로덕션에서 localhost 포함 시 경고 + 해당 항목 제외 (ALLOW_LOCALHOST_IN_PROD=true가 아닌 한)
func parseAllowedOrigins(env string, allowLocalhostInProd bool, logger *slog.Logger) ([]string, bool) {
	originsEnv := os.Getenv("ALLOWED_ORIGINS")
	isProd := strings.EqualFold(env, "production")

	// fallback: 기존 하드코딩 값
	fallbackOrigins := []string{
		"https://admin.capu.blog",
		"http://localhost:5173",
	}

	if originsEnv == "" {
		if logger != nil {
			logger.Warn("allowed_origins_not_set",
				slog.String("reason", "ALLOWED_ORIGINS 환경변수 미설정, fallback 사용"),
				slog.Any("fallback_origins", fallbackOrigins),
			)
		}
		// 프로덕션에서 fallback 사용 시에도 localhost 필터링
		if isProd && !allowLocalhostInProd {
			return filterLocalhostOrigins(fallbackOrigins, logger), true
		}
		return fallbackOrigins, true
	}

	// 환경변수에서 파싱
	rawOrigins := strings.Split(originsEnv, ",")
	origins := make([]string, 0, len(rawOrigins))
	for _, origin := range rawOrigins {
		normalized := normalizeOrigin(origin)
		if normalized == "" {
			continue
		}
		origins = append(origins, normalized)
	}

	if len(origins) == 0 {
		if logger != nil {
			logger.Warn("allowed_origins_empty_after_parse",
				slog.String("reason", "ALLOWED_ORIGINS 파싱 결과 빈 목록, fallback 사용"),
			)
		}
		if isProd && !allowLocalhostInProd {
			return filterLocalhostOrigins(fallbackOrigins, logger), true
		}
		return fallbackOrigins, true
	}

	// 프로덕션에서 localhost 필터링
	if isProd && !allowLocalhostInProd {
		return filterLocalhostOrigins(origins, logger), false
	}

	return origins, false
}

// normalizeOrigin: Origin 문자열 정규화
// - 앞뒤 공백 제거
// - 후행 슬래시 제거
func normalizeOrigin(origin string) string {
	origin = strings.TrimSpace(origin)
	origin = strings.TrimSuffix(origin, "/")
	return origin
}

// filterLocalhostOrigins: localhost Origin 필터링 (프로덕션용)
func filterLocalhostOrigins(origins []string, logger *slog.Logger) []string {
	filtered := make([]string, 0, len(origins))
	for _, origin := range origins {
		if isLocalhostOrigin(origin) {
			if logger != nil {
				logger.Warn("localhost_origin_rejected",
					slog.String("origin", origin),
					slog.String("reason", "프로덕션 환경에서 localhost Origin 거부"),
				)
			}
			continue
		}
		filtered = append(filtered, origin)
	}
	return filtered
}

// isLocalhostOrigin: localhost 관련 Origin인지 확인
func isLocalhostOrigin(origin string) bool {
	lower := strings.ToLower(origin)
	return strings.Contains(lower, "localhost") ||
		strings.Contains(lower, "127.0.0.1") ||
		strings.Contains(lower, "[::1]")
}

// UsingOriginFallback: 환경변수 미설정으로 fallback 사용 중인지 반환
func (c *SecurityConfig) UsingOriginFallback() bool {
	return c.usingOriginFallback
}

// AllowedOriginsMap: map[string]struct{} 형태로 반환 (빠른 조회용)
func (c *SecurityConfig) AllowedOriginsMap() map[string]struct{} {
	m := make(map[string]struct{}, len(c.AllowedOrigins))
	for _, origin := range c.AllowedOrigins {
		m[origin] = struct{}{}
	}
	return m
}
