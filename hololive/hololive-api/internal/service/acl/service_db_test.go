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

package acl

import (
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	dbmocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

func TestNewACLService_FirstInitUsesDefaults(t *testing.T) {
	pool := dbtest.NewPool(t)
	service := newACLServiceFromPool(t, pool, true, []string{testRoomA, testRoomB})

	assertACLStatus(t, service, true, ACLModeWhitelist, 2, []string{testRoomA, testRoomB})
	assertACLSettingValue(t, pool, dbKeyEnabled, testDBEnabledTrue)
	assertACLSettingValue(t, pool, dbKeyMode, string(ACLModeWhitelist))
	assertACLRoomCount(t, pool, testRoomA, listTypeWhitelist, 1)
	assertACLRoomCount(t, pool, testRoomB, listTypeWhitelist, 1)
}

func TestNewACLService_FirstInitBlacklistMode(t *testing.T) {
	pool := dbtest.NewPool(t)
	logger := slog.New(slog.DiscardHandler)

	dbClient := &dbmocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}

	service, err := NewACLService(
		t.Context(),
		dbClient,
		logger,
		true,
		ACLModeBlacklist,
		[]string{testBlockedRoom},
	)
	if err != nil {
		t.Fatalf("NewACLService error: %v", err)
	}

	enabled, mode, rooms := service.GetACLStatus()
	if !enabled {
		t.Fatal("expected enabled=true")
	}

	if mode != ACLModeBlacklist {
		t.Fatalf("expected mode=blacklist, got %s", mode)
	}

	if len(rooms) != 1 || rooms[0] != testBlockedRoom {
		t.Fatalf("unexpected rooms: %v", rooms)
	}

	// DB에 mode=blacklist로 저장되었는지 확인
	assertACLSettingValue(t, pool, dbKeyMode, "blacklist")

	// list_type이 blacklist인지 확인
	ctx := t.Context()

	rows, err := pool.Query(ctx, "SELECT room_id, list_type FROM acl_rooms")
	if err != nil {
		t.Fatalf("query rooms: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var roomID, listType string

		if err := rows.Scan(&roomID, &listType); err != nil {
			t.Fatalf("scan room: %v", err)
		}

		if listType != listTypeBlacklist {
			t.Fatalf("room %q list_type=%q, want=blacklist", roomID, listType)
		}
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("iterate rooms: %v", err)
	}
}

func TestNewACLService_ExistingDBStateWins(t *testing.T) {
	pool := dbtest.NewPool(t)
	// 기존 DB에 데이터가 있으면 기본값 대신 DB 값을 사용해야 한다
	mustCreateACLSetting(t, pool, dbKeyEnabled, testDBEnabledFalse)
	mustCreateACLSetting(t, pool, dbKeyMode, "blacklist")
	mustCreateACLRoom(t, pool, "existing-room", listTypeBlacklist)

	dbClient := &dbmocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}

	service, err := NewACLService(
		t.Context(),
		dbClient,
		slog.New(slog.DiscardHandler),
		true,
		ACLModeWhitelist,
		[]string{"2004"},
	)
	if err != nil {
		t.Fatalf("NewACLService error: %v", err)
	}

	enabled, mode, rooms := service.GetACLStatus()
	if enabled {
		t.Fatal("expected enabled=false from DB")
	}

	if mode != ACLModeBlacklist {
		t.Fatalf("expected mode=blacklist from DB, got %s", mode)
	}

	if len(rooms) != 1 || rooms[0] != "existing-room" {
		t.Fatalf("expected only existing room from DB, got %v", rooms)
	}
}

func TestNewACLService_ReturnsInvalidDatabaseStateError(t *testing.T) {
	t.Parallel()

	pool := dbtest.NewPool(t)
	mustCreateACLSetting(t, pool, dbKeyEnabled, "not-a-bool")

	dbClient := &dbmocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}
	service, err := NewACLService(
		t.Context(),
		dbClient,
		slog.New(slog.DiscardHandler),
		true,
		ACLModeWhitelist,
		[]string{"2004"},
	)

	if service != nil {
		t.Fatal("NewACLService returned a service for invalid database state")
	}

	if err == nil {
		t.Fatal("NewACLService error = nil, want invalid database state error")
	}

	if !strings.Contains(err.Error(), "invalid ACL enabled setting") {
		t.Fatalf("NewACLService error = %v, want invalid enabled setting", err)
	}
}

func TestACLService_SetEnabledAddRemoveRoom(t *testing.T) {
	pool := dbtest.NewPool(t)
	service := newACLServiceFromPool(t, pool, false, nil)

	assertACLSetEnabled(t, service, pool, true)
	assertACLAddRoomLifecycle(t, service)
	assertACLRoomRemovedFromDB(t, pool, testRoomX)
}

