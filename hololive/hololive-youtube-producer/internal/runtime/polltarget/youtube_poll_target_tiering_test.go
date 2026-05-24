// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package polltarget

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifyYouTubePollTier(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	activeCutoff := now.Add(-24 * time.Hour)
	warmCutoff := now.Add(-7 * 24 * time.Hour)

	tests := []struct {
		name         string
		lastActivity map[string]time.Time
		want         youtubePollTier
	}{
		{
			name:         "active within 24 hours",
			lastActivity: map[string]time.Time{"UC_TARGET": now.Add(-23 * time.Hour)},
			want:         youtubePollTierActive,
		},
		{
			name:         "active at cutoff",
			lastActivity: map[string]time.Time{"UC_TARGET": activeCutoff},
			want:         youtubePollTierActive,
		},
		{
			name:         "warm within seven days",
			lastActivity: map[string]time.Time{"UC_TARGET": now.Add(-48 * time.Hour)},
			want:         youtubePollTierWarm,
		},
		{
			name:         "warm at cutoff",
			lastActivity: map[string]time.Time{"UC_TARGET": warmCutoff},
			want:         youtubePollTierWarm,
		},
		{
			name:         "cold older than seven days",
			lastActivity: map[string]time.Time{"UC_TARGET": warmCutoff.Add(-time.Nanosecond)},
			want:         youtubePollTierCold,
		},
		{
			name:         "cold when missing activity",
			lastActivity: map[string]time.Time{"UC_OTHER": now},
			want:         youtubePollTierCold,
		},
		{
			name: "cold with nil activity map",
			want: youtubePollTierCold,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := classifyYouTubePollTier("UC_TARGET", tt.lastActivity, activeCutoff, warmCutoff)

			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAppendClassifiedYouTubePollTarget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	activeCutoff := now.Add(-24 * time.Hour)
	warmCutoff := now.Add(-7 * 24 * time.Hour)

	t.Run("skips empty channel id", func(t *testing.T) {
		t.Parallel()

		out := youtubeTieredPollTargets{}

		appendClassifiedYouTubePollTarget(&out, " \t", nil, activeCutoff, warmCutoff)

		assert.Empty(t, out.ActiveNotificationChannelIDs)
		assert.Empty(t, out.WarmNotificationChannelIDs)
		assert.Empty(t, out.ColdNotificationChannelIDs)
	})

	t.Run("assigns trimmed channel ids to tier buckets", func(t *testing.T) {
		t.Parallel()

		out := youtubeTieredPollTargets{}
		lastActivity := map[string]time.Time{
			"UC_ACTIVE": now.Add(-time.Hour),
			"UC_WARM":   activeCutoff.Add(-time.Nanosecond),
			"UC_COLD":   warmCutoff.Add(-time.Nanosecond),
		}

		appendClassifiedYouTubePollTarget(&out, " UC_ACTIVE ", lastActivity, activeCutoff, warmCutoff)
		appendClassifiedYouTubePollTarget(&out, "UC_WARM", lastActivity, activeCutoff, warmCutoff)
		appendClassifiedYouTubePollTarget(&out, "UC_COLD", lastActivity, activeCutoff, warmCutoff)

		assert.Equal(t, []string{"UC_ACTIVE"}, out.ActiveNotificationChannelIDs)
		assert.Equal(t, []string{"UC_WARM"}, out.WarmNotificationChannelIDs)
		assert.Equal(t, []string{"UC_COLD"}, out.ColdNotificationChannelIDs)
	})
}

func TestClassifyYouTubePollTargetsByActivityShortCircuits(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		targets youtubePollTargets
	}{
		{
			name: "nil db keeps notification targets active",
			targets: youtubePollTargets{
				NotificationChannelIDs: []string{"UC_ACTIVE_BY_DEFAULT", " "},
				StatsChannelIDs:        []string{"UC_STATS"},
			},
		},
		{
			name: "empty notification list preserves stats targets",
			targets: youtubePollTargets{
				StatsChannelIDs: []string{"UC_STATS"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := classifyYouTubePollTargetsByActivity(t.Context(), nil, tt.targets, now)

			require.NoError(t, err)
			assert.Equal(t, tt.targets.NotificationChannelIDs, got.NotificationChannelIDs)
			assert.Equal(t, tt.targets.NotificationChannelIDs, got.ActiveNotificationChannelIDs)
			assert.Empty(t, got.WarmNotificationChannelIDs)
			assert.Empty(t, got.ColdNotificationChannelIDs)
			assert.Equal(t, tt.targets.StatsChannelIDs, got.StatsChannelIDs)
		})
	}
}
