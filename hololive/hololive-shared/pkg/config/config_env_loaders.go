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
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	sharedenv "github.com/park285/llm-kakao-bots/shared-go/pkg/envutil"
)

func loadAppEnvironment() string {
	return sharedenv.String("APP_ENV", "production")
}

func loadValkeyConfig() ValkeyConfig {
	return ValkeyConfig{
		Host:       sharedenv.String("CACHE_HOST", "localhost"),
		Port:       sharedenv.Int("CACHE_PORT", 6379),
		Password:   sharedenv.StringRaw("CACHE_PASSWORD", ""),
		DB:         sharedenv.Int("CACHE_DB", 0),
		SocketPath: sharedenv.String("CACHE_SOCKET_PATH", ""),
	}
}

func loadPostgresConfig() PostgresConfig {
	password := sharedenv.StringRaw("POSTGRES_PASSWORD", "")
	if strings.TrimSpace(password) == "" {
		password = constants.DatabaseDefaults.Password
	}

	return PostgresConfig{
		Host:              sharedenv.String("POSTGRES_HOST", constants.DatabaseDefaults.Host),
		Port:              sharedenv.Int("POSTGRES_PORT", constants.DatabaseDefaults.Port),
		SocketPath:        sharedenv.String("POSTGRES_SOCKET_PATH", ""),
		User:              sharedenv.String("POSTGRES_USER", constants.DatabaseDefaults.User),
		Password:          password,
		Database:          sharedenv.String("POSTGRES_DB", constants.DatabaseDefaults.Database),
		SSLMode:           sharedenv.String("POSTGRES_SSLMODE", "require"),
		QueryExecMode:     sharedenv.String("POSTGRES_QUERY_EXEC_MODE", "cache_statement"),
		PoolMinConns:      sharedenv.Int("POSTGRES_POOL_MIN_CONNS", constants.DatabaseConfig.MaxIdleConns),
		PoolMaxConns:      sharedenv.Int("POSTGRES_POOL_MAX_CONNS", constants.DatabaseConfig.MaxOpenConns),
		PoolMaxIdleConns:  sharedenv.Int("POSTGRES_POOL_MAX_IDLE_CONNS", constants.DatabaseConfig.MaxIdleConns),
		AutoPrepareSchema: sharedenv.Bool("POSTGRES_AUTO_PREPARE_SCHEMA", true),
	}
}

func loadCommunityShortsBigBangCutoverAt() (time.Time, error) {
	raw := strings.TrimSpace(sharedenv.String("YOUTUBE_COMMUNITY_SHORTS_BIGBANG_CUTOVER_AT", ""))
	if raw == "" {
		return time.Time{}, nil
	}

	cutoverAt, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("YOUTUBE_COMMUNITY_SHORTS_BIGBANG_CUTOVER_AT must be RFC3339: %w", err)
	}

	return cutoverAt.UTC(), nil
}

func loadCliproxyConfig() CliproxyConfig {
	return CliproxyConfig{
		BaseURL:         sharedenv.String("CLIPROXY_BASE_URL", "https://cliproxy.capu.blog/v1"),
		APIKey:          sharedenv.String("CLIPROXY_API_KEY", ""),
		Model:           sharedenv.String("CLIPROXY_MODEL", "gpt-5.4"),
		Enabled:         sharedenv.Bool("CLIPROXY_ENABLED", false),
		ReasoningEffort: sharedenv.String("CLIPROXY_REASONING_EFFORT", "high"),
	}
}

// loadConsensusLLMConfig: prefix 기반 환경변수에서 ConsensusLLMConfig를 로드한다.
// prefix 예: "MEMBER_NEWS" -> MEMBER_NEWS_CONSENSUS_ENABLED, MEMBER_NEWS_CONSENSUS_CONFIDENCE, ...
func loadConsensusLLMConfig(prefix string) ConsensusLLMConfig {
	reviewTimeout := sharedenv.Int(prefix+"_REVIEW_TIMEOUT_SEC", 30)
	if reviewTimeout < 5 {
		reviewTimeout = 30
	}
	adjudicateTimeout := sharedenv.Int(prefix+"_ADJUDICATE_TIMEOUT_SEC", 45)
	if adjudicateTimeout < 5 {
		adjudicateTimeout = 45
	}

	return ConsensusLLMConfig{
		Enabled:           sharedenv.Bool(prefix+"_CONSENSUS_ENABLED", false),
		Confidence:        clampConfidence(sharedenv.Float(prefix+"_CONSENSUS_CONFIDENCE", 0.85)),
		ReviewerModel:     sharedenv.String(prefix+"_REVIEWER_MODEL", ""),
		AdjudicatorModel:  sharedenv.String(prefix+"_ADJUDICATOR_MODEL", ""),
		ReviewTimeout:     reviewTimeout,
		AdjudicateTimeout: adjudicateTimeout,
	}
}

func loadLLMConfig() LLMConfig {
	return LLMConfig{
		MemberNewsModel:       sharedenv.String("MEMBER_NEWS_LLM_MODEL", ""),
		MemberNewsTemperature: sharedenv.Float("MEMBER_NEWS_TEMPERATURE", 0),
		MemberNews:            loadConsensusLLMConfig("MEMBER_NEWS"),
		MajorEvent:            loadConsensusLLMConfig("MAJOREVENT"),
	}
}

func loadExaConfig() ExaConfig {
	return ExaConfig{
		Endpoint: sharedenv.String("EXA_MCP_ENDPOINT", "https://mcp.exa.ai/mcp"),
		APIKey:   sharedenv.String("EXA_API_KEY", ""),
		Enabled:  sharedenv.Bool("EXA_ENABLED", false),
	}
}