func newACLServiceFromPool(
	t *testing.T,
	pool *pgxpool.Pool,
	defaultEnabled bool,
	defaultRooms []string,
) *Service {
	t.Helper()

	dbClient := &dbmocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}

	service, err := NewACLService(
		t.Context(),
		dbClient,
		slog.New(slog.DiscardHandler),
		defaultEnabled,
		ACLModeWhitelist,
		defaultRooms,
	)
	if err != nil {
		t.Fatalf("NewACLService error: %v", err)
	}

	return service
}

// newACLServiceFromFakeStore는 fake store를 주입한 Service를 직접 구성한다.
// DB fault-injection이 필요한 테스트에 쓴다.
func newACLServiceFromFakeStore(t *testing.T, store aclStore, enabled bool) *Service {
	t.Helper()

	return &Service{
		store:          store,
		logger:         slog.New(slog.DiscardHandler),
		enabled:        enabled,
		mode:           ACLModeWhitelist,
		whitelistRooms: make(map[string]struct{}),
		blacklistRooms: make(map[string]struct{}),
	}
}

func assertACLSetEnabled(t *testing.T, service *Service, pool *pgxpool.Pool, enabled bool) {
	t.Helper()

	if err := service.SetEnabled(t.Context(), enabled); err != nil {
		t.Fatalf("SetEnabled error: %v", err)
	}

	gotEnabled, _, _ := service.GetACLStatus()
	if gotEnabled != enabled {
		t.Fatalf("enabled=%v want=%v", gotEnabled, enabled)
	}

	assertACLSettingValue(t, pool, dbKeyEnabled, strconv.FormatBool(enabled))
}

func assertACLAddRoomLifecycle(t *testing.T, service *Service) {
	t.Helper()

	assertAddRoomResult(t, service, " 1099 ", true)
	assertAddRoomResult(t, service, testRoomX, false)
	assertAddRoomResult(t, service, "   ", false)
	assertRemoveRoomResult(t, service, " 1099 ", true)
	assertRemoveRoomResult(t, service, testRoomX, false)
	assertRemoveRoomResult(t, service, "   ", false)
}

func assertAddRoomResult(t *testing.T, service *Service, room string, wantAdded bool) {
	t.Helper()

	added, err := service.AddRoom(t.Context(), room)
	if err != nil {
		t.Fatalf("AddRoom(%q) error: %v", room, err)
	}

	if added != wantAdded {
		t.Fatalf("AddRoom(%q) added=%v want=%v", room, added, wantAdded)
	}
}

func assertRemoveRoomResult(t *testing.T, service *Service, room string, wantRemoved bool) {
	t.Helper()

	removed, err := service.RemoveRoom(t.Context(), room)
	if err != nil {
		t.Fatalf("RemoveRoom(%q) error: %v", room, err)
	}

	if removed != wantRemoved {
		t.Fatalf("RemoveRoom(%q) removed=%v want=%v", room, removed, wantRemoved)
	}
}

func assertACLRoomRemovedFromDB(t *testing.T, pool *pgxpool.Pool, roomID string) {
	t.Helper()

	var count int64

	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM acl_rooms WHERE room_id = $1", roomID).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", roomID, err)
	}

	if count != 0 {
		t.Fatalf("%s should be removed from DB, count=%d", roomID, count)
	}
}

func assertACLRoomCount(t *testing.T, pool *pgxpool.Pool, roomID, listType string, wantCount int64) {
	t.Helper()

	var count int64

	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM acl_rooms WHERE room_id = $1 AND list_type = $2", roomID, listType).Scan(&count); err != nil {
		t.Fatalf("count %s/%s: %v", roomID, listType, err)
	}

	if count != wantCount {
		t.Fatalf("room %s list_type %s count=%d want=%d", roomID, listType, count, wantCount)
	}
}

func assertACLSettingValue(t *testing.T, pool *pgxpool.Pool, key, wantValue string) {
	t.Helper()

	var value string

	if err := pool.QueryRow(t.Context(), "SELECT value FROM acl_settings WHERE key = $1", key).Scan(&value); err != nil {
		t.Fatalf("query setting %s: %v", key, err)
	}

	if value != wantValue {
		t.Fatalf("setting %s value=%q want=%q", key, value, wantValue)
	}
}

