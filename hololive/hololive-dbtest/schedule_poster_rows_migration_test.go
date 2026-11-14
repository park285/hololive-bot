package dbtest

import (
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const schedulePosterRowsMigration = "019_schedule_poster_rows.sql"

func newSchedulePosterRowsPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	pool := NewPool(t)
	ctx := t.Context()

	_, err := pool.Exec(ctx, `INSERT INTO schedule_posters (id, channel_id, x_user_id, post_url, image_url, posted_at, text_excerpt,
		review_status, week_start, week_end)
		VALUES (1, 'UCROQtXcp2loQEmvpe5rhJzQ', '1', 'https://x.com/i/status/1', 'https://pbs.twimg.com/media/a.jpg', now(), '', 'confirmed', '2026-11-09', '2026-11-15'),
		       (2, 'UCROQtXcp2loQEmvpe5rhJzQ', '1', 'https://x.com/i/status/2', 'https://pbs.twimg.com/media/b.jpg', now(), '', 'pending_review', NULL, NULL)`)
	require.NoError(t, err)

	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	migration, err := fs.ReadFile(os.DirFS(dir), schedulePosterRowsMigration)
	require.NoError(t, err)
	require.NoError(t, applyMigrationContent(ctx, pool, schedulePosterRowsMigration, string(migration)), "다시 적용해도 실패하지 않는다")

	return pool
}

func TestSchedulePosterRowsMigrationAddsTheAliasOnceAndPendsConfirmedPosters(t *testing.T) {
	t.Parallel()

	pool := newSchedulePosterRowsPool(t)
	ctx := t.Context()

	var (
		aliases     int
		status      string
		rowRevision int64
	)

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM members, jsonb_array_elements_text(aliases -> 'ja') AS alias
		WHERE slug = 'asobi-mawaritai' AND alias = 'アソビ隊'`).Scan(&aliases))
	require.Equal(t, 1, aliases, "다시 적용해도 별칭을 두 번 넣지 않는다")

	require.NoError(t, pool.QueryRow(ctx, `SELECT rows_status, rows_revision FROM schedule_posters WHERE id = 1`).Scan(&status, &rowRevision))
	require.Equal(t, "pending", status, "이미 확정한 포스터도 추출 대상이 된다")
	require.EqualValues(t, 1, rowRevision)

	for name, update := range map[string]string{
		"모르는 추출 상태":   `UPDATE schedule_posters SET rows_status = 'done' WHERE id = 1`,
		"저장 시각 없는 성공": `UPDATE schedule_posters SET rows_status = 'succeeded' WHERE id = 1`,
	} {
		_, err := pool.Exec(ctx, update)
		require.Error(t, err, name)
	}

	_, err := pool.Exec(ctx, `UPDATE schedule_posters SET rows_status = 'succeeded', rows_extracted_at = now() WHERE id = 1`)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE schedule_posters SET review_status = 'rejected', revision = revision + 1 WHERE id = 1`)
	require.NoError(t, err, "행 열을 모르는 이전 image도 행을 저장한 확정본을 거절할 수 있다")
}

func TestSchedulePosterRowsMigrationFillsOnlyAnEmptyKoreanNameOfIRyS(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()

	_, err := pool.Exec(ctx, `INSERT INTO members (id, slug, channel_id, english_name, japanese_name, korean_name, short_korean_name,
		aliases, org, sync_source)
		VALUES (9001, 'irys', 'UC8rcEBzJSleTkf_-agPM20g', 'IRyS', 'IRyS', NULL, '아이리스', '{"ja": [], "ko": ["아이리스"]}', 'Hololive', 'holodex'),
		       (9002, 'gigi-murin', 'UCDHABijvPBnJm7F-KlNME3w', 'Gigi Murin', 'ジジ・ムリン', '', '지지', '{"ja": [], "ko": []}', 'Hololive', 'holodex')`)
	require.NoError(t, err)

	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	migration, err := fs.ReadFile(os.DirFS(dir), schedulePosterRowsMigration)
	require.NoError(t, err)

	koreanNames := func() map[string]*string {
		rows, queryErr := pool.Query(ctx, `SELECT slug, korean_name FROM members WHERE id IN (9001, 9002)`)
		require.NoError(t, queryErr)

		names := map[string]*string{}

		for rows.Next() {
			var (
				slug string
				name *string
			)

			require.NoError(t, rows.Scan(&slug, &name))

			names[slug] = name
		}

		require.NoError(t, rows.Err())

		return names
	}

	require.NoError(t, applyMigrationContent(ctx, pool, schedulePosterRowsMigration, string(migration)))
	require.Equal(t, map[string]*string{"irys": new("아이리스"), "gigi-murin": new("")}, koreanNames(),
		"IRyS만 채우고 다른 멤버의 빈 값은 건드리지 않는다")

	_, err = pool.Exec(ctx, `UPDATE members SET korean_name = '아이리스 운영값' WHERE id = 9001`)
	require.NoError(t, err)
	require.NoError(t, applyMigrationContent(ctx, pool, schedulePosterRowsMigration, string(migration)))
	require.Equal(t, "아이리스 운영값", *koreanNames()["irys"], "이미 채운 값은 덮지 않는다")
}

