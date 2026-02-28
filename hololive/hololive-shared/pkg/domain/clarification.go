package domain

// ClarificationResponse: LLM으로부터 받은 명확화/재질문 응답 메시지 구조체
type ClarificationResponse struct {
	Message   string `json:"message"`
	Candidate string `json:"candidate,omitempty"`
}

// Clarification: 명확화 처리 결과 (Hololive 관련 여부 및 메시지)
type Clarification struct {
	IsHololiveRelated bool   `json:"is_hololive_related"`
	Message           string `json:"message"`
	Candidate         string `json:"candidate,omitempty"`
}
