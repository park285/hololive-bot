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
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	sharedcache "github.com/kapu/hololive-shared/pkg/service/cache"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	dbmocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type aclCacheCallCounter struct {
	setCalls  int
	delCalls  int
	saddCalls int
	sremCalls int
}

type aclLoadFromDatabaseInitCreateErrorCase struct {
	name    string
	setupDB func(t *testing.T, db *gorm.DB)
	wantErr string
}

func newACLServiceWithSQLite(t *testing.T) (*gorm.DB, *cachemocks.Client, *aclCacheCallCounter) {
	t.Helper()

	dsn := fmt.Sprintf("file:acl_%d?mode=memory&cache=shared", time.Now().UnixNano())

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	migrateErr := db.AutoMigrate(&Settings{}, &Room{})
	if migrateErr != nil {
		t.Fatalf("auto migrate: %v", migrateErr)
	}

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}

	t.Cleanup(mini.Close)

	port, err := strconv.Atoi(mini.Port())
	if err != nil {
		t.Fatalf("parse miniredis port: %v", err)
	}

	cacheSvc, err := sharedcache.NewCacheService(t.Context(), sharedcache.Config{
		Host:              mini.Host(),
		Port:              port,
		DisableCache:      true,
		ForceSingleClient: true,
	}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("new cache service: %v", err)
	}

	t.Cleanup(func() {
		if err := cacheSvc.Close(); err != nil {
			t.Errorf("close cache service: %v", err)
		}
	})

	counter := &aclCacheCallCounter{}
	cacheMock := &cachemocks.Client{
		SetFunc: func(ctx context.Context, key string, value any, ttl time.Duration) error {
			counter.setCalls++
			return cacheSvc.Set(ctx, key, value, ttl)
		},
		DelFunc: func(ctx context.Context, key string) error {
			counter.delCalls++
			return cacheSvc.Del(ctx, key)
		},
		SAddFunc: func(ctx context.Context, key string, members []string) (int64, error) {
			counter.saddCalls++
			return cacheSvc.SAdd(ctx, key, members)
		},
		SRemFunc: func(ctx context.Context, key string, members []string) (int64, error) {
			counter.sremCalls++
			return cacheSvc.SRem(ctx, key, members)
		},
		SMembersFunc:  cacheSvc.SMembers,
		ExistsFunc:    cacheSvc.Exists,
		GetClientFunc: cacheSvc.GetClient,
		DoMultiFunc:   cacheSvc.DoMulti,
		BuilderFunc:   cacheSvc.Builder,
		BFunc:         cacheSvc.B,
	}

	return db, cacheMock, counter
}

type aclRoomSetCacheState struct {
	mu   sync.Mutex
	sets map[string]map[string]struct{}
}

func newACLRoomSetCacheState() *aclRoomSetCacheState {
	return &aclRoomSetCacheState{
		sets: make(map[string]map[string]struct{}),
	}
}

func (s *aclRoomSetCacheState) setMembers(key string, members ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	set := make(map[string]struct{}, len(members))
	for _, member := range members {
		set[member] = struct{}{}
	}

	s.sets[key] = set
}

func (s *aclRoomSetCacheState) del(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.sets, key)
}

func (s *aclRoomSetCacheState) addMembers(key string, members ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	set := s.sets[key]
	if set == nil {
		set = make(map[string]struct{})
		s.sets[key] = set
	}

	for _, member := range members {
		set[member] = struct{}{}
	}
}

func (s *aclRoomSetCacheState) members(key string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	set := s.sets[key]

	members := make([]string, 0, len(set))
	for member := range set {
		members = append(members, member)
	}

	sort.Strings(members)

	return members
}

func (s *aclRoomSetCacheState) keysWithPrefix(prefix string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	keys := make([]string, 0)

	for key := range s.sets {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}

	sort.Strings(keys)

	return keys
}

func newACLRoomSetStatefulCache(state *aclRoomSetCacheState) *cachemocks.Client {
	return &cachemocks.Client{
		DelFunc: func(_ context.Context, key string) error {
			state.del(key)
			return nil
		},
		SAddFunc: func(_ context.Context, key string, members []string) (int64, error) {
			state.addMembers(key, members...)
			return int64(len(members)), nil
		},
		SMembersFunc: func(_ context.Context, key string) ([]string, error) {
			return state.members(key), nil
		},
	}
}

func TestSettingsAndRoom_TableName(t *testing.T) {
	if got := (Settings{}).TableName(); got != "acl_settings" {
		t.Fatalf("Settings.TableName()=%q", got)
	}

	if got := (Room{}).TableName(); got != "acl_rooms" {
		t.Fatalf("Room.TableName()=%q", got)
	}
}

