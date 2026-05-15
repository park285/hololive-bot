package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestParsePositiveDurationMSEnvFallsBackForInvalidValues(t *testing.T) {
	t.Setenv("TEST_DURATION_MS", "bad")
	assert.Equal(t, 250*time.Millisecond, parsePositiveDurationMSEnv("TEST_DURATION_MS", 250*time.Millisecond))

	t.Setenv("TEST_DURATION_MS", "0")
	assert.Equal(t, 250*time.Millisecond, parsePositiveDurationMSEnv("TEST_DURATION_MS", 250*time.Millisecond))
}

func TestParsePositiveDurationMSEnvParsesMilliseconds(t *testing.T) {
	t.Setenv("TEST_DURATION_MS", "1500")
	assert.Equal(t, 1500*time.Millisecond, parsePositiveDurationMSEnv("TEST_DURATION_MS", time.Second))
}

func TestParsePositiveDurationSecondsEnvParsesSeconds(t *testing.T) {
	t.Setenv("TEST_DURATION_SECONDS", "60")
	assert.Equal(t, time.Minute, parsePositiveDurationSecondsEnv("TEST_DURATION_SECONDS", time.Second))
}
