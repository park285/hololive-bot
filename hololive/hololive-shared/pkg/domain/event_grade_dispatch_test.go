package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEventGradeDispatchLabelsDynamicRankRegionAndBoundary(t *testing.T) {
	payload := EventGradeDispatchPayload{RequestID: 1, Region: "as", Score: 123, Outcome: "top10000"}
	require.True(t, payload.Valid())
	require.Equal(t, "Global", payload.RegionLabel())
	require.Equal(t, "Top 10000", payload.Label())
	require.Equal(t, "Global 이벤트 · 제출 점수 123점: Top 10000", payload.Message())

	payload.Outcome = "boundary"
	require.Equal(t, "경계 동점—점수만으로 확정 불가.", payload.Label())
	require.Equal(t, "경계 동점—점수만으로 확정 불가.", payload.Message())

	payload.Outcome = "top0100"
	require.False(t, payload.Valid())
}
