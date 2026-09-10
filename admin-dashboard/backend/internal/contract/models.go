package contract

// ErrorResponse는 원문 upstream 오류나 비밀값을 포함하지 않는 BFF 오류입니다.
type ErrorResponse struct {
	Code                    string  `json:"code"`
	Error                   string  `json:"message"`
	RequestID               string  `json:"requestId"`
	AbsoluteExpired         *bool   `json:"absolute_expired,omitempty"`
	RetryAfter              *uint64 `json:"retry_after,omitempty"`
	NotDispatchedMutationID string  `json:"notDispatchedMutationId,omitempty"`
}

// AdminMetadata는 인증 정보 없이 브라우저 계약 세대만 공개합니다.
type AdminMetadata struct {
	ClientGeneration string `json:"clientGeneration"`
}

// Aliases는 멤버의 한국어·일본어 별명을 보존합니다.
type Aliases struct {
	KO []string `json:"ko"`
	JA []string `json:"ja"`
}

// Member는 upstream 정수 ID를 정밀도 손실 없이 문자열로 투영한 관리자 DTO입니다.
type Member struct {
	ID          string  `json:"id"`
	ChannelID   string  `json:"channelId"`
	Name        string  `json:"name"`
	NameJA      *string `json:"nameJa,omitempty"`
	NameKO      *string `json:"nameKo,omitempty"`
	Aliases     Aliases `json:"aliases"`
	IsGraduated bool    `json:"isGraduated"`
}
