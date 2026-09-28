package dbtest

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	nextStreamRemovalFile     = "233_alarm_next_stream_template_removal.sql"
	nextStreamRemovalOverride = "next-stream-override"
)

// 운영 DB처럼 217 이후 표준 본문과 그 본문을 복사한 채널 override가 섞인 상태에서, 233은 두 행을 모두
// NextStream 없는 본문으로 바꾸고 재적용 시 아무것도 다시 쓰지 않는다.
func TestNextStreamRemovalMigrationRewritesStandardBodiesAndReplays(t *testing.T) {
	pool := NewPool(t)
	bodies := loadNextStreamRemovalBodies(t)
	seedNextStreamRemovalUpgrade(t, pool, bodies)

	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatal(err)
	}

	if err := applyMigrationFile(t.Context(), pool, dir, nextStreamRemovalFile); err != nil {
		t.Fatal(err)
	}

	for key, body := range bodies {
		if got := templateBody(t, pool, key, ""); got != body.newBody {
			t.Errorf("global %s not rewritten", key)
		}
	}

	if got := templateBody(t, pool, "CMD_ALARM_LIST", nextStreamRemovalOverride); got != bodies["CMD_ALARM_LIST"].newBody {
		t.Error("override with the standard body not rewritten")
	}

	assertTemplateReplayStable(t, pool, dir, nextStreamRemovalFile)
}

// 표준과 다른 본문이 NextStream을 참조하면 새 코드에서 렌더가 실패하므로, 233은 표준 행까지 포함해
// 아무것도 바꾸지 않고 거절한다.
func TestNextStreamRemovalMigrationRejectsCustomNextStreamBody(t *testing.T) {
	pool := NewPool(t)
	bodies := loadNextStreamRemovalBodies(t)
	seedNextStreamRemovalUpgrade(t, pool, bodies)

	custom := "{{range .Alarms}}{{.MemberName}}{{if .NextStream}} 방송{{end}}\n{{end}}"
	if _, err := pool.Exec(t.Context(), `UPDATE notification_templates SET body=$1 WHERE template_key='CMD_ALARM_LIST' AND channel_id=$2`,
		custom, nextStreamRemovalOverride); err != nil {
		t.Fatal(err)
	}

	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatal(err)
	}

	err = applyMigrationFile(t.Context(), pool, dir, nextStreamRemovalFile)
	if err == nil || !strings.Contains(err.Error(), "still reference NextStream") {
		t.Fatalf("expected NextStream guard rejection, got %v", err)
	}

	for key, body := range bodies {
		if got := templateBody(t, pool, key, ""); got != body.oldBody {
			t.Errorf("rejected migration changed global %s", key)
		}
	}
}

func loadNextStreamRemovalBodies(t *testing.T) map[string]templateMigrationBody {
	t.Helper()

	bodies := loadTemplateMigrationBodies(t, nextStreamRemovalFile)
	if len(bodies) != 2 {
		t.Fatalf("expected 2 template updates, got %d", len(bodies))
	}

	return bodies
}

func seedNextStreamRemovalUpgrade(t *testing.T, pool *pgxpool.Pool, bodies map[string]templateMigrationBody) {
	t.Helper()

	for key, body := range bodies {
		if _, err := pool.Exec(t.Context(), `UPDATE notification_templates SET body=$1 WHERE template_key=$2 AND channel_id IS NULL`, body.oldBody, key); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := pool.Exec(t.Context(), `INSERT INTO notification_templates(template_key,channel_id,body) VALUES ('CMD_ALARM_LIST',$1,$2)`,
		nextStreamRemovalOverride, bodies["CMD_ALARM_LIST"].oldBody); err != nil {
		t.Fatal(err)
	}
}

func templateBody(t *testing.T, pool *pgxpool.Pool, key, channelID string) string {
	t.Helper()

	var body string

	if err := pool.QueryRow(t.Context(), `SELECT body FROM notification_templates WHERE template_key=$1 AND channel_id IS NOT DISTINCT FROM NULLIF($2,'')`,
		key, channelID).Scan(&body); err != nil {
		t.Fatal(err)
	}

	return body
}
