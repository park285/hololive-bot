package apiclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
)

type policyTransport func(*http.Request) (*http.Response, error)

func (f policyTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func policyResponse(status int, retryAfter string) *http.Response {
	header := make(http.Header)

	if retryAfter != "" {
		header.Set("Retry-After", retryAfter)
	}

	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader("{}"))}
}

func newPolicyClient(t *testing.T, cfg settings.HolodexConfig, transport policyTransport) *APIClient {
	t.Helper()

	client, err := NewHolodexAPIClient(&http.Client{Transport: transport}, "https://holodex.example/api/v2", testAPIKey, slog.Default(), nil, &cfg)
	if err != nil {
		t.Fatalf("NewHolodexAPIClient() = %v", err)
	}

	return client
}

func TestAPIClientConfiguredRetryCount(t *testing.T) {
	for _, retries := range []int{0, 1, 3, settings.MaxHolodexRetryAttempts} {
		t.Run(fmt.Sprint(retries), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cfg := settings.DefaultHolodexOperationalConfig()

				cfg.MaxRetryAttempts = retries
				cfg.Concurrency.RequestDelay = 0

				calls := 0
				client := newPolicyClient(t, cfg, func(*http.Request) (*http.Response, error) {
					calls++
					return policyResponse(http.StatusTooManyRequests, ""), nil
				})
				_, err := client.DoRequest(t.Context(), http.MethodGet, "/live", nil)

				if _, ok := errors.AsType[*KeyRotationError](err); !ok {
					t.Fatalf("error = %v, want 429 exhaustion", err)
				}

				if calls != retries+1 {
					t.Fatalf("requests = %d, want %d", calls, retries+1)
				}
			})
		})
	}
}

func TestAPIClientRetryAfter(t *testing.T) {
	for _, name := range []string{"seconds", "http date", "missing", "malformed"} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cfg := settings.DefaultHolodexOperationalConfig()

				cfg.MaxRetryAttempts = 1
				cfg.Concurrency.RequestDelay = 0

				start := time.Now()
				header := ""
				minimum := constants.RetryConfig.BaseDelay

				switch name {
				case "seconds":
					header = "3"
					minimum = 3 * time.Second
				case "http date":
					header = start.Add(4 * time.Second).UTC().Format(http.TimeFormat)
					minimum = 4 * time.Second
				case "malformed":
					header = "later"
				}

				calls := 0
				client := newPolicyClient(t, cfg, func(*http.Request) (*http.Response, error) {
					calls++
					if calls == 1 {
						return policyResponse(http.StatusTooManyRequests, header), nil
					}

					if elapsed := time.Since(start); elapsed < minimum {
						t.Errorf("retry after %s, want >= %s", elapsed, minimum)
					}

					return policyResponse(http.StatusOK, ""), nil
				})

				if _, err := client.DoRequest(t.Context(), http.MethodGet, "/live", nil); err != nil {
					t.Fatalf("DoRequest() = %v", err)
				}

				if calls != 2 {
					t.Fatalf("requests = %d, want 2", calls)
				}

				if (name == "missing" || name == "malformed") && time.Since(start) > constants.RetryConfig.BaseDelay+constants.RetryConfig.Jitter {
					t.Fatalf("fallback backoff = %s", time.Since(start))
				}
			})
		})
	}
}

func TestAPIClientCooldownSurvivesCancellationAndOtherGoroutines(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := settings.DefaultHolodexOperationalConfig()

		cfg.MaxRetryAttempts = 1
		cfg.Concurrency.RequestDelay = 0

		var calls atomic.Int32

		start := time.Now()
		client := newPolicyClient(t, cfg, func(*http.Request) (*http.Response, error) {
			if calls.Add(1) == 1 {
				return policyResponse(http.StatusTooManyRequests, "86400"), nil
			}

			if time.Since(start) < 24*time.Hour {
				t.Error("request bypassed retained cooldown")
			}

			return policyResponse(http.StatusOK, ""), nil
		})
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)

		defer cancel()

		if _, err := client.DoRequest(ctx, http.MethodGet, "/live", nil); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("first request error = %v", err)
		}

		if elapsed := time.Since(start); elapsed != time.Second {
			t.Fatalf("cancellation took %s", elapsed)
		}

		result := make(chan error, 1)

		go func() {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()

			_, err := client.DoRequest(ctx, http.MethodGet, "/channels", nil)
			result <- err
		}()

		if err := <-result; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("subsequent request error = %v", err)
		}

		if got := calls.Load(); got != 1 {
			t.Fatalf("requests during cooldown = %d, want 1", got)
		}

		go func() { _, err := client.DoRequest(t.Context(), http.MethodGet, "/videos", nil); result <- err }()

		if err := <-result; err != nil {
			t.Fatalf("request after cooldown = %v", err)
		}

		if calls.Load() != 2 {
			t.Fatalf("requests = %d, want 2", calls.Load())
		}
	})
}

