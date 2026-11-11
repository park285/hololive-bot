package format

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestDisplayMemberNameCountsOnlyMissingNames(t *testing.T) {
	formatter := &MessageFormatter{}
	before := testutil.ToFloat64(memberNameMissingTotal())

	require.Equal(t, "페코라", formatter.DisplayMemberName("  페코라 "))
	require.InDelta(t, before, testutil.ToFloat64(memberNameMissingTotal()), 0)

	formatter.DisplayMemberName(" ")
	require.InDelta(t, before+1, testutil.ToFloat64(memberNameMissingTotal()), 0)
}
