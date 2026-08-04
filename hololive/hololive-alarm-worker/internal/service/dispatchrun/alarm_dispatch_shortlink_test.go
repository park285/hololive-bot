package dispatchrun

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateAlarmShortLinkConfigAllowsDisabledConfiguration(t *testing.T) {
	t.Setenv(alarmShortLinkBaseURLEnv, "")

	require.NoError(t, ValidateAlarmShortLinkConfig(false))
	require.NoError(t, ValidateAlarmShortLinkConfig(true))
}

func TestValidateAlarmShortLinkConfigRejectsInvalidOrigin(t *testing.T) {
	t.Setenv(alarmShortLinkBaseURLEnv, "http://go.example.com")

	err := ValidateAlarmShortLinkConfig(false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), alarmShortLinkBaseURLEnv)
	assert.Contains(t, err.Error(), "https")
}

func TestValidateAlarmShortLinkConfigRejectsKaringConflict(t *testing.T) {
	t.Setenv(alarmShortLinkBaseURLEnv, "https://go.example.com")

	err := ValidateAlarmShortLinkConfig(true)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "ALARM_DISPATCH_KARING_ENABLED=false")
}

func TestConfiguredAlarmShortLinkBuilderBuildsLink(t *testing.T) {
	t.Setenv(alarmShortLinkBaseURLEnv, "https://go.example.com")

	builder, err := configuredAlarmShortLinkBuilder()
	require.NoError(t, err)

	link, ok := builder.URL("dQw4w9WgXcQ")
	assert.True(t, ok)
	assert.Equal(t, "https://go.example.com/l/dQw4w9WgXcQ", link)
}