func TestAPIClientCooldownAfterRetryExhaustion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := settings.DefaultHolodexOperationalConfig()

		cfg.MaxRetryAttempts = 0
		cfg.Concurrency.RequestDelay = 0

		calls := 0
		start := time.Now()
		client := newPolicyClient(t, cfg, func(*http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return policyResponse(http.StatusTooManyRequests, "5"), nil
			}

			return policyResponse(http.StatusOK, ""), nil
		})

		if _, err := client.DoRequest(t.Context(), http.MethodGet, "/live", nil); err == nil {
			t.Fatal("expected exhausted 429")
		}

		if time.Since(start) != 0 {
			t.Fatal("exhausted request waited unnecessarily")
		}

		if _, err := client.DoRequest(t.Context(), http.MethodGet, "/channels", nil); err != nil {
			t.Fatal(err)
		}

		if elapsed := time.Since(start); elapsed != 5*time.Second {
			t.Fatalf("cooldown = %s, want 5s", elapsed)
		}
	})
}

func TestAPIClientConfiguredRequestDelay(t *testing.T) {
	for _, delay := range []time.Duration{0, 125 * time.Millisecond, settings.DefaultHolodexOperationalConfig().Concurrency.RequestDelay} {
		t.Run(delay.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cfg := settings.DefaultHolodexOperationalConfig()

				cfg.Concurrency.RequestDelay = delay

				var starts []time.Time

				client := newPolicyClient(t, cfg, func(*http.Request) (*http.Response, error) {
					starts = append(starts, time.Now())
					return policyResponse(http.StatusOK, ""), nil
				})

				for range 3 {
					if _, err := client.DoRequest(t.Context(), http.MethodGet, "/live", nil); err != nil {
						t.Fatal(err)
					}
				}

				for i := 1; i < len(starts); i++ {
					if got := starts[i].Sub(starts[i-1]); got != delay {
						t.Fatalf("request interval = %s, want %s", got, delay)
					}
				}
			})
		})
	}
}

func TestNewHolodexAPIClientRejectsInvalidConcurrency(t *testing.T) {
	for _, concurrency := range []int{0, -1} {
		t.Run(fmt.Sprint(concurrency), func(t *testing.T) {
			cfg := settings.DefaultHolodexOperationalConfig()

			cfg.Concurrency.MaxConcurrentRequests = concurrency

			client, err := NewHolodexAPIClient(nil, "https://holodex.example", testAPIKey, slog.Default(), nil, &cfg)

			if client != nil || err == nil || !strings.Contains(err.Error(), "MaxConcurrentRequests") {
				t.Fatalf("constructor = (%v, %v), want config error", client, err)
			}
		})
	}
}

func TestAPIClientRetainsLongestCooldown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := &APIClient{}
		start := time.Now()

		client.rememberRetryAfter("10")
		client.rememberRetryAfter("1")
		client.rememberRetryAfter("malformed")

		if err := client.waitForCooldown(t.Context()); err != nil {
			t.Fatal(err)
		}

		if elapsed := time.Since(start); elapsed != 10*time.Second {
			t.Fatalf("cooldown = %s, want 10s", elapsed)
		}
	})
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, time.October, 4, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		header string
		delay  time.Duration
		valid  bool
	}{
		{header: "0", valid: true},
		{header: " 12 ", delay: 12 * time.Second, valid: true},
		{header: now.Add(time.Minute).Format(http.TimeFormat), delay: time.Minute, valid: true},
		{header: now.Add(-time.Minute).Format(http.TimeFormat), delay: -time.Minute, valid: true},
		{header: ""},
		{header: "-1"},
		{header: "1.5"},
		{header: "tomorrow"},
		{header: "9999999999999999999999999999"},
	} {
		t.Run(tc.header, func(t *testing.T) {
			until, ok := parseRetryAfter(tc.header, now)
			if ok != tc.valid {
				t.Fatalf("valid = %v, want %v", ok, tc.valid)
			}

			if ok && !until.Equal(now.Add(tc.delay)) {
				t.Fatalf("deadline = %s, want %s", until, now.Add(tc.delay))
			}
		})
	}
}

