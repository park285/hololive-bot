package collecterr

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestClassPreservesBoundedTypedClass(t *testing.T) {
	err := WrapClass(ParserDrift, "RpcResponseError", errors.New("parser drift"))
	if got := Class(err); got != "RpcResponseError" {
		t.Fatalf("Class() = %q, want RpcResponseError", got)
	}
	if got := Class(WrapClass(Failed, "invalid class", errors.New("failure"))); got != UnknownClass {
		t.Fatalf("Class(invalid) = %q, want %q", got, UnknownClass)
	}
}

func TestSanitizeDetailRedactsCredentialsAndBoundsUTF8(t *testing.T) {
	const secret = "diagnostic-secret"
	detail := "Authorization: Bearer " + secret + " " + strings.Repeat("가", MaxDetailBytes)
	got := SanitizeDetail(detail)
	if strings.Contains(got, secret) {
		t.Fatalf("SanitizeDetail leaked credential: %q", got)
	}
	if len(got) > MaxDetailBytes {
		t.Fatalf("SanitizeDetail length = %d, want <= %d", len(got), MaxDetailBytes)
	}
	if !utf8.ValidString(got) {
		t.Fatal("SanitizeDetail returned invalid UTF-8")
	}

	invalid := SanitizeDetail("prefix \xff\xfe suffix")
	if !utf8.ValidString(invalid) || !strings.Contains(invalid, "�") {
		t.Fatalf("SanitizeDetail invalid UTF-8 result = %q", invalid)
	}
}

func TestDetailUsesCollectorErrorMessage(t *testing.T) {
	err := WrapClass(Failed, "HelperError", errors.New("token=secret-value"))
	if got := Detail(err); got == "" || strings.Contains(got, "secret-value") {
		t.Fatalf("Detail() = %q, want redacted non-empty detail", got)
	}
}
