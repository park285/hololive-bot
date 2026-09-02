package dispatchrun

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateAlarmShortLinkConfigAllowsDisabledConfiguration(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateAlarmShortLinkConfig("", false))
	require.NoError(t, ValidateAlarmShortLinkConfig("", true))
}

func TestValidateAlarmShortLinkConfigRejectsInvalidOrigin(t *testing.T) {
	t.Parallel()

	err := ValidateAlarmShortLinkConfig("http://go.example.com", false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), alarmShortLinkBaseURLEnv)
	assert.Contains(t, err.Error(), "https")
}

func TestValidateAlarmShortLinkConfigRejectsKaringConflict(t *testing.T) {
	t.Parallel()

	err := ValidateAlarmShortLinkConfig(alarmShortLinkOrigin, true)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "ALARM_DISPATCH_KARING_ENABLED=false")
}

func TestValidateAlarmShortLinkConfigRejectsUntrustedHTTPSOrigin(t *testing.T) {
	t.Parallel()

	err := ValidateAlarmShortLinkConfig("https://go.example.com", false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), alarmShortLinkOrigin)
}

func TestConfiguredAlarmShortLinkBuilderBuildsLink(t *testing.T) {
	t.Parallel()

	builder, err := configuredAlarmShortLinkBuilder(alarmShortLinkOrigin + "/")
	require.NoError(t, err)

	link, ok := builder.URL("dQw4w9WgXcQ")
	assert.True(t, ok)
	assert.Equal(t, alarmShortLinkOrigin+"/l/dQw4w9WgXcQ", link)
}