type failedPolicyBody struct{}

func (failedPolicyBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (failedPolicyBody) Close() error             { return nil }

func TestAPIClientCooldownSurvivesBodyReadFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := settings.DefaultHolodexOperationalConfig()

		cfg.MaxRetryAttempts = 0
		cfg.Concurrency.RequestDelay = 0

		calls := 0
		start := time.Now()
		client := newPolicyClient(t, cfg, func(*http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				response := policyResponse(http.StatusTooManyRequests, "5")

				response.Body = failedPolicyBody{}

				return response, nil
			}

			return policyResponse(http.StatusOK, ""), nil
		})

		if _, err := client.DoRequest(t.Context(), http.MethodGet, "/live", nil); !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("body read error = %v", err)
		}

		if _, err := client.DoRequest(t.Context(), http.MethodGet, "/live", nil); err != nil {
			t.Fatal(err)
		}

		if elapsed := time.Since(start); elapsed != 5*time.Second {
			t.Fatalf("cooldown = %s, want 5s", elapsed)
		}
	})
}

func TestAPIClientSemaphoreWaiterObservesCooldown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := settings.DefaultHolodexOperationalConfig()

		cfg.MaxRetryAttempts = 0
		cfg.Concurrency.MaxConcurrentRequests = 1
		cfg.Concurrency.RequestDelay = 0

		var calls atomic.Int32

		firstStarted := make(chan struct{})
		releaseFirst := make(chan struct{})
		start := time.Now()
		client := newPolicyClient(t, cfg, func(*http.Request) (*http.Response, error) {
			if calls.Add(1) == 1 {
				close(firstStarted)
				<-releaseFirst

				return policyResponse(http.StatusTooManyRequests, "5"), nil
			}

			if elapsed := time.Since(start); elapsed < 5*time.Second {
				t.Errorf("semaphore waiter bypassed cooldown after %s", elapsed)
			}

			return policyResponse(http.StatusOK, ""), nil
		})
		firstResult := make(chan error, 1)

		go func() { _, err := client.DoRequest(t.Context(), http.MethodGet, "/live", nil); firstResult <- err }()

		<-firstStarted

		secondResult := make(chan error, 1)

		go func() { _, err := client.DoRequest(t.Context(), http.MethodGet, "/channels", nil); secondResult <- err }()

		synctest.Wait()
		close(releaseFirst)

		if err := <-firstResult; err == nil {
			t.Fatal("expected exhausted 429")
		}

		if err := <-secondResult; err != nil {
			t.Fatal(err)
		}

		if calls.Load() != 2 {
			t.Fatalf("requests = %d, want 2", calls.Load())
		}
	})
}

func TestAPIClientConfiguredRetriesPreserveFailureLimits(t *testing.T) {
	for _, name := range []string{"503", "network", "timeout"} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cfg := settings.DefaultHolodexOperationalConfig()

				cfg.MaxRetryAttempts = settings.MaxHolodexRetryAttempts
				cfg.Concurrency.RequestDelay = 0

				calls := 0
				client := newPolicyClient(t, cfg, func(req *http.Request) (*http.Response, error) {
					calls++

					switch name {
					case "network":
						return nil, io.ErrUnexpectedEOF
					case "timeout":
						<-req.Context().Done()

						return nil, req.Context().Err()
					default:
						return policyResponse(http.StatusServiceUnavailable, ""), nil
					}
				})

				if _, err := client.DoRequest(t.Context(), http.MethodGet, "/live", nil); err == nil {
					t.Fatal("expected failure")
				}

				if calls != constants.CircuitBreakerConfig.FailureThreshold {
					t.Fatalf("requests = %d, want circuit threshold %d", calls, constants.CircuitBreakerConfig.FailureThreshold)
				}
			})
		})
	}
}

