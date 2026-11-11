package apiclient

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	ratelimitvalkey "github.com/kapu/hololive-shared/pkg/service/ratelimit/valkey"
)

// 넘겨받은 ctx가 끝났으면 실패해 호출자 취소와 분리된 기록인지 확인할 수 있다.
type memorySharedState struct {
	mu        sync.Mutex
	deadlines map[string]time.Time
	opens     int
	err       error
}

func (s *memorySharedState) Extend(ctx context.Context, key string, until time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.failure(ctx); err != nil {
		return err
	}

	if until.After(s.deadlines[key]) {
		s.deadlines[key] = until
	}

	return nil
}

func (s *memorySharedState) OpenOnce(ctx context.Context, key string, until time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.failure(ctx); err != nil {
		return false, err
	}

	if current, ok := s.deadlines[key]; ok && time.Now().Before(current) {
		return false, nil
	}

	s.deadlines[key] = until
	s.opens++

	return true, nil
}

func (s *memorySharedState) Deadline(ctx context.Context, key string) (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.failure(ctx); err != nil {
		return time.Time{}, err
	}

	return s.deadlines[key], nil
}

func (s *memorySharedState) failure(ctx context.Context) error {
	if s.err != nil {
		return s.err
	}

	if s.deadlines == nil {
		s.deadlines = make(map[string]time.Time)
	}

	return ctx.Err()
}

func newSharedPolicyClient(t *testing.T, shared sharedState, transport policyTransport) *APIClient {
	t.Helper()

	cfg := settings.DefaultHolodexOperationalConfig()

	cfg.MaxRetryAttempts = 0
	cfg.Concurrency.RequestDelay = 0

	client, err := NewHolodexAPIClient(&http.Client{Transport: transport}, "https://holodex.example/api/v2", testAPIKey, slog.Default(), nil, shared, &cfg)
	if err != nil {
		t.Fatalf("NewHolodexAPIClient() = %v", err)
	}

	return client
}

func TestAPIClientSharedCooldownDelaysOtherProcess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		shared := &memorySharedState{}
		start := time.Now()
		limited := newSharedPolicyClient(t, shared, func(*http.Request) (*http.Response, error) {
			return policyResponse(http.StatusTooManyRequests, "5"), nil
		})

		var requestedAt time.Time

		other := newSharedPolicyClient(t, shared, func(*http.Request) (*http.Response, error) {
			requestedAt = time.Now()

			return policyResponse(http.StatusOK, ""), nil
		})

		if _, err := limited.DoRequest(t.Context(), http.MethodGet, "/live", nil); err == nil {
			t.Fatal("429 DoRequest() error = nil")
		}

		if _, err := other.DoRequest(t.Context(), http.MethodGet, "/live", nil); err != nil {
			t.Fatalf("other DoRequest() = %v", err)
		}

		if waited := requestedAt.Sub(start); waited < 5*time.Second {
			t.Fatalf("other process requested after %s, want >= 5s shared cooldown", waited)
		}
	})
}

func TestAPIClientSharedCooldownWriteSurvivesCallerCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		shared := &memorySharedState{}
		client := newSharedPolicyClient(t, shared, func(*http.Request) (*http.Response, error) {
			return policyResponse(http.StatusOK, ""), nil
		})

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		client.rememberRetryAfter(ctx, "5")

		until, err := shared.Deadline(t.Context(), client.sharedCooldownKey())
		if err != nil || time.Until(until) != 5*time.Second {
			t.Fatalf("shared cooldown remaining = %s, want 5s after canceled caller", time.Until(until))
		}
	})
}

func TestAPIClientSharedCircuitOpenRejectsWithoutRequest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		shared := &memorySharedState{}
		calls := 0
		client := newSharedPolicyClient(t, shared, func(*http.Request) (*http.Response, error) {
			calls++

			return policyResponse(http.StatusOK, ""), nil
		})

		if _, err := shared.OpenOnce(t.Context(), client.sharedCircuitKey(), time.Now().Add(circuitResetTimeout)); err != nil {
			t.Fatalf("seed shared circuit: %v", err)
		}

		_, err := client.DoRequest(t.Context(), http.MethodGet, "/live", nil)

		if apiErr, ok := errors.AsType[*APIError](err); !ok || apiErr.StatusCode != http.StatusServiceUnavailable || calls != 0 {
			t.Fatalf("DoRequest() = %v with %d requests, want 503 without request", err, calls)
		}

		time.Sleep(circuitResetTimeout)

		if _, err := client.DoRequest(t.Context(), http.MethodGet, "/live", nil); err != nil || calls != 1 {
			t.Fatalf("DoRequest() after shared circuit expiry = %v with %d requests, want success", err, calls)
		}
	})
}

