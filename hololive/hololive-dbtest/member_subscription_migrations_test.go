package dbtest

import "testing"

func TestMemberSubscriptionMigrationsReplayWithoutChangingChoices(t *testing.T) {
	pool := NewPool(t)
	ctx := t.Context()

	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatalf("resolve migrations dir: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO alarms (room_id, user_id, channel_id, alarm_types)
		VALUES ('member-replay', '', 'UC3OH5FKQ3qtl4uRme_vZTgA', ARRAY['LIVE']::alarm_type[]);
		INSERT INTO alarms (room_id, user_id, channel_id, host_id, alarm_types)
		VALUES ('member-replay', '', 'UC3OH5FKQ3qtl4uRme_vZTgA', 'reimei-mira', ARRAY['SHORTS']::alarm_type[]);
	`); err != nil {
		t.Fatalf("seed channel and member subscriptions: %v", err)
	}

	for range 2 {
		for _, filename := range []string{
			"194_unit_b_member_subscriptions.sql",
			"195_unit_b_member_subscription_index.sql",
			"196_unit_b_member_subscription_identity.sql",
		} {
			if err := applyMigrationFile(ctx, pool, dir, filename); err != nil {
				t.Fatalf("replay %s: %v", filename, err)
			}
		}
	}

	for hostID, want := range map[string]string{"": "LIVE", "reimei-mira": "SHORTS"} {
		var got string

		if err := pool.QueryRow(ctx, `
			SELECT array_to_string(alarm_types, ',') FROM alarms
			WHERE room_id = 'member-replay' AND channel_id = 'UC3OH5FKQ3qtl4uRme_vZTgA' AND host_id = $1
		`, hostID).Scan(&got); err != nil {
			t.Fatalf("read subscription after replay: %v", err)
		}

		if got != want {
			t.Errorf("host %q types after replay = %q, want %q", hostID, got, want)
		}
	}
}
