package dbtest

import (
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	helpBroadcastTimeMigration    = "371_help_broadcast_time.sql"
	helpBroadcastTimeSumMigration = "372_help_broadcast_time_sum.sql"
	helpMemberScheduleLine        = "{{mdescape .Prefix}}멤버 [이름] - 일주일 이내 방송 일정\n"
	helpBroadcastTimeLine         = "{{mdescape .Prefix}}방송시간 [멤버명] - 최근 방송 시각\n"
)

// 371은 370 표준 도움말 본문에만 !방송시간 줄을 넣는다.
func TestHelpBroadcastTimeMigrationUpdatesStandardDefaultsOnly(t *testing.T) {
	t.Parallel()

	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	var previous string

	for _, seed := range loadTemplateStyleUnifySeeds(t, dir) {
		if seed.key == "CMD_HELP" && seed.variant == "richtext" {
			previous = seed.body
		}
	}

	require.Equal(t, 1, strings.Count(previous, helpMemberScheduleLine))
	assertHelpRichtextMigration(t, dir, helpBroadcastTimeMigration, previous,
		strings.Replace(previous, helpMemberScheduleLine, helpMemberScheduleLine+helpBroadcastTimeLine, 1))
}

// 372는 371 표준 도움말 본문의 !방송시간 줄만 기간 인자·합계 설명으로 바꾼다.
func TestHelpBroadcastTimeSumMigrationUpdatesStandardDefaultsOnly(t *testing.T) {
	t.Parallel()

	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	raw, err := fs.ReadFile(os.DirFS(dir), helpBroadcastTimeMigration)
	require.NoError(t, err)

	var previous string

	for _, row := range templateStyleUnifySeedPattern.FindAllStringSubmatch(string(raw), -1) {
		if row[1] == "CMD_HELP" && row[2] == "richtext" {
			previous = row[4]
		}
	}

	require.Equal(t, 1, strings.Count(previous, helpBroadcastTimeLine))
	assertHelpRichtextMigration(t, dir, helpBroadcastTimeSumMigration, previous, strings.Replace(previous, helpBroadcastTimeLine,
		"{{mdescape .Prefix}}방송시간 [멤버명] [N일] - 방송 시간 합계(기본 7일)\n", 1))
}

// assertHelpRichtextMigration은 CMD_HELP richtext 기본값을 previous로, plain 기본값을 사용자 지정 본문으로 되돌리고
// previous를 가진 채널 override를 둔 뒤, migration이 richtext 기본값만 want로 바꾸는지 확인한다.
func assertHelpRichtextMigration(t *testing.T, dir, migration, previous, want string) {
	t.Helper()

	pool := NewPool(t)
	ctx := t.Context()

	const restore = `UPDATE notification_templates SET body=$2 WHERE template_key='CMD_HELP' AND format_variant=$1 AND channel_id IS NULL`

	_, err := pool.Exec(ctx, restore, "richtext", previous)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, restore, "plain", "사용자 지정 도움말")
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO notification_templates(template_key,channel_id,body,format_variant)
VALUES ('CMD_HELP','help-broadcast-time-override',$1,'richtext')`, previous)
	require.NoError(t, err)

	assertTemplateMigrationUpdatesStandardDefaults(t, pool, dir, migration, map[string]string{"CMD_HELP/richtext": want}, "")
}
