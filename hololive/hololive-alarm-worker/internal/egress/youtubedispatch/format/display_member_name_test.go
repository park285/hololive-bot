package format

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// members 표시명이 있으면 그대로 쓰고, 비어 있을 때만 종단 문구 경로로 가서 누락 횟수를 센다.
func TestDisplayMemberNameCountsOnlyMissingNames(t *testing.T) {
	formatter := &MessageFormatter{}
	before := testutil.ToFloat64(memberNameMissingTotal())

	require.Equal(t, "페코라", formatter.DisplayMemberName("  페코라 "))
	require.InDelta(t, before, testutil.ToFloat64(memberNameMissingTotal()), 0)

	formatter.DisplayMemberName(" ")
	require.InDelta(t, before+1, testutil.ToFloat64(memberNameMissingTotal()), 0)
}
