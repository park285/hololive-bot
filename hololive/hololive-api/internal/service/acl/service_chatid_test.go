package acl

import (
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	dbmocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

// 새 ACL 등록값은 정확한 signed i64 chatID 문자열만 받는다. 방 이름 등은 IsRoomAllowed(chatID)가
// 보지 않아 조용히 무효가 되므로(blacklist라면 fail-open) 등록 단계에서 거절한다.
func TestAddRoomRejectsNonChatIDRegistration(t *testing.T) {
	t.Parallel()

	for _, room := range []string{"홀로라이브 알림방", "room-a", "+123", "0123", "-0", "12 3", "9223372036854775808"} {
		store := newFakeACLStore()
		service := newACLServiceFromFakeStore(t, store, true)

		added, err := service.AddRoom(t.Context(), room)
		if err == nil || added {
			t.Fatalf("AddRoom(%q) = (%v, %v), want invalid chatID error", room, added, err)
		}

		if stored := fakeStoreRoomCount(t, store); stored != 0 {
			t.Fatalf("AddRoom(%q) stored %d rooms, want none", room, stored)
		}

		if _, _, rooms := service.GetACLStatus(); len(rooms) != 0 {
			t.Fatalf("AddRoom(%q) left memory rooms %q, want none", room, rooms)
		}
	}
}

func TestAddRoomAcceptsCanonicalChatID(t *testing.T) {
	t.Parallel()

	for _, room := range []string{"18398338829933617", "-4567", "0", "9223372036854775807", " 42 "} {
		store := newFakeACLStore()
		service := newACLServiceFromFakeStore(t, store, true)

		added, err := service.AddRoom(t.Context(), room)
		if err != nil || !added {
			t.Fatalf("AddRoom(%q) = (%v, %v), want added", room, added, err)
		}
	}
}

// KAKAO_ROOMS seed도 같은 계약이다. 방 이름이 들어 있으면 첫 초기화 전에 기동을 거절하고
// acl_settings·acl_rooms에 아무것도 쓰지 않는다.
func TestNewACLServiceRejectsNonChatIDDefaultRooms(t *testing.T) {
	pool := dbtest.NewPool(t)
	dbClient := &dbmocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}

	service, err := NewACLService(t.Context(), dbClient, slog.New(slog.DiscardHandler), true, ACLModeWhitelist,
		[]string{"홀로라이브 알림방", "18398338829933617"})
	if err == nil || service != nil {
		t.Fatalf("NewACLService() = (%v, %v), want invalid default room error", service, err)
	}

	var rooms, settings int

	if scanErr := pool.QueryRow(t.Context(), "SELECT count(*) FROM acl_rooms").Scan(&rooms); scanErr != nil {
		t.Fatalf("count acl_rooms: %v", scanErr)
	}

	if scanErr := pool.QueryRow(t.Context(), "SELECT count(*) FROM acl_settings").Scan(&settings); scanErr != nil {
		t.Fatalf("count acl_settings: %v", scanErr)
	}

	if rooms != 0 || settings != 0 {
		t.Fatalf("acl_rooms=%d acl_settings=%d after rejected init, want 0/0", rooms, settings)
	}
}

func fakeStoreRoomCount(t *testing.T, store *fakeACLStore) int {
	t.Helper()

	rooms, err := store.ListRooms(t.Context())
	if err != nil {
		t.Fatalf("fake ListRooms: %v", err)
	}

	return len(rooms)
}
