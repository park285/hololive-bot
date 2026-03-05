package config

// cliproxyEnvConfig: CLIPROXY_* 환경변수 로딩용 내부 구조체
type cliproxyEnvConfig struct {
	BaseURL         string `envconfig:"CLIPROXY_BASE_URL" default:"https://cliproxy.capu.blog/v1"`
	APIKey          string `envconfig:"CLIPROXY_API_KEY"`
	Model           string `envconfig:"CLIPROXY_MODEL" default:"gpt-5.3-codex"`
	Enabled         string `envconfig:"CLIPROXY_ENABLED" default:"false"`
	ReasoningEffort string `envconfig:"CLIPROXY_REASONING_EFFORT" default:"high"`
}

// llmEnvConfig: LLM 관련 환경변수 로딩용 내부 구조체
type llmEnvConfig struct {
	MemberNewsModel       string `envconfig:"MEMBER_NEWS_LLM_MODEL"`
	MemberNewsTemperature string `envconfig:"MEMBER_NEWS_TEMPERATURE" default:"0"`
}

// consensusLLMEnvConfig: prefix 기반 CONSENSUS_* 환경변수 로딩용 내부 구조체
type consensusLLMEnvConfig struct {
	ConsensusEnabled     string `envconfig:"CONSENSUS_ENABLED" default:"false"`
	ConsensusConfidence  string `envconfig:"CONSENSUS_CONFIDENCE" default:"0.85"`
	ReviewerModel        string `envconfig:"REVIEWER_MODEL"`
	AdjudicatorModel     string `envconfig:"ADJUDICATOR_MODEL"`
	ReviewTimeoutSec     string `envconfig:"REVIEW_TIMEOUT_SEC" default:"30"`
	AdjudicateTimeoutSec string `envconfig:"ADJUDICATE_TIMEOUT_SEC" default:"45"`
}

// exaEnvConfig: EXA_* 환경변수 로딩용 내부 구조체
type exaEnvConfig struct {
	Endpoint string `envconfig:"EXA_MCP_ENDPOINT" default:"https://mcp.exa.ai/mcp"`
	APIKey   string `envconfig:"EXA_API_KEY"`
	Enabled  string `envconfig:"EXA_ENABLED" default:"false"`
}

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
