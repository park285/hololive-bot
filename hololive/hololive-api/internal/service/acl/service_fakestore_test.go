package acl

import (
	"context"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5/pgconn"
)

func pgUniqueViolation(constraint string) error {
	return &pgconn.PgError{
		Code:           "23505",
		Severity:       "ERROR",
		Message:        "duplicate key value violates unique constraint " + constraint,
		ConstraintName: constraint,
	}
}

type fakeACLStore struct {
	mu       sync.Mutex
	settings map[string]string
	rooms    map[roomKey]struct{}

	getSettingHook    func(key string) error
	createSettingHook func(key, value string) error
	upsertHook        func(key, value string) error
	listRoomsHook     func() error
	createRoomHook    func(roomID, listType string) error
	deleteRoomHook    func(roomID, listType string) error
}

type roomKey struct {
	roomID   string
	listType string
}

func newFakeACLStore() *fakeACLStore {
	return &fakeACLStore{
		settings: make(map[string]string),
		rooms:    make(map[roomKey]struct{}),
	}
}

func (f *fakeACLStore) GetSetting(_ context.Context, key string) (string, bool, error) {
	if f.getSettingHook != nil {
		if err := f.getSettingHook(key); err != nil {
			return "", false, fmt.Errorf("get setting hook: %w", err)
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	value, ok := f.settings[key]

	return value, ok, nil
}

func (f *fakeACLStore) CreateSetting(_ context.Context, key, value string) error {
	if f.createSettingHook != nil {
		if err := f.createSettingHook(key, value); err != nil {
			return fmt.Errorf("create setting hook: %w", err)
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if _, exists := f.settings[key]; exists {
		if err := pgUniqueViolation("acl_settings_key_key"); err != nil {
			return fmt.Errorf("pg unique violation: %w", err)
		}

		return nil
	}

	f.settings[key] = value

	return nil
}

func (f *fakeACLStore) UpsertSetting(_ context.Context, key, value string) error {
	if f.upsertHook != nil {
		if err := f.upsertHook(key, value); err != nil {
			return fmt.Errorf("upsert hook: %w", err)
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.settings[key] = value

	return nil
}

func (f *fakeACLStore) ListRooms(_ context.Context) ([]Room, error) {
	if f.listRoomsHook != nil {
		if err := f.listRoomsHook(); err != nil {
			return nil, fmt.Errorf("list rooms hook: %w", err)
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	rooms := make([]Room, 0, len(f.rooms))
	for k := range f.rooms {
		rooms = append(rooms, Room{RoomID: k.roomID, ListType: k.listType})
	}

	return rooms, nil
}

func (f *fakeACLStore) CreateRoom(_ context.Context, roomID, listType string) (bool, error) {
	if f.createRoomHook != nil {
		if err := f.createRoomHook(roomID, listType); err != nil {
			return false, fmt.Errorf("create room hook: %w", err)
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	key := roomKey{roomID: roomID, listType: listType}
	if _, exists := f.rooms[key]; exists {
		return false, nil
	}

	f.rooms[key] = struct{}{}

	return true, nil
}

func (f *fakeACLStore) DeleteRoom(_ context.Context, roomID, listType string) (bool, error) {
	if f.deleteRoomHook != nil {
		if err := f.deleteRoomHook(roomID, listType); err != nil {
			return false, fmt.Errorf("delete room hook: %w", err)
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	key := roomKey{roomID: roomID, listType: listType}
	if _, exists := f.rooms[key]; !exists {
		return false, nil
	}

	delete(f.rooms, key)

	return true, nil
}

func (f *fakeACLStore) settingValue(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()

	v, ok := f.settings[key]
	if !ok {
		return ""
	}

	return v
}
