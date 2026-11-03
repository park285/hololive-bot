package handlers

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type calendarFinderFunc func(context.Context, int, int) ([]domain.CalendarEntry, error)

func (f calendarFinderFunc) FindMembersWithCelebrationsInMonth(ctx context.Context, month, year int) ([]domain.CalendarEntry, error) {
	return f(ctx, month, year)
}

// memoryCalendarCache는 synctest bubble 안에서 쓰는 메모리 공유 cache다. 만료는 bubble의 가짜 시간을 따른다.
type memoryCalendarCache struct {
	mu     sync.Mutex
	values map[string]memoryCalendarCacheValue
}

type memoryCalendarCacheValue struct {
	data  []byte
	until time.Time
}

func (c *memoryCalendarCache) Get(_ context.Context, key string) ([]byte, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	value, ok := c.values[key]
	if !ok || !time.Now().Before(value.until) {
		return nil, false, nil
	}

	return bytes.Clone(value.data), true, nil
}

func (c *memoryCalendarCache) Set(_ context.Context, key string, data []byte, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.values == nil {
		c.values = make(map[string]memoryCalendarCacheValue)
	}

	c.values[key] = memoryCalendarCacheValue{data: bytes.Clone(data), until: time.Now().Add(ttl)}

	return nil
}

func TestCachedCalendarFinderFollowerCancellationReturnsBeforeSharedLookup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		finder := NewCachedCelebrationCalendarFinder(calendarFinderFunc(func(context.Context, int, int) ([]domain.CalendarEntry, error) {
			<-release

			return []domain.CalendarEntry{}, nil
		}), &memoryCalendarCache{}, time.Hour)

		if finder == nil {
			t.Fatal("NewCachedCelebrationCalendarFinder() returned nil")
		}

		leaderResult := make(chan error, 1)

		go func() {
			_, err := finder.FindMembersWithCelebrationsInMonth(t.Context(), 10, 2026)
			leaderResult <- err
		}()

		synctest.Wait()

		followerCtx, cancel := context.WithCancel(t.Context())
		defer cancel()

		followerResult := make(chan error, 1)

		go func() {
			_, err := finder.FindMembersWithCelebrationsInMonth(followerCtx, 10, 2026)
			followerResult <- err
		}()

		synctest.Wait()
		cancel()
		synctest.Wait()

		select {
		case err := <-followerResult:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled follower error = %v", err)
			}
		default:
			t.Error("canceled follower is still waiting for shared lookup")
		}

		close(release)

		if err := <-leaderResult; err != nil {
			t.Fatal(err)
		}
	})
}

func TestCachedCalendarFinderLeaderCancellationDoesNotFailLiveFollower(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		finder := NewCachedCelebrationCalendarFinder(calendarFinderFunc(func(ctx context.Context, _, _ int) ([]domain.CalendarEntry, error) {
			<-release

			return []domain.CalendarEntry{}, ctx.Err()
		}), &memoryCalendarCache{}, time.Hour)

		if finder == nil {
			t.Fatal("NewCachedCelebrationCalendarFinder() returned nil")
		}

		leaderCtx, cancel := context.WithCancel(t.Context())

		defer cancel()

		leaderResult := make(chan error, 1)

		go func() { _, err := finder.FindMembersWithCelebrationsInMonth(leaderCtx, 10, 2026); leaderResult <- err }()

		synctest.Wait()

		followerResult := make(chan error, 1)

		go func() {
			_, err := finder.FindMembersWithCelebrationsInMonth(t.Context(), 10, 2026)
			followerResult <- err
		}()

		synctest.Wait()

		cancel()
		close(release)

		if err := <-leaderResult; !errors.Is(err, context.Canceled) {
			t.Fatalf("leader error = %v", err)
		}

		if err := <-followerResult; err != nil {
			t.Fatalf("live follower inherited leader cancellation: %v", err)
		}
	})
}

func TestCachedCalendarFinderAbandonedLookupHasOwnedDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		finished := make(chan error, 1)
		finder := NewCachedCelebrationCalendarFinder(calendarFinderFunc(func(ctx context.Context, _, _ int) ([]domain.CalendarEntry, error) {
			<-ctx.Done()

			finished <- ctx.Err()

			return nil, ctx.Err()
		}), &memoryCalendarCache{}, time.Hour)

		if finder == nil {
			t.Fatal("NewCachedCelebrationCalendarFinder() returned nil")
		}

		ctx, cancel := context.WithCancel(t.Context())
		result := make(chan error, 1)

		go func() { _, err := finder.FindMembersWithCelebrationsInMonth(ctx, 10, 2026); result <- err }()

		synctest.Wait()
		cancel()

		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatalf("caller error = %v", err)
		}

		time.Sleep(constants.RequestTimeout.BotCommand)

		if err := <-finished; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("abandoned shared lookup error = %v", err)
		}
	})
}

func TestCachedCalendarFinderSharedLookupPanicReturnsError(t *testing.T) {
	cause := errors.New("calendar repository panic")
	finder := NewCachedCelebrationCalendarFinder(calendarFinderFunc(func(context.Context, int, int) ([]domain.CalendarEntry, error) {
		panic(cause)
	}), &memoryCalendarCache{}, time.Hour)

	if finder == nil {
		t.Fatal("NewCachedCelebrationCalendarFinder() returned nil")
	}

	_, err := finder.FindMembersWithCelebrationsInMonth(t.Context(), 10, 2026)

	if !errors.Is(err, cause) {
		t.Fatalf("shared lookup panic error = %v, want panic cause", err)
	}
}
