package handlers

import (
	"context"
	"errors"
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

func TestCachedCalendarFinderFollowerCancellationReturnsBeforeSharedLookup(t *testing.T) {
	dir := t.TempDir()

	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		finder := newCachedCelebrationCalendarFinder(calendarFinderFunc(func(context.Context, int, int) ([]domain.CalendarEntry, error) {
			<-release

			return []domain.CalendarEntry{}, nil
		}), dir, time.Hour, time.Now)

		if finder == nil {
			t.Fatal("newCachedCelebrationCalendarFinder() returned nil")
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
	dir := t.TempDir()

	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		finder := newCachedCelebrationCalendarFinder(calendarFinderFunc(func(ctx context.Context, _, _ int) ([]domain.CalendarEntry, error) {
			<-release

			return []domain.CalendarEntry{}, ctx.Err()
		}), dir, time.Hour, time.Now)

		if finder == nil {
			t.Fatal("newCachedCelebrationCalendarFinder() returned nil")
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
	dir := t.TempDir()

	synctest.Test(t, func(t *testing.T) {
		finished := make(chan error, 1)
		finder := newCachedCelebrationCalendarFinder(calendarFinderFunc(func(ctx context.Context, _, _ int) ([]domain.CalendarEntry, error) {
			<-ctx.Done()

			finished <- ctx.Err()

			return nil, ctx.Err()
		}), dir, time.Hour, time.Now)

		if finder == nil {
			t.Fatal("newCachedCelebrationCalendarFinder() returned nil")
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
	finder := newCachedCelebrationCalendarFinder(calendarFinderFunc(func(context.Context, int, int) ([]domain.CalendarEntry, error) {
		panic(cause)
	}), t.TempDir(), time.Hour, time.Now)

	if finder == nil {
		t.Fatal("newCachedCelebrationCalendarFinder() returned nil")
	}

	_, err := finder.FindMembersWithCelebrationsInMonth(t.Context(), 10, 2026)

	if !errors.Is(err, cause) {
		t.Fatalf("shared lookup panic error = %v, want panic cause", err)
	}
}
