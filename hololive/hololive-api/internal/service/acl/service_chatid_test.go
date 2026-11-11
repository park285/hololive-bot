package acl

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	dbmocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

// 방 이름 등은 IsRoomAllowed(chatID)가 보지 않아 조용히 무효가 되므로(blacklist라면 fail-open) 등록 단계에서 거절한다.
func TestAddRoomRejectsNonChatIDRegistration(t *testing.T) {
	t.Parallel()

	for _, room := range []string{"홀로라이브 알림방", "room-a", "+123", "0123", "-0", "12 3", "9223372036854775808"} {
		store := newFakeACLStore()
		service := newACLServiceFromFakeStore(t, store, true)

		added, err := service.AddRoom(t.Context(), currentACLMode(service), room)
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

		added, err := service.AddRoom(t.Context(), currentACLMode(service), room)
		if err != nil || !added {
			t.Fatalf("AddRoom(%q) = (%v, %v), want added", room, added, err)
		}
	}
}

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

func TestRemoveRoomRejectsNonChatID(t *testing.T) {
	t.Parallel()

	service := newACLServiceFromFakeStore(t, newFakeACLStore(), true)

	removed, err := service.RemoveRoom(t.Context(), currentACLMode(service), "홀로라이브 알림방")
	if !errors.Is(err, ErrInvalidRoomChatID) || removed {
		t.Fatalf("RemoveRoom() = (%v, %v), want ErrInvalidRoomChatID", removed, err)
	}
}

func TestLoadRejectsStoredNonChatIDRoom(t *testing.T) {
	t.Parallel()

	store := newFakeACLStore()

	store.settings[dbKeyEnabled] = testDBEnabledTrue
	store.settings[dbKeyMode] = string(ACLModeWhitelist)
	store.rooms[roomKey{roomID: "room-legacy", listType: listTypeWhitelist}] = struct{}{}

	service := &Service{store: store, logger: slog.New(slog.DiscardHandler)}
	if err := service.loadFromDatabase(t.Context(), false, ACLModeWhitelist, nil); !errors.Is(err, ErrInvalidRoomChatID) {
		t.Fatalf("loadFromDatabase() error = %v, want ErrInvalidRoomChatID", err)
	}
}

func currentACLMode(service *Service) ACLMode {
	_, mode, _ := service.GetACLStatus()

	return mode
}

func TestRoomChangeWithStaleModeDoesNotWrite(t *testing.T) {
	t.Parallel()

	store := newFakeACLStore()

	store.createRoomHook = func(string, string) error {
		t.Error("stale mode add wrote to the store")

		return nil
	}
	store.deleteRoomHook = func(string, string) error {
		t.Error("stale mode remove wrote to the store")

		return nil
	}

	service := newACLServiceFromFakeStore(t, store, true)

	if added, err := service.AddRoom(t.Context(), ACLModeBlacklist, "3001"); !errors.Is(err, ErrACLModeChanged) || added {
		t.Fatalf("AddRoom() = (%v, %v), want ErrACLModeChanged", added, err)
	}

	if removed, err := service.RemoveRoom(t.Context(), ACLModeBlacklist, "3001"); !errors.Is(err, ErrACLModeChanged) || removed {
		t.Fatalf("RemoveRoom() = (%v, %v), want ErrACLModeChanged", removed, err)
	}
}
