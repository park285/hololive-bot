// Package notificationdelivery는 notification_delivery_outbox의 저장 JSON 계약만 소유한다.
// Producer는 Message만 적재하고 alarm-worker는 claim 이후 불변 요청을 기록한다.
// 필드 순서와 JSON 태그는 기존 저장 바이트를 유지한다. SQL의 rearm·CAS 조건도 같은 키를 읽는다.
// Claim, 재발급, 재시도, 격리와 발송 정산 정책은 worker에 남는다.
package notificationdelivery

// Payload는 producer와 worker가 공유하는 저장 envelope다.
type Payload struct {
	Message string   `json:"message"`
	Request *Request `json:"request,omitempty"`
}

// Request는 worker가 외부 발송 전에 고정하는 본문·경로·요청 ID 세대다.
// Body는 JSON이나 Markdown을 포함해 다시 렌더링하지 않는 불변 문자열이다.
type Request struct {
	Body       string `json:"body"`
	Route      string `json:"route"`
	BaseID     string `json:"base_id"`
	Generation int    `json:"generation"`
	Exhausted  bool   `json:"exhausted,omitzero"`
}
