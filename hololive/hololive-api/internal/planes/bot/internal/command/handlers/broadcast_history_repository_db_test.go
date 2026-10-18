package handlers

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
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
// 종료 세션은 보존 삭제가 없어 계속 늘어난다. 운영 풀은 prepared statement를 쓰므로 같은 연결에서 반복되면
// generic plan으로 바뀔 수 있다. 커서·since·채널 조건이 매개변수 값과 무관하게 인덱스 범위가 되어야 다음 페이지가
// 커서보다 새 행이나 다른 채널 행을 건너뛰며 읽지 않는다.
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
		for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				stats := explainHistoryPage(t, pool, mode, tc.channelID, since, tc.cursorAt)

				require.False(t, stats.seqScan, "history page sequentially scanned youtube_live_sessions")
				require.Contains(t, stats.indexes, tc.wantIndex)
				require.LessOrEqual(t, stats.examined, float64(historyPlanPageSize),
					"history page examined sessions outside one page")
			})
		}
	}
}

// explainHistoryPage는 운영 풀처럼 이름 있는 prepared statement로 실행한다. 매개변수를 EXPLAIN에 바로 넘기면
// 값을 아는 상태로 계획해 plan_cache_mode가 generic plan을 강제하지 못한다.
func explainHistoryPage(t *testing.T, pool *pgxpool.Pool, mode, channelID string, since time.Time, cursorAt *time.Time) historyPlanStats {
	t.Helper()

	ctx := t.Context()

	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)

	defer conn.Release()

	cursor := "NULL"

	if cursorAt != nil {
		cursor = "'" + cursorAt.Format(time.RFC3339) + "'"
	}

	for _, statement := range []string{
		"SET plan_cache_mode = " + mode,
		"PREPARE history_page_plan(text, timestamptz, timestamptz, text, text, integer) AS " + broadcastHistoryPageSQL(channelID),
	} {
		_, err = conn.Exec(ctx, statement)
		require.NoError(t, err)
	}

	defer func() {
		_, resetErr := conn.Exec(context.WithoutCancel(ctx), "DEALLOCATE history_page_plan; RESET plan_cache_mode")
		require.NoError(t, resetErr)
	}()

	var raw []byte

	err = conn.QueryRow(ctx, fmt.Sprintf("EXPLAIN (ANALYZE, FORMAT JSON) EXECUTE history_page_plan('%s', '%s', %s, '', '', %d)",
		channelID, since.Format(time.RFC3339), cursor, historyPlanPageSize)).Scan(&raw)
	require.NoError(t, err)

	var plans []struct {
		Plan historyPlanNode `json:"Plan"`
	}

	require.NoError(t, jsonv2.Unmarshal(raw, &plans))
	require.Len(t, plans, 1)

	return collectHistoryPlanStats(plans[0].Plan)
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
	// origin이 비면 열 기본값(legacy_unknown)으로 저장한다.
	origin string
}

func seedHistorySessions(t *testing.T, pool *pgxpool.Pool, sessions []historySession) {
	t.Helper()

	for _, session := range sessions {
		_, err := pool.Exec(t.Context(), `
			INSERT INTO youtube_live_sessions(video_id, channel_id, status, title, scheduled_start_time, started_at, ended_at, last_seen_at)
			VALUES ($1, $2, $3, 'history', $4, $5, $6, $7)
		`, session.videoID, session.channelID, string(session.status), session.scheduled, session.started, session.ended, session.lastSeen)
		require.NoError(t, err, "insert %s", session.videoID)

		if session.origin != "" {
			_, err = pool.Exec(t.Context(), `UPDATE youtube_live_sessions SET lifecycle_origin = $2 WHERE video_id = $1`, session.videoID, session.origin)
			require.NoError(t, err, "set origin %s", session.videoID)
		}
	}
}

// 목록과 썸네일 단건 조회는 같은 scan을 쓰므로 둘 다 lifecycle_origin과 reconciliation head의 마지막 live 확인 시각을
// 읽어야 한다. LIVE를 확인하지 못한 종료는 head가 있어도 observed이고 확인 시각이 nil이며, 옛 기록은 head가 없어도
// legacy_unknown으로 구분되어야 한다.
func TestPgBroadcastHistoryRepositoryReadsTimeEvidence(t *testing.T) {
	pool := dbtest.NewPool(t)
	base := time.Date(2026, time.October, 17, 12, 0, 0, 0, time.UTC)
	lastPositive := base.Add(89 * time.Minute)

	seedHistorySessions(t, pool, []historySession{
		{videoID: "v-with-head", channelID: historyPagingChannel, status: domain.LiveStatusEnded, started: new(base), ended: new(base.Add(90 * time.Minute)), lastSeen: base.Add(90 * time.Minute), origin: observedLifecycleOrigin},
		{videoID: "v-head-no-live", channelID: historyPagingChannel, status: domain.LiveStatusEnded, ended: new(base.Add(60 * time.Minute)), lastSeen: base.Add(60 * time.Minute), origin: observedLifecycleOrigin},
		{videoID: "v-legacy", channelID: historyPagingChannel, status: domain.LiveStatusEnded, started: new(base), ended: new(base.Add(30 * time.Minute)), lastSeen: base.Add(30 * time.Minute)},
	})

	_, err := pool.Exec(t.Context(), `
		INSERT INTO youtube_live_reconciliation_heads(video_id, status, last_live_positive_at, last_live_positive_seen_at)
		VALUES ('v-with-head', 'ENDED', $1, $1), ('v-head-no-live', 'ENDED', NULL, NULL)
	`, lastPositive)
	require.NoError(t, err)

	repo := &pgBroadcastHistoryRepository{pool: pool}
	query := &handlercore.BroadcastHistoryQuery{ChannelID: historyPagingChannel}

	page, err := repo.listEndedBroadcastPage(t.Context(), query, new(base.Add(-time.Hour)), nil, "", 10)
	require.NoError(t, err)

	got := make(map[string]handlercore.BroadcastHistoryEntry, len(page))
	for _, entry := range page {
		got[entry.VideoID] = entry
	}

	require.Len(t, got, 3)
	require.Equal(t, observedLifecycleOrigin, got["v-with-head"].LifecycleOrigin)
	require.NotNil(t, got["v-with-head"].LastLivePositiveAt)
	require.True(t, got["v-with-head"].LastLivePositiveAt.Equal(lastPositive))
	require.Equal(t, observedLifecycleOrigin, got["v-head-no-live"].LifecycleOrigin)
	require.Nil(t, got["v-head-no-live"].LastLivePositiveAt)
	require.Equal(t, legacyLifecycleOrigin, got["v-legacy"].LifecycleOrigin)
	require.Nil(t, got["v-legacy"].LastLivePositiveAt)

	entry, err := repo.GetEndedBroadcast(t.Context(), handlercore.BroadcastThumbnailQuery{VideoID: "v-with-head"})
	require.NoError(t, err)
	require.Equal(t, observedLifecycleOrigin, entry.LifecycleOrigin)
	require.NotNil(t, entry.LastLivePositiveAt)
	require.True(t, entry.LastLivePositiveAt.Equal(lastPositive))

	legacy, err := repo.GetEndedBroadcast(t.Context(), handlercore.BroadcastThumbnailQuery{VideoID: "v-legacy"})
	require.NoError(t, err)
	require.Equal(t, legacyLifecycleOrigin, legacy.LifecycleOrigin)
}
