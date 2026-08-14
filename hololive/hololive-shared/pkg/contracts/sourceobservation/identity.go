package sourceobservation

import "time"

type snapshotIdentityV1 struct {
	Provider     Provider        `json:"provider"`
	Kind         ObservationKind `json:"kind"`
	SubjectKey   string          `json:"subject_key"`
	ScopeSHA256  string          `json:"scope_sha256"`
	ScheduledFor time.Time       `json:"scheduled_for"`
}

type viewerIdentityV1 struct {
	Provider          Provider        `json:"provider"`
	Kind              ObservationKind `json:"kind"`
	SubjectKey        string          `json:"subject_key"`
	ScopeSHA256       string          `json:"scope_sha256"`
	SampleWindowStart time.Time       `json:"sample_window_start"`
}

func SnapshotObservationKey(
	provider Provider,
	kind ObservationKind,
	subjectKey string,
	scopeSHA256 string,
	scheduledFor time.Time,
) string {
	canonical, _ := canonicalJSON(snapshotIdentityV1{
		Provider: provider, Kind: kind, SubjectKey: subjectKey,
		ScopeSHA256: scopeSHA256, ScheduledFor: scheduledFor.UTC(),
	})
	return SHA256Hex(canonical)
}

func ViewerSampleObservationKey(
	provider Provider,
	subjectKey string,
	scopeSHA256 string,
	sampleWindowStart time.Time,
) string {
	canonical, _ := canonicalJSON(viewerIdentityV1{
		Provider: provider, Kind: KindViewerSample, SubjectKey: subjectKey,
		ScopeSHA256: scopeSHA256, SampleWindowStart: sampleWindowStart.UTC(),
	})
	return SHA256Hex(canonical)
}

func ObservationKeyForEnvelope(envelope Envelope, canonicalScope []byte) string {
	scopeSHA256 := SHA256Hex(canonicalScope)
	if envelope.ObservationKind == KindViewerSample {
		var payload ViewerSampleV1
		if err := decodeStrictJSON(envelope.Payload, &payload); err == nil {
			return ViewerSampleObservationKey(
				envelope.Provider,
				envelope.SubjectKey,
				scopeSHA256,
				payload.SampleWindowStart,
			)
		}
	}
	return SnapshotObservationKey(
		envelope.Provider,
		envelope.ObservationKind,
		envelope.SubjectKey,
		scopeSHA256,
		envelope.ScheduledFor,
	)
}
