package durability

import (
	"errors"
	"strings"
	"testing"
)

func TestMessageRepositoryErrorPreservesCauseWithoutExposingRawIdentity(t *testing.T) {
	const rawID = "message:raw-private-sentinel"

	cause := errors.New("database rejected " + rawID)
	err := safeMessageRepositoryError("heartbeat webhook inbox row", rawID, cause)

	if errors.Is(err, cause) == false {
		t.Fatal("safe repository error did not preserve its cause")
	}

	if strings.Contains(err.Error(), rawID) {
		t.Fatalf("safe repository error exposed raw message identity: %s", err)
	}

	if !strings.Contains(err.Error(), "message_token=anon:") || !strings.Contains(err.Error(), "reason=database_operation_failed") {
		t.Fatalf("safe repository error omitted bounded diagnostics: %s", err)
	}
}

func TestRepositoryErrorDoesNotExposeLowTrustCauseText(t *testing.T) {
	const sentinel = "raw-private-sentinel"

	err := safeRepositoryError("claim webhook inbox row", errors.New(sentinel))

	if strings.Contains(err.Error(), sentinel) {
		t.Fatalf("safe repository error exposed low-trust cause: %s", err)
	}

	if !strings.Contains(err.Error(), "reason=database_operation_failed") {
		t.Fatalf("safe repository error omitted bounded reason: %s", err)
	}
}
