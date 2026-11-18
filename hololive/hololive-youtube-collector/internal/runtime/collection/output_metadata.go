package collection

import (
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

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

func (o RunOutput) ObservationCount() int {
	return len(o.observations)
}

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
