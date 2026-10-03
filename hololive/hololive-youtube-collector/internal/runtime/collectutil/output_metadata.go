package collectutil

import (
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

// ObservationMetadata는 수집 결과 검증과 metrics에 필요한 값만 담고 가변 payload를 노출하지 않습니다.
type ObservationMetadata struct {
	Provider           contract.Provider
	ObservationKind    contract.ObservationKind
	SubjectKey         string
	ObservationKey     string
	ContractGeneration int64
	ScheduledFor       time.Time
	ScopeSHA256        string
	Completeness       contract.Completeness
	Continuity         contract.Continuity
	EvidenceSHA256     string
	CollectorInstance  string
	Lease              contract.LeaseProof
}

// CheckpointMetadata는 관측과의 결합 검증에 필요한 값만 담고 가변 cursor를 노출하지 않습니다.
type CheckpointMetadata struct {
	Provider           contract.Provider
	ObservationKind    contract.ObservationKind
	SubjectKey         string
	ScopeSHA256        string
	ContractGeneration int64
	LastObservationKey string
	LastEvidenceSHA256 string
	LastScheduledFor   time.Time
	Continuity         contract.Continuity
}

// ObservationCount는 payload 복사 없이 관측 수를 반환합니다.
func (o RunOutput) ObservationCount() int {
	return len(o.observations)
}

// CheckpointCount는 cursor 복사 없이 체크포인트 수를 반환합니다.
func (o RunOutput) CheckpointCount() int {
	return len(o.checkpoints)
}

// ObservationMetadata는 [0, ObservationCount()) 범위의 관측 메타데이터를 값으로 반환합니다.
func (o RunOutput) ObservationMetadata(index int) ObservationMetadata {
	envelope := &o.observations[index]

	return ObservationMetadata{
		Provider: envelope.Provider, ObservationKind: envelope.ObservationKind,
		SubjectKey: envelope.SubjectKey, ObservationKey: envelope.ObservationKey,
		ContractGeneration: envelope.ContractGeneration, ScheduledFor: envelope.ScheduledFor,
		ScopeSHA256: envelope.ScopeSHA256, Completeness: envelope.Completeness,
		Continuity: envelope.Continuity, EvidenceSHA256: envelope.EvidenceSHA256,
		CollectorInstance: envelope.CollectorInstance, Lease: envelope.Lease,
	}
}

// CheckpointMetadata는 [0, CheckpointCount()) 범위의 체크포인트 메타데이터를 값으로 반환합니다.
func (o RunOutput) CheckpointMetadata(index int) CheckpointMetadata {
	checkpoint := &o.checkpoints[index]

	return CheckpointMetadata{
		Provider: checkpoint.Provider, ObservationKind: checkpoint.ObservationKind,
		SubjectKey: checkpoint.SubjectKey, ScopeSHA256: checkpoint.ScopeSHA256,
		ContractGeneration: checkpoint.ContractGeneration, LastObservationKey: checkpoint.LastObservationKey,
		LastEvidenceSHA256: checkpoint.LastEvidenceSHA256, LastScheduledFor: checkpoint.LastScheduledFor,
		Continuity: checkpoint.Continuity,
	}
}
