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

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestAlarmTypeQueriesUseContainmentAndKeepEmptyArrayDefault(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := dbtest.NewPool(t)
	channelID := "UC_alarm_type_contains"
	baseTime := time.Date(2026, time.July, 3, 10, 0, 0, 0, time.UTC)

	insertAlarmForTypeQuery(t, pool, "room-live", channelID, domain.AlarmTypes{domain.AlarmTypeLive}, baseTime)
	insertAlarmForTypeQuery(t, pool, "room-empty", channelID, domain.AlarmTypes{}, baseTime.Add(time.Second))
	insertAlarmForTypeQuery(t, pool, testCommunityRoomID, channelID, domain.AlarmTypes{domain.AlarmTypeCommunity}, baseTime.Add(2*time.Second))
	insertAlarmForTypeQuery(t, pool, "room-other-channel", "UC_other_channel", domain.AlarmTypes{domain.AlarmTypeLive}, baseTime.Add(3*time.Second))

	repository := &Repository{pool: pool}

	got, err := repository.FindByChannelAndType(ctx, channelID, domain.AlarmTypeLive)
	if err != nil {
		t.Fatalf("FindByChannelAndType() error = %v", err)
	}

	requireAlarmRoomIDs(t, got, []string{"room-live", "room-empty"})

	subscribers, err := NewSubscriberResolver(nil, pool).loadChannelSubscriberAlarms(ctx, channelID, domain.AlarmTypeLive)
	if err != nil {
		t.Fatalf("loadChannelSubscriberAlarms() error = %v", err)
	}

	requireAlarmRoomIDs(t, subscribers, []string{"room-live", "room-empty"})
}

