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
)

type photoSyncTestMemberRepository struct {
	queryCount atomic.Int32
	reached    chan struct{}
	closeOnce  sync.Once
}

func (r *photoSyncTestMemberRepository) GetAllChannelIDs(context.Context) ([]string, error) {
	return nil, nil
}

func (r *photoSyncTestMemberRepository) GetMembersNeedingPhotoSync(context.Context, time.Duration) ([]string, error) {
	if r.queryCount.Add(1) >= 2 {
		r.closeOnce.Do(func() {
			close(r.reached)
		})
	}
	return nil, nil
}

func (r *photoSyncTestMemberRepository) UpdatePhoto(context.Context, string, string) error {
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

	reached := make(chan struct{})
	repository := &photoSyncTestMemberRepository{reached: reached}

	ps := &PhotoSyncService{
		memberRepository: repository,
		logger:           slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		syncInterval:     10 * time.Millisecond,
		staleThreshold:   time.Hour,
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
		t.Fatalf("photo sync periodic query count = %d, want at least 2", repository.queryCount.Load())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("runPeriodicSync did not stop after context cancellation")
	}
}
