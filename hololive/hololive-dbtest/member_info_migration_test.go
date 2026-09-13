package dbtest

import "testing"

func TestMemberInfoMigrationReplayPreservesIdentityAndSubscriptions(t *testing.T) {
	pool := NewPool(t)
	ctx := t.Context()

	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatal(err)
	}

	_, err = pool.Exec(ctx, `INSERT INTO members(slug,channel_id,english_name,org,sync_source,aliases,status,is_graduated)
 VALUES ('shirakami-fubuki','preserved-channel','Shirakami Fubuki','Hololive','manual','{}','graduated',true);
 INSERT INTO alarms(room_id,user_id,channel_id,alarm_types) VALUES ('info-replay','','preserved-channel',ARRAY['LIVE']::alarm_type[]);`)
	if err != nil {
		t.Fatal(err)
	}

	var id int

	if err = pool.QueryRow(ctx, `SELECT id FROM members WHERE slug='shirakami-fubuki'`).Scan(&id); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if err = applyMigrationFile(ctx, pool, dir, "197_member_info_units.sql"); err != nil {
			t.Fatal(err)
		}
	}

	var count int

	if err = pool.QueryRow(ctx, `SELECT count(*) FROM members WHERE id=$1 AND slug='shirakami-fubuki' AND channel_id='preserved-channel' AND is_graduated AND cardinality(units)=2`, id).Scan(&count); err != nil || count != 1 {
		t.Fatalf("identity check: count=%d err=%v", count, err)
	}

	if err = pool.QueryRow(ctx, `SELECT count(*) FROM members WHERE slug IN ('izuki-michiru','hanazono-sayaka','kazeshiro-yuki') AND channel_id='UCozx5csNhCx1wsVq3SZVkBQ' AND birthday IS NULL AND debut_date IS NOT NULL AND units=ARRAY['holoAN'] AND official_link IS NOT NULL`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("holoAN seed: count=%d err=%v", count, err)
	}

	if err = pool.QueryRow(ctx, `SELECT count(*) FROM alarms WHERE room_id='info-replay' AND alarm_types=ARRAY['LIVE']::alarm_type[]`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("subscription changed: count=%d err=%v", count, err)
	}
}

func TestMemberInfoMigrationRejectsConflictingIdentity(t *testing.T) {
	pool := NewPool(t)
	ctx := t.Context()

	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatal(err)
	}

	if _, err = pool.Exec(ctx, `UPDATE members SET channel_id='different-channel' WHERE slug='izuki-michiru'`); err != nil {
		t.Fatal(err)
	}

	if err = applyMigrationFile(ctx, pool, dir, "197_member_info_units.sql"); err == nil {
		t.Fatal("conflicting member identity was accepted")
	}

	var channelID string

	if err = pool.QueryRow(ctx, `SELECT channel_id FROM members WHERE slug='izuki-michiru'`).Scan(&channelID); err != nil {
		t.Fatal(err)
	}

	if channelID != "different-channel" {
		t.Fatalf("conflicting identity overwritten: %q", channelID)
	}
}

func TestMemberInfoMigrationBackfillsOnlyMissingVerifiedDates(t *testing.T) {
	pool := NewPool(t)
	ctx := t.Context()

	_, err := pool.Exec(ctx, `INSERT INTO members(slug,english_name,org,sync_source,aliases,birthday,debut_date,status,is_graduated) VALUES
 ('gawr-gura','Gawr Gura','Hololive','manual','{}',NULL,NULL,'graduated',true),
 ('watson-amelia','Watson Amelia','Hololive','manual','{}',DATE '1999-01-02',DATE '2010-01-02','graduated',true)`)
	if err != nil {
		t.Fatal(err)
	}

	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if err = applyMigrationFile(ctx, pool, dir, "197_member_info_units.sql"); err != nil {
			t.Fatal(err)
		}
	}

	var birthday, debut string

	if err = pool.QueryRow(ctx, `SELECT birthday::text,debut_date::text FROM members WHERE slug='gawr-gura'`).Scan(&birthday, &debut); err != nil {
		t.Fatal(err)
	}

	if birthday != "2000-06-20" || debut != "2020-09-13" {
		t.Fatalf("missing dates not restored: %s / %s", birthday, debut)
	}

	if err = pool.QueryRow(ctx, `SELECT birthday::text,debut_date::text FROM members WHERE slug='watson-amelia'`).Scan(&birthday, &debut); err != nil {
		t.Fatal(err)
	}

	if birthday != "1999-01-02" || debut != "2010-01-02" {
		t.Fatalf("existing dates overwritten: %s / %s", birthday, debut)
	}
}

func TestHoloANBootstrapKeepsGroupAsChannelRepresentative(t *testing.T) {
	pool := NewPool(t)

	var slug string

	err := pool.QueryRow(t.Context(), `SELECT slug FROM members WHERE channel_id='UCozx5csNhCx1wsVq3SZVkBQ' ORDER BY id LIMIT 1`).Scan(&slug)
	if err != nil {
		t.Fatal(err)
	}

	if slug != "holoan-room" {
		t.Fatalf("fresh bootstrap representative=%q", slug)
	}
}
