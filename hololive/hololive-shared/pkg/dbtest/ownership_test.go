package dbtest

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateOwnershipEvidence(t *testing.T) {
	t.Run("rejects missing proof", func(t *testing.T) {
		err := validateOwnershipEvidence("", "", errors.New("sentinel missing"), false)
		if err == nil || !strings.Contains(err.Error(), "unproven database ownership") {
			t.Fatalf("validateOwnershipEvidence() error = %v, want unproven ownership", err)
		}
	})

	t.Run("allows explicit disposable external server", func(t *testing.T) {
		if err := validateOwnershipEvidence("", "", errors.New("sentinel missing"), true); err != nil {
			t.Fatalf("validateOwnershipEvidence() error = %v, want nil", err)
		}
	})

	t.Run("accepts matching sentinel", func(t *testing.T) {
		if err := validateOwnershipEvidence("owner-token", "owner-token", nil, false); err != nil {
			t.Fatalf("validateOwnershipEvidence() error = %v, want nil", err)
		}
	})

	t.Run("rejects sentinel read failure", func(t *testing.T) {
		err := validateOwnershipEvidence("owner-token", "", errors.New("relation does not exist"), false)
		if err == nil || !strings.Contains(err.Error(), "read ownership sentinel") {
			t.Fatalf("validateOwnershipEvidence() error = %v, want sentinel read failure", err)
		}
	})

	t.Run("rejects sentinel mismatch", func(t *testing.T) {
		err := validateOwnershipEvidence("owner-token", "other-token", nil, false)
		if err == nil || !strings.Contains(err.Error(), "sentinel mismatch") {
			t.Fatalf("validateOwnershipEvidence() error = %v, want sentinel mismatch", err)
		}
	})
}

func TestProvisionBaseDSNRejectsUnprovenPreset(t *testing.T) {
	t.Setenv(testDatabaseURLEnv, "postgres://example.invalid/test")
	t.Setenv(testDatabaseOwnerTokenEnv, "")
	t.Setenv(allowExternalTestDBEnv, "")

	dsn, err := provisionBaseDSN()
	if err == nil || !strings.Contains(err.Error(), "unproven database ownership") {
		t.Fatalf("provisionBaseDSN() = %q, %v, want unproven ownership", dsn, err)
	}
}

func TestProvisionBaseDSNAllowsExplicitDisposableServer(t *testing.T) {
	const dsn = "postgres://example.invalid/test"
	t.Setenv(testDatabaseURLEnv, dsn)
	t.Setenv(testDatabaseOwnerTokenEnv, "")
	t.Setenv(allowExternalTestDBEnv, "true")

	got, err := provisionBaseDSN()
	if err != nil || got != dsn {
		t.Fatalf("provisionBaseDSN() = %q, %v, want %q, nil", got, err, dsn)
	}
}
