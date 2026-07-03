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

package retention

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRetentionDaysEnvClampsNegativeToZero(t *testing.T) {
	t.Setenv(statsHistoryDaysEnv, "-5")
	require.Equal(t, 0, retentionDaysEnv(statsHistoryDaysEnv))
}

func TestRetentionDaysEnvParseFailureIsZero(t *testing.T) {
	t.Setenv(statsHistoryDaysEnv, "not-a-number")
	require.Equal(t, 0, retentionDaysEnv(statsHistoryDaysEnv))
}

func TestRetentionDaysEnvPositivePassesThrough(t *testing.T) {
	t.Setenv(statsHistoryDaysEnv, "30")
	require.Equal(t, 30, retentionDaysEnv(statsHistoryDaysEnv))
}

func TestCutoffBoundaryPreservesRowAtCutoff(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	cutoff := cutoffFor(now, 30)
	require.Equal(t, now.AddDate(0, 0, -30), cutoff)

	rowAtCutoff := cutoff
	rowBeforeCutoff := cutoff.Add(-time.Microsecond)
	require.False(t, rowAtCutoff.Before(cutoff))
	require.True(t, rowBeforeCutoff.Before(cutoff))
}

func TestConfigEnabled(t *testing.T) {
	require.False(t, Config{}.Enabled())
	require.True(t, Config{StatsHistoryDays: 1}.Enabled())
	require.True(t, Config{ChannelSnapshotsDays: 1}.Enabled())
	require.True(t, Config{LiveSessionsDays: 1}.Enabled())
	require.True(t, Config{ViewerSamplesDays: 1}.Enabled())
}
