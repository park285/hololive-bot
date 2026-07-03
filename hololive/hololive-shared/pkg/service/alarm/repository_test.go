// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package alarm

import (
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestAlarmTypeQueriesUseContainmentAndKeepEmptyArrayDefault(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := dbtest.NewPool(t)
	channelID := "UC_alarm_type_contains"
	baseTime := time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC)

	insertAlarmForTypeQuery(t, pool, "room-live", channelID, domain.AlarmTypes{domain.AlarmTypeLive}, baseTime)
	insertAlarmForTypeQuery(t, pool, "room-empty", channelID, domain.AlarmTypes{}, baseTime.Add(time.Second))
	insertAlarmForTypeQuery(t, pool, "room-community", channelID, domain.AlarmTypes{domain.AlarmTypeCommunity}, baseTime.Add(2*time.Second))
	insertAlarmForTypeQuery(t, pool, "room-other-channel", "UC_other_channel", domain.AlarmTypes{domain.AlarmTypeLive}, baseTime.Add(3*time.Second))

	repository := &Repository{pool: pool}
	got, err := repository.FindByChannelAndType(ctx, channelID, domain.AlarmTypeLive)
	if err != nil {
		t.Fatalf("FindByChannelAndType() error = %v", err)
	}
	requireAlarmRoomIDs(t, got, []string{"room-live", "room-empty"})

	subscribers, err := loadChannelSubscriberAlarms(ctx, pool, channelID, domain.AlarmTypeLive)
	if err != nil {
		t.Fatalf("loadChannelSubscriberAlarms() error = %v", err)
	}
	requireAlarmRoomIDs(t, subscribers, []string{"room-live", "room-empty"})
}

func TestMemberNameQueriesUseMemberDisplayNameAndLatestNonEmptyAlarmFallback(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := &Repository{pool: pool}

	if _, err := pool.Exec(ctx, `
		INSERT INTO members (slug, channel_id, english_name, korean_name, short_korean_name, status, is_graduated, org, sync_source)
		VALUES ($1, $2, $3, $4, $5, 'active', false, 'Hololive', 'manual')
	`, "display-member", "UC_display_name", "Display Member", "표시 멤버", "표시"); err != nil {
		t.Fatalf("insert display member: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO alarms (room_id, user_id, channel_id, member_name, alarm_types, created_at)
		VALUES
			('room-display', 'user-display', 'UC_display_name', '', ARRAY['LIVE']::alarm_type[], $1),
			('room-fallback-old', 'user-fallback-old', 'UC_alarm_fallback', 'Old Fallback', ARRAY['LIVE']::alarm_type[], $2),
			('room-fallback-new', 'user-fallback-new', 'UC_alarm_fallback', 'New Fallback', ARRAY['LIVE']::alarm_type[], $3)
	`, time.Date(2026, 7, 3, 11, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 3, 11, 1, 0, 0, time.UTC),
		time.Date(2026, 7, 3, 11, 2, 0, 0, time.UTC)); err != nil {
		t.Fatalf("insert member-name alarms: %v", err)
	}

	displayName, err := repository.GetMemberName(ctx, "UC_display_name")
	if err != nil {
		t.Fatalf("GetMemberName(display) error = %v", err)
	}
	if displayName != "표시" {
		t.Fatalf("display member name = %q, want 표시", displayName)
	}

	fallbackName, err := repository.GetMemberName(ctx, "UC_alarm_fallback")
	if err != nil {
		t.Fatalf("GetMemberName(fallback) error = %v", err)
	}
	if fallbackName != "New Fallback" {
		t.Fatalf("fallback member name = %q, want New Fallback", fallbackName)
	}

	names, err := repository.GetAllMemberNames(ctx)
	if err != nil {
		t.Fatalf("GetAllMemberNames() error = %v", err)
	}
	if names["UC_display_name"] != "표시" {
		t.Fatalf("all member names display = %q, want 표시", names["UC_display_name"])
	}
	if names["UC_alarm_fallback"] != "New Fallback" {
		t.Fatalf("all member names fallback = %q, want New Fallback", names["UC_alarm_fallback"])
	}
}

func insertAlarmForTypeQuery(t *testing.T, db *pgxpool.Pool, roomID, channelID string, alarmTypes domain.AlarmTypes, createdAt time.Time) {
	t.Helper()

	typesValue, err := alarmTypes.Value()
	if err != nil {
		t.Fatalf("encode alarm types: %v", err)
	}
	if _, err := db.Exec(t.Context(), `
		INSERT INTO alarms (room_id, user_id, channel_id, member_name, room_name, user_name, alarm_types, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7::alarm_type[], $8)
	`, roomID, roomID+"-user", channelID, roomID+"-member", roomID+"-room", roomID+"-user-name", typesValue, createdAt); err != nil {
		t.Fatalf("insert alarm %s: %v", roomID, err)
	}
}

func requireAlarmRoomIDs(t *testing.T, alarms []*domain.Alarm, want []string) {
	t.Helper()

	got := make([]string, 0, len(alarms))
	for _, alarm := range alarms {
		got = append(got, alarm.RoomID)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("room IDs = %v, want %v", got, want)
	}
}
