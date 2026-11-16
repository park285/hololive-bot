package dbtest

import (
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const (
	schedulePosterChangesMigration = "020_schedule_poster_changes.sql"
	schedulePostChannel            = "UCROQtXcp2loQEmvpe5rhJzQ"
)

func newSchedulePosterChangesPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	pool := NewPool(t)
	ctx := t.Context()

	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	migration, err := fs.ReadFile(os.DirFS(dir), schedulePosterChangesMigration)
	require.NoError(t, err)
	require.NoError(t, applyMigrationContent(ctx, pool, schedulePosterChangesMigration, string(migration)), "다시 적용해도 실패하지 않는다")

	_, err = pool.Exec(ctx, `INSERT INTO schedule_poster_posts (post_id, channel_id, posted_at, body) VALUES (1, $1, now(), '告知')`, schedulePostChannel)
	require.NoError(t, err)

	return pool
}

func TestSchedulePosterChangesMigrationRejectsPostsOutsideTheLimits(t *testing.T) {
	t.Parallel()

	pool := newSchedulePosterChangesPool(t)
	ctx := t.Context()

	var status, reason string

	require.NoError(t, pool.QueryRow(ctx, `SELECT status, reason FROM schedule_poster_posts WHERE post_id = 1`).Scan(&status, &reason))
	require.Equal(t, "unjudged", status)
	require.Empty(t, reason)

	for name, insert := range map[string]string{
		"0 이하 글 번호": `INSERT INTO schedule_poster_posts (post_id, channel_id, posted_at, body) VALUES (0, 'c', now(), 'a')`,
		"빈 본문":      `INSERT INTO schedule_poster_posts (post_id, channel_id, posted_at, body) VALUES (2, 'c', now(), '')`,
		"본문 4001자":  `INSERT INTO schedule_poster_posts (post_id, channel_id, posted_at, body) VALUES (2, 'c', now(), repeat('가', 4001))`,
		"같은 글 두 번":  `INSERT INTO schedule_poster_posts (post_id, channel_id, posted_at, body) VALUES (1, '` + schedulePostChannel + `', now(), 'a')`,
		"모르는 상태":    `INSERT INTO schedule_poster_posts (post_id, channel_id, posted_at, body, status) VALUES (2, 'c', now(), 'a', 'done')`,
		"모르는 종류":    `INSERT INTO schedule_poster_posts (post_id, channel_id, posted_at, body, status, schedule_info, kind) VALUES (2, 'c', now(), 'a', 'relevant', 0.9, 'news')`,
		"점수만 있음":    `INSERT INTO schedule_poster_posts (post_id, channel_id, posted_at, body, status, schedule_info) VALUES (2, 'c', now(), 'a', 'judge_failed', 0.9)`,
		"점수 1 초과":   `INSERT INTO schedule_poster_posts (post_id, channel_id, posted_at, body, status, schedule_info, kind) VALUES (2, 'c', now(), 'a', 'relevant', 1.5, 'change')`,
		"답 없는 관련":   `INSERT INTO schedule_poster_posts (post_id, channel_id, posted_at, body, status) VALUES (2, 'c', now(), 'a', 'relevant')`,
		"답 있는 미판별":  `INSERT INTO schedule_poster_posts (post_id, channel_id, posted_at, body, schedule_info, kind) VALUES (2, 'c', now(), 'a', 0.9, 'change')`,
		"사유 없는 실패":  `INSERT INTO schedule_poster_posts (post_id, channel_id, posted_at, body, status) VALUES (2, 'c', now(), 'a', 'judge_failed')`,
		"사유 있는 관련":  `INSERT INTO schedule_poster_posts (post_id, channel_id, posted_at, body, status, reason, schedule_info, kind) VALUES (2, 'c', now(), 'a', 'relevant', 'x', 0.9, 'change')`,
		"사유 41자":    `INSERT INTO schedule_poster_posts (post_id, channel_id, posted_at, body, status, reason) VALUES (2, 'c', now(), 'a', 'judge_failed', repeat('a', 41))`,
		"도장 없는 대응":  `INSERT INTO schedule_poster_posts (post_id, channel_id, posted_at, body, status, schedule_info, kind) VALUES (2, 'c', now(), 'a', 'matched', 0.9, 'change')`,
		"도장 있는 관련":  `INSERT INTO schedule_poster_posts (post_id, channel_id, posted_at, body, status, schedule_info, kind, matched_poster_id, matched_rows_at) VALUES (2, 'c', now(), 'a', 'relevant', 0.9, 'change', 1, now())`,
		"도장 한쪽만":    `INSERT INTO schedule_poster_posts (post_id, channel_id, posted_at, body, status, schedule_info, kind, matched_poster_id) VALUES (2, 'c', now(), 'a', 'matched', 0.9, 'change', 1)`,
		"채널 65자":    `INSERT INTO schedule_poster_posts (post_id, channel_id, posted_at, body) VALUES (2, repeat('c', 65), now(), 'a')`,
		"답 없는 철회":   `INSERT INTO schedule_poster_posts (post_id, channel_id, posted_at, body, status) VALUES (2, 'c', now(), 'a', 'revoked')`,
		"도장 있는 철회":  `INSERT INTO schedule_poster_posts (post_id, channel_id, posted_at, body, status, schedule_info, kind, matched_poster_id, matched_rows_at) VALUES (2, 'c', now(), 'a', 'revoked', 0.9, 'change', 1, now())`,
		"사유 있는 철회":  `INSERT INTO schedule_poster_posts (post_id, channel_id, posted_at, body, status, reason, schedule_info, kind) VALUES (2, 'c', now(), 'a', 'revoked', 'x', 0.9, 'change')`,
	} {
		_, err := pool.Exec(ctx, insert)
		require.Error(t, err, name)
	}

	_, err := pool.Exec(ctx, `INSERT INTO schedule_poster_posts (post_id, channel_id, posted_at, body, status, reason, schedule_info, kind, matched_poster_id, matched_rows_at)
		VALUES (2, 'c', now(), repeat('가', 4000), 'match_failed', 'unavailable', 1, 'cancel', 9, now()),
		       (3, 'c', now(), 'a', 'judge_failed', 'blocked', NULL, NULL, NULL, NULL),
		       (4, 'c', now(), 'a', 'revoked', '', 0.9, 'change', NULL, NULL),
		       (1, 'c', now(), 'a', 'unjudged', '', NULL, NULL, NULL, NULL)`)
	require.NoError(t, err, "본문 상한은 문자 수이고, 판별 답 없는 실패와 포스터 FK 없는 도장, 판별 답을 지닌 철회, 다른 멤버 앞으로 온 같은 글을 받는다")
}

