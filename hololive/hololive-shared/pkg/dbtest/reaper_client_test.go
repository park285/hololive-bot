package dbtest

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReaperSessionFilterLineUsesCurrentSessionAndOptionalVersion(t *testing.T) {
	line := reaperSessionFilterLine("0.13.0")

	require.True(t, strings.HasSuffix(line, "\n"))
	require.Contains(t, line, "label=org.testcontainers=true")
	require.Contains(t, line, "label=org.testcontainers.lang=go")
	require.Contains(t, line, "label=org.testcontainers.reap=true")
	require.Contains(t, line, "label=org.testcontainers.sessionId=")
	require.Contains(t, line, "label=org.testcontainers.version=0.13.0")
	require.NotContains(t, reaperSessionFilterLine(""), reaperVersionLabel+"=")
}
