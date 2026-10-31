package dbtest

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLiveStreamsElapsedMigrationUpgradesStandardBodyAndPreservesCustomBodies(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)

	const (
		file            = "322_live_streams_elapsed.sql"
		custom          = "사용자 지정 라이브 안내 {{.Count}}"
		overrideChannel = "live-elapsed-override"
	)

	body := loadTemplateMigrationBodies(t, file)["CMD_LIVE_STREAMS"]

	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(t.Context(), `UPDATE notification_templates SET body=$1 WHERE template_key='CMD_LIVE_STREAMS' AND channel_id IS NULL`, custom); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(t.Context(), `INSERT INTO notification_templates(template_key,channel_id,body) VALUES('CMD_LIVE_STREAMS',$1,$2)`, overrideChannel, body.oldBody); err != nil {
		t.Fatal(err)
	}

	if err := applyMigrationFile(t.Context(), pool, dir, file); err != nil {
		t.Fatal(err)
	}

	if liveStreamsTemplateBody(t, pool, "") != custom || liveStreamsTemplateBody(t, pool, overrideChannel) != body.oldBody {
		t.Fatal("migration rewrote a custom global body or channel override")
	}

	if _, err := pool.Exec(t.Context(), `UPDATE notification_templates SET body=$1 WHERE template_key='CMD_LIVE_STREAMS' AND channel_id IS NULL`, body.oldBody); err != nil {
		t.Fatal(err)
	}

	if err := applyMigrationFile(t.Context(), pool, dir, file); err != nil {
		t.Fatal(err)
	}

	if liveStreamsTemplateBody(t, pool, "") != body.newBody || liveStreamsTemplateBody(t, pool, overrideChannel) != body.oldBody {
		t.Fatal("standard global body was not upgraded or the channel override changed")
	}

	assertTemplateReplayStable(t, pool, dir, file)
}

func liveStreamsTemplateBody(t *testing.T, pool *pgxpool.Pool, channel string) string {
	t.Helper()

	var body string

	if err := pool.QueryRow(t.Context(), `SELECT body FROM notification_templates WHERE template_key='CMD_LIVE_STREAMS' AND COALESCE(channel_id,'')=$1`, channel).Scan(&body); err != nil {
		t.Fatal(err)
	}

	return body
}
