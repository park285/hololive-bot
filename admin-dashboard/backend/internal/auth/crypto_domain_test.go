package auth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
)

func TestSigningSubkeysArePurposeSeparated(t *testing.T) {
	secret := "0123456789abcdef0123456789abcdef"
	if bytes.Equal(
		deriveSigningKey(secret, sessionSigningContext),
		deriveSigningKey(secret, csrfSigningContext),
	) {
		t.Fatal("session and CSRF signing subkeys must differ")
	}
}

func TestTemporarySessionCannotAuthenticateWithPreviousServerSigningDomain(t *testing.T) {
	secret := "synthetic-signing-key-with-at-least-32-bytes"
	id := TestSessionPrefix + strings.Repeat("a", 64)
	cookie := SignSessionID(id, secret)
	_, signature, _ := strings.Cut(cookie, ".")
	oldMAC := hmac.New(sha256.New, deriveSigningKey(secret, sessionSigningContext))

	_, _ = oldMAC.Write([]byte(id))

	oldSignature := base64.RawURLEncoding.EncodeToString(oldMAC.Sum(nil))

	if signature == oldSignature {
		t.Fatal("구형 서버에서 임시 세션을 일반 관리자로 받아들일 수 있습니다")
	}

	if _, ok := ValidateSessionSignature(id+"."+oldSignature, secret); ok {
		t.Fatal("일반 관리자 서명을 임시 세션에 사용할 수 없습니다")
	}

	if actual, ok := ValidateSessionSignature(cookie, secret); !ok || actual != id {
		t.Fatal("임시 세션 자체의 서명 검증이 실패했습니다")
	}

	primary := strings.Repeat("b", 64)
	if actual, ok := ValidateSessionSignature(SignSessionID(primary, secret), secret); !ok || actual != primary {
		t.Fatal("일반 관리자 서명 계약이 달라졌습니다")
	}
}
