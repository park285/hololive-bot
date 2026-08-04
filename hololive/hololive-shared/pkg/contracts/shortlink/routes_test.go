package shortlink

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestYouTubeRouteContract(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "/l/", YouTubePathPrefix)
	assert.Equal(t, "/l/:videoID", YouTubeRoute)
}
