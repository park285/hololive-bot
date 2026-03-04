package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	t.Run("nil adapter and base", func(t *testing.T) {
		var adapter *memberNewsSearcherAdapter
		results, err := adapter.Search(context.Background(), "query")
		require.NoError(t, err)
		assert.Nil(t, results)
	})

	t.Run("nil base", func(t *testing.T) {
		adapter := &memberNewsSearcherAdapter{base: nil}
		results, err := adapter.Search(context.Background(), "query")
		require.NoError(t, err)
		assert.Nil(t, results)
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
