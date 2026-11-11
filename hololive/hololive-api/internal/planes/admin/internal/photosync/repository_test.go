package photosync

import (
	"slices"
	"testing"
	"time"

	dbtest "github.com/kapu/hololive-dbtest"
)

func TestRepositoryPhotoOperationsPreserveSemantics(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := repository{pool: pool}

	stale := time.Now().Add(-2 * time.Hour)
	fresh := time.Now()

	if _, err := pool.Exec(ctx, `
		INSERT INTO members (slug, channel_id, english_name, japanese_name, korean_name, status, is_graduated, aliases, org, sync_source, photo, photo_updated_at)
		VALUES
			('pgx-photo-null', 'UC_PGX_NULL', 'PGX Photo Null', NULL, NULL, 'active', false, '{"ko":[],"ja":[]}', 'Hololive', 'manual', NULL, NULL),
			('pgx-photo-stale', 'UC_PGX_STALE', 'PGX Photo Stale', NULL, NULL, 'active', false, '{"ko":[],"ja":[]}', 'Hololive', 'manual', 'old', $1),
			('pgx-photo-fresh', 'UC_PGX_FRESH', 'PGX Photo Fresh', NULL, NULL, 'active', false, '{"ko":[],"ja":[]}', 'Hololive', 'manual', 'fresh', $2),
			('pgx-photo-no-channel', NULL, 'PGX Photo No Channel', NULL, NULL, 'active', false, '{"ko":[],"ja":[]}', 'Hololive', 'manual', NULL, NULL)
	`, stale, fresh); err != nil {
		t.Fatalf("insert photo fixtures: %v", err)
	}

	if err := repo.updatePhoto(ctx, "UC_PGX_NULL", "https://example.com/photo=s1024"); err != nil {
		t.Fatalf("updatePhoto() error = %v, want nil", err)
	}

	var got string

	if err := pool.QueryRow(ctx, `SELECT photo FROM members WHERE channel_id = 'UC_PGX_NULL'`).Scan(&got); err != nil || got != "https://example.com/photo=s1024" {
		t.Fatalf("photo after updatePhoto = %q, %v; want updated photo nil", got, err)
	}

	needingSync, err := repo.membersNeedingSync(ctx, time.Hour)
	if err != nil {
		t.Fatalf("membersNeedingSync() error = %v, want nil", err)
	}

	if !slices.Contains(needingSync, "UC_PGX_STALE") || slices.Contains(needingSync, "UC_PGX_FRESH") {
		t.Fatalf("needing sync = %#v, want stale and not fresh", needingSync)
	}
}
