package auth

import (
	"testing"

	"github.com/park285/shared-go/pkg/httputil"
)

func TestSessionSignatureRoundTrip(t *testing.T) {
	signed := SignSessionID("abcdef123456", "secret-key")
	id, ok := ValidateSessionSignature(signed, "secret-key")
	if !ok || id != "abcdef123456" {
		t.Fatalf("signature validation failed: %s %v", id, ok)
	}
	if _, ok := ValidateSessionSignature(signed, "wrong"); ok {
		t.Fatal("wrong secret must fail")
	}
}

func TestCSRFTokenRoundTrip(t *testing.T) {
	token, err := NewCSRFToken("session", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if !ValidateCSRFToken("session", token, "secret") {
		t.Fatal("valid token rejected")
	}
	if ValidateCSRFToken("other", token, "secret") {
		t.Fatal("wrong session accepted")
	}
}

func TestConstantTimeStringEqual(t *testing.T) {
	if !httputil.ConstantTimeStringEqual("admin", "admin") {
		t.Fatal("same value must match")
	}
	if httputil.ConstantTimeStringEqual("admin", "Admin") {
		t.Fatal("different value must fail")
	}
}
