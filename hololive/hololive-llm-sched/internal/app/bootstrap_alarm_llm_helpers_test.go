package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	mesummarizer "github.com/kapu/hololive-llm-sched/internal/service/majorevent/summarizer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-llm-sched/internal/service/membernews"
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

func TestMemberNewsSearcherAdapterSearch_NilBase(t *testing.T) {
	t.Run("nil base", func(t *testing.T) {
		adapter := &memberNewsSearcherAdapter{base: nil}
		results, err := adapter.Search(context.Background(), "query")
		require.NoError(t, err)
		assert.Nil(t, results)
	})
}

type fakeMajorEventSearcher struct {
	results []mesummarizer.SearchResult
	err     error
}

func (f *fakeMajorEventSearcher) Search(context.Context, string) ([]mesummarizer.SearchResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.results, nil
}

func TestMemberNewsSearcherAdapterSearch_ConvertsAndWrapsError(t *testing.T) {
	t.Run("convert results", func(t *testing.T) {
		base := &fakeMajorEventSearcher{
			results: []mesummarizer.SearchResult{
				{Title: "A", URL: "https://example.com/a", Content: "content-a", PublishedDate: "2026-03-04"},
				{Title: "B", URL: "https://example.com/b", Content: "content-b", PublishedDate: "2026-03-05"},
			},
		}
		adapter := &memberNewsSearcherAdapter{base: base}

		got, err := adapter.Search(context.Background(), "hololive news")
		require.NoError(t, err)
		require.Len(t, got, 2)

		assert.Equal(t, membernews.SearchResult{
			Title:         "A",
			URL:           "https://example.com/a",
			Content:       "content-a",
			PublishedDate: "2026-03-04",
		}, got[0])
		assert.Equal(t, membernews.SearchResult{
			Title:         "B",
			URL:           "https://example.com/b",
			Content:       "content-b",
			PublishedDate: "2026-03-05",
		}, got[1])
	})

	t.Run("wrap base error", func(t *testing.T) {
		base := &fakeMajorEventSearcher{err: errors.New("search failed")}
		adapter := &memberNewsSearcherAdapter{base: base}

		got, err := adapter.Search(context.Background(), "hololive news")
		require.Error(t, err)
		assert.Nil(t, got)
		assert.ErrorContains(t, err, "search member news context")
	})
}

func TestInitMemberNewsService_BuildsServiceWithOfflineConfig(t *testing.T) {
	t.Run("basic config without consensus", func(t *testing.T) {
		svc := initMemberNewsService(
			context.Background(),
			config.CliproxyConfig{},
			config.LLMConfig{},
			config.ExaConfig{},
			nil,
			nil,
			nil,
			testRuntimeLogger(),
		)
		require.NotNil(t, svc)
	})

	t.Run("consensus config enabled", func(t *testing.T) {
		cliproxyCfg := config.CliproxyConfig{
			Enabled:         true,
			BaseURL:         "https://example.com",
			APIKey:          "dummy-api-key",
			Model:           "gpt-4.1",
			ReasoningEffort: "medium",
		}
		llmCfg := config.LLMConfig{
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

		svc := initMemberNewsService(
			context.Background(),
			cliproxyCfg,
			llmCfg,
			config.ExaConfig{},
			nil,
			nil,
			nil,
			testRuntimeLogger(),
		)
		require.NotNil(t, svc)
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
		svc := membernews.NewService(nil, nil, nil, nil, logger)
		weekly, monthly := buildMemberNewsComponents(svc, nil, nil, nil, logger)
		require.NotNil(t, weekly)
		require.NotNil(t, monthly)
	})
}

func TestBuildMajorEventComponents_NilRepositoryReturnsNilScraper(t *testing.T) {
	weekly, monthly, scraper := buildMajorEventComponents(
		context.Background(),
		nil,
		nil,
		nil,
		nil,
		nil,
		testRuntimeLogger(),
		false,
	)

	require.NotNil(t, weekly)
	require.NotNil(t, monthly)
	assert.Nil(t, scraper)
}
