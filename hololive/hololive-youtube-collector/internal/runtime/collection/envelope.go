package collection

import (
	"errors"
	"fmt"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
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
		return contract.Envelope{}, payloadFailure(errors.New("build collection envelope: lease is not configured"))
	}

	raw, err := contract.MarshalPayloadV1(payload)
	if err != nil {
		return contract.Envelope{}, payloadFailure(fmt.Errorf("marshal payload V1: %w", err))
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
		return out, payloadFailure(fmt.Errorf("prepare envelope: %w", err))
	}

	return out, nil
}

// payloadFailure는 저장 크기 초과를 parser drift와 구별해 기존 bounded DEFER 정책으로 보낸다.
func payloadFailure(err error) error {
	if errors.Is(err, contract.ErrPayloadTooLarge) {
		return collecterr.Wrap(contract.ErrorResponseTooLarge, contract.ClassResourceLimit, err)
	}

	return collecterr.Wrap(contract.ErrorParserDrift, contract.ClassDataContract, err)
}
