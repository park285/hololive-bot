package parser

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractPublishedAtFromHTML_MetaItemprop(t *testing.T) {
	html := `<html><head><meta itemprop="datePublished" content="2026-04-10T10:11:12+09:00"></head></html>`
	publishedAt, err := ExtractPublishedAtFromHTML(html)
	require.NoError(t, err)
	require.NotNil(t, publishedAt)
	assert.Equal(t, "2026-04-10T01:11:12Z", publishedAt.Format(time.RFC3339))
}

func TestExtractPublishedAtFromHTML_MetaPropertyLowercased(t *testing.T) {
	html := `<html><head><meta property="uploaddate" content="2026-05-01T00:00:00Z"></head></html>`
	publishedAt, err := ExtractPublishedAtFromHTML(html)
	require.NoError(t, err)
	require.NotNil(t, publishedAt)
	assert.Equal(t, "2026-05-01T00:00:00Z", publishedAt.Format(time.RFC3339))
}

func TestExtractPublishedAtFromHTML_JSONLD(t *testing.T) {
	html := `<script type="application/ld+json">{"datePublished":"2026-04-10T10:11:12+09:00"}</script>`
	publishedAt, err := ExtractPublishedAtFromHTML(html)
	require.NoError(t, err)
	require.NotNil(t, publishedAt)
	assert.Equal(t, "2026-04-10T01:11:12Z", publishedAt.Format(time.RFC3339))
}

func TestExtractPublishedAtFromHTML_MetaPrecedesJSON(t *testing.T) {
	html := `<html><head><meta itemprop="datePublished" content="2026-04-10T10:11:12+09:00"></head><body>{"datePublished":"2020-01-01T00:00:00Z"}</body></html>`
	publishedAt, err := ExtractPublishedAtFromHTML(html)
	require.NoError(t, err)
	require.NotNil(t, publishedAt)
	assert.Equal(t, "2026-04-10T01:11:12Z", publishedAt.Format(time.RFC3339))
}

func TestExtractPublishedAtFromHTML_InvalidCandidate(t *testing.T) {
	publishedAt, err := ExtractPublishedAtFromHTML(`<meta itemprop="datePublished" content="garbage">`)
	require.Error(t, err)
	assert.Nil(t, publishedAt)
	assert.True(t, errors.Is(err, ErrPublishedAtNotFound))
}

func TestExtractPublishedAtFromHTML_MetaWithoutContent(t *testing.T) {
	publishedAt, err := ExtractPublishedAtFromHTML(`<meta itemprop="datePublished">`)
	require.Error(t, err)
	assert.Nil(t, publishedAt)
	assert.True(t, errors.Is(err, ErrPublishedAtNotFound))
}

func TestExtractPublishedAtFromHTML_GarbageInput(t *testing.T) {
	publishedAt, err := ExtractPublishedAtFromHTML("not html at all <<< >>>")
	require.Error(t, err)
	assert.Nil(t, publishedAt)
	assert.True(t, errors.Is(err, ErrPublishedAtNotFound))
}

func TestExtractPublishedAtFromHTML_EmptyInput(t *testing.T) {
	publishedAt, err := ExtractPublishedAtFromHTML("")
	require.Error(t, err)
	assert.Nil(t, publishedAt)
	assert.True(t, errors.Is(err, ErrPublishedAtNotFound))
}

func TestExtractCommunityPublishedAtFromHTML_HappyPath(t *testing.T) {
	html := `<html><head><meta itemprop="datePublished" content="2026-04-10T10:11:12+09:00"></head></html>`
	publishedAt, err := ExtractCommunityPublishedAtFromHTML(html)
	require.NoError(t, err)
	require.NotNil(t, publishedAt)
	assert.Equal(t, "2026-04-10T01:11:12Z", publishedAt.Format(time.RFC3339))
}

func TestExtractCommunityPublishedAtFromHTML_NotFoundMapsToCommunityError(t *testing.T) {
	publishedAt, err := ExtractCommunityPublishedAtFromHTML("<html></html>")
	require.Error(t, err)
	assert.Nil(t, publishedAt)
	assert.True(t, errors.Is(err, ErrCommunityPublishedAtNotFound))
	assert.False(t, errors.Is(err, ErrPublishedAtNotFound))
}

func TestExtractCommunityPublishedAtFromHTML_EmptyInput(t *testing.T) {
	publishedAt, err := ExtractCommunityPublishedAtFromHTML("")
	require.Error(t, err)
	assert.Nil(t, publishedAt)
	assert.True(t, errors.Is(err, ErrCommunityPublishedAtNotFound))
}

func TestNormalizePublishedAtCandidate(t *testing.T) {
	t.Run("valid offset normalized to UTC", func(t *testing.T) {
		value, ok := NormalizePublishedAtCandidate("2026-04-10T10:11:12+09:00")
		require.True(t, ok)
		require.NotNil(t, value)
		assert.Equal(t, "2026-04-10T01:11:12Z", value.Format(time.RFC3339))
	})

	t.Run("invalid returns false", func(t *testing.T) {
		value, ok := NormalizePublishedAtCandidate("not-a-date")
		assert.False(t, ok)
		assert.Nil(t, value)
	})

	t.Run("empty returns false", func(t *testing.T) {
		value, ok := NormalizePublishedAtCandidate("")
		assert.False(t, ok)
		assert.Nil(t, value)
	})
}