const (
	scheduleChangeInsert = `INSERT INTO schedule_poster_changes (post_id, channel_id, item, kind, reason, slot, target_post_id, target_item, day,
		start_time, status, source_text, title_ko, with_ko, terms)
		VALUES ($1, $15, $2, $3, $4, $5, $6, $7, $8::date, $9, $10, $11, $12, $13, $14::jsonb)`
	scheduleChangeRejected = "rejected"
	scheduleChangeSame     = "same"
)

func validScheduleChange() []any {
	return []any{1, 1, "time_changed", "", 3, nil, nil, nil, "25:30", nil, "25時半から", "노래", []string{}, `[]`, schedulePostChannel}
}

func TestSchedulePosterChangesMigrationRejectsChangesOutsideTheLimits(t *testing.T) {
	t.Parallel()

	pool := newSchedulePosterChangesPool(t)
	ctx := t.Context()

	for name, edit := range map[string]func(args []any){
		"없는 글":        func(args []any) { args[0] = 99 },
		"그 멤버에게 없는 글": func(args []any) { args[14] = "UCy9mgxB8pn2C4aNK_MPthDQ" },
		"항목 0":        func(args []any) { args[1] = 0 },
		"항목 21":       func(args []any) { args[1] = 21 },
		"모르는 판정":      func(args []any) { args[2] = "renamed" },
		"사유 있는 반영":    func(args []any) { args[3] = "format" },
		"사유 없는 거절":    func(args []any) { args[2], args[8], args[10], args[11] = scheduleChangeRejected, nil, "", nil },
		"칸 101":       func(args []any) { args[4] = 101 },
		"칸과 더한 방송":    func(args []any) { args[5], args[6] = 1, 1 },
		"더한 방송 글만":    func(args []any) { args[4], args[5] = nil, 1 },
		"더한 방송 항목 21": func(args []any) { args[4], args[5], args[6] = nil, 1, 21 },
		"대상 없는 시각 변경": func(args []any) { args[4] = nil },
		"제목 있는 같음":    func(args []any) { args[2] = scheduleChangeSame },
		"참가자 있는 같음":   func(args []any) { args[2], args[11], args[12] = scheduleChangeSame, nil, []string{"쿄코"} },
		"값 있는 거절": func(args []any) {
			args[2], args[3], args[10], args[11] = scheduleChangeRejected, "time_collision", "", nil
		},
		"날짜 없는 옮김": func(args []any) { args[2] = "moved" },
		"시각 없는 새 방송": func(args []any) {
			args[2], args[4], args[7], args[8], args[9] = "added", nil, "2026-11-12", nil, "planned"
		},
		"시 30":         func(args []any) { args[8] = "30:00" },
		"시각 형식":        func(args []any) { args[8] = "7:5" },
		"모르는 상태":       func(args []any) { args[9] = "done" },
		"원문 501자":      func(args []any) { args[10] = strings.Repeat("가", 501) },
		"빈 제목":         func(args []any) { args[11] = "" },
		"함께 41명":       func(args []any) { args[12] = make([]string, 41) },
		"배열이 아닌 terms": func(args []any) { args[13] = `{}` },
		"원문 있는 거절": func(args []any) {
			args[2], args[3], args[8], args[11] = scheduleChangeRejected, "no_such_row", nil, nil
		},
		"번역 칸 있는 거절": func(args []any) {
			args[2], args[3], args[8], args[10], args[11], args[12] = scheduleChangeRejected, "evidence", nil, "", nil, []string{"쿄코"}
		},
		"더한 방송을 가리키는 거절": func(args []any) {
			args[2], args[3], args[4], args[5], args[6], args[8], args[10], args[11] = scheduleChangeRejected, "evidence", nil, 1, 1, nil, "", nil
		},
	} {
		args := validScheduleChange()
		edit(args)

		_, err := pool.Exec(ctx, scheduleChangeInsert, args...)
		require.Error(t, err, name)
	}
}

