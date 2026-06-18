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

package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-llm-sched/internal/service/membernews"
	"github.com/kapu/hololive-shared/pkg/config"
)

func TestResolveMemberNewsXAllowlistPath(t *testing.T) {
	t.Run("env override", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_X_ALLOWLIST_PATH", "/tmp/custom-allowlist.json")
		assert.Equal(t, "/tmp/custom-allowlist.json", resolveMemberNewsXAllowlistPath())
	})

	t.Run("fallback empty when file missing", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_X_ALLOWLIST_PATH", "")

		oldWD, err := os.Getwd()
		require.NoError(t, err)

		tmpDir := t.TempDir()
		require.NoError(t, os.Chdir(tmpDir))
		t.Cleanup(func() {
			require.NoError(t, os.Chdir(oldWD))
		})

		assert.Equal(t, "", resolveMemberNewsXAllowlistPath())
	})

	t.Run("env trim", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "allowlist.json")
		t.Setenv("MEMBER_NEWS_X_ALLOWLIST_PATH", "  "+tmp+"  ")
		assert.Equal(t, "  "+tmp+"  ", resolveMemberNewsXAllowlistPath())
	})
}

func TestInitMemberNewsService_BuildsServiceWithOfflineConfig(t *testing.T) {
	t.Run("basic config without consensus", func(t *testing.T) {
		service := initMemberNewsService(
			context.Background(),
			config.CliproxyConfig{},
			&config.LLMConfig{},
			config.ExaConfig{},
			nil,
			nil,
			nil,
			testRuntimeLogger(),
		)
		require.NotNil(t, service)
	})

	t.Run("consensus config enabled", func(t *testing.T) {
		apiKey := strings.Join([]string{"dummy", "api", "key"}, "-")
		cliproxyConfig := config.CliproxyConfig{
			Enabled:         true,
			BaseURL:         "https://example.com",
			APIKey:          apiKey,
			Model:           "gpt-4.1",
			ReasoningEffort: "medium",
		}
		llmConfig := &config.LLMConfig{
			MemberNewsModel: "gpt-4.1",
			MemberNews: config.ConsensusLLMConfig{
				Enabled:           true,
				Confidence:        0.7,
				ReviewerModel:     "gpt-4.1-mini",
				AdjudicatorModel:  "gpt-4.1",
				ReviewTimeout:     1,
				AdjudicateTimeout: 1,
			},
		}

		service := initMemberNewsService(
			context.Background(),
			cliproxyConfig,
			llmConfig,
			config.ExaConfig{},
			nil,
			nil,
			nil,
			testRuntimeLogger(),
		)
		require.NotNil(t, service)
	})
}

func TestBuildMemberNewsComponents(t *testing.T) {
	logger := testRuntimeLogger()

	t.Run("nil service disables schedulers", func(t *testing.T) {
		weekly, monthly := buildMemberNewsComponents(nil, nil, nil, nil, logger)
		assert.Nil(t, weekly)
		assert.Nil(t, monthly)
	})

	t.Run("non-nil service builds schedulers", func(t *testing.T) {
		service := membernews.NewService(nil, nil, nil, nil, logger)
		weekly, monthly := buildMemberNewsComponents(service, nil, nil, nil, logger)
		require.NotNil(t, weekly)
		require.NotNil(t, monthly)
	})
}

func TestBuildMajorEventComponents_NilRepositoryReturnsNilScraper(t *testing.T) {
	weekly, monthly, scraper := buildMajorEventComponents(
		nil,
		nil,
		nil,
		nil,
		nil,
		testRuntimeLogger(),
	)

	require.NotNil(t, weekly)
	require.NotNil(t, monthly)
	assert.Nil(t, scraper)
}
