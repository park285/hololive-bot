package youtubejs

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

func TestRateLimiterSpacesConsecutiveWaits(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		limiter := NewRateLimiter(2 * time.Second)
		start := time.Now()

		for i, want := range []time.Duration{0, 2 * time.Second, 4 * time.Second} {
			if err := limiter.Wait(t.Context()); err != nil {
				t.Fatalf("Wait() #%d error = %v", i, err)
			}

			if got := time.Since(start); got != want {
				t.Fatalf("Wait() #%d returned at %s, want %s", i, got, want)
			}
		}
	})
}

func TestRateLimiterWithoutIntervalDoesNotWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		limiter := NewRateLimiter(0)
		start := time.Now()

		for i := range 3 {
			if err := limiter.Wait(t.Context()); err != nil {
				t.Fatalf("Wait() #%d error = %v", i, err)
			}
		}

		if got := time.Since(start); got != 0 {
			t.Fatalf("zero interval waited %s", got)
		}
	})
}

func TestRateLimiterRejectsCanceledContextWithoutReserving(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for _, interval := range []time.Duration{0, 2 * time.Second} {
			limiter := NewRateLimiter(interval)

			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			if err := limiter.Wait(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("interval %s: Wait(canceled) error = %v, want context.Canceled", interval, err)
			}

			start := time.Now()

			if err := limiter.Wait(t.Context()); err != nil {
				t.Fatalf("interval %s: Wait() error = %v", interval, err)
			}

			if got := time.Since(start); got != 0 {
				t.Fatalf("interval %s: canceled call reserved a slot; next Wait() waited %s", interval, got)
			}
		}
	})
}

// 기다리다 취소된 호출의 예약은 되돌려져 다음 호출이 원래 간격에 시작한다.
func TestRateLimiterCanceledWaitReleasesReservation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		limiter := NewRateLimiter(2 * time.Second)
		start := time.Now()

		if err := limiter.Wait(t.Context()); err != nil {
			t.Fatal(err)
		}

		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()

		if err := limiter.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Wait(timeout) error = %v, want context.DeadlineExceeded", err)
		}

		if err := limiter.Wait(t.Context()); err != nil {
			t.Fatal(err)
		}

		if got := time.Since(start); got != 2*time.Second {
			t.Fatalf("Wait() after canceled reservation returned at %s, want 2s", got)
		}
	})
}

// 취소된 호출 뒤에 이미 다른 예약이 있으면 되돌리지 않는다. 되돌리면 뒤 예약의 간격이 사라진다.
func TestRateLimiterCanceledWaitKeepsLaterReservation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		limiter := NewRateLimiter(2 * time.Second)
		start := time.Now()

		if err := limiter.Wait(t.Context()); err != nil {
			t.Fatal(err)
		}

		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()

		canceled := make(chan error, 1)

		go func() { canceled <- limiter.Wait(ctx) }()

		synctest.Wait()

		later := make(chan error, 1)

		go func() { later <- limiter.Wait(t.Context()) }()

		synctest.Wait()

		if err := <-canceled; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("canceled Wait() error = %v, want context.DeadlineExceeded", err)
		}

		if err := <-later; err != nil {
			t.Fatalf("later Wait() error = %v", err)
		}

		if got := time.Since(start); got != 4*time.Second {
			t.Fatalf("later Wait() returned at %s, want 4s", got)
		}

		if err := limiter.Wait(t.Context()); err != nil {
			t.Fatal(err)
		}

		if got := time.Since(start); got != 6*time.Second {
			t.Fatalf("Wait() after later reservation returned at %s, want 6s", got)
		}
	})
}
