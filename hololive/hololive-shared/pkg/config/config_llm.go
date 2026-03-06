package config

// CliproxyConfig: Cliproxy API 직접 호출 설정 (이벤트 요약용)
type CliproxyConfig struct {
	BaseURL         string
	APIKey          string
	Model           string
	Enabled         bool
	ReasoningEffort string // reasoning 모델용 사고 깊이 (high, xhigh 등)
}

// ConsensusLLMConfig: dual-agent review(consensus) 공통 설정
type ConsensusLLMConfig struct {
	Enabled           bool
	Confidence        float64
	ReviewerModel     string
	AdjudicatorModel  string
	ReviewTimeout     int
	AdjudicateTimeout int
}

// LLMConfig: LLM 서비스별 모델 설정
type LLMConfig struct {
	MemberNewsModel       string  // 최종 모델명 (dual-read 해결 완료, 빈 문자열이면 Cliproxy.Model 사용)
	MemberNewsTemperature float64 // MEMBER_NEWS_TEMPERATURE

	MemberNews ConsensusLLMConfig // MEMBER_NEWS_CONSENSUS_* 환경변수 그룹
	MajorEvent ConsensusLLMConfig // MAJOREVENT_CONSENSUS_* 환경변수 그룹
}

// ExaConfig: Exa MCP 검색 설정 (이벤트 요약용)
type ExaConfig struct {
	Endpoint string
	APIKey   string
	Enabled  bool
}
