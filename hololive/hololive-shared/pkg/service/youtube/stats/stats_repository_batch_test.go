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

package stats

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func makeBatchStats(n int) []*domain.TimestampedStats {
	stats := make([]*domain.TimestampedStats, n)
	for i := range stats {
		stats[i] = &domain.TimestampedStats{
			Timestamp:       time.Date(2026, 3, 1, 0, i, 0, 0, time.UTC),
			ChannelID:       "UC" + strings.Repeat("0", 3-len(string(rune('A'+i%26)))) + string(rune('A'+i%26)),
			MemberName:      "member-" + string(rune('A'+i%26)),
			SubscriberCount: uint64(1000 + i),
			VideoCount:      uint64(100 + i),
			ViewCount:       uint64(50000 + i),
		}
	}
	return stats
}

func TestSaveStatsBatch(t *testing.T) {
	testCases := []struct {
		name      string
		count     int
		wantExecs int
	}{
		{name: "empty slice", count: 0, wantExecs: 0},
		{name: "single item", count: 1, wantExecs: 1},
		{name: "50 items", count: 50, wantExecs: 1},
		{name: "100 items (max batch)", count: 100, wantExecs: 1},
		{name: "101 items (two chunks)", count: 101, wantExecs: 2},
		{name: "250 items (three chunks)", count: 250, wantExecs: 3},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			historyExecCount := 0
			latestExecCount := 0
			db := &fakeStatsRepositoryDB{
				execFn: func(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
					if strings.Contains(query, "youtube_channel_latest_stats") {
						latestExecCount++

						wantArgs := min(tc.count-(latestExecCount-1)*saveBatchMaxSize, saveBatchMaxSize) * columnsPerRow
						if len(args) != wantArgs {
							t.Fatalf("latest exec %d: got %d args, want %d", latestExecCount, len(args), wantArgs)
						}

						return pgconn.NewCommandTag("INSERT 0 1"), nil
					}
					historyExecCount++

					// INSERT VALUES 절이 포함되었는지 검증
					if !strings.Contains(query, "INSERT INTO youtube_stats_history") {
						t.Fatalf("unexpected query: %s", query)
					}
					if !strings.Contains(query, "ON CONFLICT") {
						t.Fatalf("missing ON CONFLICT clause: %s", query)
					}

					// 파라미터 수 검증
					wantArgs := min(tc.count-(historyExecCount-1)*saveBatchMaxSize, saveBatchMaxSize) * columnsPerRow
					if len(args) != wantArgs {
						t.Fatalf("history exec %d: got %d args, want %d", historyExecCount, len(args), wantArgs)
					}

					return pgconn.NewCommandTag("INSERT 0 1"), nil
				},
			}
			repo := newTestStatsRepository(db)

			stats := makeBatchStats(tc.count)
			if err := repo.SaveStatsBatch(context.Background(), stats); err != nil {
				t.Fatalf("SaveStatsBatch error: %v", err)
			}

			if historyExecCount != tc.wantExecs {
				t.Fatalf("history exec count = %d, want %d", historyExecCount, tc.wantExecs)
			}
			if latestExecCount != tc.wantExecs {
				t.Fatalf("latest exec count = %d, want %d", latestExecCount, tc.wantExecs)
			}
		})
	}
}

func TestSaveStatsBatch_Error(t *testing.T) {
	db := &fakeStatsRepositoryDB{
		execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag(""), &pgconn.PgError{Code: "23505", Message: "duplicate key"}
		},
	}
	repo := newTestStatsRepository(db)

	stats := makeBatchStats(3)
	err := repo.SaveStatsBatch(context.Background(), stats)
	if err == nil {
		t.Fatal("SaveStatsBatch error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "failed to batch save stats") {
		t.Fatalf("error = %q, want contains 'failed to batch save stats'", err.Error())
	}
}

func TestSaveStatsBatch_UpsertsLatestSnapshot(t *testing.T) {
	historyExecCount := 0
	latestExecCount := 0

	db := &fakeStatsRepositoryDB{
		execFn: func(_ context.Context, query string, _ ...any) (pgconn.CommandTag, error) {
			if strings.Contains(query, "youtube_channel_latest_stats") {
				latestExecCount++
				return pgconn.NewCommandTag("INSERT 0 1"), nil
			}
			historyExecCount++
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		},
	}
	repo := newTestStatsRepository(db)

	if err := repo.SaveStatsBatch(context.Background(), makeBatchStats(3)); err != nil {
		t.Fatalf("SaveStatsBatch error: %v", err)
	}

	if historyExecCount != 1 {
		t.Fatalf("historyExecCount = %d, want 1", historyExecCount)
	}
	if latestExecCount != 1 {
		t.Fatalf("latestExecCount = %d, want 1", latestExecCount)
	}
}

func TestSaveStatsBatch_DisablesLatestSnapshotOnUndefinedTable(t *testing.T) {
	historyExecCount := 0
	latestExecCount := 0

	db := &fakeStatsRepositoryDB{
		execFn: func(_ context.Context, query string, _ ...any) (pgconn.CommandTag, error) {
			if strings.Contains(query, "youtube_channel_latest_stats") {
				latestExecCount++
				return pgconn.NewCommandTag(""), &pgconn.PgError{Code: "42P01"}
			}
			historyExecCount++
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		},
	}
	repo := newTestStatsRepository(db)

	if err := repo.SaveStatsBatch(context.Background(), makeBatchStats(3)); err != nil {
		t.Fatalf("first SaveStatsBatch error: %v", err)
	}
	if err := repo.SaveStatsBatch(context.Background(), makeBatchStats(3)); err != nil {
		t.Fatalf("second SaveStatsBatch error: %v", err)
	}

	if historyExecCount != 2 {
		t.Fatalf("historyExecCount = %d, want 2", historyExecCount)
	}
	if latestExecCount != 1 {
		t.Fatalf("latestExecCount = %d, want 1", latestExecCount)
	}
	if repo.isLatestTableAvailable() {
		t.Fatal("latestTableAvailable should be false after undefined table error")
	}
}

func TestSaveStatsBatch_ReturnsErrorOnLatestSnapshotFailure(t *testing.T) {
	db := &fakeStatsRepositoryDB{
		execFn: func(_ context.Context, query string, _ ...any) (pgconn.CommandTag, error) {
			if strings.Contains(query, "youtube_channel_latest_stats") {
				return pgconn.NewCommandTag(""), &pgconn.PgError{Code: "08006", Message: "connection failure"}
			}
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		},
	}
	repo := newTestStatsRepository(db)

	err := repo.SaveStatsBatch(context.Background(), makeBatchStats(2))
	if err == nil {
		t.Fatal("SaveStatsBatch error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "failed to save latest stats snapshot batch") {
		t.Fatalf("error = %q, want contains %q", err.Error(), "failed to save latest stats snapshot batch")
	}
	if !repo.isLatestTableAvailable() {
		t.Fatal("latestTableAvailable should remain true on non-undefined-table error")
	}
}