func TestAPIClientLocalCircuitOpenPublishesSharedOpenOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		shared := &memorySharedState{}
		failing := newSharedPolicyClient(t, shared, func(*http.Request) (*http.Response, error) {
			return policyResponse(http.StatusInternalServerError, ""), nil
		})

		for range circuitFailureThreshold {
			if _, err := failing.DoRequest(t.Context(), http.MethodGet, "/live", nil); err == nil {
				t.Fatal("5xx DoRequest() error = nil")
			}
		}

		if !failing.IsCircuitOpen() || shared.opens != 1 {
			t.Fatalf("local open = %v, shared opens = %d; want open once", failing.IsCircuitOpen(), shared.opens)
		}

		calls := 0
		other := newSharedPolicyClient(t, shared, func(*http.Request) (*http.Response, error) {
			calls++

			return policyResponse(http.StatusOK, ""), nil
		})

		if _, err := other.DoRequest(t.Context(), http.MethodGet, "/live", nil); err == nil || calls != 0 {
			t.Fatalf("other DoRequest() = %v with %d requests, want shared circuit rejection", err, calls)
		}
	})
}

func TestAPIClientSharedStateFailureFallsBackToLocalCooldown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		shared := &memorySharedState{err: errors.New("valkey unavailable")}
		writeFailures := promtestutil.ToFloat64(sharedStateFailures().WithLabelValues(sharedStateCooldownWrite))
		start := time.Now()
		calls := 0

		var retriedAt time.Time

		client := newSharedPolicyClient(t, shared, func(*http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return policyResponse(http.StatusTooManyRequests, "3"), nil
			}

			retriedAt = time.Now()

			return policyResponse(http.StatusOK, ""), nil
		})

		client.maxRetryAttempts = 1

		if _, err := client.DoRequest(t.Context(), http.MethodGet, "/live", nil); err != nil {
			t.Fatalf("DoRequest() during shared state failure = %v, want local cooldown then success", err)
		}

		if waited := retriedAt.Sub(start); waited < 3*time.Second {
			t.Fatalf("retry after %s, want >= 3s local cooldown", waited)
		}

		if got := promtestutil.ToFloat64(sharedStateFailures().WithLabelValues(sharedStateCooldownWrite)); got != writeFailures+1 {
			t.Fatalf("cooldown write failures = %v, want %v", got, writeFailures+1)
		}
	})
}

func TestAPIClientSharedCircuitOpenedDuringRetryStopsRequest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		shared := &memorySharedState{}
		calls := 0

		var client *APIClient

		client = newSharedPolicyClient(t, shared, func(*http.Request) (*http.Response, error) {
			calls++

			// 첫 응답이 오는 사이 다른 프로세스가 circuit을 연 상황이다.
			if _, err := shared.OpenOnce(t.Context(), client.sharedCircuitKey(), time.Now().Add(circuitResetTimeout)); err != nil {
				t.Errorf("seed shared circuit: %v", err)
			}

			return policyResponse(http.StatusInternalServerError, ""), nil
		})

		client.maxRetryAttempts = 2

		_, err := client.DoRequest(t.Context(), http.MethodGet, "/live", nil)

		if apiErr, ok := errors.AsType[*APIError](err); !ok || apiErr.StatusCode != http.StatusServiceUnavailable || calls != 1 {
			t.Fatalf("DoRequest() = %v with %d requests, want 503 after the first request", err, calls)
		}
	})
}

func TestAPIClientMergesSharedCooldownAfterDistributedWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		shared := &memorySharedState{}
		start := time.Now()

		var requestedAt time.Time

		waiting := newSharedPolicyClient(t, shared, func(*http.Request) (*http.Response, error) {
			requestedAt = time.Now()

			return policyResponse(http.StatusOK, ""), nil
		})

		denied := false

		waiting.distributed = policyDistributedLimiter(func(context.Context, string, int, time.Duration) (ratelimitvalkey.Decision, error) {
			if !denied {
				denied = true

				return ratelimitvalkey.Decision{Allowed: false, RetryAfter: time.Second}, nil
			}

			return ratelimitvalkey.Decision{Allowed: true}, nil
		})

		limited := newSharedPolicyClient(t, shared, func(*http.Request) (*http.Response, error) {
			return policyResponse(http.StatusOK, ""), nil
		})

		go func() { time.Sleep(100 * time.Millisecond); limited.rememberRetryAfter(t.Context(), "5") }()

		if _, err := waiting.DoRequest(t.Context(), http.MethodGet, "/live", nil); err != nil {
			t.Fatalf("DoRequest() = %v", err)
		}

		if elapsed := requestedAt.Sub(start); elapsed != 5100*time.Millisecond {
			t.Fatalf("request after %s, want shared cooldown end at 5.1s", elapsed)
		}
	})
}

func TestAPIClientCanceledCallerDoesNotCountSharedStateFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		shared := &memorySharedState{}
		client := newSharedPolicyClient(t, shared, func(*http.Request) (*http.Response, error) {
			t.Error("canceled caller sent an HTTP request")

			return policyResponse(http.StatusOK, ""), nil
		})
		readFailures := promtestutil.ToFloat64(sharedStateFailures().WithLabelValues(sharedStateCircuitRead))

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		if _, err := client.DoRequest(ctx, http.MethodGet, "/live", nil); !errors.Is(err, context.Canceled) {
			t.Fatalf("DoRequest(canceled) = %v, want context.Canceled", err)
		}

		if got := promtestutil.ToFloat64(sharedStateFailures().WithLabelValues(sharedStateCircuitRead)); got != readFailures {
			t.Fatalf("circuit read failures = %v, want unchanged %v", got, readFailures)
		}
	})
}
