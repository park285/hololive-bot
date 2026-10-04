package collection

import (
	"errors"
	"fmt"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func ClampLatency(started time.Time) time.Duration {
	latency := min(max(time.Since(started), 0), MaxCollectionLatency)
	return latency
}

func Envelope(
	provider contract.Provider,
	kind contract.ObservationKind,
	subject string,
	generation int64,
	lease *contract.LeaseProof,
	completeness contract.Completeness,
	continuity contract.Continuity,
	payload any,
) (contract.Envelope, error) {
	if lease == nil {
		return contract.Envelope{}, errors.New("build collection envelope: lease is not configured")
	}

	raw, err := contract.MarshalPayloadV1(payload)
	if err != nil {
		return contract.Envelope{}, fmt.Errorf("marshal payload V1: %w", err)
	}

	schemaVersion := contract.SchemaVersionV1

	if kind == contract.KindVideoLiveCheck && generation == contract.VideoLifecycleContractGeneration {
		schemaVersion = contract.VideoLifecycleSchemaVersion
	}

	out, err := contract.PrepareEnvelope(contract.Envelope{
		Provider:           provider,
		ObservationKind:    kind,
		SubjectKey:         subject,
		SchemaVersion:      schemaVersion,
		ContractGeneration: generation,
		ScheduledFor:       lease.ScheduledFor,
		ObservedAt:         time.Now().UTC(),
		Completeness:       completeness,
		Continuity:         continuity,
		Payload:            raw,
		CollectorInstance:  lease.OwnerInstance,
		Lease:              *lease,
	})
	if err != nil {
		return out, fmt.Errorf("prepare envelope: %w", err)
	}

	return out, nil
}
