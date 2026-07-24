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
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-dbtest"
	sharedcache "github.com/kapu/hololive-shared/pkg/service/cache"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	dbmocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

type aclCacheCallCounter struct {
	setCalls  int
	delCalls  int
	saddCalls int
	sremCalls int
}

// newACLServiceWithPgx는 격리된 PG pool과 miniredis 기반 cache mock을 준비한다.
// 실제 prod migration이 적용된 PG에서 동작하는 통합 테스트용 픽스처다.
func newACLServiceWithPgx(t *testing.T) (*pgxpool.Pool, *cachemocks.Client, *aclCacheCallCounter) {
	t.Helper()

	pool := dbtest.NewPool(t)

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}

	t.Cleanup(mini.Close)

	port, err := strconv.Atoi(mini.Port())
	if err != nil {
		t.Fatalf("parse miniredis port: %v", err)
	}

	cacheClient, err := sharedcache.NewCacheService(t.Context(), sharedcache.Config{
		Host:              mini.Host(),
		Port:              port,
		DisableCache:      true,
		ForceSingleClient: true,
	}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("new cache service: %v", err)
	}

	t.Cleanup(func() {
		if err := cacheClient.Close(); err != nil {
			t.Errorf("close cache service: %v", err)
		}
	})

	counter := &aclCacheCallCounter{}
	cacheMock := &cachemocks.Client{
		SetFunc: func(ctx context.Context, key string, value any, ttl time.Duration) error {
			counter.setCalls++
			return cacheClient.Set(ctx, key, value, ttl)
		},
		DelFunc: func(ctx context.Context, key string) error {
			counter.delCalls++
			return cacheClient.Del(ctx, key)
		},
		SAddFunc: func(ctx context.Context, key string, members []string) (int64, error) {
			counter.saddCalls++
			return cacheClient.SAdd(ctx, key, members)
		},
		SRemFunc: func(ctx context.Context, key string, members []string) (int64, error) {
			counter.sremCalls++
			return cacheClient.SRem(ctx, key, members)
		},
		SMembersFunc:  cacheClient.SMembers,
		ExistsFunc:    cacheClient.Exists,
		GetClientFunc: cacheClient.GetClient,
		DoMultiFunc:   cacheClient.DoMulti,
		BuilderFunc:   cacheClient.Builder,
		BFunc:         cacheClient.B,
	}

	return pool, cacheMock, counter
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

func testACLRoomsTempKeyPrefix(key string) string {
	if testHasValkeyHashTag(key) {
		return key + aclRoomsTempKeySeparator
	}
	return "{" + key + "}" + aclRoomsTempKeySeparator
}

