package sourceobservation

import (
	"errors"
	"fmt"
	"time"
)

type snapshotIdentityV1 struct {
	Provider     Provider        `json:"provider"`
	Kind         ObservationKind `json:"kind"`
	SubjectKey   string          `json:"subject_key"`
	ScopeSHA256  string          `json:"scope_sha256"`
	ScheduledFor time.Time       `json:"scheduled_for"`
}

func SnapshotObservationKey(
	provider Provider,
	kind ObservationKind,
	subjectKey string,
	scopeSHA256 string,
	scheduledFor time.Time,
) (string, error) {
	canonical, err := canonicalJSON(snapshotIdentityV1{
		Provider: provider, Kind: kind, SubjectKey: subjectKey,
		ScopeSHA256: scopeSHA256, ScheduledFor: scheduledFor.UTC(),
	})
	if err != nil {
		return "", fmt.Errorf("build snapshot observation key: %w", err)
	}

	return SHA256Hex(canonical), nil
}

func ObservationKeyForEnvelope(envelope *Envelope, canonicalScope []byte) (string, error) {
	if envelope == nil {
		return "", errors.New("build observation key: envelope is nil")
	}

	scopeSHA256 := SHA256Hex(canonicalScope)

	out, err := SnapshotObservationKey(
		envelope.Provider,
		envelope.ObservationKind,
		envelope.SubjectKey,
		scopeSHA256,
		envelope.ScheduledFor,
	)
	if err != nil {
		return out, fmt.Errorf("snapshot observation key: %w", err)
	}

	return out, nil
}
