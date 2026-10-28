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

import (
	"fmt"
	"math"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"

	"github.com/kapu/hololive-shared/pkg/config/envload"
)

// LLM·CLIProxy·Gemini·Exa 설정은 hololive-api llm plane만 소비하므로 이 module이 타입과 env loader를 소유한다.

const (
	LLMProviderCliproxy = "cliproxy"
	LLMProviderGemini   = "gemini"
)

type CliproxyConfig struct {
	BaseURL         string
	APIKey          string
	Model           string
	Enabled         bool
	ReasoningEffort string
}

type GeminiConfig struct {
	BaseURL       string
	APIKey        string
	Model         string
	Enabled       bool
	ThinkingLevel string
}

type LLMProviderConfig struct {
	Name     string
	Cliproxy CliproxyConfig
	Gemini   GeminiConfig
}

type ConsensusLLMConfig struct {
	Enabled           bool
	Confidence        float64
	ReviewerModel     string
	AdjudicatorModel  string
	ReviewTimeout     int
	AdjudicateTimeout int
}

type LLMConfig struct {
	MemberNewsModel       string
	MemberNewsTemperature float64

	MemberNews ConsensusLLMConfig
	MajorEvent ConsensusLLMConfig
}

type ExaConfig struct {
	Endpoint string
	APIKey   string
	Enabled  bool
}

func LoadCliproxyConfig() (CliproxyConfig, error) {
	enabled, err := sharedenv.BoolE("CLIPROXY_ENABLED", false)
	if err != nil {
		return CliproxyConfig{}, fmt.Errorf("load cliproxy config: %w", err)
	}

	return CliproxyConfig{
		BaseURL:         sharedenv.String("CLIPROXY_BASE_URL", ""),
		APIKey:          sharedenv.String("CLIPROXY_API_KEY", ""),
		Model:           sharedenv.String("CLIPROXY_MODEL", "gpt-5.4"),
		Enabled:         enabled,
		ReasoningEffort: sharedenv.String("CLIPROXY_REASONING_EFFORT", "high"),
	}, nil
}

func LoadGeminiConfig() (GeminiConfig, error) {
	enabled, err := sharedenv.BoolE("GEMINI_ENABLED", false)
	if err != nil {
		return GeminiConfig{}, fmt.Errorf("load gemini config: %w", err)
	}

	return GeminiConfig{
		BaseURL:       sharedenv.String("GEMINI_BASE_URL", "https://generativelanguage.googleapis.com"),
		APIKey:        sharedenv.String("GEMINI_API_KEY", ""),
		Model:         sharedenv.String("GEMINI_MODEL", "gemini-3.7-flash"),
		Enabled:       enabled,
		ThinkingLevel: sharedenv.String("GEMINI_THINKING_LEVEL", "high"),
	}, nil
}

// loadConsensusLLMConfig: prefix 기반 환경변수에서 ConsensusLLMConfig를 로드한다.
func loadConsensusLLMConfig(env *envload.StrictEnv, prefix string) ConsensusLLMConfig {
	reviewTimeout := env.Int(prefix+"_REVIEW_TIMEOUT_SEC", 30)
	if reviewTimeout < 5 {
		reviewTimeout = 30
	}

	adjudicateTimeout := env.Int(prefix+"_ADJUDICATE_TIMEOUT_SEC", 45)
	if adjudicateTimeout < 5 {
		adjudicateTimeout = 45
	}

	return ConsensusLLMConfig{
		Enabled:           env.Bool(prefix+"_CONSENSUS_ENABLED", false),
		Confidence:        clampConfidence(env.Float(prefix+"_CONSENSUS_CONFIDENCE", 0.85)),
		ReviewerModel:     sharedenv.String(prefix+"_REVIEWER_MODEL", ""),
		AdjudicatorModel:  sharedenv.String(prefix+"_ADJUDICATOR_MODEL", ""),
		ReviewTimeout:     reviewTimeout,
		AdjudicateTimeout: adjudicateTimeout,
	}
}

func LoadLLMConfig() (LLMConfig, error) {
	var env envload.StrictEnv

	config := LLMConfig{
		MemberNewsModel:       sharedenv.String("MEMBER_NEWS_LLM_MODEL", ""),
		MemberNewsTemperature: env.Float("MEMBER_NEWS_TEMPERATURE", 0),
		MemberNews:            loadConsensusLLMConfig(&env, "MEMBER_NEWS"),
		MajorEvent:            loadConsensusLLMConfig(&env, "MAJOREVENT"),
	}

	if err := env.Err(); err != nil {
		return LLMConfig{}, fmt.Errorf("load LLM config: %w", err)
	}

	return config, nil
}

func LoadExaConfig() (ExaConfig, error) {
	enabled, err := sharedenv.BoolE("EXA_ENABLED", false)
	if err != nil {
		return ExaConfig{}, fmt.Errorf("load exa config: %w", err)
	}

	return ExaConfig{
		Endpoint: sharedenv.String("EXA_MCP_ENDPOINT", "https://mcp.exa.ai/mcp"),
		APIKey:   sharedenv.String("EXA_API_KEY", ""),
		Enabled:  enabled,
	}, nil
}

// clampConfidence: confidence 값을 [0, 1] 범위로 정규화한다.
// NaN/Inf 입력 시 기본값(0.85)을 반환한다.
func clampConfidence(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0.85
	}

	if v < 0 {
		return 0.0
	}

	if v > 1 {
		return 1.0
	}

	return v
}
