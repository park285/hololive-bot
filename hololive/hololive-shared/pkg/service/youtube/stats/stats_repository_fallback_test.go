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
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type fakeStatsRepositoryDB struct {
	execFn     func(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error)
	queryFn    func(ctx context.Context, query string, args ...any) (pgx.Rows, error)
	queryRowFn func(ctx context.Context, query string, args ...any) pgx.Row
}

func (f *fakeStatsRepositoryDB) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if f.execFn == nil {
		return pgconn.NewCommandTag(""), nil
	}
	return f.execFn(ctx, query, args...)
}

func (f *fakeStatsRepositoryDB) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	if f.queryFn == nil {
		return nil, errors.New("query not implemented")
	}
	return f.queryFn(ctx, query, args...)
}

func (f *fakeStatsRepositoryDB) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	if f.queryRowFn == nil {
		return fakeRow{scanFn: func(_ ...any) error { return errors.New("query row not implemented") }}
	}
	return f.queryRowFn(ctx, query, args...)
}

type fakeRow struct {
	scanFn func(dest ...any) error
}

func (r fakeRow) Scan(dest ...any) error {
	return r.scanFn(dest...)
}

func newTestStatsRepository(db statsRepositoryDB) *StatsRepository {
	return &StatsRepository{
		pool:                 db,
		logger:               slog.New(slog.NewTextHandler(io.Discard, nil)),
		latestTableAvailable: true,
	}
}

func newTestTimestampedStats(channelID string) *domain.TimestampedStats {
	return &domain.TimestampedStats{
		Timestamp:       time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		ChannelID:       channelID,
		MemberName:      "test-member",
		SubscriberCount: 123_456,
		VideoCount:      100,
		ViewCount:       1_000_000,
	}
}

func TestSaveStats_DisablesLatestSnapshotOnUndefinedTable(t *testing.T) {
	t.Helper()

	var historyExecCount int
	var latestExecCount int

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

	if err := repo.SaveStats(context.Background(), newTestTimestampedStats("UC1")); err != nil {
		t.Fatalf("first SaveStats returned error: %v", err)
	}
	if err := repo.SaveStats(context.Background(), newTestTimestampedStats("UC1")); err != nil {
		t.Fatalf("second SaveStats returned error: %v", err)
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

func TestSaveStats_ReturnsErrorOnLatestSnapshotFailure(t *testing.T) {
	t.Helper()

	db := &fakeStatsRepositoryDB{
		execFn: func(_ context.Context, query string, _ ...any) (pgconn.CommandTag, error) {
			if strings.Contains(query, "youtube_channel_latest_stats") {
				return pgconn.NewCommandTag(""), errors.New("connection reset")
			}
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		},
	}
	repo := newTestStatsRepository(db)

	err := repo.SaveStats(context.Background(), newTestTimestampedStats("UC2"))
	if err == nil {
		t.Fatal("SaveStats error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "failed to save latest stats snapshot") {
		t.Fatalf("error = %q, want contains %q", err.Error(), "failed to save latest stats snapshot")
	}
	if !repo.isLatestTableAvailable() {
		t.Fatal("latestTableAvailable should remain true on non-undefined-table error")
	}
}

func TestGetLatestStats_FallsBackToHistoryWhenSnapshotTableMissing(t *testing.T) {
	t.Helper()

	calls := make([]string, 0, 2)

	db := &fakeStatsRepositoryDB{
		queryRowFn: func(_ context.Context, query string, _ ...any) pgx.Row {
			switch {
			case strings.Contains(query, "youtube_channel_latest_stats"):
				calls = append(calls, "snapshot")
				return fakeRow{
					scanFn: func(_ ...any) error {
						return &pgconn.PgError{Code: "42P01"}
					},
				}
			case strings.Contains(query, "youtube_stats_history"):
				calls = append(calls, "history")
				return fakeRow{
					scanFn: func(dest ...any) error {
						timestamp := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
						memberName := "fallback-member"
						*dest[0].(*time.Time) = timestamp
						*dest[1].(*string) = "UC3"
						*dest[2].(**string) = &memberName
						*dest[3].(*uint64) = 999_999
						*dest[4].(*uint64) = 77
						*dest[5].(*uint64) = 8_888_888
						return nil
					},
				}
			default:
				return fakeRow{
					scanFn: func(_ ...any) error {
						return errors.New("unexpected query")
					},
				}
			}
		},
	}
	repo := newTestStatsRepository(db)

	got, err := repo.GetLatestStats(context.Background(), "UC3")
	if err != nil {
		t.Fatalf("GetLatestStats error: %v", err)
	}
	if got == nil {
		t.Fatal("GetLatestStats returned nil, want stats")
	}
	if got.ChannelID != "UC3" || got.MemberName != "fallback-member" || got.SubscriberCount != 999_999 {
		t.Fatalf("unexpected stats: %#v", got)
	}
	if len(calls) != 2 || calls[0] != "snapshot" || calls[1] != "history" {
		t.Fatalf("query call order = %#v, want [snapshot history]", calls)
	}
	if repo.isLatestTableAvailable() {
		t.Fatal("latestTableAvailable should be false after snapshot table missing")
	}
}