func testHasValkeyHashTag(key string) bool {
	_, after, ok := strings.Cut(key, "{")
	if !ok {
		return false
	}
	end := strings.IndexByte(after, '}')
	return end > 0
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

func TestNewACLService_FirstInitUsesDefaults(t *testing.T) {
	pool, cacheMock, calls := newACLServiceWithPgx(t)
	service := newACLServiceFromPool(t, pool, cacheMock, true, []string{"room-a", "room-b"})

	assertACLStatus(t, service, true, ACLModeWhitelist, 2, []string{"room-a", "room-b"})
	assertACLSettingValue(t, pool, dbKeyEnabled, "true")
	assertACLSettingValue(t, pool, dbKeyMode, "whitelist")
	assertACLRoomCount(t, pool, "room-a", listTypeWhitelist, 1)
	assertACLRoomCount(t, pool, "room-b", listTypeWhitelist, 1)
	assertACLCacheSyncTriggered(t, calls)
}

func TestNewACLService_FirstInitBlacklistMode(t *testing.T) {
	pool, cacheMock, _ := newACLServiceWithPgx(t)
	logger := slog.New(slog.DiscardHandler)

	dbClient := &dbmocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}

	service, err := NewACLService(
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

	enabled, mode, rooms := service.GetACLStatus()
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
	pool, cacheMock, _ := newACLServiceWithPgx(t)
	// 기존 DB에 데이터가 있으면 기본값 대신 DB 값을 사용해야 한다
	mustCreateACLSetting(t, pool, dbKeyEnabled, "false")
	mustCreateACLSetting(t, pool, dbKeyMode, "blacklist")
	mustCreateACLRoom(t, pool, "existing-room", listTypeBlacklist)

	dbClient := &dbmocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}

	service, err := NewACLService(
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

func TestACLService_SetEnabledAddRemoveRoom(t *testing.T) {
	pool, cacheMock, calls := newACLServiceWithPgx(t)
	service := newACLServiceFromPool(t, pool, cacheMock, false, nil)

	baselineSet := calls.setCalls
	baselineSAdd := calls.saddCalls
	baselineSRem := calls.sremCalls

	assertACLSetEnabled(t, service, pool, true)
	assertACLAddRoomLifecycle(t, service)
	assertACLRoomRemovedFromDB(t, pool, "room-x")
	assertACLCacheCountersAdvanced(t, calls, baselineSet, baselineSAdd, baselineSRem)
}

func newACLServiceFromPool(
	t *testing.T,
	pool *pgxpool.Pool,
	cacheMock *cachemocks.Client,
	defaultEnabled bool,
	defaultRooms []string,
) *Service {
	t.Helper()

	dbClient := &dbmocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}

	service, err := NewACLService(
		t.Context(),
		dbClient,
		cacheMock,
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
func newACLServiceFromFakeStore(t *testing.T, store aclStore, cacheClient *cachemocks.Client, enabled bool) *Service {
	t.Helper()

	return &Service{
		store:          store,
		cache:          cacheClient,
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

	assertAddRoomResult(t, service, " room-x ", true)
	assertAddRoomResult(t, service, "room-x", false)
	assertAddRoomResult(t, service, "   ", false)
	assertRemoveRoomResult(t, service, " room-x ", true)
	assertRemoveRoomResult(t, service, "room-x", false)
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

func assertACLSetMode(t *testing.T, service *Service, mode ACLMode) {
	t.Helper()

	if err := service.SetMode(t.Context(), mode); err != nil {
		t.Fatalf("SetMode(%s) error: %v", mode, err)
	}
}

func TestACLService_SetMode(t *testing.T) {
	pool, cacheMock, _ := newACLServiceWithPgx(t)
	service := newACLServiceFromPool(t, pool, cacheMock, true, []string{"wl-room"})

	assertACLStatus(t, service, true, ACLModeWhitelist, 1, []string{"wl-room"})
	assertACLSetMode(t, service, ACLModeBlacklist)
	assertAddRoomResult(t, service, "bl-room", true)
	assertACLStatus(t, service, true, ACLModeBlacklist, 1, []string{"bl-room"})
	assertACLSetMode(t, service, ACLModeWhitelist)
	assertACLStatus(t, service, true, ACLModeWhitelist, 1, []string{"wl-room"})
	assertACLSettingValue(t, pool, dbKeyMode, "whitelist")
}

func TestACLService_AddRemoveRoomWithListType(t *testing.T) {
	pool, cacheMock, _ := newACLServiceWithPgx(t)
	service := newACLServiceFromPool(t, pool, cacheMock, true, nil)

	// 화이트리스트 모드에서 방 추가
	if added, addErr := service.AddRoom(t.Context(), "shared-room"); addErr != nil || !added {
		t.Fatalf("AddRoom whitelist: added=%v err=%v", added, addErr)
	}

	// 블랙리스트 모드로 전환 후 같은 이름의 방 추가 (다른 list_type이므로 가능)
	assertACLSetMode(t, service, ACLModeBlacklist)

	if added, addErr := service.AddRoom(t.Context(), "shared-room"); addErr != nil || !added {
		t.Fatalf("AddRoom blacklist: added=%v err=%v", added, addErr)
	}

	// DB에 두 개의 레코드가 있어야 함 (같은 room_id, 다른 list_type)
	var roomCount int64
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM acl_rooms WHERE room_id = $1", "shared-room").Scan(&roomCount); err != nil {
		t.Fatalf("query rooms: %v", err)
	}

	if roomCount != 2 {
		t.Fatalf("expected 2 rooms with same ID but different list_type, got %d", roomCount)
	}

	// 블랙리스트에서 제거해도 화이트리스트는 유지
	if removed, removeErr := service.RemoveRoom(t.Context(), "shared-room"); removeErr != nil || !removed {
		t.Fatalf("RemoveRoom blacklist: removed=%v err=%v", removed, removeErr)
	}

	assertACLSetMode(t, service, ACLModeWhitelist)

	_, _, rooms := service.GetACLStatus()
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
				if strings.HasPrefix(key, testACLRoomsTempKeyPrefix(aclBlacklistRoomsKey)) {
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

	// fake store에 DB 상태를 미리 채워 "기존 DB 상태가 defaults를 이긴다"를 재현한다.
	store := newFakeACLStore()
	store.settings[dbKeyEnabled] = "false"
	store.settings[dbKeyMode] = "blacklist"
	store.rooms[roomKey{roomID: "blocked-room", listType: listTypeBlacklist}] = struct{}{}

	cacheMock := newACLRoomSetStatefulCacheForLoad()
	setupCache(cacheMock)

	var logBuf bytes.Buffer

	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	service := &Service{
		store:          store,
		cache:          cacheMock,
		logger:         logger,
		whitelistRooms: make(map[string]struct{}),
		blacklistRooms: make(map[string]struct{}),
	}

	if err := service.loadFromDatabase(t.Context(), true, ACLModeWhitelist, []string{"default-room"}); err != nil {
		t.Fatalf("loadFromDatabase error: %v", err)
	}

	enabled, mode, rooms := service.GetACLStatus()
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

// newACLRoomSetStatefulCacheForLoad는 loadFromDatabase의 Valkey sync 경로(Set/Del/SAdd)를
// 충족하는 최소 cache mock을 만든다. setupCache가 특정 키만 실패시키도록 덮어쓴다.
func newACLRoomSetStatefulCacheForLoad() *cachemocks.Client {
	return &cachemocks.Client{
		SetFunc:  func(context.Context, string, any, time.Duration) error { return nil },
		DelFunc:  func(context.Context, string) error { return nil },
		SAddFunc: func(_ context.Context, _ string, members []string) (int64, error) { return int64(len(members)), nil },
	}
}

func TestACLService_SetEnabledRollsBackStateOnCacheSyncError(t *testing.T) {
	t.Parallel()

	store := newFakeACLStore()
	store.settings[dbKeyEnabled] = "false"

	cacheMock := &cachemocks.Client{
		SetFunc: func(_ context.Context, key string, _ any, _ time.Duration) error {
			if key == aclSettingsKey {
				return errors.New("set failed")
			}

			return nil
		},
	}

	service := newACLServiceFromFakeStore(t, store, cacheMock, false)

	err := service.SetEnabled(t.Context(), true)
	if err == nil {
		t.Fatal("expected cache sync error")
	}

	if !strings.Contains(err.Error(), "sync acl settings to cache") {
		t.Fatalf("expected error to contain sync acl settings to cache, got %v", err)
	}

	enabled, _, _ := service.GetACLStatus()
	if enabled {
		t.Fatal("expected in-memory enabled=false after rollback")
	}

	if v := store.settingValue(dbKeyEnabled); v != "false" {
		t.Fatalf("enabled setting value=%q want=false", v)
	}
}

func TestACLService_SetModeRollsBackStateOnCacheSyncError(t *testing.T) {
	t.Parallel()

	store := newFakeACLStore()
	store.settings[dbKeyMode] = "whitelist"

	cacheMock := &cachemocks.Client{
		SetFunc: func(_ context.Context, key string, _ any, _ time.Duration) error {
			if key == aclModeKey {
				return errors.New("set failed")
			}

			return nil
		},
	}

	service := newACLServiceFromFakeStore(t, store, cacheMock, false)

	err := service.SetMode(t.Context(), ACLModeBlacklist)
	if err == nil {
		t.Fatal("expected cache sync error")
	}

	if !strings.Contains(err.Error(), "sync acl mode to cache") {
		t.Fatalf("expected error to contain sync acl mode to cache, got %v", err)
	}

	_, mode, _ := service.GetACLStatus()
	if mode != ACLModeWhitelist {
		t.Fatalf("expected in-memory mode=whitelist after rollback, got %s", mode)
	}

	if v := store.settingValue(dbKeyMode); v != "whitelist" {
		t.Fatalf("mode setting value=%q want=whitelist", v)
	}
}

func TestACLService_AddRoomRollsBackStateOnCacheSyncError(t *testing.T) {
	t.Parallel()

	store := newFakeACLStore()

	cacheMock := &cachemocks.Client{
		SAddFunc: func(_ context.Context, key string, members []string) (int64, error) {
			if key == aclWhitelistRoomsKey && len(members) == 1 && members[0] == "room-x" {
				return 0, errors.New("sadd failed")
			}

			return 1, nil
		},
	}

	service := newACLServiceFromFakeStore(t, store, cacheMock, false)

	added, err := service.AddRoom(t.Context(), "room-x")
	if added {
		t.Fatal("expected added=false on cache sync error")
	}

	if err == nil {
		t.Fatal("expected cache sync error")
	}

	if !strings.Contains(err.Error(), "sync acl room add to cache") {
		t.Fatalf("expected error to contain sync acl room add to cache, got %v", err)
	}

	_, _, rooms := service.GetACLStatus()
	if len(rooms) != 0 {
		t.Fatalf("expected in-memory room rollback, got %v", rooms)
	}

	count, err := store.CountRooms(t.Context(), "room-x", listTypeWhitelist)
	if err != nil {
		t.Fatalf("count rooms: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected room rollback in store, count=%d", count)
	}
}

func TestACLService_RemoveRoomRollsBackStateOnCacheSyncError(t *testing.T) {
	t.Parallel()

	store := newFakeACLStore()
	store.rooms[roomKey{roomID: "room-x", listType: listTypeWhitelist}] = struct{}{}

	cacheMock := &cachemocks.Client{
		SRemFunc: func(_ context.Context, key string, members []string) (int64, error) {
			if key == aclWhitelistRoomsKey && len(members) == 1 && members[0] == "room-x" {
				return 0, errors.New("srem failed")
			}

			return 1, nil
		},
	}

	service := newACLServiceFromFakeStore(t, store, cacheMock, false)
	service.whitelistRooms["room-x"] = struct{}{}

	removed, err := service.RemoveRoom(t.Context(), "room-x")
	if removed {
		t.Fatal("expected removed=false on cache sync error")
	}

	if err == nil {
		t.Fatal("expected cache sync error")
	}

	if !strings.Contains(err.Error(), "sync acl room removal to cache") {
		t.Fatalf("expected error to contain sync acl room removal to cache, got %v", err)
	}

	_, _, rooms := service.GetACLStatus()
	if len(rooms) != 1 || rooms[0] != "room-x" {
		t.Fatalf("expected in-memory room rollback, got %v", rooms)
	}

	count, err := store.CountRooms(t.Context(), "room-x", listTypeWhitelist)
	if err != nil {
		t.Fatalf("count rooms: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected room restored in store, count=%d", count)
	}
}

func TestACLService_SyncRoomsToValkeyAtomicSuccess(t *testing.T) {
	state := newACLRoomSetCacheState()
	state.setMembers(aclWhitelistRoomsKey, "legacy-room")

	cacheMock := newACLRoomSetStatefulCache(state)

	service := &Service{
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

	if err := service.syncRoomsToValkey(t.Context(), ACLModeWhitelist); err != nil {
		t.Fatalf("syncRoomsToValkey error: %v", err)
	}

	got := state.members(aclWhitelistRoomsKey)
	if len(got) != 2 || got[0] != "room-a" || got[1] != "room-b" {
		t.Fatalf("target set=%v want=[room-a room-b]", got)
	}

	if tempKeys := state.keysWithPrefix(testACLRoomsTempKeyPrefix(aclWhitelistRoomsKey)); len(tempKeys) != 0 {
		t.Fatalf("expected no temp keys after successful swap, got %v", tempKeys)
	}
}

func TestACLService_SyncRoomsToValkeyKeepsExistingRoomsOnTempWriteFailure(t *testing.T) {
	state := newACLRoomSetCacheState()
	state.setMembers(aclWhitelistRoomsKey, "legacy-room")

	cacheMock := newACLRoomSetStatefulCache(state)

	cacheMock.SAddFunc = func(_ context.Context, key string, members []string) (int64, error) {
		if strings.HasPrefix(key, testACLRoomsTempKeyPrefix(aclWhitelistRoomsKey)) {
			return 0, errors.New("sadd failed")
		}

		state.addMembers(key, members...)

		return int64(len(members)), nil
	}

	service := &Service{
		cache: cacheMock,
		whitelistRooms: map[string]struct{}{
			"room-a": {},
		},
		blacklistRooms: make(map[string]struct{}),
	}

	err := service.syncRoomsToValkey(t.Context(), ACLModeWhitelist)
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

	if tempKeys := state.keysWithPrefix(testACLRoomsTempKeyPrefix(aclWhitelistRoomsKey)); len(tempKeys) != 0 {
		t.Fatalf("expected no temp keys after temp write failure, got %v", tempKeys)
	}
}

func TestACLService_SyncRoomsToValkeyKeepsExistingRoomsOnSwapFailure(t *testing.T) {
	state := newACLRoomSetCacheState()
	state.setMembers(aclWhitelistRoomsKey, "legacy-room")

	cacheMock := newACLRoomSetStatefulCache(state)

	service := &Service{
		cache: cacheMock,
		whitelistRooms: map[string]struct{}{
			"room-a": {},
		},
		blacklistRooms: make(map[string]struct{}),
		renameRoomsKeyFunc: func(context.Context, string, string, []string) error {
			return errors.New("rename failed")
		},
	}

	err := service.syncRoomsToValkey(t.Context(), ACLModeWhitelist)
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

	if tempKeys := state.keysWithPrefix(testACLRoomsTempKeyPrefix(aclWhitelistRoomsKey)); len(tempKeys) != 0 {
		t.Fatalf("expected no temp keys after swap failure, got %v", tempKeys)
	}
}

func TestACLService_SetEnabled_DoesNotMutateMemoryOnDBFailure(t *testing.T) {
	t.Parallel()

	store := newFakeACLStore()
	store.settings[dbKeyEnabled] = "false"
	store.upsertHook = func(key, _ string) error {
		if key == dbKeyEnabled {
			return errors.New("forced update failure")
		}

		return nil
	}

	cacheMock := newACLRoomSetStatefulCacheForLoad()
	service := newACLServiceFromFakeStore(t, store, cacheMock, false)

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

	if v := store.settingValue(dbKeyEnabled); v != "false" {
		t.Fatalf("enabled setting value=%q want=false", v)
	}
}

func TestACLService_SetMode_DoesNotMutateMemoryOnDBFailure(t *testing.T) {
	t.Parallel()

	store := newFakeACLStore()
	store.settings[dbKeyMode] = "whitelist"
	store.upsertHook = func(key, _ string) error {
		if key == dbKeyMode {
			return errors.New("forced update failure")
		}

		return nil
	}

	cacheMock := newACLRoomSetStatefulCacheForLoad()
	service := newACLServiceFromFakeStore(t, store, cacheMock, true)

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

	if v := store.settingValue(dbKeyMode); v != "whitelist" {
		t.Fatalf("mode setting value=%q want=whitelist", v)
	}
}

// TestACLService_AddRoom_UsesCapturedModeForValkeySync는 CreateRoom 직후 mode가
// 블랙리스트로 바뀌어도, AddRoom이 캡처한 화이트리스트 mode 키로 Valkey sync함을 검증한다.
// (create-after 시점에서 mode를 바꾸던 동작을 fake store afterCreateRoom으로 재현.)
func TestACLService_AddRoom_UsesCapturedModeForValkeySync(t *testing.T) {
	t.Parallel()

	store := newFakeACLStore()

	var gotKey string

	cacheMock := &cachemocks.Client{
		SAddFunc: func(_ context.Context, key string, members []string) (int64, error) {
			gotKey = key
			return int64(len(members)), nil
		},
	}

	service := newACLServiceFromFakeStore(t, store, cacheMock, true)

	store.afterCreateRoom = func(string, string) {
		service.mu.Lock()
		service.mode = ACLModeBlacklist
		service.mu.Unlock()
	}

	added, err := service.AddRoom(t.Context(), "room-x")
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

// TestACLService_RemoveRoom_UsesCapturedModeForValkeySync는 DeleteRoom 직후 mode가
// 바뀌어도 RemoveRoom이 캡처한 mode 키로 Valkey sync함을 검증한다.
func TestACLService_RemoveRoom_UsesCapturedModeForValkeySync(t *testing.T) {
	t.Parallel()

	store := newFakeACLStore()
	store.rooms[roomKey{roomID: "room-x", listType: listTypeWhitelist}] = struct{}{}

	var gotKey string

	cacheMock := &cachemocks.Client{
		SRemFunc: func(_ context.Context, key string, members []string) (int64, error) {
			gotKey = key
			return int64(len(members)), nil
		},
	}

	service := newACLServiceFromFakeStore(t, store, cacheMock, true)
	service.whitelistRooms["room-x"] = struct{}{}

	store.afterDeleteRoom = func(string, string) {
		service.mu.Lock()
		service.mode = ACLModeBlacklist
		service.mu.Unlock()
	}

	removed, err := service.RemoveRoom(t.Context(), "room-x")
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
	t.Parallel()

	for _, tc := range aclLoadFromDatabaseInitCreateErrorTests() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runACLLoadFromDatabaseInitCreateErrorCase(t, tc)
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
		store: store,
		cache: &cachemocks.Client{
			SetFunc:  func(context.Context, string, any, time.Duration) error { return nil },
			DelFunc:  func(context.Context, string) error { return nil },
			SAddFunc: func(context.Context, string, []string) (int64, error) { return 0, nil },
		},
		logger:         slog.New(slog.DiscardHandler),
		whitelistRooms: make(map[string]struct{}),
		blacklistRooms: make(map[string]struct{}),
	}

	err := service.loadFromDatabase(t.Context(), true, ACLModeWhitelist, []string{"room-a"})
	if err == nil {
		t.Fatal("expected loadFromDatabase error")
	}

	if !strings.Contains(err.Error(), tc.wantErr) {
		t.Fatalf("expected error to contain %q, got %v", tc.wantErr, err)
	}
}
