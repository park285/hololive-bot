package trigger

// Result는 알림 intent 적재 결과이며 실제 발송이나 handoff 완료 증거가 아니다.
// Queued에는 이미 존재하는 durable intent의 멱등 적재도 포함한다.
// Partial은 일부 방 적재 실패를 표시하며 전체 트리거 자동 재시도를 지시하지 않는다.
type Result struct {
	Status  string `json:"status"`
	Queued  int    `json:"queued"`
	Skipped int    `json:"skipped"`
	Failed  int    `json:"failed"`
}

// NewResult는 방별 적재 집계를 HTTP와 호출자가 공유하는 결과로 만든다.
// 전부 실패한 경우 호출자는 기존 오류 경로를 함께 반환한다.
func NewResult(queued, skipped, failed int) Result {
	status := "skipped"

	if failed > 0 {
		status = "failed"
	}

	if queued > 0 {
		status = "queued"

		if failed > 0 {
			status = "partial"
		}
	}

	return Result{Status: status, Queued: queued, Skipped: skipped, Failed: failed}
}
