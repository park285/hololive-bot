package collecterr

import "slices"

type OperationCode string

const (
	OperationCandidateLoadFailed  OperationCode = "candidate_load_failed"
	OperationLeaseAcquireFailed   OperationCode = "lease_acquire_failed"
	OperationLeaseDeferFailed     OperationCode = "lease_defer_failed"
	OperationHelperExited         OperationCode = "helper_exited"
	OperationReadinessProbeFailed OperationCode = "readiness_probe_failed"
	OperationSchedulerFatal       OperationCode = "scheduler_fatal"

	CandidateFailed = OperationCandidateLoadFailed
	AcquireFailed   = OperationLeaseAcquireFailed
	DeferFailed     = OperationLeaseDeferFailed
)

func (c OperationCode) Valid() bool {
	_, ok := operationCodeSet[c]
	return ok
}

func AllOperationCodes() []OperationCode {
	return slices.Clone(allOperationCodes)
}

var (
	allOperationCodes = []OperationCode{
		OperationCandidateLoadFailed,
		OperationHelperExited,
		OperationLeaseAcquireFailed,
		OperationLeaseDeferFailed,
		OperationReadinessProbeFailed,
		OperationSchedulerFatal,
	}
	operationCodeSet = func() map[OperationCode]struct{} {
		out := make(map[OperationCode]struct{}, len(allOperationCodes))
		for _, code := range allOperationCodes {
			out[code] = struct{}{}
		}

		return out
	}()
)
