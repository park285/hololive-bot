package alarmdispatch

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAlarmShortLinkBuilderAllowsDisabledConfiguration(t *testing.T) {
	t.Parallel()

	builder, err := NewAlarmShortLinkBuilder("")
	require.NoError(t, err)
	assert.False(t, builder.Enabled())
}

func TestNewAlarmShortLinkBuilderRejectsInvalidOrigin(t *testing.T) {
	t.Parallel()

	_, err := NewAlarmShortLinkBuilder("http://go.example.com")

	require.Error(t, err)
	assert.Contains(t, err.Error(), alarmShortLinkBaseURLEnv)
	assert.Contains(t, err.Error(), "https")
}

func TestNewAlarmShortLinkBuilderRejectsUntrustedHTTPSOrigin(t *testing.T) {
	t.Parallel()

	_, err := NewAlarmShortLinkBuilder("https://go.example.com")

	require.Error(t, err)
	assert.Contains(t, err.Error(), alarmShortLinkOrigin)
}

func TestNewAlarmShortLinkBuilderBuildsLink(t *testing.T) {
	t.Parallel()

	builder, err := NewAlarmShortLinkBuilder(alarmShortLinkOrigin + "/")
	require.NoError(t, err)

	link, ok := builder.URL("dQw4w9WgXcQ")
	assert.True(t, ok)
	assert.Equal(t, alarmShortLinkOrigin+"/l/dQw4w9WgXcQ", link)
}
