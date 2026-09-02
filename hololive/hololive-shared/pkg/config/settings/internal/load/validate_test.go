package load

import "testing"

func TestIsValidPostgresSSLMode(t *testing.T) {
	t.Parallel()

	valid := []string{"disable", "allow", "prefer", "require", "verify-ca", PostgresSSLModeVerifyFull}
	for _, mode := range valid {
		if !isValidPostgresSSLMode(mode) {
			t.Fatalf("isValidPostgresSSLMode(%q) = false, want true", mode)
		}
	}

	if isValidPostgresSSLMode("invalid") {
		t.Fatal("isValidPostgresSSLMode(\"invalid\") = true, want false")
	}
}