// 표시명은 members 정본만 쓴다. 멤버 데이터에 한국어 표시명이 없는 채널은 alarms.member_name이 있어도 빈 값이고 전체 목록에서 빠진다.
func TestMemberNameQueriesUseOnlyMemberDisplayName(t *testing.T) {
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
	`, time.Date(2026, time.July, 3, 11, 0, 0, 0, time.UTC),
		time.Date(2026, time.July, 3, 11, 1, 0, 0, time.UTC),
		time.Date(2026, time.July, 3, 11, 2, 0, 0, time.UTC)); err != nil {
		t.Fatalf("insert member-name alarms: %v", err)
	}

	displayName, err := repository.GetMemberName(ctx, "UC_display_name")
	if err != nil {
		t.Fatalf("GetMemberName(display) error = %v", err)
	}

	if displayName != "표시" {
		t.Fatalf("display member name = %q, want 표시", displayName)
	}

	missingName, err := repository.GetMemberName(ctx, "UC_alarm_fallback")
	if err != nil {
		t.Fatalf("GetMemberName(missing) error = %v", err)
	}

	if missingName != "" {
		t.Fatalf("member name without members display name = %q, want empty", missingName)
	}

	names, err := repository.GetAllMemberNames(ctx)
	if err != nil {
		t.Fatalf("GetAllMemberNames() error = %v", err)
	}

	if names["UC_display_name"] != "표시" {
		t.Fatalf("all member names display = %q, want 표시", names["UC_display_name"])
	}

	if name, ok := names["UC_alarm_fallback"]; ok {
		t.Fatalf("all member names include channel without members display name: %q", name)
	}
}

// 방 이름 대표값은 created_at이 아니라 room_name이 마지막으로 바뀐 행을 따른다. 먼저 만든 구독에 upsert로 새 이름이
// 들어오면 나중에 만든 구독의 옛 이름에 가려지면 안 되고, 같은 이름으로 재등록한 구독이 대표값을 되가져가도 안 된다.
func TestListAlarmEntriesUsesMostRecentlyChangedKakaoRoomName(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	repository := &Repository{pool: dbtest.NewPool(t)}
	roomID := "room-renamed"

	add := func(channelID, roomName string, alarmTypes domain.AlarmTypes) {
		t.Helper()

		if err := repository.Add(ctx, &domain.Alarm{
			RoomID:     roomID,
			UserID:     "user-renamed",
			ChannelID:  channelID,
			RoomName:   roomName,
			AlarmTypes: alarmTypes,
		}); err != nil {
			t.Fatalf("Add(%s, %q) error = %v", channelID, roomName, err)
		}
	}

	requireRoomName := func(want string) {
		t.Helper()

		entries, err := repository.ListAlarmEntries(ctx)
		if err != nil {
			t.Fatalf("ListAlarmEntries() error = %v", err)
		}

		if len(entries) != 2 {
			t.Fatalf("entries = %d, want 2", len(entries))
		}

		for _, entry := range entries {
			if entry.RoomName != want {
				t.Fatalf("entry %s/%s room name = %q, want %q", entry.RoomID, entry.ChannelID, entry.RoomName, want)
			}
		}
	}

	add("UC_room_name_a", "옛 방", domain.AlarmTypes{domain.AlarmTypeLive})
	add("UC_room_name_b", "옛 방", domain.AlarmTypes{domain.AlarmTypeLive})
	add("UC_room_name_a", "새 방", domain.AlarmTypes{domain.AlarmTypeLive, domain.AlarmTypeShorts})
	requireRoomName("새 방")

	add("UC_room_name_b", "옛 방", domain.AlarmTypes{domain.AlarmTypeLive, domain.AlarmTypeShorts})
	requireRoomName("새 방")
}

// Iris webhook에는 방 제목이 없다. 옛 bot(v7.0.0까지)은 방 ID를 RoomName으로 넘겼고, 지금 bot(v7.0.1)은 빈 이름을
// 넘긴다. 운영 alarms에는 room_name = room_id인 행이 실제 Kakao 이름 행보다 나중에 바뀐 것으로 섞여 있다. 대표값은
// 관리자 이름 → Kakao 이름 → 방 ID이고, 방 ID 자리표시자는 Kakao 이름을 가리지 못하며 재등록 upsert는 방 ID든 빈
// 이름이든 저장된 Kakao 이름을 덮어쓰지 못한다. 먼저 배포한 worker는 옛 API가 보내는 방 ID도 받는다.
func TestListAlarmEntriesPrefersKakaoRoomNameOverRoomIDPlaceholder(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := &Repository{pool: pool}
	baseTime := time.Date(2026, time.September, 1, 9, 0, 0, 0, time.UTC)

	// 옛 버전이 남긴 혼합 행: 실제 이름 행이 먼저, 방 ID 자리표시자 행이 더 최근 room_name_updated_at·더 큰 id로 들어 있다.
	if _, err := pool.Exec(ctx, `
		INSERT INTO alarms (room_id, user_id, channel_id, room_name, alarm_types, room_name_updated_at)
		VALUES
			('room-mixed', 'user-1', 'UC_mixed_a', '홀로 방', ARRAY['LIVE']::alarm_type[], $1),
			('room-mixed', 'user-1', 'UC_mixed_b', '홀로 방', ARRAY['LIVE']::alarm_type[], $1),
			('room-mixed', 'user-1', 'UC_mixed_c', 'room-mixed', ARRAY['LIVE']::alarm_type[], $2),
			('room-id-only', 'user-2', 'UC_id_only', 'room-id-only', ARRAY['LIVE']::alarm_type[], $2),
			('room-aliased', 'user-3', 'UC_aliased', 'Kakao 방', ARRAY['LIVE']::alarm_type[], $1)
	`, baseTime, baseTime.Add(time.Hour)); err != nil {
		t.Fatalf("insert mixed room-name alarms: %v", err)
	}

	if err := repository.SetRoomDisplayName(ctx, "room-aliased", "관리자 이름"); err != nil {
		t.Fatalf("SetRoomDisplayName() error = %v", err)
	}

	// 옛 API(방 ID)와 v7.0.1 API(빈 이름)의 재등록·새 채널 등록.
	addWithoutKakaoName := func(channelID, roomName string) {
		t.Helper()

		if err := repository.Add(ctx, &domain.Alarm{
			RoomID:     "room-mixed",
			UserID:     "user-1",
			ChannelID:  channelID,
			RoomName:   roomName,
			AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive, domain.AlarmTypeShorts},
		}); err != nil {
			t.Fatalf("Add(%s) error = %v", channelID, err)
		}
	}

	addWithoutKakaoName("UC_mixed_a", "room-mixed")
	addWithoutKakaoName("UC_mixed_b", "")
	addWithoutKakaoName("UC_mixed_d", "room-mixed")
	addWithoutKakaoName("UC_mixed_e", "")

	requireStoredRoomName(t, pool, "UC_mixed_a", "홀로 방", baseTime)
	requireStoredRoomName(t, pool, "UC_mixed_b", "홀로 방", baseTime)
	requireStoredRoomName(t, pool, "UC_mixed_d", "", time.Time{})
	requireStoredRoomName(t, pool, "UC_mixed_e", "", time.Time{})
	requireListedRoomNames(t, repository, 7, map[string]string{
		"room-mixed":   "홀로 방",
		"room-id-only": "",
		"room-aliased": "관리자 이름",
	})
}

// requireStoredRoomName은 room-mixed 방의 채널 행에 저장된 room_name을 확인한다. 기대 시각이 0이 아니면
// room_name_updated_at도 그대로여야 한다.
func requireStoredRoomName(t *testing.T, pool *pgxpool.Pool, channelID, wantName string, wantUpdatedAt time.Time) {
	t.Helper()

	var (
		name      string
		updatedAt time.Time
	)

	if err := pool.QueryRow(t.Context(), `
		SELECT COALESCE(room_name, ''), room_name_updated_at FROM alarms WHERE room_id = 'room-mixed' AND channel_id = $1
	`, channelID).Scan(&name, &updatedAt); err != nil {
		t.Fatalf("select alarm %s: %v", channelID, err)
	}

	if name != wantName || (!wantUpdatedAt.IsZero() && !updatedAt.Equal(wantUpdatedAt)) {
		t.Fatalf("alarm %s room name = %q updated %s, want %q updated %s", channelID, name, updatedAt, wantName, wantUpdatedAt)
	}
}

func requireListedRoomNames(t *testing.T, repository *Repository, wantPairs int, want map[string]string) {
	t.Helper()

	entries, err := repository.ListAlarmEntries(t.Context())
	if err != nil {
		t.Fatalf("ListAlarmEntries() error = %v", err)
	}

	if len(entries) != wantPairs {
		t.Fatalf("entries = %d, want %d room-channel pairs", len(entries), wantPairs)
	}

	for _, entry := range entries {
		wantName, ok := want[entry.RoomID]
		if !ok {
			t.Fatalf("unexpected entry %s/%s", entry.RoomID, entry.ChannelID)
		}

		if entry.RoomName != wantName {
			t.Fatalf("entry %s/%s room name = %q, want %q", entry.RoomID, entry.ChannelID, entry.RoomName, wantName)
		}
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
