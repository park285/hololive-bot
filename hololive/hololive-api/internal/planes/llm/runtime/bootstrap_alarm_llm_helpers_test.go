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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews"
	"github.com/kapu/hololive-shared/pkg/config/settings"
)

// X allowlist 경로는 MEMBER_NEWS_X_ALLOWLIST_PATH 하나로만 받는다. 설정했는데 읽지 못하면 빈 allowlist로
// 내려가지 않고 기동을 실패시킨다(stack audit B5).
func TestInitMemberNewsSourceValidator(t *testing.T) {
	t.Run("unset path runs without x allowlist", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_X_ALLOWLIST_PATH", "")

		validator, err := initMemberNewsSourceValidator(nil, testRuntimeLogger())
		require.NoError(t, err)
		require.NotNil(t, validator)
	})

	t.Run("configured missing file fails", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_X_ALLOWLIST_PATH", filepath.Join(t.TempDir(), "missing.json"))

		_, err := initMemberNewsSourceValidator(nil, testRuntimeLogger())
		require.Error(t, err)
	})

	t.Run("working directory candidates are not searched", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_X_ALLOWLIST_PATH", "")

		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "configs"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "configs", "hololive_official_x_accounts.json"), []byte("not json"), 0o600))
		t.Chdir(dir)

		_, err := initMemberNewsSourceValidator(nil, testRuntimeLogger())
		require.NoError(t, err, "a file in ./configs must not be picked up implicitly")
	})

	t.Run("configured path is trimmed", func(t *testing.T) {
		allowlist := filepath.Join(t.TempDir(), "allowlist.json")
		require.NoError(t, os.WriteFile(allowlist, []byte(`["hololivetv"]`), 0o600))
		t.Setenv("MEMBER_NEWS_X_ALLOWLIST_PATH", "  "+allowlist+"  ")

		_, err := initMemberNewsSourceValidator(nil, testRuntimeLogger())
		require.NoError(t, err)
	})
}

// provider가 켜져 있는데 client를 만들지 못하면 member news를 조용히 결정적 digest로 줄이지 않고 기동을 실패시킨다.
func TestInitMemberNewsServiceFailsWhenEnabledLLMClientCannotInitialize(t *testing.T) {
	t.Setenv("MEMBER_NEWS_X_ALLOWLIST_PATH", "")

	_, err := initMemberNewsService(
		t.Context(),
		cliproxyProvider(settings.CliproxyConfig{Enabled: true, APIKey: testProviderKey, BaseURL: ""}),
		&settings.LLMConfig{MemberNewsModel: "test-model"},
		settings.ExaConfig{},
		nil,
		nil,
		&llmGuards{},
		testRuntimeLogger(),
	)
	require.Error(t, err)
}

func TestInitMemberNewsService_BuildsServiceWithOfflineConfig(t *testing.T) {
	t.Run("basic config without consensus", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_X_ALLOWLIST_PATH", "")

		service, err := initMemberNewsService(
			t.Context(),
			cliproxyProvider(settings.CliproxyConfig{}),
			&settings.LLMConfig{},
			settings.ExaConfig{},
			nil,
			nil,
			&llmGuards{},
			testRuntimeLogger(),
		)
		require.NoError(t, err)
		require.NotNil(t, service)
	})

	t.Run("consensus config enabled", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_X_ALLOWLIST_PATH", "")

		apiKey := strings.Join([]string{"dummy", "api", "key"}, "-")
		cliproxyConfig := cliproxyProvider(settings.CliproxyConfig{
			Enabled:         true,
			BaseURL:         "https://example.com",
			APIKey:          apiKey,
			Model:           "gpt-4.1",
			ReasoningEffort: "medium",
		})
		llmConfig := &settings.LLMConfig{
			MemberNewsModel: "gpt-4.1",
			MemberNews: settings.ConsensusLLMConfig{
				Enabled:           true,
				Confidence:        0.7,
				ReviewerModel:     "gpt-4.1-mini",
				AdjudicatorModel:  "gpt-4.1",
				ReviewTimeout:     1,
				AdjudicateTimeout: 1,
			},
		}

		service, err := initMemberNewsService(
			t.Context(),
			cliproxyConfig,
			llmConfig,
			settings.ExaConfig{},
			nil,
			nil,
			&llmGuards{},
			testRuntimeLogger(),
		)
		require.NoError(t, err)
		require.NotNil(t, service)
	})
}

func TestBuildMemberNewsComponents(t *testing.T) {
	logger := testRuntimeLogger()

	t.Run("nil service disables schedulers", func(t *testing.T) {
		weekly, monthly := buildMemberNewsComponents(nil, nil, nil, nil, nil, logger)
		assert.Nil(t, weekly)
		assert.Nil(t, monthly)
	})

	t.Run("non-nil service builds schedulers", func(t *testing.T) {
		service := membernews.NewService(nil, nil, nil, nil, logger)
		weekly, monthly := buildMemberNewsComponents(service, nil, nil, nil, nil, logger)
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
		&llmGuards{},
		testRuntimeLogger(),
	)

	require.NotNil(t, weekly)
	require.NotNil(t, monthly)
	assert.Nil(t, scraper)
}
