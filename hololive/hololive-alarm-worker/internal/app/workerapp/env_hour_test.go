package workerapp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseHourOfDayEnvReadsValidValues(t *testing.T) {
	for value, want := range map[string]int{"": 9, "0": 0, "23": 23} {
		t.Setenv("TEST_WORKERAPP_HOUR", value)

		hour, err := parseHourOfDayEnv("TEST_WORKERAPP_HOUR", 9)
		require.NoError(t, err, value)
		assert.Equal(t, want, hour, value)
	}
}

// 잘못된 값은 기본값으로 바뀌지 않고 key를 담은 오류가 된다.
func TestParseHourOfDayEnvRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"noon", "-1", "24", "99"} {
		t.Setenv("TEST_WORKERAPP_HOUR", value)

		_, err := parseHourOfDayEnv("TEST_WORKERAPP_HOUR", 0)
		require.Error(t, err, value)
		assert.Contains(t, err.Error(), "TEST_WORKERAPP_HOUR", value)
	}
}
