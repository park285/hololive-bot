package dbtest

import (
	"strings"
	"testing"
)

func TestAsobiMemberMigrationReplayPreservesExistingData(t *testing.T) {
	pool := NewPool(t)
	ctx := t.Context()

	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatal(err)
	}

	var count int

	err = pool.QueryRow(ctx, `SELECT count(*) FROM members
WHERE units=ARRAY['ASOBI★MAWARI-TAI!'] AND org='Hololive' AND status='active'
AND NOT is_graduated AND channel_id ~ '^UC[A-Za-z0-9_-]{22}$'
AND aliases->'ko' ? short_korean_name AND official_link IS NOT NULL`).Scan(&count)
	if err != nil || count != 5 {
		t.Fatalf("new member channels: count=%d err=%v", count, err)
	}

	err = pool.QueryRow(ctx, `SELECT count(*) FROM members m JOIN (VALUES
('hyakuto-kyoko','2000-05-08','2026-09-25'),
('achichi-mela','2000-04-16','2026-09-24'),
('suzuna-tsuzuri','2000-06-28','2026-09-25'),
('sorashina-sopia','2000-06-13','2026-09-24')
) AS expected(slug,birthday,debut) USING(slug)
WHERE m.birthday=expected.birthday::date AND m.debut_date=expected.debut::date`).Scan(&count)
	if err != nil || count != 4 {
		t.Fatalf("personal dates: count=%d err=%v", count, err)
	}

	err = pool.QueryRow(ctx, `SELECT count(*) FROM members
WHERE slug='asobi-mawaritai' AND birthday IS NULL AND debut_date IS NULL`).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("group has personal celebration dates: count=%d err=%v", count, err)
	}

	_, err = pool.Exec(ctx, `UPDATE members SET korean_name='기존 이름', aliases='{"ko":["기존 별칭"]}',
status='graduated', is_graduated=true WHERE slug='achichi-mela';
INSERT INTO alarms(room_id,user_id,channel_id,alarm_types)
VALUES ('asobi-replay','','UC8eitCE9Z6EwUCs-VUi1blg',ARRAY['LIVE']::alarm_type[]);`)
	if err != nil {
		t.Fatal(err)
	}

	const snapshot = `SELECT jsonb_agg(to_jsonb(m) || jsonb_build_object('xmin',m.xmin::text) ORDER BY id)::text FROM members m`

	var before, after string

	if err = pool.QueryRow(ctx, snapshot).Scan(&before); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if err = applyMigrationFile(ctx, pool, dir, "203_asobi_mawaritai_members.sql"); err != nil {
			t.Fatal(err)
		}
	}

	if err = pool.QueryRow(ctx, snapshot).Scan(&after); err != nil {
		t.Fatal(err)
	}

	if before != after {
		t.Fatal("replay changed existing member data or row versions")
	}

	err = pool.QueryRow(ctx, `SELECT count(*) FROM alarms WHERE room_id='asobi-replay'
AND channel_id='UC8eitCE9Z6EwUCs-VUi1blg' AND alarm_types=ARRAY['LIVE']::alarm_type[]`).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("subscription changed: count=%d err=%v", count, err)
	}
}

func TestAsobiMemberMigrationRejectsIdentityConflictsAtomically(t *testing.T) {
	for name, setup := range map[string]string{
		"channel": `UPDATE members SET channel_id='different-channel' WHERE slug='achichi-mela'`,
		"org":     `UPDATE members SET org='Other' WHERE slug='achichi-mela'`,
		"slug":    `UPDATE members SET slug='previously-registered-mela' WHERE slug='achichi-mela'`,
	} {
		t.Run(name, func(t *testing.T) {
			pool := NewPool(t)
			ctx := t.Context()

			dir, err := resolveMigrationsDir()
			if err != nil {
				t.Fatal(err)
			}

			if _, err = pool.Exec(ctx, setup); err != nil {
				t.Fatal(err)
			}

			if _, err = pool.Exec(ctx, `DELETE FROM members WHERE slug='hyakuto-kyoko'`); err != nil {
				t.Fatal(err)
			}

			err = applyMigrationFile(ctx, pool, dir, "203_asobi_mawaritai_members.sql")
			if err == nil || !strings.Contains(err.Error(), "ASOBI MAWARI-TAI member identity conflict") {
				t.Fatalf("expected identity conflict: %v", err)
			}

			var count int

			if err = pool.QueryRow(ctx, `SELECT count(*) FROM members WHERE slug='hyakuto-kyoko'`).Scan(&count); err != nil {
				t.Fatal(err)
			}

			if count != 0 {
				t.Fatal("conflicting seed left a partial insert")
			}
		})
	}
}
