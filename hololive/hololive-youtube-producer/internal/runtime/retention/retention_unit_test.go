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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestRetentionDaysEnvClampsNegativeToZero(t *testing.T) {
	t.Setenv(channelSnapshotsDaysEnv, "-5")
	require.Equal(t, 0, retentionDaysEnv(channelSnapshotsDaysEnv))
}

func TestRetentionDaysEnvParseFailureIsZero(t *testing.T) {
	t.Setenv(channelSnapshotsDaysEnv, "not-a-number")
	require.Equal(t, 0, retentionDaysEnv(channelSnapshotsDaysEnv))
}

func TestRetentionDaysEnvPositivePassesThrough(t *testing.T) {
	t.Setenv(channelSnapshotsDaysEnv, "30")
	require.Equal(t, 30, retentionDaysEnv(channelSnapshotsDaysEnv))
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
	require.True(t, Config{ChannelSnapshotsDays: 1}.Enabled())
	require.True(t, Config{LiveSessionsDays: 1}.Enabled())
	require.True(t, Config{ViewerSamplesDays: 1}.Enabled())
}

func TestConfigCleanupBudgetsAreBounded(t *testing.T) {
	config := Config{
		BatchSize:        defaultBatchSize * 2,
		MaxBatches:       maxCleanupBatches * 2,
		MaxDuration:      maxCleanupDuration * 2,
		StatementTimeout: cleanupStatementTimeout * 2,
	}

	require.Equal(t, defaultBatchSize, config.effectiveBatchSize())
	require.Equal(t, maxCleanupBatches, config.effectiveMaxBatches())
	require.Equal(t, maxCleanupDuration, config.effectiveMaxDuration())
	require.Equal(t, cleanupStatementTimeout, config.effectiveStatementTimeout())
}

type fakeBatchExecutor struct {
	rowsPerExec int64
	statements  []string
}

func (f *fakeBatchExecutor) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	f.statements = append(f.statements, sql)
	return pgconn.NewCommandTag(fmt.Sprintf("DELETE %d", f.rowsPerExec)), nil
}

func TestCleanupTargetsRotatesStartAcrossTicks(t *testing.T) {
	cleaner := NewCleaner(nil, Config{
		ChannelSnapshotsDays: 30,
		LiveSessionsDays:     30,
		BatchSize:            1,
		MaxBatches:           1,
	}, nil)

	firstTick := &fakeBatchExecutor{rowsPerExec: 1}
	deleted, err := cleaner.cleanupTargets(t.Context(), firstTick)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	require.Equal(t, []string{deleteChannelSnapshotsSQL}, firstTick.statements)

	secondTick := &fakeBatchExecutor{rowsPerExec: 1}
	deleted, err = cleaner.cleanupTargets(t.Context(), secondTick)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	require.Equal(t, []string{deleteLiveSessionsSQL}, secondTick.statements)

	thirdTick := &fakeBatchExecutor{rowsPerExec: 1}
	_, err = cleaner.cleanupTargets(t.Context(), thirdTick)
	require.NoError(t, err)
	require.Equal(t, []string{deleteChannelSnapshotsSQL}, thirdTick.statements)
}

func TestCleanupTargetsBudgetExhaustedLogsStarvedTargetCount(t *testing.T) {
	var logs bytes.Buffer
	cleaner := NewCleaner(nil, Config{
		ChannelSnapshotsDays: 30,
		LiveSessionsDays:     30,
		BatchSize:            2,
		MaxBatches:           1,
	}, slog.New(slog.NewJSONHandler(&logs, nil)))

	deleted, err := cleaner.cleanupTargets(t.Context(), &fakeBatchExecutor{rowsPerExec: 1})
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)

	entry := findLogEntry(t, logs.String(), "Youtube retention cleanup budget exhausted")
	require.Equal(t, "youtube_live_sessions", entry["table"])
	require.EqualValues(t, 0, entry["deleted"])
}

func findLogEntry(t *testing.T, logOutput, msg string) map[string]any {
	t.Helper()
	for line := range strings.SplitSeq(strings.TrimSpace(logOutput), "\n") {
		entry := map[string]any{}
		require.NoError(t, json.Unmarshal([]byte(line), &entry))
		if entry["msg"] == msg {
			return entry
		}
	}
	t.Fatalf("log entry %q not found in %q", msg, logOutput)
	return nil
}