func TestAPIClientCooldownExtensionPreservesRequestSpacing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := settings.DefaultHolodexOperationalConfig()

		cfg.Concurrency.MaxConcurrentRequests = 3
		cfg.Concurrency.RequestDelay = 500 * time.Millisecond

		starts := make(chan time.Time, 4)
		client := newPolicyClient(t, cfg, func(*http.Request) (*http.Response, error) {
			starts <- time.Now()
			return policyResponse(http.StatusOK, ""), nil
		})
		// 첫 승인을 소비한 뒤 여러 호출이 간격을 기다리는 중 429가 도착합니다.
		if _, err := client.DoRequest(t.Context(), http.MethodGet, "/live", nil); err != nil {
			t.Fatal(err)
		}

		<-starts

		results := make(chan error, 3)

		for range 3 {
			go func() { _, err := client.DoRequest(t.Context(), http.MethodGet, "/live", nil); results <- err }()
		}

		synctest.Wait()

		start := time.Now()

		client.rememberRetryAfter("5")
		time.Sleep(time.Second)
		// 이미 cooldown을 기다리는 호출도 연장된 시각을 재확인해야 합니다.
		client.rememberRetryAfter("10")

		for range 3 {
			if err := <-results; err != nil {
				t.Fatal(err)
			}
		}

		previous := <-starts
		if elapsed := previous.Sub(start); elapsed != 11*time.Second {
			t.Fatalf("first request after extension = %s, want 11s", elapsed)
		}

		for range 2 {
			current := <-starts
			if gap := current.Sub(previous); gap != cfg.Concurrency.RequestDelay {
				t.Fatalf("request gap after cooldown = %s, want %s", gap, cfg.Concurrency.RequestDelay)
			}

			previous = current
		}
	})
}

type policyDistributedLimiter func(context.Context, string, int, time.Duration) (ratelimit.Decision, error)

func (f policyDistributedLimiter) Allow(ctx context.Context, bucket string, limit int, window time.Duration) (ratelimit.Decision, error) {
	return f(ctx, bucket, limit, window)
}

func TestAPIClientRefreshesAdmissionAfterCooldownDuringDistributedWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := settings.DefaultHolodexOperationalConfig()

		cfg.Concurrency.RequestDelay = 0

		start := time.Now()
		client := newPolicyClient(t, cfg, func(*http.Request) (*http.Response, error) {
			if elapsed := time.Since(start); elapsed != 5100*time.Millisecond {
				t.Errorf("request after %s, want refreshed cooldown at 5.1s", elapsed)
			}

			return policyResponse(http.StatusOK, ""), nil
		})

		var approvals []time.Time

		client.distributed = policyDistributedLimiter(func(context.Context, string, int, time.Duration) (ratelimit.Decision, error) {
			approvals = append(approvals, time.Now())
			if len(approvals) == 1 {
				return ratelimit.Decision{Allowed: false, RetryAfter: time.Second}, nil
			}

			return ratelimit.Decision{Allowed: true}, nil
		})

		go func() { time.Sleep(100 * time.Millisecond); client.rememberRetryAfter("5") }()

		if _, err := client.DoRequest(t.Context(), http.MethodGet, "/live", nil); err != nil {
			t.Fatal(err)
		}

		if len(approvals) != 3 {
			t.Fatalf("distributed approvals = %d, want 3", len(approvals))
		}

		if elapsed := approvals[2].Sub(start); elapsed != 5100*time.Millisecond {
			t.Fatalf("refreshed distributed approval after %s", elapsed)
		}
	})
}

