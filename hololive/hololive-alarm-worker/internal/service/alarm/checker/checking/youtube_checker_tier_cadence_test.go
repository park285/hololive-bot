package checking

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/tier"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// Tier1 채널은 정렬 tick의 깨어남 지연이 줄어도 다음 tick에 다시 due가 되고,
// 직전 점검 시각이 iteration 시각이라 평가 창이 75초 cap에 걸리지 않아야 한다.
func TestYouTubeCheckerTier1ChannelEvaluatedEveryTickWithUncappedWindow(t *testing.T) {
	t.Parallel()

	checker, _, base := newYouTubeBuilderFixture(t)

	checker.tierScheduler = tier.NewTieredScheduler(newCheckerTestLogger())

	start := base.Add(20 * time.Minute)
	streams := map[string][]*domain.Stream{
		testChIDShort1: {{
			ID:             "tier1-upcoming",
			ChannelID:      testChIDShort1,
			Status:         domain.StreamStatusUpcoming,
			StartScheduled: &start,
			Channel:        &domain.Channel{ID: testChIDShort1, Name: testChannelName1},
		}},
	}
	subscribers := map[string][]string{testChIDShort1: {testRoomShort1}}
	ticks := []time.Time{
		base.Add(900 * time.Millisecond),
		base.Add(time.Minute + 5*time.Millisecond),
		base.Add(2*time.Minute + 400*time.Millisecond),
	}

	for index, now := range ticks {
		due := checker.selectDueYouTubeChannels(t.Context(), []string{testChIDShort1}, now)
		require.Equal(t, []string{testChIDShort1}, due, "tick %d", index)

		if index > 0 {
			work, ok := checker.prepareYouTubeChannelWork(testChIDShort1, streams, subscribers, now)
			require.True(t, ok)
			assert.False(t, work.window.Capped, "tick %d", index)
		}

		_, err := checker.collectDueYouTubeNotifications(t.Context(), due, streams, nil, subscribers, nil, now)
		require.NoError(t, err)
	}
}
