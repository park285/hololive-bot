package handlers

import (
	jsonv2 "encoding/json/v2"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	historyPagingChannel = "history-paging-channel"
	historyOtherChannel  = "history-other-channel"
)

// 커서는 Go의 broadcastHistorySortTime으로 만들고 SQL은 history_sort_at으로 정렬한다. 두 우선순위
// (ended → started → scheduled → last_seen)와 같은 시각의 video_id 역순이 어긋나면 페이지 경계에서 행이 겹치거나 빠진다.
func TestPgBroadcastHistoryRepositoryPagesAcrossSortTimeFallbacks(t *testing.T) {
	pool := dbtest.NewPool(t)
	base := time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC)
	at := func(hours int) time.Time { return base.Add(time.Duration(hours) * time.Hour) }

	seedHistorySessions(t, pool, []historySession{
		{videoID: "v-ended", channelID: historyPagingChannel, status: domain.LiveStatusEnded, scheduled: new(at(1)), started: new(at(2)), ended: new(at(5)), lastSeen: at(9)},
		{videoID: "v-tie-b", channelID: historyPagingChannel, status: domain.LiveStatusEnded, ended: new(at(4)), lastSeen: at(9)},
		{videoID: "v-tie-a", channelID: historyPagingChannel, status: domain.LiveStatusEnded, ended: new(at(4)), lastSeen: at(9)},
		{videoID: "v-started", channelID: historyPagingChannel, status: domain.LiveStatusEnded, scheduled: new(at(1)), started: new(at(4)), lastSeen: at(9)},
		{videoID: "v-other", channelID: historyOtherChannel, status: domain.LiveStatusEnded, ended: new(at(3)), lastSeen: at(9)},
		{videoID: "v-scheduled", channelID: historyPagingChannel, status: domain.LiveStatusEnded, scheduled: new(at(3)), lastSeen: at(9)},
		{videoID: "v-last-seen", channelID: historyPagingChannel, status: domain.LiveStatusEnded, lastSeen: at(2)},
		{videoID: "v-live", channelID: historyPagingChannel, status: domain.LiveStatusLive, started: new(at(8)), lastSeen: at(9)},
		{videoID: "v-before-since", channelID: historyPagingChannel, status: domain.LiveStatusEnded, ended: new(at(-2)), lastSeen: at(9)},
	})

	since := at(0)
	repo := &pgBroadcastHistoryRepository{pool: pool}

	for name, tc := range map[string]struct {
		channelID string
		want      []string
	}{
		"all channels": {want: []string{"v-ended", "v-tie-b", "v-tie-a", "v-started", "v-scheduled", "v-other", "v-last-seen"}},
		"one channel":  {channelID: historyPagingChannel, want: []string{"v-ended", "v-tie-b", "v-tie-a", "v-started", "v-scheduled", "v-last-seen"}},
	} {
		t.Run(name, func(t *testing.T) {
			query := &handlercore.BroadcastHistoryQuery{ChannelID: tc.channelID}

			var (
				got           []string
				cursorAt      *time.Time
				cursorVideoID string
			)

			// 페이지 크기 2는 같은 시각 세 행(at(4))을 페이지 경계로 나눈다.
			for range len(tc.want) + 1 {
				page, err := repo.listEndedBroadcastPage(t.Context(), query, &since, cursorAt, cursorVideoID, 2)
				require.NoError(t, err)

				if len(page) == 0 {
					break
				}

				for _, entry := range page {
					got = append(got, entry.VideoID)
				}

				last := page[len(page)-1]

				cursorAt = new(broadcastHistorySortTime(&last))
				cursorVideoID = last.VideoID
			}

			require.Equal(t, tc.want, got)
		})
	}
}

const (
	historyPlanSessions = 20000
	historyPlanChannels = 40
	historyPlanPageSize = broadcastHistoryPageSize + 1
)

type historyPlanNode struct {
	NodeType      string            `json:"Node Type"`
	Relation      string            `json:"Relation Name"`
	IndexName     string            `json:"Index Name"`
	Rows          float64           `json:"Actual Rows"`
	Loops         float64           `json:"Actual Loops"`
	FilterRemoved float64           `json:"Rows Removed by Filter"`
	Plans         []historyPlanNode `json:"Plans"`
}

