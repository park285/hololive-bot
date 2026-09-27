package envconfig

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseUsesDefaultWhenUnsetOrBlank(t *testing.T) {
	for _, value := range []string{"", "   "} {
		t.Setenv("TEST_ENVCONFIG", value)

		count, err := ParsePositiveInt("TEST_ENVCONFIG", 7)
		require.NoError(t, err)
		assert.Equal(t, 7, count)

		hour, err := ParseHourOfDay("TEST_ENVCONFIG", 9)
		require.NoError(t, err)
		assert.Equal(t, 9, hour)

		interval, err := ParsePositiveDurationMS("TEST_ENVCONFIG", 250*time.Millisecond)
		require.NoError(t, err)
		assert.Equal(t, 250*time.Millisecond, interval)
	}
}

func TestParseReadsValidValues(t *testing.T) {
	t.Setenv("TEST_ENVCONFIG", "23")

	count, err := ParsePositiveInt("TEST_ENVCONFIG", 1)
	require.NoError(t, err)
	assert.Equal(t, 23, count)

	hour, err := ParseHourOfDay("TEST_ENVCONFIG", 0)
	require.NoError(t, err)
	assert.Equal(t, 23, hour)

	t.Setenv("TEST_ENVCONFIG", "1500")

	interval, err := ParsePositiveDurationMS("TEST_ENVCONFIG", time.Second)
	require.NoError(t, err)
	assert.Equal(t, 1500*time.Millisecond, interval)

	t.Setenv("TEST_ENVCONFIG", "0")

	hour, err = ParseHourOfDay("TEST_ENVCONFIG", 9)
	require.NoError(t, err)
	assert.Equal(t, 0, hour)
}

// 잘못된 값은 기본값으로 바뀌지 않고 key를 담은 오류가 된다(holo-alarm-worker-envconfig-silent-defaults).
func TestParseRejectsInvalidValues(t *testing.T) {
	parsers := map[string]func() error{
		"positive int": func() error {
			_, err := ParsePositiveInt("TEST_ENVCONFIG", 1000)
			return err
		},
		"hour": func() error {
			_, err := ParseHourOfDay("TEST_ENVCONFIG", 0)
			return err
		},
		"positive ms": func() error {
			_, err := ParsePositiveDurationMS("TEST_ENVCONFIG", time.Hour)
			return err
		},
	}

	invalid := map[string][]string{
		"positive int": {"bad", "1.5", "0", "-1", "99999999999999999999"},
		"hour":         {"noon", "-1", "24", "99"},
		"positive ms":  {"30m", "0", "-5", "9223372036854776"},
	}

	for name, values := range invalid {
		for _, value := range values {
			t.Run(name+"="+value, func(t *testing.T) {
				t.Setenv("TEST_ENVCONFIG", value)

				err := parsers[name]()
				require.Error(t, err)
				assert.Contains(t, err.Error(), "TEST_ENVCONFIG")
			})
		}
	}
}
