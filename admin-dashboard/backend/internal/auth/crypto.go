package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	sessionSigningContext     = "admin-dashboard/session-signing/v1"
	testSessionSigningContext = "admin-dashboard/test-session-signing/v1"
	csrfSigningContext        = "admin-dashboard/csrf-signing/v1"
)

// TestSessionPrefix는 임시 계정 세션의 저장·서명 영역을 일반 관리자와 구분합니다.
const TestSessionPrefix = "test-"

func GenerateSessionID() (string, error) {
	var b [32]byte

	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("read: %w", err)
	}

	return hex.EncodeToString(b[:]), nil
}

// SignSessionID는 ID에 대응하는 서명 영역으로 cookie 값을 만듭니다.
// 임시 계정 영역은 구형 서버가 조회 전용 세션을 일반 관리자로 수용하지 못하게 합니다.
func SignSessionID(sessionID, secret string) string {
	purpose := sessionSigningContext

	if strings.HasPrefix(sessionID, TestSessionPrefix) {
		purpose = testSessionSigningContext
	}

	mac := hmac.New(sha256.New, deriveSigningKey(secret, purpose))

	_, _ = mac.Write([]byte(sessionID))

	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return sessionID + "." + sig
}

// ValidateSessionSignature는 ID의 관리자·임시 계정 서명 영역이 일치할 때만 원래 ID를 반환합니다.
func ValidateSessionSignature(fullID, secret string) (string, bool) {
	sessionID, sig, ok := strings.Cut(fullID, ".")
	if !ok || sessionID == "" || sig == "" {
		return "", false
	}

	expected := SignSessionID(sessionID, secret)
	_, expectedSig, _ := strings.Cut(expected, ".")

	if subtle.ConstantTimeCompare([]byte(sig), []byte(expectedSig)) != 1 {
		return "", false
	}

	return sessionID, true
}

func NewCSRFToken(sessionID, secret string) (string, error) {
	var nonceBytes [32]byte

	if _, err := rand.Read(nonceBytes[:]); err != nil {
		return "", fmt.Errorf("read: %w", err)
	}

	nonce := hex.EncodeToString(nonceBytes[:])
	mac := hmac.New(sha256.New, deriveSigningKey(secret, csrfSigningContext))

	_, _ = mac.Write([]byte(nonce))
	_, _ = mac.Write([]byte(sessionID))

	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return nonce + "." + sig, nil
}

func ValidateCSRFToken(sessionID, token, secret string) bool {
	nonce, sig, ok := strings.Cut(token, ".")
	if !ok || nonce == "" || sig == "" {
		return false
	}

	mac := hmac.New(sha256.New, deriveSigningKey(secret, csrfSigningContext))

	_, _ = mac.Write([]byte(nonce))
	_, _ = mac.Write([]byte(sessionID))

	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) == 1
}

func deriveSigningKey(secret, purpose string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))

	_, _ = mac.Write([]byte(purpose))

	return mac.Sum(nil)
}