func TestNewACLService_FirstInitUsesDefaults(t *testing.T) {
	db, cacheMock, calls := newACLServiceWithSQLite(t)
	svc := newACLServiceFromDB(t, db, cacheMock, true, ACLModeWhitelist, []string{"room-a", "room-b"})

	assertACLStatus(t, svc, true, ACLModeWhitelist, 2, []string{"room-a", "room-b"})
	assertACLSettingValue(t, db, dbKeyEnabled, "true")
	assertACLSettingValue(t, db, dbKeyMode, "whitelist")
	assertACLRoomCount(t, db, "room-a", listTypeWhitelist, 1)
	assertACLRoomCount(t, db, "room-b", listTypeWhitelist, 1)
	assertACLCacheSyncTriggered(t, calls)
}

func TestNewACLService_FirstInitBlacklistMode(t *testing.T) {
	db, cacheMock, _ := newACLServiceWithSQLite(t)
	logger := slog.New(slog.DiscardHandler)

	dbClient := &dbmocks.Client{GetGormDBFunc: func() *gorm.DB { return db }}

	svc, err := NewACLService(
		t.Context(),
		dbClient,
		cacheMock,
		logger,
		true,
		ACLModeBlacklist,
		[]string{"blocked-room"},
	)
	if err != nil {
		t.Fatalf("NewACLService error: %v", err)
	}

	enabled, mode, rooms := svc.GetACLStatus()
	if !enabled {
		t.Fatal("expected enabled=true")
	}

	if mode != ACLModeBlacklist {
		t.Fatalf("expected mode=blacklist, got %s", mode)
	}

	if len(rooms) != 1 || rooms[0] != "blocked-room" {
		t.Fatalf("unexpected rooms: %v", rooms)
	}

	// DB에 mode=blacklist로 저장되었는지 확인
	var modeSetting Settings
	if err := db.Where("key = ?", dbKeyMode).First(&modeSetting).Error; err != nil {
		t.Fatalf("query mode setting: %v", err)
	}

	if modeSetting.Value != "blacklist" {
		t.Fatalf("mode setting value=%q want=blacklist", modeSetting.Value)
	}

	// list_type이 blacklist인지 확인
	var dbRooms []Room
	if err := db.Find(&dbRooms).Error; err != nil {
		t.Fatalf("query rooms: %v", err)
	}

	for _, r := range dbRooms {
		if r.ListType != listTypeBlacklist {
			t.Fatalf("room %q list_type=%q, want=blacklist", r.RoomID, r.ListType)
		}
	}
}

