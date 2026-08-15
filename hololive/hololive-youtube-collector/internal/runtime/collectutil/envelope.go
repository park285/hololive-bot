package collectutil

import (
	"sort"
	"strings"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
)

func ClampLatency(started time.Time) time.Duration {
	latency := max(time.Since(started), 0)
	if latency > sourceobservation.MaxCollectionLatency {
		latency = sourceobservation.MaxCollectionLatency
	}
	return latency
}

func Checkpoint(envelope contract.Envelope) sourceobservation.CheckpointEntry {
	return sourceobservation.CheckpointEntry{
		Provider:           envelope.Provider,
		ObservationKind:    envelope.ObservationKind,
		SubjectKey:         envelope.SubjectKey,
		ScopeSHA256:        envelope.ScopeSHA256,
		ContractGeneration: envelope.ContractGeneration,
		LastObservationKey: envelope.ObservationKey,
		LastEvidenceSHA256: envelope.EvidenceSHA256,
		LastScheduledFor:   envelope.ScheduledFor,
		Continuity:         envelope.Continuity,
	}
}

func Envelope(
	provider contract.Provider,
	kind contract.ObservationKind,
	subject string,
	generation int64,
	lease contract.LeaseProof,
	completeness contract.Completeness,
	continuity contract.Continuity,
	payload any,
) (contract.Envelope, error) {
	raw, err := contract.MarshalPayloadV1(payload)
	if err != nil {
		return contract.Envelope{}, err
	}
	return contract.PrepareEnvelope(contract.Envelope{
		Provider:           provider,
		ObservationKind:    kind,
		SubjectKey:         subject,
		SchemaVersion:      contract.SchemaVersionV1,
		ContractGeneration: generation,
		ScheduledFor:       lease.ScheduledFor,
		ObservedAt:         time.Now().UTC(),
		Completeness:       completeness,
		Continuity:         continuity,
		Payload:            raw,
		CollectorInstance:  lease.OwnerInstance,
		Lease:              lease,
	})
}

func UniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	sort.Strings(result)
	return result
}
