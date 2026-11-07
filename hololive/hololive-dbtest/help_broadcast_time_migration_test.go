package dbtest

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const helpBroadcastTimeMigration = "371_help_broadcast_time.sql"

// 371은 370 표준 도움말 본문에만 !방송시간 줄을 넣는다. 사용자 지정 plain 기본값과 표준 본문을 가진 override는 그대로 둔다.
func TestHelpBroadcastTimeMigrationUpdatesStandardDefaultsOnly(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()

	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	previous := make(map[string]string, 2)

	for _, seed := range loadTemplateStyleUnifySeeds(t, dir) {
		if seed.key == "CMD_HELP" {
			previous[seed.variant] = seed.body
		}
	}

	require.Len(t, previous, 2)

	const restore = `UPDATE notification_templates SET body=$2 WHERE template_key='CMD_HELP' AND format_variant=$1 AND channel_id IS NULL`

	_, err = pool.Exec(ctx, restore, "richtext", previous["richtext"])
	require.NoError(t, err)

	_, err = pool.Exec(ctx, restore, "plain", "사용자 지정 도움말")
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO notification_templates(template_key,channel_id,body,format_variant)
VALUES ('CMD_HELP','help-broadcast-time-override',$1,'richtext')`, previous["richtext"])
	require.NoError(t, err)

	anchor := "{{mdescape .Prefix}}멤버 [이름] - 일주일 이내 방송 일정\n"
	require.Equal(t, 1, strings.Count(previous["richtext"], anchor))

	changed := map[string]string{
		"CMD_HELP/richtext": strings.Replace(previous["richtext"], anchor, anchor+"{{mdescape .Prefix}}방송시간 [멤버명] - 최근 방송 시각\n", 1),
	}

	assertTemplateMigrationUpdatesStandardDefaults(t, pool, dir, helpBroadcastTimeMigration, changed, "")
}
