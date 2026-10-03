package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/outputguard"
	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/model"
	mnscheduler "github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/scheduler"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

type blockedRuntimeDigest struct{ entered, release chan struct{} }

func (s *blockedRuntimeDigest) ListSubscribedRooms(context.Context) ([]model.SubscribedRoom, error) {
	return []model.SubscribedRoom{{RoomID: "room-a"}}, nil
}

func (s *blockedRuntimeDigest) PrepareDigestRun(context.Context, model.Period, time.Time) (model.DigestGenerator, error) {
	return s, nil
}

func (s *blockedRuntimeDigest) GenerateRoomDigest(context.Context, string, model.Period) (*model.Digest, error) {
	close(s.entered)
	<-s.release

	return nil, model.ErrNoSubscribedMembers
}

type runtimeDigestLocker struct{}

func (runtimeDigestLocker) TryAcquire(context.Context, string, time.Duration) (string, bool, error) {
	return "token", true, nil
}
func (runtimeDigestLocker) Release(context.Context, string, string) error { return nil }

func TestLLMCloseContextTimeoutKeepsActiveSchedulerResourcesOpen(t *testing.T) {
	service := &blockedRuntimeDigest{entered: make(chan struct{}), release: make(chan struct{})}

	var cleanups atomic.Int32

	runtime := &LLMSchedulerRuntime{
		Managed: lifecycle.NewManaged(func() { cleanups.Add(1) }), Logger: testRuntimeLogger(),
		MemberNewsScheduler: mnscheduler.NewScheduler(service, nil, runtimeDigestLocker{}, nil, testRuntimeLogger(), mnscheduler.WithOutputGuard(outputguard.NewGuard())),
	}
	runtime.MemberNewsScheduler.SetClock(func() time.Time { return time.Date(2000, time.January, 3, 0, 0, 0, 0, time.UTC) })
	runtime.Start(t.Context(), make(chan error, 1))

	select {
	case <-service.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not enter digest")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)

	defer cancel()

	if err := runtime.CloseContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close error=%v", err)
	}

	if cleanups.Load() != 0 {
		t.Fatal("closed resources while digest still running")
	}

	runtime.Start(t.Context(), make(chan error, 1))
	close(service.release)

	if err := runtime.CloseContext(t.Context()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("original scheduler drain error was lost: %v", err)
	}

	if err := runtime.CloseContext(t.Context()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("original scheduler drain error was lost: %v", err)
	}

	if cleanups.Load() != 1 {
		t.Fatalf("cleanups=%d", cleanups.Load())
	}
}

func TestLLMCloseContextWaitsForSameCleanupAfterTimeout(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})

	var cleanups atomic.Int32

	runtime := &LLMSchedulerRuntime{Logger: testRuntimeLogger(), Managed: lifecycle.NewManaged(func() {
		cleanups.Add(1)
		close(entered)
		<-release
	})}
	ctx, cancel := context.WithCancel(t.Context())
	firstResult := make(chan error, 1)

	go func() { firstResult <- runtime.CloseContext(ctx) }()

	<-entered
	cancel()

	if err := <-firstResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("close error=%v", err)
	}

	if err := runtime.CloseContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("second close error=%v", err)
	}

	if cleanups.Load() != 1 {
		t.Fatalf("cleanup restarted: %d", cleanups.Load())
	}

	close(release)

	if err := runtime.CloseContext(t.Context()); err != nil {
		t.Fatal(err)
	}

	if cleanups.Load() != 1 {
		t.Fatalf("cleanup count=%d", cleanups.Load())
	}
}

var _ delivery.NotificationLocker = runtimeDigestLocker{}