// 방송 이력 페이지는 종료 세션 전체를 정렬하지 않고 history_sort_at 정렬 인덱스에서 한 페이지만 읽어야 한다.
// 종료 세션은 보존 삭제가 없어 계속 늘어난다.
func TestBroadcastHistoryPageReadsSortIndexInsteadOfEndedHistory(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	now := time.Date(2026, time.October, 12, 0, 0, 0, 0, time.UTC)

	_, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions(video_id, channel_id, status, title, ended_at, last_seen_at)
		SELECT 'plan-' || n, 'plan-channel-' || (n % $2), 'ENDED', 'plan',
		       $3::timestamptz - n * INTERVAL '1 minute', $3::timestamptz
		FROM generate_series(1, $1::int) AS n
	`, historyPlanSessions, historyPlanChannels, now)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, "ANALYZE youtube_live_sessions")
	require.NoError(t, err)

	since := now.Add(-maxBroadcastHistoryDays * 24 * time.Hour)
	cursorAt := now.Add(-time.Hour)

	for name, tc := range map[string]struct {
		channelID string
		cursorAt  *time.Time
		wantIndex string
	}{
		"all channels first page": {wantIndex: "idx_yls_ended_history_sort"},
		"all channels next page":  {cursorAt: &cursorAt, wantIndex: "idx_yls_ended_history_sort"},
		"one channel first page":  {channelID: "plan-channel-7", wantIndex: "idx_yls_ended_channel_history_sort"},
		"one channel next page":   {channelID: "plan-channel-7", cursorAt: &cursorAt, wantIndex: "idx_yls_ended_channel_history_sort"},
	} {
		t.Run(name, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			require.NoError(t, err)

			defer func() { require.NoError(t, tx.Rollback(ctx)) }()

			_, err = tx.Exec(ctx, "SET LOCAL plan_cache_mode = force_custom_plan")
			require.NoError(t, err)

			var raw []byte

			err = tx.QueryRow(ctx, "EXPLAIN (ANALYZE, FORMAT JSON) "+broadcastHistoryListPageSQL,
				tc.channelID, since, tc.cursorAt, "", "", historyPlanPageSize).Scan(&raw)
			require.NoError(t, err)

			var plans []struct {
				Plan historyPlanNode `json:"Plan"`
			}

			require.NoError(t, jsonv2.Unmarshal(raw, &plans))
			require.Len(t, plans, 1)

			// 커서 조건은 OR라 인덱스 시작 위치를 정하지 못하고, 다음 페이지는 커서보다 새 행을 앞에서부터 건너뛴다.
			// 그 행과 한 페이지를 넘어 읽으면 종료 이력 전체를 읽는 계획으로 바뀐 것이다.
			var newer int

			err = tx.QueryRow(ctx, `
				SELECT count(*) FROM youtube_live_sessions
				WHERE status = 'ENDED' AND ($1 = '' OR channel_id = $1)
				  AND $2::timestamptz IS NOT NULL AND history_sort_at >= $2
			`, tc.channelID, tc.cursorAt).Scan(&newer)
			require.NoError(t, err)

			stats := collectHistoryPlanStats(plans[0].Plan)
			require.False(t, stats.seqScan, "history page sequentially scanned youtube_live_sessions")
			require.Contains(t, stats.indexes, tc.wantIndex)
			require.LessOrEqual(t, stats.examined, float64(newer+historyPlanPageSize),
				"history page examined sessions beyond the cursor window and one page")
		})
	}
}

type historyPlanStats struct {
	seqScan  bool
	indexes  []string
	examined float64
}

func collectHistoryPlanStats(node historyPlanNode) historyPlanStats {
	var stats historyPlanStats

	if node.Relation == "youtube_live_sessions" {
		stats.seqScan = node.NodeType == "Seq Scan"
		stats.examined = (node.Rows + node.FilterRemoved) * max(node.Loops, 1)

		if node.IndexName != "" {
			stats.indexes = append(stats.indexes, node.IndexName)
		}
	}

	for _, child := range node.Plans {
		childStats := collectHistoryPlanStats(child)

		stats.examined += childStats.examined

		stats.seqScan = stats.seqScan || childStats.seqScan
		stats.indexes = append(stats.indexes, childStats.indexes...)
	}

	return stats
}

type historySession struct {
	videoID, channelID        string
	status                    domain.LiveStatus
	scheduled, started, ended *time.Time
	lastSeen                  time.Time
}

func seedHistorySessions(t *testing.T, pool *pgxpool.Pool, sessions []historySession) {
	t.Helper()

	for _, session := range sessions {
		_, err := pool.Exec(t.Context(), `
			INSERT INTO youtube_live_sessions(video_id, channel_id, status, title, scheduled_start_time, started_at, ended_at, last_seen_at)
			VALUES ($1, $2, $3, 'history', $4, $5, $6, $7)
		`, session.videoID, session.channelID, string(session.status), session.scheduled, session.started, session.ended, session.lastSeen)
		require.NoError(t, err, "insert %s", session.videoID)
	}
}
