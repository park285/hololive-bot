package dispatch

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolvePayloadPublishedAtParsesPointer(t *testing.T) {
	want := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)

	t.Run("video payload published_at", func(t *testing.T) {
		got := resolveVideoPayloadPublishedAt(`{"published_at":"2026-06-24T10:00:00Z"}`)
		require.NotNil(t, got)
		assert.True(t, got.Equal(want))
	})

	t.Run("community payload published_at", func(t *testing.T) {
		got := resolveCommunityPayloadPublishedAt(`{"published_at":"2026-06-24T10:00:00Z"}`)
		require.NotNil(t, got)
		assert.True(t, got.Equal(want))
	})

	t.Run("missing published_at returns nil", func(t *testing.T) {
		assert.Nil(t, resolveVideoPayloadPublishedAt(`{}`))
		assert.Nil(t, resolveCommunityPayloadPublishedAt(`{}`))
	})

	t.Run("invalid payload returns nil", func(t *testing.T) {
		assert.Nil(t, resolveVideoPayloadPublishedAt(`not json`))
	})
}
