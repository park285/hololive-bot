package alarm

// Iris /reply-status handoff 확인 결과 코드다. 같은 값을 alarm dispatch·YouTube 발송 원장은 last_error_code에, 봇
// 응답 원장은 last_error에 남긴다.
const (
	HandoffCodeFailed         = "handoff_failed"
	HandoffCodeOutcomeUnknown = "handoff_outcome_unknown"
	HandoffCodeStatusMissing  = "handoff_status_missing"
	HandoffCodeUnconfirmed    = "handoff_unconfirmed"
)
