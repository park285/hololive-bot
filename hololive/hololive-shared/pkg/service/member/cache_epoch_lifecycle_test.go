package member

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/valkey-io/valkey-go"
)

type countingMemberEpochAuthority struct {
	*fakeMemberEpochAuthority

	reads atomic.Int64
}

func (a *countingMemberEpochAuthority) Current(ctx context.Context) (uint64, error) {
	a.reads.Add(1)

	return a.fakeMemberEpochAuthority.Current(ctx)
}

func TestCacheEpoch_ClientClosingJoinsReconcileWorker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authority := &countingMemberEpochAuthority{fakeMemberEpochAuthority: &fakeMemberEpochAuthority{
			epoch:       1,
			subscribed:  make(chan struct{}, 1),
			messages:    make(chan string),
			disconnects: make(chan error, 1),
		}}
		c := newEpochTestCache(authority)

		c.epochReconcileInterval = time.Second

		ctx, cancel := context.WithCancel(t.Context())

		defer cancel()

		done := make(chan struct{})

		go func() {
			c.runEpochReconciliation(ctx)
			close(done)
		}()

		<-authority.subscribed
		synctest.Wait()

		authority.disconnects <- valkey.ErrClosing

		<-done
		synctest.Wait()

		readsAtStop := authority.reads.Load()

		time.Sleep(3 * time.Second)
		synctest.Wait()

		if reads := authority.reads.Load(); reads != readsAtStop {
			t.Fatalf("epoch reads after subscriber stopped = %d, want 0", reads-readsAtStop)
		}
	})
}

type blockedMemberEpochAuthority struct {
	*fakeMemberEpochAuthority

	started chan context.Context
	stopped chan struct{}
}

func (a *blockedMemberEpochAuthority) Current(ctx context.Context) (uint64, error) {
	a.started <- ctx

	<-ctx.Done()
	close(a.stopped)

	return 0, ctx.Err()
}

func TestCacheCloseCancelsAndJoinsEpochWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		authority := &blockedMemberEpochAuthority{
			fakeMemberEpochAuthority: &fakeMemberEpochAuthority{
				subscribed: make(chan struct{}, 1),
			},
			started: make(chan context.Context, 1),
			stopped: make(chan struct{}),
		}
		c := newEpochTestCache(authority)

		var logs bytes.Buffer

		c.logger = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))

		bootstrapCtx, cancelBootstrap := context.WithCancel(t.Context())

		defer cancelBootstrap()

		c.startEpochReconciliation(bootstrapCtx)

		defer c.Close()

		readCtx := <-authority.started

		cancelBootstrap()
		synctest.Wait()

		if err := readCtx.Err(); err != nil {
			t.Fatalf("runtime inherited bootstrap cancellation: %v", err)
		}

		c.Close()

		select {
		case <-authority.stopped:
		default:
			t.Fatal("Close returned before the active epoch read stopped")
		}

		select {
		case <-c.epochRuntimeDone:
		default:
			t.Fatal("Close returned before the epoch subscription stopped")
		}

		if strings.Contains(logs.String(), `"level":"WARN"`) {
			t.Fatalf("Close logged a warning for runtime cancellation: %s", logs.String())
		}
	})
}

func TestCacheCloseWithoutEpochAuthority(_ *testing.T) {
	var nilCache *Cache

	nilCache.Close()

	c := &Cache{}
	c.Close()
	c.Close()
}
