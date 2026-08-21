package auth

import (
	"bytes"
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
