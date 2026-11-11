package handlers

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	broadcasttype "github.com/kapu/hololive-api/internal/planes/bot/internal/broadcasttype"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
)

type fakeHistoryPageCall struct {
	cursorAt      *time.Time
	cursorVideoID string
	pageLimit     int
}

// fakeHistoryDB는 정렬된 rows에서 커서 행 다음부터 pageLimit 행을 돌려주고 페이지 요청을 기록한다. 오류를 돌려주는
// fail 호출은 그 페이지 조회를 실패시킨다.
type fakeHistoryDB struct {
	broadcastHistoryDB

	rows  []handlercore.BroadcastHistoryEntry
	fail  func(ctx context.Context, call int) error
	calls []fakeHistoryPageCall
}

func (db *fakeHistoryDB) Query(ctx context.Context, _ string, args ...any) (pgx.Rows, error) {
	if len(args) < 6 {
		return nil, fmt.Errorf("fake history query: got %d args, want 6", len(args))
	}

	call := fakeHistoryPageCall{}

	if cursorAt, ok := args[2].(*time.Time); ok {
		call.cursorAt = cursorAt
	}

	if cursorVideoID, ok := args[3].(string); ok {
		call.cursorVideoID = cursorVideoID
	}

	if pageLimit, ok := args[5].(int); ok {
		call.pageLimit = pageLimit
	}

	db.calls = append(db.calls, call)

	if db.fail != nil {
		if err := db.fail(ctx, len(db.calls)-1); err != nil {
			return nil, err
		}
	}

	start := 0

	if call.cursorVideoID != "" {
		start = slices.IndexFunc(db.rows, func(entry handlercore.BroadcastHistoryEntry) bool {
			return entry.VideoID == call.cursorVideoID
		}) + 1
	}

	return &fakeHistoryRows{entries: db.rows[start:min(start+call.pageLimit, len(db.rows))]}, nil
}

type fakeHistoryRows struct {
	pgx.Rows

	entries []handlercore.BroadcastHistoryEntry
	current int
}

func (r *fakeHistoryRows) Next() bool {
	r.current++

	return r.current <= len(r.entries)
}

func (r *fakeHistoryRows) Scan(dest ...any) error {
	entry := &r.entries[r.current-1]

	values := []any{
		entry.VideoID, entry.ChannelID, entry.MemberName, entry.Title, entry.TopicID, entry.ThumbnailURL,
		entry.ScheduledStartTime, entry.StartedAt, entry.EndedAt, entry.LastSeenAt, entry.LifecycleOrigin,
		entry.LastLivePositiveAt, entry.TimesSource, entry.FinalizationOutcome, entry.FinalizationReason,
	}
	if len(dest) != len(values) {
		return fmt.Errorf("fake history scan: got %d destinations, want %d", len(dest), len(values))
	}

	for i, value := range values {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(value))
	}

	return nil
}

func (*fakeHistoryRows) Close() {}

func (*fakeHistoryRows) Err() error { return nil }

func fakeHistoryEntries(prefix, topicID string, count int, newest time.Time) []handlercore.BroadcastHistoryEntry {
	entries := make([]handlercore.BroadcastHistoryEntry, 0, count)
	for i := range count {
		endedAt := newest.Add(-time.Duration(i) * time.Minute)

		entries = append(entries, handlercore.BroadcastHistoryEntry{
			VideoID: fmt.Sprintf("%s-%03d", prefix, i), ChannelID: "channel-a", Title: "test", TopicID: topicID,
			EndedAt: &endedAt, LastSeenAt: endedAt,
		})
	}

	return entries
}

func historyVideoIDs(entries []handlercore.BroadcastHistoryEntry) []string {
	ids := make([]string, 0, len(entries))
	for i := range entries {
		ids = append(ids, entries[i].VideoID)
	}

	return ids
}

