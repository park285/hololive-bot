package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 토큰 메트릭은 상한 설정과 무관하게 항상 기록돼야 한다.
func TestProvideLLMCostTrackerIsAlwaysNonNil(t *testing.T) {
	require.NotNil(t, ProvideLLMCostTracker())
}
