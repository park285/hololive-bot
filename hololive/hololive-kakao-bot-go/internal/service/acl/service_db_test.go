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
	"context"
	"fmt"
	"log/slog"
	"sort"
	"testing"
	"time"

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

func newACLServiceWithSQLite(t *testing.T) (*gorm.DB, *cachemocks.Client, *aclCacheCallCounter) {
	t.Helper()

	dsn := fmt.Sprintf("file:acl_%d?mode=memory&cache=shared", time.Now().UnixNano())

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := db.AutoMigrate(&Settings{}, &Room{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	counter := &aclCacheCallCounter{}
	cacheMock := &cachemocks.Client{
		SetFunc: func(_ context.Context, _ string, _ any, _ time.Duration) error {
			counter.setCalls++
			return nil
		},
		DelFunc: func(_ context.Context, _ string) error {
			counter.delCalls++
			return nil
		},
		SAddFunc: func(_ context.Context, _ string, _ []string) (int64, error) {
			counter.saddCalls++
			return 1, nil
		},
		SRemFunc: func(_ context.Context, _ string, _ []string) (int64, error) {
			counter.sremCalls++
			return 1, nil
		},
	}

	return db, cacheMock, counter
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
	logger := slog.New(slog.DiscardHandler)

	dbClient := &dbmocks.Client{GetGormDBFunc: func() *gorm.DB { return db }}

	svc, err := NewACLService(
		t.Context(),
		dbClient,
		cacheMock,
		logger,
		true,
		ACLModeWhitelist,
		[]string{"room-a", "room-b"},
	)
	if err != nil {
		t.Fatalf("NewACLService error: %v", err)
	}

	enabled, mode, rooms := svc.GetACLStatus()
	if !enabled {
		t.Fatal("expected enabled=true")
	}

	if mode != ACLModeWhitelist {
		t.Fatalf("expected mode=whitelist, got %s", mode)
	}

	sort.Strings(rooms)

	if len(rooms) != 2 || rooms[0] != "room-a" || rooms[1] != "room-b" {
		t.Fatalf("unexpected rooms: %v", rooms)
	}

	// DB에 설정이 저장되었는지 확인
	var settings Settings
	if err := db.Where("key = ?", dbKeyEnabled).First(&settings).Error; err != nil {
		t.Fatalf("query settings: %v", err)
	}

	if settings.Value != "true" {
		t.Fatalf("enabled setting value=%q want=true", settings.Value)
	}

	var modeSetting Settings
	if err := db.Where("key = ?", dbKeyMode).First(&modeSetting).Error; err != nil {
		t.Fatalf("query mode setting: %v", err)
	}

	if modeSetting.Value != "whitelist" {
		t.Fatalf("mode setting value=%q want=whitelist", modeSetting.Value)
	}

	var dbRooms []Room
	if err := db.Find(&dbRooms).Error; err != nil {
		t.Fatalf("query rooms: %v", err)
	}

	if len(dbRooms) != 2 {
		t.Fatalf("db rooms len=%d want=2", len(dbRooms))
	}

	// list_type이 whitelist인지 확인
	for _, r := range dbRooms {
		if r.ListType != listTypeWhitelist {
			t.Fatalf("room %q list_type=%q, want=whitelist", r.RoomID, r.ListType)
		}
	}

	if calls.setCalls == 0 || calls.delCalls == 0 {
		t.Fatalf("expected cache sync calls, got %+v", calls)
	}
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
	dbClient := &dbmocks.Client{GetGormDBFunc: func() *gorm.DB { return db }}

	svc, err := NewACLService(
		t.Context(),
		dbClient,
		cacheMock,
		slog.New(slog.DiscardHandler),
		false,
		ACLModeWhitelist,
		nil,
	)
	if err != nil {
		t.Fatalf("NewACLService error: %v", err)
	}

	baselineSet := calls.setCalls
	baselineSAdd := calls.saddCalls
	baselineSRem := calls.sremCalls

	if err := svc.SetEnabled(t.Context(), true); err != nil {
		t.Fatalf("SetEnabled error: %v", err)
	}

	enabled, _, _ := svc.GetACLStatus()
	if !enabled {
		t.Fatal("expected enabled=true after SetEnabled")
	}

	added, err := svc.AddRoom(t.Context(), " room-x ")
	if err != nil || !added {
		t.Fatalf("AddRoom first call expected added=true err=nil, got added=%v err=%v", added, err)
	}

	added, err = svc.AddRoom(t.Context(), "room-x")
	if err != nil || added {
		t.Fatalf("AddRoom duplicate expected added=false err=nil, got added=%v err=%v", added, err)
	}

	added, err = svc.AddRoom(t.Context(), "   ")
	if err != nil || added {
		t.Fatalf("AddRoom blank expected added=false err=nil, got added=%v err=%v", added, err)
	}

	removed, err := svc.RemoveRoom(t.Context(), " room-x ")
	if err != nil || !removed {
		t.Fatalf("RemoveRoom existing expected removed=true err=nil, got removed=%v err=%v", removed, err)
	}

	removed, err = svc.RemoveRoom(t.Context(), "room-x")
	if err != nil || removed {
		t.Fatalf("RemoveRoom missing expected removed=false err=nil, got removed=%v err=%v", removed, err)
	}

	removed, err = svc.RemoveRoom(t.Context(), "   ")
	if err != nil || removed {
		t.Fatalf("RemoveRoom blank expected removed=false err=nil, got removed=%v err=%v", removed, err)
	}

	var settings Settings
	if err := db.Where("key = ?", dbKeyEnabled).First(&settings).Error; err != nil {
		t.Fatalf("query enabled setting: %v", err)
	}

	if settings.Value != "true" {
		t.Fatalf("enabled setting should be true, got %q", settings.Value)
	}

	var count int64
	if err := db.Model(&Room{}).Where("room_id = ?", "room-x").Count(&count).Error; err != nil {
		t.Fatalf("count room-x: %v", err)
	}

	if count != 0 {
		t.Fatalf("room-x should be removed from DB, count=%d", count)
	}

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

func TestACLService_SetMode(t *testing.T) {
	db, cacheMock, _ := newACLServiceWithSQLite(t)
	dbClient := &dbmocks.Client{GetGormDBFunc: func() *gorm.DB { return db }}

	svc, err := NewACLService(
		t.Context(),
		dbClient,
		cacheMock,
		slog.New(slog.DiscardHandler),
		true,
		ACLModeWhitelist,
		[]string{"wl-room"},
	)
	if err != nil {
		t.Fatalf("NewACLService error: %v", err)
	}

	// 화이트리스트 모드에서 방 추가 확인
	_, mode, rooms := svc.GetACLStatus()
	if mode != ACLModeWhitelist {
		t.Fatalf("initial mode should be whitelist, got %s", mode)
	}

	if len(rooms) != 1 || rooms[0] != "wl-room" {
		t.Fatalf("initial whitelist rooms should be [wl-room], got %v", rooms)
	}

	// 블랙리스트 모드로 전환
	if err := svc.SetMode(t.Context(), ACLModeBlacklist); err != nil {
		t.Fatalf("SetMode error: %v", err)
	}

	// 블랙리스트에 방 추가
	added, err := svc.AddRoom(t.Context(), "bl-room")
	if err != nil || !added {
		t.Fatalf("AddRoom to blacklist: added=%v err=%v", added, err)
	}

	// 블랙리스트 모드 상태 확인
	_, mode, rooms = svc.GetACLStatus()
	if mode != ACLModeBlacklist {
		t.Fatalf("mode should be blacklist, got %s", mode)
	}

	if len(rooms) != 1 || rooms[0] != "bl-room" {
		t.Fatalf("blacklist rooms should be [bl-room], got %v", rooms)
	}

	// 화이트리스트 모드로 복귀하면 기존 화이트리스트 목록이 그대로 유지되어야 함
	if err := svc.SetMode(t.Context(), ACLModeWhitelist); err != nil {
		t.Fatalf("SetMode back to whitelist error: %v", err)
	}

	_, mode, rooms = svc.GetACLStatus()
	if mode != ACLModeWhitelist {
		t.Fatalf("mode should be whitelist, got %s", mode)
	}

	sort.Strings(rooms)

	if len(rooms) != 1 || rooms[0] != "wl-room" {
		t.Fatalf("whitelist rooms should still be [wl-room], got %v", rooms)
	}

	// DB에 mode 저장 확인
	var modeSetting Settings
	if err := db.Where("key = ?", dbKeyMode).First(&modeSetting).Error; err != nil {
		t.Fatalf("query mode setting: %v", err)
	}

	if modeSetting.Value != "whitelist" {
		t.Fatalf("mode should be whitelist in DB, got %q", modeSetting.Value)
	}
}

func TestACLService_AddRemoveRoomWithListType(t *testing.T) {
	db, cacheMock, _ := newACLServiceWithSQLite(t)
	dbClient := &dbmocks.Client{GetGormDBFunc: func() *gorm.DB { return db }}

	svc, err := NewACLService(
		t.Context(),
		dbClient,
		cacheMock,
		slog.New(slog.DiscardHandler),
		true,
		ACLModeWhitelist,
		nil,
	)
	if err != nil {
		t.Fatalf("NewACLService error: %v", err)
	}

	// 화이트리스트 모드에서 방 추가
	added, err := svc.AddRoom(t.Context(), "shared-room")
	if err != nil || !added {
		t.Fatalf("AddRoom whitelist: added=%v err=%v", added, err)
	}

	// 블랙리스트 모드로 전환 후 같은 이름의 방 추가 (다른 list_type이므로 가능)
	if err := svc.SetMode(t.Context(), ACLModeBlacklist); err != nil {
		t.Fatalf("SetMode error: %v", err)
	}

	added, err = svc.AddRoom(t.Context(), "shared-room")
	if err != nil || !added {
		t.Fatalf("AddRoom blacklist: added=%v err=%v", added, err)
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
	removed, err := svc.RemoveRoom(t.Context(), "shared-room")
	if err != nil || !removed {
		t.Fatalf("RemoveRoom blacklist: removed=%v err=%v", removed, err)
	}

	if err := svc.SetMode(t.Context(), ACLModeWhitelist); err != nil {
		t.Fatalf("SetMode back error: %v", err)
	}

	_, _, rooms := svc.GetACLStatus()
	if len(rooms) != 1 || rooms[0] != "shared-room" {
		t.Fatalf("whitelist should still have shared-room, got %v", rooms)
	}
}