func TestAPIClientLongDistributedWaitPreservesActualRequestSpacing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := settings.DefaultHolodexOperationalConfig()

		cfg.Concurrency.MaxConcurrentRequests = 3
		cfg.Concurrency.RequestDelay = 500 * time.Millisecond

		start := time.Now()
		openAt := start.Add(10 * time.Second)
		starts := make(chan time.Time, 3)
		client := newPolicyClient(t, cfg, func(*http.Request) (*http.Response, error) {
			starts <- time.Now()
			return policyResponse(http.StatusOK, ""), nil
		})

		client.distributed = policyDistributedLimiter(func(ctx context.Context, _ string, _ int, _ time.Duration) (ratelimit.Decision, error) {
			if delay := time.Until(openAt); delay > 0 {
				select {
				case <-time.After(delay):
				case <-ctx.Done():
					return ratelimit.Decision{}, ctx.Err()
				}
			}

			return ratelimit.Decision{Allowed: true}, nil
		})

		results := make(chan error, 3)

		for range 3 {
			go func() {
				_, err := client.DoRequest(t.Context(), http.MethodGet, "/live", nil)
				results <- err
			}()
		}

		synctest.Wait()
		time.Sleep(100 * time.Millisecond)
		client.rememberRetryAfter("5")

		for range 3 {
			if err := <-results; err != nil {
				t.Fatal(err)
			}
		}

		for i := range 3 {
			got := (<-starts).Sub(start)
			want := 10*time.Second + time.Duration(i)*cfg.Concurrency.RequestDelay

			if got != want {
				t.Fatalf("HTTP request %d at %s, want %s", i, got, want)
			}
		}
	})
}

func TestAPIClientAdmissionPreservesConcurrentHTTPRequests(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := settings.DefaultHolodexOperationalConfig()

		cfg.Concurrency.MaxConcurrentRequests = 2
		cfg.Concurrency.RequestDelay = 500 * time.Millisecond

		starts := make(chan time.Time, 2)
		release := make(chan struct{})
		client := newPolicyClient(t, cfg, func(req *http.Request) (*http.Response, error) {
			starts <- time.Now()

			select {
			case <-release:
				return policyResponse(http.StatusOK, ""), nil
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
		})
		results := make(chan error, 2)

		for range 2 {
			go func() {
				_, err := client.DoRequest(t.Context(), http.MethodGet, "/live", nil)
				results <- err
			}()
		}

		first := <-starts
		second := <-starts

		if gap := second.Sub(first); gap != cfg.Concurrency.RequestDelay {
			t.Fatalf("concurrent HTTP starts separated by %s, want %s", gap, cfg.Concurrency.RequestDelay)
		}

		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)

		defer cancel()

		if _, err := client.DoRequest(ctx, http.MethodGet, "/live", nil); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("third request bypassed concurrency limit: %v", err)
		}

		close(release)

		for range 2 {
			if err := <-results; err != nil {
				t.Fatal(err)
			}
		}
	})
}

func TestAPIClientAdmissionCancellationPreservesCooldown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := settings.DefaultHolodexOperationalConfig()

		cfg.Concurrency.MaxConcurrentRequests = 3

		var calls atomic.Int32

		start := time.Now()
		client := newPolicyClient(t, cfg, func(*http.Request) (*http.Response, error) {
			calls.Add(1)

			if elapsed := time.Since(start); elapsed != 5100*time.Millisecond {
				t.Errorf("HTTP request after %s, want cooldown at 5.1s", elapsed)
			}

			return policyResponse(http.StatusOK, ""), nil
		})
		entered := make(chan struct{})
		approvals := 0

		client.distributed = policyDistributedLimiter(func(ctx context.Context, _ string, _ int, _ time.Duration) (ratelimit.Decision, error) {
			approvals++
			if approvals == 1 {
				close(entered)
				<-ctx.Done()

				return ratelimit.Decision{}, ctx.Err()
			}

			return ratelimit.Decision{Allowed: true}, nil
		})

		firstResult := make(chan error, 1)

		go func() {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()

			_, err := client.DoRequest(ctx, http.MethodGet, "/live", nil)
			firstResult <- err
		}()

		<-entered

		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)

		defer cancel()

		if _, err := client.DoRequest(ctx, http.MethodGet, "/channels", nil); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("admission gate cancellation = %v", err)
		}

		client.rememberRetryAfter("5")

		if err := <-firstResult; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("distributed wait cancellation = %v", err)
		}

		if calls.Load() != 0 {
			t.Fatal("canceled requests reached HTTP transport")
		}

		if _, err := client.DoRequest(t.Context(), http.MethodGet, "/videos", nil); err != nil {
			t.Fatal(err)
		}

		if calls.Load() != 1 || approvals != 2 {
			t.Fatalf("requests = %d, distributed approvals = %d; want 1, 2", calls.Load(), approvals)
		}
	})
}