func TestSchedulePosterRowsMigrationCorrectsOnlyTheOldShortKoreanNames(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()

	corrections := [][3]string{
		{"aki-rosenthal", "로젠탈", "아키"},
		{"la-darknesss", "다크니스", "라플라스"},
		{"moona-hoshinova", "호시노바", "무나"},
		{"airani-iofifteen", "이오피프틴", "이오피"},
		{"anya-melfissa", "멜피사", "아냐"},
		{"kaela-kovalskia", "코발스키아", "카엘라"},
		{"kobo-kanaeru", "카나에루", "코보"},
		{"shiori-novella", "노벨라", "시오리"},
		{"nerissa-ravencroft", "레이븐크로프트", "네리사"},
		{"elizabeth-rose-bloodflame", "블러드플레임", "엘리자베스"},
		{"cecilia-immergreen", "이머그린", "세실리아"},
		{"raora-panthera", "판테라", "라오라"},
	}

	want := map[string]string{"not-listed": "카나에루"}

	_, err := pool.Exec(ctx, `INSERT INTO members (id, slug, english_name, short_korean_name, aliases, org, sync_source)
		VALUES (9100, 'not-listed', 'Not Listed', '카나에루', '{"ja": [], "ko": []}', 'Hololive', 'holodex')`)
	require.NoError(t, err)

	for i, fix := range corrections {
		_, err = pool.Exec(ctx, `INSERT INTO members (id, slug, english_name, short_korean_name, aliases, org, sync_source)
			VALUES ($1, $2, $2, $3, '{"ja": [], "ko": []}', 'Hololive', 'holodex')`, 9110+i, fix[0], fix[1])
		require.NoError(t, err)

		want[fix[0]] = fix[2]
	}

	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	migration, err := fs.ReadFile(os.DirFS(dir), schedulePosterRowsMigration)
	require.NoError(t, err)

	shortNames := func() map[string]string {
		rows, queryErr := pool.Query(ctx, `SELECT slug, short_korean_name FROM members WHERE id >= 9100`)
		require.NoError(t, queryErr)

		names := map[string]string{}

		for rows.Next() {
			var slug, name string

			require.NoError(t, rows.Scan(&slug, &name))

			names[slug] = name
		}

		require.NoError(t, rows.Err())

		return names
	}

	require.NoError(t, applyMigrationContent(ctx, pool, schedulePosterRowsMigration, string(migration)))
	require.Equal(t, want, shortNames(), "옛 값인 12명만 고치고 목록에 없는 멤버는 같은 값이어도 그대로 둔다")

	_, err = pool.Exec(ctx, `UPDATE members SET short_korean_name = '코보쨩' WHERE slug = 'kobo-kanaeru'`)
	require.NoError(t, err)

	want["kobo-kanaeru"] = "코보쨩"

	require.NoError(t, applyMigrationContent(ctx, pool, schedulePosterRowsMigration, string(migration)))
	require.Equal(t, want, shortNames(), "다시 적용해도 운영자가 바꾼 값을 덮지 않는다")

	var aliases int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM members, jsonb_array_elements(aliases -> 'ko') WHERE id >= 9100`).Scan(&aliases))
	require.Zero(t, aliases, "옛 값을 별칭으로 남기지 않는다")
}

func TestSchedulePosterRowsMigrationRejectsRowsOutsideTheLimits(t *testing.T) {
	t.Parallel()

	pool := newSchedulePosterRowsPool(t)
	ctx := t.Context()

	const row = `INSERT INTO schedule_poster_rows (poster_id, ordinal, day, start_time, end_time, tz, status, source_text, title_ko, badges, terms)
		VALUES ($1, $2, '2026-11-10', $3, $4, $5, $6, $7, $8, $9, $10::jsonb)`

	valid := []any{1, 1, "25:30", nil, "JST", "planned", "歌枠", "노래", []string{"コラボ"}, `[]`}

	for name, change := range map[string]func(args []any){
		"없는 포스터":       func(args []any) { args[0] = 99 },
		"ordinal 0":    func(args []any) { args[1] = 0 },
		"ordinal 101":  func(args []any) { args[1] = 101 },
		"시각 형식":        func(args []any) { args[2] = "7:5" },
		"분 60":         func(args []any) { args[3] = "21:60" },
		"시 30":         func(args []any) { args[2] = "30:00" },
		"시간대 41자":      func(args []any) { args[4] = strings.Repeat("a", 41) },
		"모르는 상태":       func(args []any) { args[5] = "done" },
		"빈 원문":         func(args []any) { args[6] = "" },
		"빈 제목":         func(args []any) { args[7] = "" },
		"제목 1001자":     func(args []any) { args[7] = strings.Repeat("가", 1001) },
		"배지 41개":       func(args []any) { args[8] = make([]string, 41) },
		"배열이 아닌 terms": func(args []any) { args[9] = `{}` },
	} {
		args := append([]any(nil), valid...)
		change(args)

		_, err := pool.Exec(ctx, row, args...)
		require.Error(t, err, name)
	}

	valid[7] = strings.Repeat("가", 1000)

	_, err := pool.Exec(ctx, row, valid...)
	require.NoError(t, err, "글 상한은 바이트가 아니라 문자 수다")

	_, err = pool.Exec(ctx, `DELETE FROM schedule_posters WHERE id = 1`)
	require.NoError(t, err)

	var remaining int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM schedule_poster_rows`).Scan(&remaining))
	require.Zero(t, remaining, "포스터를 지우면 행도 함께 지운다")
}
