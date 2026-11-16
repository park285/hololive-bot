package alarm

// alarm_dispatch_deliveries.status 값이다. 철자는 alarm_dispatch_deliveries_status_check와 같아야 하며, worker 발송과
// API 관리자 재처리·정산이 같은 값을 쓴다.
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
