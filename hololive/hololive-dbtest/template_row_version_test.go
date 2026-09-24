package dbtest

import "testing"

func TestTemplateRowVersionMigration(t *testing.T) {
	pool := NewPool(t)
	ctx := t.Context()

	var id, version int64

	if err := pool.QueryRow(ctx, `INSERT INTO notification_templates(template_key,body) VALUES ('VERSION_TEST','original') RETURNING id,row_version`).Scan(&id, &version); err != nil {
		t.Fatal(err)
	}

	if version != 1 {
		t.Fatalf("initial version=%d", version)
	}

	for _, tc := range []struct {
		body string
		want int64
	}{{"original", 1}, {"changed", 2}, {"original", 3}} {
		if err := pool.QueryRow(ctx, `UPDATE notification_templates SET body=$1,updated_at='2000-01-01',row_version=999 WHERE id=$2 RETURNING row_version`, tc.body, id).Scan(&version); err != nil {
			t.Fatal(err)
		}

		if version != tc.want {
			t.Fatalf("body=%s version=%d want=%d", tc.body, version, tc.want)
		}
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = tx.Exec(ctx, `UPDATE notification_templates SET body='rolled back' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}

	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if err := applyMigrationFile(ctx, pool, dir, "209_template_row_version.sql"); err != nil {
			t.Fatal(err)
		}
	}

	var body string

	if err := pool.QueryRow(ctx, `SELECT body,row_version FROM notification_templates WHERE id=$1`, id).Scan(&body, &version); err != nil {
		t.Fatal(err)
	}

	if version != 3 || body != "original" {
		t.Fatalf("rollback/replay changed row: %s/%d", body, version)
	}

	if err := pool.QueryRow(ctx, `UPDATE notification_templates SET channel_id='new-channel' WHERE id=$1 RETURNING row_version`, id).Scan(&version); err != nil {
		t.Fatal(err)
	}

	if version != 4 {
		t.Fatalf("identity update version=%d", version)
	}
}
