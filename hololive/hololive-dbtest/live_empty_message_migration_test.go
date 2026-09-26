package dbtest

import "testing"

func TestLiveEmptyMessageMigrationPreservesCustomBodiesAndReplay(t *testing.T) {
	pool := NewPool(t)
	ctx := t.Context()

	const (
		file            = "220_live_query_confirmed_empty_message.sql"
		custom          = "사용자 지정 라이브 안내 {{.Count}}"
		overrideChannel = "live-empty-override"
	)

	previous := loadTemplateMigrationBodies(t, file)["CMD_LIVE_STREAMS"].oldBody

	var memberBody string

	if err := pool.QueryRow(ctx, `SELECT body FROM notification_templates WHERE template_key='CMD_MEMBER_NOT_LIVE' AND channel_id IS NULL`).Scan(&memberBody); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `UPDATE notification_templates SET body=$1 WHERE template_key='CMD_LIVE_STREAMS' AND channel_id IS NULL`, custom); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `INSERT INTO notification_templates(template_key,channel_id,body) VALUES('CMD_LIVE_STREAMS',$1,$2)`, overrideChannel, previous); err != nil {
		t.Fatal(err)
	}

	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatal(err)
	}

	if err := applyMigrationFile(ctx, pool, dir, file); err != nil {
		t.Fatal(err)
	}

	for _, row := range []struct{ key, channel, want string }{
		{"CMD_LIVE_STREAMS", "", custom},
		{"CMD_LIVE_STREAMS", overrideChannel, previous},
		{"CMD_MEMBER_NOT_LIVE", "", memberBody},
	} {
		var got string

		if err := pool.QueryRow(ctx, `SELECT body FROM notification_templates WHERE template_key=$1 AND COALESCE(channel_id,'')=$2`, row.key, row.channel).Scan(&got); err != nil {
			t.Fatal(err)
		}

		if got != row.want {
			t.Fatalf("migration rewrote protected template %s/%s", row.key, row.channel)
		}
	}

	assertTemplateReplayStable(t, pool, dir, file)
}
