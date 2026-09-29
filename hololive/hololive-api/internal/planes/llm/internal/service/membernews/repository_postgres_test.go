package membernews

import (
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

func newPostgresMemberNewsRepository(t *testing.T) (*Repository, *pgxpool.Pool) {
	t.Helper()

	pool := dbtest.NewPool(t)

	return NewRepository(&databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}), pool
}

func TestRepositoryPostgres_GetRoomMembersResolvesNamesFromAlarmsAndMembers(t *testing.T) {
	repository, pool := newPostgresMemberNewsRepository(t)
	ctx := t.Context()

	// migration seed와 겹치지 않도록 테스트 전용 channel/slug/room 접두사를 쓴다.
	const (
		roomID    = "b20-room-main"
		otherRoom = "b20-room-other"
	)

	_, err := pool.Exec(ctx, `
		INSERT INTO members (slug, channel_id, english_name, japanese_name, korean_name, org, sync_source) VALUES
			('b20-korean',   'UCb20TestKorean00000000', 'Echo English',  'Echo Japanese',  'Delta Korean', 'Hololive', 'manual'),
			('b20-english',  'UCb20TestEnglish0000000', 'Foxtrot English', 'Foxtrot Japanese', '',          'Hololive', 'manual'),
			('b20-japanese', 'UCb20TestJapanese000000', '',              'Golf Japanese',  NULL,           'Hololive', 'manual'),
			('b20-dup',      'UCb20TestDuplicate00000', 'Hotel English', NULL,             'Alpha Shared', 'Hololive', 'manual'),
			('b20-override', 'UCb20TestOverride000000', 'India English', NULL,             'India Korean', 'Hololive', 'manual'),
			('b20-other',    'UCb20TestOtherRoom00000', 'Juliet English', NULL,            'Juliet Korean', 'Hololive', 'manual')`)
	if err != nil {
		t.Fatalf("seed members: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO alarms (room_id, user_id, channel_id, member_name, alarm_types) VALUES
			($1, 'b20-user', 'UCb20TestKorean00000000',   '',             ARRAY['LIVE']::alarm_type[]),
			($1, 'b20-user', 'UCb20TestEnglish0000000',   NULL,           ARRAY['COMMUNITY']::alarm_type[]),
			($1, 'b20-user', 'UCb20TestJapanese000000',   '',             ARRAY['COMMUNITY']::alarm_type[]),
			($1, 'b20-user', 'UCb20TestDuplicate00000',   '',             ARRAY['LIVE']::alarm_type[]),
			($1, 'b20-user', 'UCb20TestNoMemberNamed0',   'Alpha Shared', ARRAY['LIVE']::alarm_type[]),
			($1, 'b20-user', 'UCb20TestNoMemberBlank0',   '',             ARRAY['LIVE']::alarm_type[]),
			($1, 'b20-user', 'UCb20TestOverride000000',   'Bravo Alarm',  ARRAY['COMMUNITY']::alarm_type[]),
			($2, 'b20-user', 'UCb20TestOtherRoom00000',   '',             ARRAY['LIVE']::alarm_type[])`,
		roomID, otherRoom)
	if err != nil {
		t.Fatalf("seed alarms: %v", err)
	}

	got, err := repository.GetRoomMembers(ctx, roomID)
	if err != nil {
		t.Fatalf("GetRoomMembers: %v", err)
	}

	// 비-LIVE 알람도 포함하고, alarms.member_name이 비면 korean→english→japanese 순으로 대체한다.
	// members에 없는 채널은 알람 이름이 있을 때만 남고, 같은 이름은 한 번만 나온다.
	want := []string{"Alpha Shared", "Bravo Alarm", "Delta Korean", "Foxtrot English", "Golf Japanese"}
	if !slices.Equal(got, want) {
		t.Fatalf("GetRoomMembers(%q) = %q, want %q", roomID, got, want)
	}

	other, err := repository.GetRoomMembers(ctx, otherRoom)
	if err != nil {
		t.Fatalf("GetRoomMembers other room: %v", err)
	}

	if !slices.Equal(other, []string{"Juliet Korean"}) {
		t.Fatalf("GetRoomMembers(%q) = %q, want only its own member", otherRoom, other)
	}

	empty, err := repository.GetRoomMembers(ctx, "b20-room-without-alarms")
	if err != nil {
		t.Fatalf("GetRoomMembers empty room: %v", err)
	}

	if len(empty) != 0 {
		t.Fatalf("GetRoomMembers empty room = %q, want none", empty)
	}
}

const (
	subRoomA = "b20-sub-a"
	subRoomB = "b20-sub-b"
	subRoomC = "b20-sub-c"
)

type subscribedRoomView struct{ id, name string }

func TestRepositoryPostgres_SubscriptionLifecycle(t *testing.T) {
	repository, pool := newPostgresMemberNewsRepository(t)
	ctx := t.Context()

	// bot은 방 이름을 모르므로(Iris webhook에 방 제목 없음) 빈 이름으로 구독한다. 빈/공백 이름은 모름(NULL)이다.
	for _, room := range []subscribedRoomView{{subRoomA, "Alpha"}, {subRoomB, "Bravo"}, {subRoomC, ""}} {
		if err := repository.Subscribe(ctx, room.id, room.name); err != nil {
			t.Fatalf("Subscribe(%q): %v", room.id, err)
		}
	}

	var unknownStoredAsNull bool

	if err := pool.QueryRow(ctx, `SELECT room_name IS NULL FROM member_news_subscriptions WHERE room_id = $1`, subRoomC).
		Scan(&unknownStoredAsNull); err != nil {
		t.Fatalf("read stored room_name: %v", err)
	}

	if !unknownStoredAsNull {
		t.Fatal("unknown room name was stored as a value instead of NULL")
	}

	// 재구독은 upsert다. 모르는 이름(빈/공백)은 저장된 이름을 덮어쓰지 않는다.
	for _, room := range []subscribedRoomView{{subRoomA, ""}, {subRoomB, "   "}} {
		if err := repository.Subscribe(ctx, room.id, room.name); err != nil {
			t.Fatalf("resubscribe %q with %q: %v", room.id, room.name, err)
		}
	}

	// 삽입 순서·id 순서와 다른 created_at을 두어 목록이 created_at 기준임을 확인한다.
	base := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	offsets := map[string]time.Duration{subRoomC: 0, subRoomA: time.Hour, subRoomB: 2 * time.Hour}

	for roomID, offset := range offsets {
		if _, err := pool.Exec(ctx, `UPDATE member_news_subscriptions SET created_at = $2 WHERE room_id = $1`, roomID, base.Add(offset)); err != nil {
			t.Fatalf("set created_at for %q: %v", roomID, err)
		}
	}

	assertSubscribedRooms(t, repository, []subscribedRoomView{{subRoomC, ""}, {subRoomA, "Alpha"}, {subRoomB, "Bravo"}})

	// 아는 이름은 저장된 이름을 바꾸고, 모르던 이름을 채운다.
	for _, room := range []subscribedRoomView{{subRoomA, "Alpha 2"}, {subRoomC, "Charlie"}} {
		if err := repository.Subscribe(ctx, room.id, room.name); err != nil {
			t.Fatalf("resubscribe %q with %q: %v", room.id, room.name, err)
		}
	}

	assertSubscribedRooms(t, repository, []subscribedRoomView{{subRoomC, "Charlie"}, {subRoomA, "Alpha 2"}, {subRoomB, "Bravo"}})

	if err := repository.Unsubscribe(ctx, subRoomA); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}

	// 없는 방 해지는 오류 없이 무시된다.
	if err := repository.Unsubscribe(ctx, "b20-sub-missing"); err != nil {
		t.Fatalf("Unsubscribe missing room: %v", err)
	}

	assertSubscriptionStates(t, repository, map[string]bool{subRoomA: false, subRoomB: true, "b20-sub-missing": false})
}

func assertSubscribedRooms(t *testing.T, repository *Repository, want []subscribedRoomView) {
	t.Helper()

	rooms, err := repository.ListSubscribedRooms(t.Context())
	if err != nil {
		t.Fatalf("ListSubscribedRooms: %v", err)
	}

	got := make([]subscribedRoomView, 0, len(rooms))
	for _, room := range rooms {
		got = append(got, subscribedRoomView{room.RoomID, room.RoomName})
	}

	if !slices.Equal(got, want) {
		t.Fatalf("ListSubscribedRooms = %+v, want %+v", got, want)
	}
}

func assertSubscriptionStates(t *testing.T, repository *Repository, want map[string]bool) {
	t.Helper()

	for roomID, wantSubscribed := range want {
		subscribed, err := repository.IsSubscribed(t.Context(), roomID)
		if err != nil {
			t.Fatalf("IsSubscribed(%q): %v", roomID, err)
		}

		if subscribed != wantSubscribed {
			t.Fatalf("IsSubscribed(%q) = %v, want %v", roomID, subscribed, wantSubscribed)
		}
	}
}