func TestNewACLService_ExistingDBStateWins(t *testing.T) {
	db, cacheMock, _ := newACLServiceWithSQLite(t)
	// 기존 DB에 데이터가 있으면 기본값 대신 DB 값을 사용해야 한다
	if err := db.Create(&Settings{Key: dbKeyEnabled, Value: "false"}).Error; err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	if err := db.Create(&Settings{Key: dbKeyMode, Value: "blacklist"}).Error; err != nil {
		t.Fatalf("seed mode: %v", err)
	}

	if err := db.Create(&Room{RoomID: "existing-room", ListType: listTypeBlacklist}).Error; err != nil {
		t.Fatalf("seed room: %v", err)
	}

	dbClient := &dbmocks.Client{GetGormDBFunc: func() *gorm.DB { return db }}

	svc, err := NewACLService(
		t.Context(),
		dbClient,
		cacheMock,
		slog.New(slog.DiscardHandler),
		true,
		ACLModeWhitelist,
		[]string{"default-room"},
	)
	if err != nil {
		t.Fatalf("NewACLService error: %v", err)
	}

	enabled, mode, rooms := svc.GetACLStatus()
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

func TestACLService_SetEnabledAddRemoveRoom(t *testing.T) {
	db, cacheMock, calls := newACLServiceWithSQLite(t)
	svc := newACLServiceFromDB(t, db, cacheMock, false, ACLModeWhitelist, nil)

	baselineSet := calls.setCalls
	baselineSAdd := calls.saddCalls
	baselineSRem := calls.sremCalls

	assertACLSetEnabled(t, svc, db, true)
	assertACLAddRoomLifecycle(t, svc)
	assertACLRoomRemovedFromDB(t, db, "room-x")
	assertACLCacheCountersAdvanced(t, calls, baselineSet, baselineSAdd, baselineSRem)
}

func newACLServiceFromDB(
	t *testing.T,
	db *gorm.DB,
	cacheMock *cachemocks.Client,
	defaultEnabled bool,
	defaultMode ACLMode,
	defaultRooms []string,
) *Service {
	t.Helper()

	dbClient := &dbmocks.Client{GetGormDBFunc: func() *gorm.DB { return db }}

	svc, err := NewACLService(
		t.Context(),
		dbClient,
		cacheMock,
		slog.New(slog.DiscardHandler),
		defaultEnabled,
		defaultMode,
		defaultRooms,
	)
	if err != nil {
		t.Fatalf("NewACLService error: %v", err)
	}

	return svc
}

func assertACLSetEnabled(t *testing.T, svc *Service, db *gorm.DB, enabled bool) {
	t.Helper()

	if err := svc.SetEnabled(t.Context(), enabled); err != nil {
		t.Fatalf("SetEnabled error: %v", err)
	}

	gotEnabled, _, _ := svc.GetACLStatus()
	if gotEnabled != enabled {
		t.Fatalf("enabled=%v want=%v", gotEnabled, enabled)
	}

	var setting Settings
	if err := db.Where("key = ?", dbKeyEnabled).First(&setting).Error; err != nil {
		t.Fatalf("query enabled setting: %v", err)
	}

	wantValue := strconv.FormatBool(enabled)
	if setting.Value != wantValue {
		t.Fatalf("enabled setting should be %q, got %q", wantValue, setting.Value)
	}
}

func assertACLAddRoomLifecycle(t *testing.T, svc *Service) {
	t.Helper()

	assertAddRoomResult(t, svc, " room-x ", true)
	assertAddRoomResult(t, svc, "room-x", false)
	assertAddRoomResult(t, svc, "   ", false)
	assertRemoveRoomResult(t, svc, " room-x ", true)
	assertRemoveRoomResult(t, svc, "room-x", false)
	assertRemoveRoomResult(t, svc, "   ", false)
}

func assertAddRoomResult(t *testing.T, svc *Service, room string, wantAdded bool) {
	t.Helper()

	added, err := svc.AddRoom(t.Context(), room)
	if err != nil {
		t.Fatalf("AddRoom(%q) error: %v", room, err)
	}

	if added != wantAdded {
		t.Fatalf("AddRoom(%q) added=%v want=%v", room, added, wantAdded)
	}
}

func assertRemoveRoomResult(t *testing.T, svc *Service, room string, wantRemoved bool) {
	t.Helper()

	removed, err := svc.RemoveRoom(t.Context(), room)
	if err != nil {
		t.Fatalf("RemoveRoom(%q) error: %v", room, err)
	}

	if removed != wantRemoved {
		t.Fatalf("RemoveRoom(%q) removed=%v want=%v", room, removed, wantRemoved)
	}
}

func assertACLRoomRemovedFromDB(t *testing.T, db *gorm.DB, roomID string) {
	t.Helper()

	var count int64
	if err := db.Model(&Room{}).Where("room_id = ?", roomID).Count(&count).Error; err != nil {
		t.Fatalf("count %s: %v", roomID, err)
	}

	if count != 0 {
		t.Fatalf("%s should be removed from DB, count=%d", roomID, count)
	}
}

func assertACLRoomCount(t *testing.T, db *gorm.DB, roomID, listType string, wantCount int64) {
	t.Helper()

	var count int64
	if err := db.Model(&Room{}).Where("room_id = ? AND list_type = ?", roomID, listType).Count(&count).Error; err != nil {
		t.Fatalf("count %s/%s: %v", roomID, listType, err)
	}

	if count != wantCount {
		t.Fatalf("room %s list_type %s count=%d want=%d", roomID, listType, count, wantCount)
	}
}

func assertACLSettingValue(t *testing.T, db *gorm.DB, key, wantValue string) {
	t.Helper()

	var setting Settings
	if err := db.Where("key = ?", key).First(&setting).Error; err != nil {
		t.Fatalf("query setting %s: %v", key, err)
	}

	if setting.Value != wantValue {
		t.Fatalf("setting %s value=%q want=%q", key, setting.Value, wantValue)
	}
}

func mustCreateACLSetting(t *testing.T, db *gorm.DB, key, value string) {
	t.Helper()

	if err := db.Create(&Settings{Key: key, Value: value}).Error; err != nil {
		t.Fatalf("seed setting %s: %v", key, err)
	}
}

func mustCreateACLRoom(t *testing.T, db *gorm.DB, roomID, listType string) {
	t.Helper()

	if err := db.Create(&Room{RoomID: roomID, ListType: listType}).Error; err != nil {
		t.Fatalf("seed room %s: %v", roomID, err)
	}
}

func assertACLCacheCountersAdvanced(t *testing.T, calls *aclCacheCallCounter, baselineSet, baselineSAdd, baselineSRem int) {
	t.Helper()

	if calls.setCalls <= baselineSet {
		t.Fatalf("expected additional cache Set call after SetEnabled, got %+v", calls)
	}

	if calls.saddCalls <= baselineSAdd {
		t.Fatalf("expected additional cache SAdd call after AddRoom, got %+v", calls)
	}

	if calls.sremCalls <= baselineSRem {
		t.Fatalf("expected additional cache SRem call after RemoveRoom, got %+v", calls)
	}
}

func assertACLCacheSyncTriggered(t *testing.T, calls *aclCacheCallCounter) {
	t.Helper()

	if calls.setCalls == 0 || calls.delCalls == 0 {
		t.Fatalf("expected cache sync calls, got %+v", calls)
	}
}

func assertACLSetMode(t *testing.T, svc *Service, mode ACLMode) {
	t.Helper()

	if err := svc.SetMode(t.Context(), mode); err != nil {
		t.Fatalf("SetMode(%s) error: %v", mode, err)
	}
}

func TestACLService_SetMode(t *testing.T) {
	db, cacheMock, _ := newACLServiceWithSQLite(t)
	svc := newACLServiceFromDB(t, db, cacheMock, true, ACLModeWhitelist, []string{"wl-room"})

	assertACLStatus(t, svc, true, ACLModeWhitelist, 1, []string{"wl-room"})
	assertACLSetMode(t, svc, ACLModeBlacklist)
	assertAddRoomResult(t, svc, "bl-room", true)
	assertACLStatus(t, svc, true, ACLModeBlacklist, 1, []string{"bl-room"})
	assertACLSetMode(t, svc, ACLModeWhitelist)
	assertACLStatus(t, svc, true, ACLModeWhitelist, 1, []string{"wl-room"})
	assertACLSettingValue(t, db, dbKeyMode, "whitelist")
}

func TestACLService_AddRemoveRoomWithListType(t *testing.T) {
	db, cacheMock, _ := newACLServiceWithSQLite(t)
	svc := newACLServiceFromDB(t, db, cacheMock, true, ACLModeWhitelist, nil)

	// 화이트리스트 모드에서 방 추가
	if added, addErr := svc.AddRoom(t.Context(), "shared-room"); addErr != nil || !added {
		t.Fatalf("AddRoom whitelist: added=%v err=%v", added, addErr)
	}

	// 블랙리스트 모드로 전환 후 같은 이름의 방 추가 (다른 list_type이므로 가능)
	assertACLSetMode(t, svc, ACLModeBlacklist)

	if added, addErr := svc.AddRoom(t.Context(), "shared-room"); addErr != nil || !added {
		t.Fatalf("AddRoom blacklist: added=%v err=%v", added, addErr)
	}

	// DB에 두 개의 레코드가 있어야 함 (같은 room_id, 다른 list_type)
	var dbRooms []Room
	if err := db.Where("room_id = ?", "shared-room").Find(&dbRooms).Error; err != nil {
		t.Fatalf("query rooms: %v", err)
	}

	if len(dbRooms) != 2 {
		t.Fatalf("expected 2 rooms with same ID but different list_type, got %d", len(dbRooms))
	}

	// 블랙리스트에서 제거해도 화이트리스트는 유지
	if removed, removeErr := svc.RemoveRoom(t.Context(), "shared-room"); removeErr != nil || !removed {
		t.Fatalf("RemoveRoom blacklist: removed=%v err=%v", removed, removeErr)
	}

	assertACLSetMode(t, svc, ACLModeWhitelist)

	_, _, rooms := svc.GetACLStatus()
	if len(rooms) != 1 || rooms[0] != "shared-room" {
		t.Fatalf("whitelist should still have shared-room, got %v", rooms)
	}
}

func TestNewACLService_LogsSettingsCacheSyncFailureAndKeepsLoadedState(t *testing.T) {
	t.Parallel()

	assertNewACLServiceKeepsLoadedStateOnCacheSyncFailure(t,
		func(cacheMock *cachemocks.Client) {
			cacheMock.SetFunc = func(_ context.Context, key string, _ any, _ time.Duration) error {
				if key == aclSettingsKey {
					return errors.New("set failed")
				}

				return nil
			}
		},
		"Failed to sync ACL settings to cache",
	)
}

func TestNewACLService_LogsModeCacheSyncFailureAndKeepsLoadedState(t *testing.T) {
	t.Parallel()

	assertNewACLServiceKeepsLoadedStateOnCacheSyncFailure(t,
		func(cacheMock *cachemocks.Client) {
			cacheMock.SetFunc = func(_ context.Context, key string, _ any, _ time.Duration) error {
				if key == aclModeKey {
					return errors.New("set failed")
				}

				return nil
			}
		},
		"Failed to sync ACL mode to cache",
	)
}

func TestNewACLService_LogsRoomsCacheSyncFailureAndKeepsLoadedState(t *testing.T) {
	t.Parallel()

	assertNewACLServiceKeepsLoadedStateOnCacheSyncFailure(t,
		func(cacheMock *cachemocks.Client) {
			cacheMock.SAddFunc = func(_ context.Context, key string, _ []string) (int64, error) {
				if strings.HasPrefix(key, aclBlacklistRoomsKey+aclRoomsTempKeySeparator) {
					return 0, errors.New("sadd failed")
				}

				return 1, nil
			}
		},
		"Failed to sync ACL rooms to cache",
	)
}

func assertNewACLServiceKeepsLoadedStateOnCacheSyncFailure(
	t *testing.T,
	setupCache func(*cachemocks.Client),
	wantLog string,
) {
	t.Helper()

	db, cacheMock, _ := newACLServiceWithSQLite(t)
	mustCreateACLSetting(t, db, dbKeyEnabled, "false")
	mustCreateACLSetting(t, db, dbKeyMode, "blacklist")
	mustCreateACLRoom(t, db, "blocked-room", listTypeBlacklist)
	setupCache(cacheMock)

	var logBuf bytes.Buffer

	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	dbClient := &dbmocks.Client{GetGormDBFunc: func() *gorm.DB { return db }}

	svc, err := NewACLService(
		t.Context(),
		dbClient,
		cacheMock,
		logger,
		true,
		ACLModeWhitelist,
		[]string{"default-room"},
	)
	if err != nil {
		t.Fatalf("NewACLService error: %v", err)
	}

	enabled, mode, rooms := svc.GetACLStatus()
	if enabled {
		t.Fatal("expected DB-loaded enabled=false to remain active")
	}

	if mode != ACLModeBlacklist {
		t.Fatalf("expected DB-loaded mode=blacklist, got %s", mode)
	}

	if len(rooms) != 1 || rooms[0] != "blocked-room" {
		t.Fatalf("expected DB-loaded rooms to remain active, got %v", rooms)
	}

	if !strings.Contains(logBuf.String(), wantLog) {
		t.Fatalf("expected log to contain %q, got %q", wantLog, logBuf.String())
	}
}

func TestACLService_SetEnabledRollsBackStateOnCacheSyncError(t *testing.T) {
	t.Parallel()

	db, cacheMock, _ := newACLServiceWithSQLite(t)

	cacheMock.SetFunc = func(_ context.Context, key string, _ any, _ time.Duration) error {
		if key == aclSettingsKey {
			return errors.New("set failed")
		}

		return nil
	}

	svc := newACLServiceFromDB(t, db, cacheMock, false, ACLModeWhitelist, nil)

	err := svc.SetEnabled(t.Context(), true)
	if err == nil {
		t.Fatal("expected cache sync error")
	}

	if !strings.Contains(err.Error(), "sync acl settings to cache") {
		t.Fatalf("expected error to contain sync acl settings to cache, got %v", err)
	}

	enabled, _, _ := svc.GetACLStatus()
	if enabled {
		t.Fatal("expected in-memory enabled=false after rollback")
	}

	assertACLSettingValue(t, db, dbKeyEnabled, "false")
}

func TestACLService_SetModeRollsBackStateOnCacheSyncError(t *testing.T) {
	t.Parallel()

	db, cacheMock, _ := newACLServiceWithSQLite(t)

	cacheMock.SetFunc = func(_ context.Context, key string, _ any, _ time.Duration) error {
		if key == aclModeKey {
			return errors.New("set failed")
		}

		return nil
	}

	svc := newACLServiceFromDB(t, db, cacheMock, false, ACLModeWhitelist, nil)

	err := svc.SetMode(t.Context(), ACLModeBlacklist)
	if err == nil {
		t.Fatal("expected cache sync error")
	}

	if !strings.Contains(err.Error(), "sync acl mode to cache") {
		t.Fatalf("expected error to contain sync acl mode to cache, got %v", err)
	}

	_, mode, _ := svc.GetACLStatus()
	if mode != ACLModeWhitelist {
		t.Fatalf("expected in-memory mode=whitelist after rollback, got %s", mode)
	}

	assertACLSettingValue(t, db, dbKeyMode, "whitelist")
}

func TestACLService_AddRoomRollsBackStateOnCacheSyncError(t *testing.T) {
	t.Parallel()

	db, cacheMock, _ := newACLServiceWithSQLite(t)

	cacheMock.SAddFunc = func(_ context.Context, key string, members []string) (int64, error) {
		if key == aclWhitelistRoomsKey && len(members) == 1 && members[0] == "room-x" {
			return 0, errors.New("sadd failed")
		}

		return 1, nil
	}

	svc := newACLServiceFromDB(t, db, cacheMock, false, ACLModeWhitelist, nil)

	added, err := svc.AddRoom(t.Context(), "room-x")
	if added {
		t.Fatal("expected added=false on cache sync error")
	}

	if err == nil {
		t.Fatal("expected cache sync error")
	}

	if !strings.Contains(err.Error(), "sync acl room add to cache") {
		t.Fatalf("expected error to contain sync acl room add to cache, got %v", err)
	}

	_, _, rooms := svc.GetACLStatus()
	if len(rooms) != 0 {
		t.Fatalf("expected in-memory room rollback, got %v", rooms)
	}

	assertACLRoomCount(t, db, "room-x", listTypeWhitelist, 0)
}

func TestACLService_RemoveRoomRollsBackStateOnCacheSyncError(t *testing.T) {
	t.Parallel()

	db, cacheMock, _ := newACLServiceWithSQLite(t)
	mustCreateACLRoom(t, db, "room-x", listTypeWhitelist)

	cacheMock.SRemFunc = func(_ context.Context, key string, members []string) (int64, error) {
		if key == aclWhitelistRoomsKey && len(members) == 1 && members[0] == "room-x" {
			return 0, errors.New("srem failed")
		}

		return 1, nil
	}

	svc := newACLServiceFromDB(t, db, cacheMock, false, ACLModeWhitelist, nil)

	removed, err := svc.RemoveRoom(t.Context(), "room-x")
	if removed {
		t.Fatal("expected removed=false on cache sync error")
	}

	if err == nil {
		t.Fatal("expected cache sync error")
	}

	if !strings.Contains(err.Error(), "sync acl room removal to cache") {
		t.Fatalf("expected error to contain sync acl room removal to cache, got %v", err)
	}

	_, _, rooms := svc.GetACLStatus()
	if len(rooms) != 1 || rooms[0] != "room-x" {
		t.Fatalf("expected in-memory room rollback, got %v", rooms)
	}

	assertACLRoomCount(t, db, "room-x", listTypeWhitelist, 1)
}

func TestACLService_SyncRoomsToValkeyAtomicSuccess(t *testing.T) {
	state := newACLRoomSetCacheState()
	state.setMembers(aclWhitelistRoomsKey, "legacy-room")

	cacheMock := newACLRoomSetStatefulCache(state)

	svc := &Service{
		cache: cacheMock,
		whitelistRooms: map[string]struct{}{
			"room-a": {},
			"room-b": {},
		},
		blacklistRooms: make(map[string]struct{}),
		renameRoomsKeyFunc: func(_ context.Context, tempKey, key string, _ []string) error {
			state.setMembers(key, state.members(tempKey)...)
			state.del(tempKey)

			return nil
		},
	}

	if err := svc.syncRoomsToValkey(t.Context(), ACLModeWhitelist); err != nil {
		t.Fatalf("syncRoomsToValkey error: %v", err)
	}

	got := state.members(aclWhitelistRoomsKey)
	if len(got) != 2 || got[0] != "room-a" || got[1] != "room-b" {
		t.Fatalf("target set=%v want=[room-a room-b]", got)
	}

	if tempKeys := state.keysWithPrefix(aclWhitelistRoomsKey + aclRoomsTempKeySeparator); len(tempKeys) != 0 {
		t.Fatalf("expected no temp keys after successful swap, got %v", tempKeys)
	}
}

func TestACLService_SyncRoomsToValkeyKeepsExistingRoomsOnTempWriteFailure(t *testing.T) {
	state := newACLRoomSetCacheState()
	state.setMembers(aclWhitelistRoomsKey, "legacy-room")

	cacheMock := newACLRoomSetStatefulCache(state)

	cacheMock.SAddFunc = func(_ context.Context, key string, members []string) (int64, error) {
		if strings.HasPrefix(key, aclWhitelistRoomsKey+aclRoomsTempKeySeparator) {
			return 0, errors.New("sadd failed")
		}

		state.addMembers(key, members...)

		return int64(len(members)), nil
	}

	svc := &Service{
		cache: cacheMock,
		whitelistRooms: map[string]struct{}{
			"room-a": {},
		},
		blacklistRooms: make(map[string]struct{}),
	}

	err := svc.syncRoomsToValkey(t.Context(), ACLModeWhitelist)
	if err == nil {
		t.Fatal("expected temp write failure")
	}

	if !strings.Contains(err.Error(), "populate temp") {
		t.Fatalf("expected temp write error, got %v", err)
	}

	got := state.members(aclWhitelistRoomsKey)
	if len(got) != 1 || got[0] != "legacy-room" {
		t.Fatalf("target set=%v want=[legacy-room]", got)
	}

	if tempKeys := state.keysWithPrefix(aclWhitelistRoomsKey + aclRoomsTempKeySeparator); len(tempKeys) != 0 {
		t.Fatalf("expected no temp keys after temp write failure, got %v", tempKeys)
	}
}

func TestACLService_SyncRoomsToValkeyKeepsExistingRoomsOnSwapFailure(t *testing.T) {
	state := newACLRoomSetCacheState()
	state.setMembers(aclWhitelistRoomsKey, "legacy-room")

	cacheMock := newACLRoomSetStatefulCache(state)

	svc := &Service{
		cache: cacheMock,
		whitelistRooms: map[string]struct{}{
			"room-a": {},
		},
		blacklistRooms: make(map[string]struct{}),
		renameRoomsKeyFunc: func(context.Context, string, string, []string) error {
			return errors.New("rename failed")
		},
	}

	err := svc.syncRoomsToValkey(t.Context(), ACLModeWhitelist)
	if err == nil {
		t.Fatal("expected swap failure")
	}

	if !strings.Contains(err.Error(), "swap") {
		t.Fatalf("expected swap error, got %v", err)
	}

	got := state.members(aclWhitelistRoomsKey)
	if len(got) != 1 || got[0] != "legacy-room" {
		t.Fatalf("target set=%v want=[legacy-room]", got)
	}

	if tempKeys := state.keysWithPrefix(aclWhitelistRoomsKey + aclRoomsTempKeySeparator); len(tempKeys) != 0 {
		t.Fatalf("expected no temp keys after swap failure, got %v", tempKeys)
	}
}

func TestACLService_SetEnabled_DoesNotMutateMemoryOnDBFailure(t *testing.T) {
	db, cacheMock, _ := newACLServiceWithSQLite(t)
	svc := newACLServiceFromDB(t, db, cacheMock, false, ACLModeWhitelist, nil)

	if err := db.Callback().Update().Before("gorm:update").Register("acl_fail_enabled_update", func(tx *gorm.DB) {
		if tx.Statement.Table == (Settings{}).TableName() {
			_ = tx.AddError(errors.New("forced update failure"))
		}
	}); err != nil {
		t.Fatalf("register update callback: %v", err)
	}

	err := svc.SetEnabled(t.Context(), true)
	if err == nil {
		t.Fatal("expected SetEnabled error")
	}

	if !strings.Contains(err.Error(), "failed to save ACL enabled setting") {
		t.Fatalf("unexpected error: %v", err)
	}

	enabled, _, _ := svc.GetACLStatus()
	if enabled {
		t.Fatal("expected in-memory enabled=false after DB failure")
	}

	var setting Settings
	if err := db.Where("key = ?", dbKeyEnabled).First(&setting).Error; err != nil {
		t.Fatalf("query enabled setting: %v", err)
	}

	if setting.Value != "false" {
		t.Fatalf("enabled setting value=%q want=false", setting.Value)
	}
}

func TestACLService_SetMode_DoesNotMutateMemoryOnDBFailure(t *testing.T) {
	db, cacheMock, _ := newACLServiceWithSQLite(t)
	svc := newACLServiceFromDB(t, db, cacheMock, true, ACLModeWhitelist, nil)

	if err := db.Callback().Update().Before("gorm:update").Register("acl_fail_mode_update", func(tx *gorm.DB) {
		if tx.Statement.Table == (Settings{}).TableName() {
			_ = tx.AddError(errors.New("forced update failure"))
		}
	}); err != nil {
		t.Fatalf("register update callback: %v", err)
	}

	err := svc.SetMode(t.Context(), ACLModeBlacklist)
	if err == nil {
		t.Fatal("expected SetMode error")
	}

	if !strings.Contains(err.Error(), "failed to save ACL mode setting") {
		t.Fatalf("unexpected error: %v", err)
	}

	_, mode, _ := svc.GetACLStatus()
	if mode != ACLModeWhitelist {
		t.Fatalf("expected in-memory mode=whitelist after DB failure, got %s", mode)
	}

	var setting Settings
	if err := db.Where("key = ?", dbKeyMode).First(&setting).Error; err != nil {
		t.Fatalf("query mode setting: %v", err)
	}

	if setting.Value != "whitelist" {
		t.Fatalf("mode setting value=%q want=whitelist", setting.Value)
	}
}

func TestACLService_AddRoom_UsesCapturedModeForValkeySync(t *testing.T) {
	db, cacheMock, _ := newACLServiceWithSQLite(t)
	svc := newACLServiceFromDB(t, db, cacheMock, true, ACLModeWhitelist, nil)

	origSAdd := cacheMock.SAddFunc

	var gotKey string

	cacheMock.SAddFunc = func(ctx context.Context, key string, members []string) (int64, error) {
		gotKey = key
		return origSAdd(ctx, key, members)
	}

	if err := db.Callback().Create().After("gorm:create").Register("acl_switch_mode_after_add_room", func(tx *gorm.DB) {
		if tx.Statement.Table != (Room{}).TableName() {
			return
		}

		svc.mu.Lock()
		svc.mode = ACLModeBlacklist
		svc.mu.Unlock()
	}); err != nil {
		t.Fatalf("register create callback: %v", err)
	}

	added, err := svc.AddRoom(t.Context(), "room-x")
	if err != nil {
		t.Fatalf("AddRoom error: %v", err)
	}

	if !added {
		t.Fatal("expected room to be added")
	}

	if gotKey != aclWhitelistRoomsKey {
		t.Fatalf("cache key=%q want=%q", gotKey, aclWhitelistRoomsKey)
	}
}

func TestACLService_RemoveRoom_UsesCapturedModeForValkeySync(t *testing.T) {
	db, cacheMock, _ := newACLServiceWithSQLite(t)
	svc := newACLServiceFromDB(t, db, cacheMock, true, ACLModeWhitelist, []string{"room-x"})

	origSRem := cacheMock.SRemFunc

	var gotKey string

	cacheMock.SRemFunc = func(ctx context.Context, key string, members []string) (int64, error) {
		gotKey = key
		return origSRem(ctx, key, members)
	}

	if err := db.Callback().Delete().After("gorm:delete").Register("acl_switch_mode_after_remove_room", func(tx *gorm.DB) {
		if tx.Statement.Table != (Room{}).TableName() {
			return
		}

		svc.mu.Lock()
		svc.mode = ACLModeBlacklist
		svc.mu.Unlock()
	}); err != nil {
		t.Fatalf("register delete callback: %v", err)
	}

	removed, err := svc.RemoveRoom(t.Context(), "room-x")
	if err != nil {
		t.Fatalf("RemoveRoom error: %v", err)
	}

	if !removed {
		t.Fatal("expected room to be removed")
	}

	if gotKey != aclWhitelistRoomsKey {
		t.Fatalf("cache key=%q want=%q", gotKey, aclWhitelistRoomsKey)
	}
}

func TestACLService_LoadFromDatabase_ReturnsInitCreateError(t *testing.T) {
	for _, tc := range aclLoadFromDatabaseInitCreateErrorTests() {
		t.Run(tc.name, func(t *testing.T) {
			runACLLoadFromDatabaseInitCreateErrorCase(t, tc)
		})
	}
}

func aclLoadFromDatabaseInitCreateErrorTests() []aclLoadFromDatabaseInitCreateErrorCase {
	return []aclLoadFromDatabaseInitCreateErrorCase{
		{
			name:    "enabled setting create failure",
			setupDB: setupACLInitEnabledCreateFailure,
			wantErr: "failed to initialize ACL enabled setting",
		},
		{
			name:    "mode setting create failure",
			setupDB: setupACLInitModeCreateFailure,
			wantErr: "failed to initialize ACL mode setting",
		},
		{
			name:    "room create failure",
			setupDB: setupACLInitRoomCreateFailure,
			wantErr: "failed to initialize ACL room",
		},
	}
}

func setupACLInitEnabledCreateFailure(t *testing.T, db *gorm.DB) {
	t.Helper()

	createCalls := 0

	if err := db.Callback().Create().Before("gorm:create").Register("acl_fail_init_enabled_create", func(tx *gorm.DB) {
		if tx.Statement.Table != (Settings{}).TableName() {
			return
		}

		if createCalls == 0 {
			_ = tx.AddError(errors.New("forced enabled create failure"))
		}

		createCalls++
	}); err != nil {
		t.Fatalf("register create callback: %v", err)
	}
}

func setupACLInitModeCreateFailure(t *testing.T, db *gorm.DB) {
	t.Helper()

	createCalls := 0

	if err := db.Callback().Create().Before("gorm:create").Register("acl_fail_init_mode_create", func(tx *gorm.DB) {
		if tx.Statement.Table != (Settings{}).TableName() {
			return
		}

		if createCalls == 1 {
			_ = tx.AddError(errors.New("forced mode create failure"))
		}

		createCalls++
	}); err != nil {
		t.Fatalf("register create callback: %v", err)
	}
}

func setupACLInitRoomCreateFailure(t *testing.T, db *gorm.DB) {
	t.Helper()

	if err := db.Callback().Create().Before("gorm:create").Register("acl_fail_init_room_create", func(tx *gorm.DB) {
		if tx.Statement.Table == (Room{}).TableName() {
			_ = tx.AddError(errors.New("forced room create failure"))
		}
	}); err != nil {
		t.Fatalf("register create callback: %v", err)
	}
}

func runACLLoadFromDatabaseInitCreateErrorCase(t *testing.T, tc aclLoadFromDatabaseInitCreateErrorCase) {
	t.Helper()

	db, _, _ := newACLServiceWithSQLite(t)
	tc.setupDB(t, db)

	svc := &Service{
		db: db,
		cache: &cachemocks.Client{
			SetFunc:  func(context.Context, string, any, time.Duration) error { return nil },
			DelFunc:  func(context.Context, string) error { return nil },
			SAddFunc: func(context.Context, string, []string) (int64, error) { return 0, nil },
		},
		logger:         slog.New(slog.DiscardHandler),
		whitelistRooms: make(map[string]struct{}),
		blacklistRooms: make(map[string]struct{}),
	}

	err := svc.loadFromDatabase(t.Context(), true, ACLModeWhitelist, []string{"room-a"})
	if err == nil {
		t.Fatal("expected loadFromDatabase error")
	}

	if !strings.Contains(err.Error(), tc.wantErr) {
		t.Fatalf("expected error to contain %q, got %v", tc.wantErr, err)
	}
}
