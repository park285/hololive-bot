package polltarget

import (
	"fmt"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/stretchr/testify/require"
)

func TestClassifyByActivitySeedsChannelsAcrossTierCutoffs(t *testing.T) {
	pool := dbtest.NewPool(t)

	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	activeCutoff := now.Add(-24 * time.Hour)
	warmCutoff := now.Add(-7 * 24 * time.Hour)

	channels := []struct {
		channelID  string
		activityAt time.Time
		seed       bool
		wantTier   string
	}{
		{"UC_ACTIVE_RECENT", now.Add(-time.Hour), true, "active"},
		{"UC_ACTIVE_AT_CUTOFF", activeCutoff, true, "active"},
		{"UC_WARM_JUST_PAST_ACTIVE", activeCutoff.Add(-time.Microsecond), true, "warm"},
		{"UC_WARM_MID", now.Add(-3 * 24 * time.Hour), true, "warm"},
		{"UC_WARM_AT_CUTOFF", warmCutoff, true, "warm"},
		{"UC_COLD_JUST_PAST_WARM", warmCutoff.Add(-time.Microsecond), true, "cold"},
		{"UC_COLD_OLD", warmCutoff.Add(-48 * time.Hour), true, "cold"},
		{"UC_COLD_NO_ACTIVITY", time.Time{}, false, "cold"},
	}

	notificationChannelIDs := make([]string, 0, len(channels))
	for i, c := range channels {
		notificationChannelIDs = append(notificationChannelIDs, c.channelID)
		if c.seed {
			seedPollTargetVideo(t, pool, fmt.Sprintf("tiervid-%d", i), c.channelID, c.activityAt, c.activityAt)
		}
	}

	targets := Targets{
		NotificationChannelIDs: notificationChannelIDs,
		StatsChannelIDs:        []string{"UC_STATS"},
	}

	got, err := ClassifyByActivity(t.Context(), pool, targets, now)
	require.NoError(t, err)

	tierOf := map[string]string{}
	for _, id := range got.ActiveNotificationChannelIDs {
		tierOf[id] = "active"
	}
	for _, id := range got.WarmNotificationChannelIDs {
		tierOf[id] = "warm"
	}
	for _, id := range got.ColdNotificationChannelIDs {
		tierOf[id] = "cold"
	}

	for _, c := range channels {
		require.Equal(t, c.wantTier, tierOf[c.channelID], "channel %s", c.channelID)
	}

	require.ElementsMatch(t, []string{"UC_ACTIVE_RECENT", "UC_ACTIVE_AT_CUTOFF"}, got.ActiveNotificationChannelIDs)
	require.ElementsMatch(t, []string{"UC_WARM_JUST_PAST_ACTIVE", "UC_WARM_MID", "UC_WARM_AT_CUTOFF"}, got.WarmNotificationChannelIDs)
	require.ElementsMatch(t, []string{"UC_COLD_JUST_PAST_WARM", "UC_COLD_OLD", "UC_COLD_NO_ACTIVITY"}, got.ColdNotificationChannelIDs)

	require.Equal(t, notificationChannelIDs, got.NotificationChannelIDs)
	require.Equal(t, targets.StatsChannelIDs, got.StatsChannelIDs)
}
