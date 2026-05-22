package holodexprovider

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"gorm.io/gorm"
)

type photoSyncTestDBClient struct {
	db *gorm.DB
}

func (c photoSyncTestDBClient) GetPool() *pgxpool.Pool {
	return nil
}

func (c photoSyncTestDBClient) GetGormDB() *gorm.DB {
	return c.db
}

func (c photoSyncTestDBClient) Ping(context.Context) error {
	return nil
}

func (c photoSyncTestDBClient) Close() error {
	return nil
}

func TestPhotoSyncRunPeriodicSyncLogsStoppedWhenContextCanceled(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	ps := &PhotoSyncService{
		logger:       slog.New(slog.NewTextHandler(&logs, nil)),
		syncInterval: time.Hour,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ps.runPeriodicSync(ctx)

	if !strings.Contains(logs.String(), "Photo sync service stopped") {
		t.Fatalf("runPeriodicSync() log = %q, want stop message", logs.String())
	}
}

func TestPhotoSyncRunPeriodicSyncCallsSyncOnPeriodicTick(t *testing.T) {
	t.Parallel()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&member.Model{}); err != nil {
		t.Fatalf("migrate members: %v", err)
	}

	var queryCount atomic.Int32
	var closeReached sync.Once
	reached := make(chan struct{})
	if err := db.Callback().Query().After("gorm:query").Register("photo_sync_periodic_tick", func(tx *gorm.DB) {
		if tx.Statement.Table != "members" {
			return
		}
		if queryCount.Add(1) >= 2 {
			closeReached.Do(func() {
				close(reached)
			})
		}
	}); err != nil {
		t.Fatalf("register query callback: %v", err)
	}

	ps := &PhotoSyncService{
		memberRepository:     member.NewMemberRepository(photoSyncTestDBClient{db: db}, slog.Default()),
		logger:         slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		syncInterval:   10 * time.Millisecond,
		staleThreshold: time.Hour,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		ps.runPeriodicSync(ctx)
		close(done)
	}()

	select {
	case <-reached:
	case <-time.After(500 * time.Millisecond):
		cancel()
		t.Fatalf("photo sync periodic query count = %d, want at least 2", queryCount.Load())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("runPeriodicSync did not stop after context cancellation")
	}
}