// Type·TopicID가 없는 조회는 표시 건수+1행만 읽는다. Go에서 거르는 조회는 101행 페이지를 유지하고, 다음 페이지 커서는
// 다음 페이지 존재 확인용 101번째 행이 아니라 처리한 100번째 행이다.
func TestPgBroadcastHistoryRepositoryPageSizeFollowsGoFilters(t *testing.T) {
	t.Parallel()

	newest := time.Date(2026, time.November, 1, 12, 0, 0, 0, time.UTC)
	unfiltered := fakeHistoryEntries("game", testTopicMinecraft, 20, newest)
	// 처리 페이지 100행은 모두 거르는 대상이고, 101번째 행부터 일치 행이다.
	filtered := slices.Concat(
		fakeHistoryEntries("talk", "", broadcastHistoryPageSize, newest),
		fakeHistoryEntries("game", testTopicMinecraft, 3, newest.Add(-broadcastHistoryPageSize*time.Minute)),
	)
	pageCursor := filtered[broadcastHistoryPageSize-1]
	filteredCalls := []fakeHistoryPageCall{
		{pageLimit: broadcastHistoryPageSize + 1},
		{cursorAt: pageCursor.EndedAt, cursorVideoID: pageCursor.VideoID, pageLimit: broadcastHistoryPageSize + 1},
	}

	for name, tc := range map[string]struct {
		query     handlercore.BroadcastHistoryQuery
		rows      []handlercore.BroadcastHistoryEntry
		wantCalls []fakeHistoryPageCall
		wantIDs   []string
	}{
		"no filter": {
			query:     handlercore.BroadcastHistoryQuery{Limit: defaultBroadcastHistoryLimit},
			rows:      unfiltered,
			wantCalls: []fakeHistoryPageCall{{pageLimit: defaultBroadcastHistoryLimit + 1}},
			wantIDs:   historyVideoIDs(unfiltered[:defaultBroadcastHistoryLimit]),
		},
		"no filter shorter than limit": {
			query:     handlercore.BroadcastHistoryQuery{Limit: defaultBroadcastHistoryLimit},
			rows:      unfiltered[:5],
			wantCalls: []fakeHistoryPageCall{{pageLimit: defaultBroadcastHistoryLimit + 1}},
			wantIDs:   historyVideoIDs(unfiltered[:5]),
		},
		"type filter": {
			query:     handlercore.BroadcastHistoryQuery{Type: string(broadcasttype.Game), Limit: defaultBroadcastHistoryLimit},
			rows:      filtered,
			wantCalls: filteredCalls,
			wantIDs:   historyVideoIDs(filtered[broadcastHistoryPageSize:]),
		},
		"topic filter": {
			query:     handlercore.BroadcastHistoryQuery{TopicID: testTopicMinecraft, Limit: defaultBroadcastHistoryLimit},
			rows:      filtered,
			wantCalls: filteredCalls,
			wantIDs:   historyVideoIDs(filtered[broadcastHistoryPageSize:]),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			db := &fakeHistoryDB{rows: tc.rows}
			repo := &pgBroadcastHistoryRepository{pool: db}

			result, err := repo.ListEndedBroadcasts(t.Context(), &tc.query)
			require.NoError(t, err)
			require.False(t, result.Truncated)
			require.Equal(t, tc.wantIDs, historyVideoIDs(result.Entries))
			require.Equal(t, tc.wantCalls, db.calls)
		})
	}
}

// 조회 예산 deadline에 걸린 페이지는 오류가 아니라 그때까지 모은 결과와 Truncated로 끝난다.
func TestPgBroadcastHistoryRepositoryElapsedBudgetReturnsTruncatedResult(t *testing.T) {
	t.Parallel()

	newest := time.Date(2026, time.November, 1, 12, 0, 0, 0, time.UTC)
	rows := slices.Concat(
		fakeHistoryEntries("game", testTopicMinecraft, 1, newest),
		fakeHistoryEntries("talk", "", broadcastHistoryPageSize+1, newest.Add(-time.Minute)),
	)
	db := &fakeHistoryDB{rows: rows, fail: func(ctx context.Context, call int) error {
		if call == 0 {
			return nil
		}

		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > broadcastHistoryQueryBudget {
			return errors.New("page query has no elapsed budget deadline")
		}

		return fmt.Errorf("query page: %w", context.DeadlineExceeded)
	}}
	repo := &pgBroadcastHistoryRepository{pool: db}

	result, err := repo.ListEndedBroadcasts(t.Context(), &handlercore.BroadcastHistoryQuery{
		Type:  string(broadcasttype.Game),
		Limit: defaultBroadcastHistoryLimit,
	})
	require.NoError(t, err)
	require.True(t, result.Truncated)
	require.Equal(t, historyVideoIDs(rows[:1]), historyVideoIDs(result.Entries))
	require.Len(t, db.calls, 2)
}
