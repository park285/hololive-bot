package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"strings"

	"github.com/park285/shared-go/pkg/httputil"
	"github.com/park285/shared-go/pkg/stringutil"
)

func GenerateSessionID() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func SignSessionID(sessionID, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(sessionID))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return sessionID + "." + sig
}

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
		return "", err
	}
	nonce := hex.EncodeToString(nonceBytes[:])
	mac := hmac.New(sha256.New, []byte(secret))
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
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(nonce))
	_, _ = mac.Write([]byte(sessionID))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) == 1
}

func ConstantTimeStringEqual(left, right string) bool {
	return httputil.ConstantTimeStringEqual(left, right)
}

func TruncateSessionID(sessionID string) string {
	return stringutil.TruncateString(sessionID, 8)
}
