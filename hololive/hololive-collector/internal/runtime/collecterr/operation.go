package collecterr

type OperationCode string

const (
	OperationCandidateLoadFailed OperationCode = "candidate_load_failed"
	OperationLeaseAcquireFailed  OperationCode = "lease_acquire_failed"
	OperationLeaseDeferFailed    OperationCode = "lease_defer_failed"
)
