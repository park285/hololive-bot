package dbtest

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// reloptions는 schema_snapshot.golden.sql 직렬화(컬럼·제약·인덱스)에 포함되지 않아
// 111의 autovacuum 튜닝은 골든 대신 이 구조 테스트로 고정한다.
func TestTelemetryHotTableAutovacuumTuned(t *testing.T) {
	pool := NewPool(t)
	ctx := context.Background()

	var reloptions []string
	if err := pool.QueryRow(ctx,
		"SELECT COALESCE(reloptions, '{}') FROM pg_class WHERE relname = 'youtube_notification_delivery_telemetry' AND relnamespace = current_schema()::regnamespace",
	).Scan(&reloptions); err != nil {
		t.Fatalf("query reloptions: %v", err)
	}

	want := map[string]bool{
		"autovacuum_vacuum_scale_factor=0.02":  false,
		"autovacuum_vacuum_threshold=50":       false,
		"autovacuum_analyze_scale_factor=0.02": false,
		"autovacuum_analyze_threshold=50":      false,
	}
	for _, opt := range reloptions {
		if _, ok := want[opt]; ok {
			want[opt] = true
		}
	}
	for opt, found := range want {
		if !found {
			t.Errorf("youtube_notification_delivery_telemetry missing storage parameter %q (got %v)", opt, reloptions)
		}
	}
}

func TestMigration014OutboxGroupTemplatesSeedIsReapplySafe(t *testing.T) {
	pool := NewPool(t)
	ctx := context.Background()

	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatalf("resolve migrations dir: %v", err)
	}
	// #nosec G304 -- 리포 내 마이그레이션 SSOT 디렉터리의 고정 파일명만 읽는다(사용자 입력 아님).
	raw, err := os.ReadFile(filepath.Join(dir, "014-add-outbox-group-templates.sql"))
	if err != nil {
		t.Fatalf("read 014 seed: %v", err)
	}

	if _, err := pool.Exec(ctx, string(raw)); err != nil {
		t.Fatalf("파일 중간 crash 후 재실행 시 014 전체가 재적용되므로 seed는 재실행-안전해야 한다: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM notification_templates WHERE template_key IN ('OUTBOX_VIDEO_GROUP','OUTBOX_SHORTS_GROUP','OUTBOX_COMMUNITY_GROUP') AND channel_id IS NULL",
	).Scan(&count); err != nil {
		t.Fatalf("count seeded templates: %v", err)
	}
	if count != 3 {
		t.Fatalf("seeded default templates = %d, want 3 (no duplicates after re-apply)", count)
	}
}