func TestSchedulePosterChangesMigrationKeepsChangesOnlyWithTheirPost(t *testing.T) {
	t.Parallel()

	pool := newSchedulePosterChangesPool(t)
	ctx := t.Context()

	for item, args := range [][]any{
		validScheduleChange(),
		{1, 2, "time_changed", "", 1, nil, nil, nil, "", nil, "", nil, []string{}, `[]`, schedulePostChannel},
		{1, 3, "added", "", nil, nil, nil, "2026-11-12", "9:05", "planned", "", nil, []string{}, `[]`, schedulePostChannel},
		{1, 4, "moved_out", "", nil, 1, 3, "2026-11-20", nil, nil, "", nil, []string{}, `[]`, schedulePostChannel},
		{1, 5, scheduleChangeRejected, "no_content", 2, nil, nil, nil, nil, nil, "", nil, []string{}, `[]`, schedulePostChannel},
		{1, 6, scheduleChangeSame, "", 4, nil, nil, nil, nil, nil, "", nil, []string{}, `[]`, schedulePostChannel},
		{1, 8, scheduleChangeSame, "", 4, nil, nil, "2026-11-12", "21:00", "planned", "21時から", nil, []string{}, `[]`, schedulePostChannel},
		{1, 7, "content_changed", "", 5, nil, nil, nil, nil, nil, strings.Repeat("가", 500), strings.Repeat("가", 1000), []string{"쿄코"}, `[]`, schedulePostChannel},
	} {
		_, err := pool.Exec(ctx, scheduleChangeInsert, args...)
		require.NoError(t, err, "항목 %d", item+1)
	}

	const otherChannel = "UCy9mgxB8pn2C4aNK_MPthDQ"

	_, err := pool.Exec(ctx, `INSERT INTO schedule_poster_posts (post_id, channel_id, posted_at, body) VALUES (1, $1, now(), '告知')`, otherChannel)
	require.NoError(t, err)

	other := validScheduleChange()

	other[14] = otherChannel

	_, err = pool.Exec(ctx, scheduleChangeInsert, other...)
	require.NoError(t, err, "같은 글의 같은 항목 번호를 멤버마다 따로 가진다")

	_, err = pool.Exec(ctx, `DELETE FROM schedule_poster_posts WHERE post_id = 1 AND channel_id = $1`, schedulePostChannel)
	require.NoError(t, err)

	var remaining []string

	rows, err := pool.Query(ctx, `SELECT channel_id FROM schedule_poster_changes`)
	require.NoError(t, err)

	remaining, err = pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(t, err)
	require.Equal(t, []string{otherChannel}, remaining, "글 행을 지우면 그 멤버의 변경만 함께 지운다")
}
