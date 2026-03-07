// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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