func mustCreateACLSetting(t *testing.T, pool *pgxpool.Pool, key, value string) {
	t.Helper()

	if _, err := pool.Exec(t.Context(), "INSERT INTO acl_settings (key, value) VALUES ($1, $2)", key, value); err != nil {
		t.Fatalf("seed setting %s: %v", key, err)
	}
}

func mustCreateACLRoom(t *testing.T, pool *pgxpool.Pool, roomID, listType string) {
	t.Helper()

	if _, err := pool.Exec(t.Context(), "INSERT INTO acl_rooms (room_id, list_type) VALUES ($1, $2)", roomID, listType); err != nil {
		t.Fatalf("seed room %s: %v", roomID, err)
	}
}

func assertACLSetMode(t *testing.T, service *Service, mode ACLMode) {
	t.Helper()

	if err := service.SetMode(t.Context(), mode); err != nil {
		t.Fatalf("SetMode(%s) error: %v", mode, err)
	}
}

func TestACLService_SetMode(t *testing.T) {
	pool := dbtest.NewPool(t)
	service := newACLServiceFromPool(t, pool, true, []string{"2001"})

	assertACLStatus(t, service, true, ACLModeWhitelist, 1, []string{"2001"})
	assertACLSetMode(t, service, ACLModeBlacklist)
	assertAddRoomResult(t, service, "2002", true)
	assertACLStatus(t, service, true, ACLModeBlacklist, 1, []string{"2002"})
	assertACLSetMode(t, service, ACLModeWhitelist)
	assertACLStatus(t, service, true, ACLModeWhitelist, 1, []string{"2001"})
	assertACLSettingValue(t, pool, dbKeyMode, string(ACLModeWhitelist))
}

func TestACLService_AddRemoveRoomWithListType(t *testing.T) {
	pool := dbtest.NewPool(t)
	service := newACLServiceFromPool(t, pool, true, nil)

	// 화이트리스트 모드에서 방 추가
	if added, addErr := service.AddRoom(t.Context(), "2003"); addErr != nil || !added {
		t.Fatalf("AddRoom whitelist: added=%v err=%v", added, addErr)
	}

	// 블랙리스트 모드로 전환 후 같은 이름의 방 추가 (다른 list_type이므로 가능)
	assertACLSetMode(t, service, ACLModeBlacklist)

	if added, addErr := service.AddRoom(t.Context(), "2003"); addErr != nil || !added {
		t.Fatalf("AddRoom blacklist: added=%v err=%v", added, addErr)
	}

	// DB에 두 개의 레코드가 있어야 함 (같은 room_id, 다른 list_type)
	var roomCount int64

	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM acl_rooms WHERE room_id = $1", "2003").Scan(&roomCount); err != nil {
		t.Fatalf("query rooms: %v", err)
	}

	if roomCount != 2 {
		t.Fatalf("expected 2 rooms with same ID but different list_type, got %d", roomCount)
	}

	// 블랙리스트에서 제거해도 화이트리스트는 유지
	if removed, removeErr := service.RemoveRoom(t.Context(), "2003"); removeErr != nil || !removed {
		t.Fatalf("RemoveRoom blacklist: removed=%v err=%v", removed, removeErr)
	}

	assertACLSetMode(t, service, ACLModeWhitelist)

	_, _, rooms := service.GetACLStatus()
	if len(rooms) != 1 || rooms[0] != "2003" {
		t.Fatalf("whitelist should still have shared-room, got %v", rooms)
	}
}

func TestACLService_SetEnabled_DoesNotMutateMemoryOnDBFailure(t *testing.T) {
	t.Parallel()

	store := newFakeACLStore()

	store.settings[dbKeyEnabled] = testDBEnabledFalse
	store.upsertHook = func(key, _ string) error {
		if key == dbKeyEnabled {
			return errors.New("forced update failure")
		}

		return nil
	}

	service := newACLServiceFromFakeStore(t, store, false)

	err := service.SetEnabled(t.Context(), true)
	if err == nil {
		t.Fatal("expected SetEnabled error")
	}

	if !strings.Contains(err.Error(), "failed to save ACL enabled setting") {
		t.Fatalf("unexpected error: %v", err)
	}

	enabled, _, _ := service.GetACLStatus()
	if enabled {
		t.Fatal("expected in-memory enabled=false after DB failure")
	}

	if v := store.settingValue(dbKeyEnabled); v != testDBEnabledFalse {
		t.Fatalf("enabled setting value=%q want=false", v)
	}
}

