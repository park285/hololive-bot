package shortlink

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewYouTubeBuilderBuildsBoundedLink(t *testing.T) {
	t.Parallel()

	builder, err := NewYouTubeBuilder("  https://go.example.com/  ")
	require.NoError(t, err)
	assert.True(t, builder.Enabled())

	link, ok := builder.URL("dQw4w9WgXcQ")
	assert.True(t, ok)
	assert.Equal(t, "https://go.example.com/l/dQw4w9WgXcQ", link)
}

func TestNewYouTubeBuilderAllowsDisabledConfiguration(t *testing.T) {
	t.Parallel()

	builder, err := NewYouTubeBuilder("   ")
	require.NoError(t, err)
	assert.False(t, builder.Enabled())

	link, ok := builder.URL("dQw4w9WgXcQ")
	assert.False(t, ok)
	assert.Empty(t, link)
}

func TestNewYouTubeBuilderRejectsUnsafeOrigins(t *testing.T) {
	t.Parallel()

	for _, origin := range []string{
		"http://go.example.com",
		"https://user:pass@go.example.com",
		"https://go.example.com/prefix",
		"https://go.example.com?next=https://example.net",
		"https://go.example.com/#fragment",
		"https://go.example.com?",
		"https://go.example.com/%2F",
		"not-a-url",
	} {
		t.Run(origin, func(t *testing.T) {
			t.Parallel()

			builder, err := NewYouTubeBuilder(origin)
			require.Error(t, err)
			assert.False(t, builder.Enabled())
		})
	}
}

func TestValidYouTubeVideoID(t *testing.T) {
	t.Parallel()

	assert.True(t, ValidYouTubeVideoID("dQw4w9WgXcQ"))
	assert.True(t, ValidYouTubeVideoID("abcDEF_12-3"))
	assert.False(t, ValidYouTubeVideoID("too-short"))
	assert.False(t, ValidYouTubeVideoID("dQw4w9WgXc/"))
	assert.False(t, ValidYouTubeVideoID("가나다라마바사"))
}
