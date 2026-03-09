// Package middleware: HTTP 미들웨어
package middleware

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/auth"
)

const (
	csrfCookieName = "csrf_token"
	csrfHeaderName = "X-CSRF-Token"
)

// NewCSRFToken: 세션에 바인딩된 CSRF 토큰 생성
//
// 포맷: nonceHex.signature
// - nonceHex: 32바이트 랜덤 값을 hex 인코딩 (64자)
// - signature: HMAC-SHA256("csrf:" + sessionID + ":" + nonceHex, secret) 를 base64url 인코딩
func NewCSRFToken(sessionID, secret string) string {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		// crypto/rand 실패는 매우 예외적이며, 호출자는 빈 문자열을 실패로 처리한다.
		return ""
	}

	nonceHex := hex.EncodeToString(nonce)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("csrf:" + sessionID + ":" + nonceHex))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return nonceHex + "." + signature
}

// ValidateCSRFToken: 세션 바인딩된 CSRF 토큰 검증
func ValidateCSRFToken(sessionID, token, secret string) bool {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return false
	}

	nonceHex, providedSig := parts[0], parts[1]
	decodedNonce, err := hex.DecodeString(nonceHex)
	if err != nil || len(decodedNonce) != 32 {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("csrf:" + sessionID + ":" + nonceHex))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(providedSig), []byte(expectedSig))
}

// SetCSRFCookie: CSRF 토큰 쿠키 설정
// - HttpOnly=false (프론트엔드가 JS로 읽어 헤더에 실어야 함)
// - SameSite=Strict
// - Secure: TLS 감지 또는 forceHTTPS
func SetCSRFCookie(c *gin.Context, token string, forceHTTPS bool) {
	isSecure := c.Request.TLS != nil || forceHTTPS
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(csrfCookieName, token, 0, "/", "", isSecure, false)
}

// ClearCSRFCookie: CSRF 토큰 쿠키 삭제
func ClearCSRFCookie(c *gin.Context, forceHTTPS bool) {
	isSecure := c.Request.TLS != nil || forceHTTPS
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(csrfCookieName, "", -1, "/", "", isSecure, false)
}

// CSRFProtection: Signed Double Submit Cookie 패턴 기반 CSRF 방어
//
// 동작:
// - POST/PUT/DELETE 요청만 검사
// - X-CSRF-Token 헤더 값과 csrf_token 쿠키 값이 일치해야 함
// - 토큰 서명이 유효해야 함 (세션 바인딩)
// - sessionID는 admin_session 쿠키에서 추출
//
// 3상태 모드:
// - enforce: 검증 실패 시 403 반환
// - monitor: 검증 실패 시 로그만 남기고 허용
// - off: 검증 건너뛰기
func CSRFProtection(secret string) gin.HandlerFunc {
	return CSRFProtectionWithMode(secret, "enforce", nil)
}

// validateCSRFRequest: CSRF 요청 검증 헬퍼
func validateCSRFRequest(c *gin.Context, secret string) string {
	headerToken := c.GetHeader(csrfHeaderName)
	if headerToken == "" {
		return "missing_header_token"
	}

	cookieToken, err := c.Cookie(csrfCookieName)
	if err != nil || cookieToken == "" {
		return "missing_cookie_token"
	}

	if headerToken != cookieToken {
		return "token_mismatch"
	}

	signedSessionID, err := c.Cookie(auth.SessionCookieName)
	if err != nil || signedSessionID == "" {
		return "missing_session"
	}

	sessionID, ok := auth.ValidateSessionSignature(signedSessionID, secret)
	if !ok {
		return "invalid_session_signature"
	}

	if !ValidateCSRFToken(sessionID, headerToken, secret) {
		return "invalid_csrf_token"
	}

	return ""
}

// CSRFProtectionWithMode: 모드 지정 CSRF 보호 미들웨어
func CSRFProtectionWithMode(secret, mode string, logger interface{ Warn(msg string, args ...any) }) gin.HandlerFunc {
	return func(c *gin.Context) {
		method := c.Request.Method
		if method != http.MethodPost && method != http.MethodPut && method != http.MethodDelete {
			c.Next()
			return
		}

		// off 모드: 검증 건너뛰기
		if mode == "off" {
			c.Next()
			return
		}

		// 검증 실패 시 호출되는 헬퍼
		handleViolation := func(reason string) bool {
			if mode == "monitor" {
				if logger != nil {
					logger.Warn("csrf_violation",
						"reason", reason,
						"path", c.Request.URL.Path,
						"method", method,
						"mode", "monitor",
					)
				}
				c.Next()
				return true
			}
			c.AbortWithStatus(http.StatusForbidden)
			return false
		}

		reason := validateCSRFRequest(c, secret)
		if reason != "" {
			if handleViolation(reason) {
				return
			}
			return
		}

		c.Next()
	}
}