func TestACLService_SetMode_DoesNotMutateMemoryOnDBFailure(t *testing.T) {
	t.Parallel()

	store := newFakeACLStore()

	store.settings[dbKeyMode] = string(ACLModeWhitelist)
	store.upsertHook = func(key, _ string) error {
		if key == dbKeyMode {
			return errors.New("forced update failure")
		}

		return nil
	}

	service := newACLServiceFromFakeStore(t, store, true)

	err := service.SetMode(t.Context(), ACLModeBlacklist)
	if err == nil {
		t.Fatal("expected SetMode error")
	}

	if !strings.Contains(err.Error(), "failed to save ACL mode setting") {
		t.Fatalf("unexpected error: %v", err)
	}

	_, mode, _ := service.GetACLStatus()
	if mode != ACLModeWhitelist {
		t.Fatalf("expected in-memory mode=whitelist after DB failure, got %s", mode)
	}

	if v := store.settingValue(dbKeyMode); v != string(ACLModeWhitelist) {
		t.Fatalf("mode setting value=%q want=whitelist", v)
	}
}

func TestACLService_LoadFromDatabase_ReturnsInitCreateError(t *testing.T) {
	t.Parallel()

	for _, tc := range aclLoadFromDatabaseInitCreateErrorTests() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runACLLoadFromDatabaseInitCreateErrorCase(t, tc)
		})
	}
}

func TestACLService_LoadFromDatabase_RejectsInvalidStoredValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setup     func(*fakeACLStore)
		wantError string
	}{
		{
			name: "enabled",
			setup: func(store *fakeACLStore) {
				store.settings[dbKeyEnabled] = "not-a-bool"
				store.settings[dbKeyMode] = string(ACLModeWhitelist)
			},
			wantError: "invalid ACL enabled setting",
		},
		{
			name: "mode",
			setup: func(store *fakeACLStore) {
				store.settings[dbKeyEnabled] = testDBEnabledTrue
				store.settings[dbKeyMode] = "not-a-mode"
			},
			wantError: "unsupported acl mode",
		},
		{
			name: "list type",
			setup: func(store *fakeACLStore) {
				store.settings[dbKeyEnabled] = testDBEnabledTrue
				store.settings[dbKeyMode] = string(ACLModeWhitelist)
				store.rooms[roomKey{roomID: testRoomA, listType: "not-a-list"}] = struct{}{}
			},
			wantError: "invalid ACL room",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := newFakeACLStore()
			tc.setup(store)

			service := newACLServiceFromFakeStore(t, store, true)

			err := service.loadFromDatabase(t.Context(), true, ACLModeWhitelist, []string{"default-room"})
			if err == nil {
				t.Fatal("loadFromDatabase error = nil, want invalid stored value error")
			}

			if !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("loadFromDatabase error = %v, want %q", err, tc.wantError)
			}
		})
	}
}

type aclLoadFromDatabaseInitCreateErrorCase struct {
	name      string
	setupHook func(store *fakeACLStore)
	wantErr   string
}

func aclLoadFromDatabaseInitCreateErrorTests() []aclLoadFromDatabaseInitCreateErrorCase {
	return []aclLoadFromDatabaseInitCreateErrorCase{
		{
			name: "enabled setting create failure",
			setupHook: func(store *fakeACLStore) {
				store.createSettingHook = func(key, _ string) error {
					if key == dbKeyEnabled {
						return errors.New("forced enabled create failure")
					}

					return nil
				}
			},
			wantErr: "failed to initialize ACL enabled setting",
		},
		{
			name: "mode setting create failure",
			setupHook: func(store *fakeACLStore) {
				store.createSettingHook = func(key, _ string) error {
					if key == dbKeyMode {
						return errors.New("forced mode create failure")
					}

					return nil
				}
			},
			wantErr: "failed to initialize ACL mode setting",
		},
		{
			name: "room create failure",
			setupHook: func(store *fakeACLStore) {
				store.createRoomHook = func(string, string) error {
					return errors.New("forced room create failure")
				}
			},
			wantErr: "failed to initialize ACL room",
		},
	}
}

func runACLLoadFromDatabaseInitCreateErrorCase(t *testing.T, tc aclLoadFromDatabaseInitCreateErrorCase) {
	t.Helper()

	store := newFakeACLStore()
	tc.setupHook(store)

	service := &Service{
		store:          store,
		logger:         slog.New(slog.DiscardHandler),
		whitelistRooms: make(map[string]struct{}),
		blacklistRooms: make(map[string]struct{}),
	}

	err := service.loadFromDatabase(t.Context(), true, ACLModeWhitelist, []string{testRoomA})
	if err == nil {
		t.Fatal("expected loadFromDatabase error")
	}

	if !strings.Contains(err.Error(), tc.wantErr) {
		t.Fatalf("expected error to contain %q, got %v", tc.wantErr, err)
	}
}
