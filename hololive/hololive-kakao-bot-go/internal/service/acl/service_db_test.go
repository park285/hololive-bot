package acl

import (
	"context"
	"fmt"
	"io"
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
		SetFunc: func(_ context.Context, key string, _ any, _ time.Duration) error {
			if key != aclSettingsKey {
				t.Fatalf("unexpected Set key: %s", key)
			}
			counter.setCalls++
			return nil
		},
		DelFunc: func(_ context.Context, key string) error {
			if key != aclRoomsKey {
				t.Fatalf("unexpected Del key: %s", key)
			}
			counter.delCalls++
			return nil
		},
		SAddFunc: func(_ context.Context, key string, _ []string) (int64, error) {
			if key != aclRoomsKey {
				t.Fatalf("unexpected SAdd key: %s", key)
			}
			counter.saddCalls++
			return 1, nil
		},
		SRemFunc: func(_ context.Context, key string, _ []string) (int64, error) {
			if key != aclRoomsKey {
				t.Fatalf("unexpected SRem key: %s", key)
			}
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
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	dbClient := &dbmocks.Client{GetGormDBFunc: func() *gorm.DB { return db }}
	svc, err := NewACLService(
		context.Background(),
		dbClient,
		cacheMock,
		logger,
		true,
		[]string{"room-a", "room-b"},
	)
	if err != nil {
		t.Fatalf("NewACLService error: %v", err)
	}

	enabled, rooms := svc.GetACLStatus()
	if !enabled {
		t.Fatalf("expected enabled=true")
	}
	sort.Strings(rooms)
	if len(rooms) != 2 || rooms[0] != "room-a" || rooms[1] != "room-b" {
		t.Fatalf("unexpected rooms: %v", rooms)
	}

	var settings Settings
	if err := db.Where("key = ?", "enabled").First(&settings).Error; err != nil {
		t.Fatalf("query settings: %v", err)
	}
	if settings.Value != "true" {
		t.Fatalf("enabled setting value=%q want=true", settings.Value)
	}

	var dbRooms []Room
	if err := db.Find(&dbRooms).Error; err != nil {
		t.Fatalf("query rooms: %v", err)
	}
	if len(dbRooms) != 2 {
		t.Fatalf("db rooms len=%d want=2", len(dbRooms))
	}

	if calls.setCalls == 0 || calls.delCalls == 0 || calls.saddCalls == 0 {
		t.Fatalf("expected cache sync calls, got %+v", calls)
	}
}

func TestNewACLService_ExistingDBStateWins(t *testing.T) {
	db, cacheMock, _ := newACLServiceWithSQLite(t)
	if err := db.Create(&Settings{Key: "enabled", Value: "false"}).Error; err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	if err := db.Create(&Room{RoomID: "existing-room"}).Error; err != nil {
		t.Fatalf("seed room: %v", err)
	}

	dbClient := &dbmocks.Client{GetGormDBFunc: func() *gorm.DB { return db }}
	svc, err := NewACLService(
		context.Background(),
		dbClient,
		cacheMock,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		true,
		[]string{"default-room"},
	)
	if err != nil {
		t.Fatalf("NewACLService error: %v", err)
	}

	enabled, rooms := svc.GetACLStatus()
	if enabled {
		t.Fatalf("expected enabled=false from DB")
	}
	if len(rooms) != 1 || rooms[0] != "existing-room" {
		t.Fatalf("expected only existing room from DB, got %v", rooms)
	}
}

func TestACLService_SetEnabledAddRemoveRoom(t *testing.T) {
	db, cacheMock, calls := newACLServiceWithSQLite(t)
	dbClient := &dbmocks.Client{GetGormDBFunc: func() *gorm.DB { return db }}
	svc, err := NewACLService(
		context.Background(),
		dbClient,
		cacheMock,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		false,
		nil,
	)
	if err != nil {
		t.Fatalf("NewACLService error: %v", err)
	}

	baselineSet := calls.setCalls
	baselineSAdd := calls.saddCalls
	baselineSRem := calls.sremCalls

	if err := svc.SetEnabled(context.Background(), true); err != nil {
		t.Fatalf("SetEnabled error: %v", err)
	}
	enabled, _ := svc.GetACLStatus()
	if !enabled {
		t.Fatalf("expected enabled=true after SetEnabled")
	}

	added, err := svc.AddRoom(context.Background(), " room-x ")
	if err != nil || !added {
		t.Fatalf("AddRoom first call expected added=true err=nil, got added=%v err=%v", added, err)
	}
	added, err = svc.AddRoom(context.Background(), "room-x")
	if err != nil || added {
		t.Fatalf("AddRoom duplicate expected added=false err=nil, got added=%v err=%v", added, err)
	}
	added, err = svc.AddRoom(context.Background(), "   ")
	if err != nil || added {
		t.Fatalf("AddRoom blank expected added=false err=nil, got added=%v err=%v", added, err)
	}

	removed, err := svc.RemoveRoom(context.Background(), " room-x ")
	if err != nil || !removed {
		t.Fatalf("RemoveRoom existing expected removed=true err=nil, got removed=%v err=%v", removed, err)
	}
	removed, err = svc.RemoveRoom(context.Background(), "room-x")
	if err != nil || removed {
		t.Fatalf("RemoveRoom missing expected removed=false err=nil, got removed=%v err=%v", removed, err)
	}
	removed, err = svc.RemoveRoom(context.Background(), "   ")
	if err != nil || removed {
		t.Fatalf("RemoveRoom blank expected removed=false err=nil, got removed=%v err=%v", removed, err)
	}

	var settings Settings
	if err := db.Where("key = ?", "enabled").First(&settings).Error; err != nil {
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
