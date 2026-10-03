package collection

import (
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

// ObservationMetadata는 수집 결과 검증과 metrics에 필요한 값만 담고 가변 payload를 노출하지 않습니다.
type ObservationMetadata struct {
	Provider           contract.Provider
	ObservationKind    contract.ObservationKind
	SubjectKey         string
	ContractGeneration int64
	ScheduledFor       time.Time
	Completeness       contract.Completeness
	Continuity         contract.Continuity
	CollectorInstance  string
	Lease              contract.LeaseProof
}

// ObservationCount는 payload 복사 없이 관측 수를 반환합니다.
func (o RunOutput) ObservationCount() int {
	return len(o.observations)
}

// ObservationMetadata는 [0, ObservationCount()) 범위의 관측 메타데이터를 값으로 반환합니다.
func (o RunOutput) ObservationMetadata(index int) ObservationMetadata {
	envelope := &o.observations[index]

	return ObservationMetadata{
		Provider: envelope.Provider, ObservationKind: envelope.ObservationKind,
		SubjectKey: envelope.SubjectKey, ContractGeneration: envelope.ContractGeneration,
		ScheduledFor: envelope.ScheduledFor, Completeness: envelope.Completeness,
		Continuity: envelope.Continuity, CollectorInstance: envelope.CollectorInstance,
		Lease: envelope.Lease,
	}
}
