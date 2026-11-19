package alarm

const (
	DispatchStatusPending     = "pending"
	DispatchStatusLeased      = "leased"
	DispatchStatusRetry       = "retry"
	DispatchStatusSending     = "sending"
	DispatchStatusSent        = "sent"
	DispatchStatusDLQ         = "dlq"
	DispatchStatusQuarantined = "quarantined"
	DispatchStatusCancelled   = "cancelled" //nolint:misspell // 저장된 행의 실제 값이라 canceled로 바꾸면 기존 행과 상태 비교가 어긋난다.
)
