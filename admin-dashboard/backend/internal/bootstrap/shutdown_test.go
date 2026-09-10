package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"

	"github.com/kapu/admin-dashboard/internal/adapters/holo"
	"github.com/kapu/admin-dashboard/internal/auth"
	"github.com/kapu/admin-dashboard/internal/config"
	"github.com/kapu/admin-dashboard/internal/contract"
	"github.com/kapu/admin-dashboard/internal/httpapi"
	"github.com/kapu/admin-dashboard/internal/observations"
	"github.com/kapu/admin-dashboard/internal/session"
)

// 종료 시험은 store Get에서 막힌 실제 HTTP 요청을 사용합니다. 다른 store 동작은 허용하지 않습니다.
type stoppingStore struct {
	entered  chan struct{}
	finished chan struct{}
	release  <-chan struct{}
}

func (s *stoppingStore) Get(ctx context.Context, _ string) (session.Session, bool, error) {
	close(s.entered)
	defer close(s.finished)

	select {
	case <-ctx.Done():
		return session.Session{}, false, fmt.Errorf("stopping store: %w", ctx.Err())
	case <-s.release:
		return session.Session{}, false, nil
	}
}

func (*stoppingStore) Create(context.Context) (session.Session, error) {
	return session.Session{}, errors.New("unexpected create")
}

func (*stoppingStore) Delete(context.Context, string) error { return errors.New("unexpected delete") }

func (*stoppingStore) FamilyActive(context.Context, string) (bool, error) {
	return false, errors.New("unexpected family lookup")
}

func (*stoppingStore) RevokeFamily(context.Context, string) error {
	return errors.New("unexpected revoke")
}

func (*stoppingStore) Refresh(context.Context, string, bool) (session.RefreshResult, error) {
	return session.RefreshResult{}, errors.New("unexpected refresh")
}

func (*stoppingStore) Rotate(context.Context, string) (session.Session, bool, error) {
	return session.Session{}, false, errors.New("unexpected rotate")
}
func (*stoppingStore) Close() {}

func (*stoppingStore) ClaimMutation(context.Context, session.Session, string) (bool, error) {
	return false, errors.New("unexpected mutation")
}

func stoppingRuntime(t *testing.T, store *stoppingStore) *Runtime {
	t.Helper()

	logger := slog.New(slog.DiscardHandler)
	limiter, err := session.NewLoginLimiter(t.Context(), miniredis.RunT(t).Addr())
	require.NoError(t, err)

	client, err := holo.NewClient("http://127.0.0.1:1", "synthetic")
	require.NoError(t, err)

	r := &Runtime{logger: logger, limiter: limiter, holo: client, sampler: observations.NewSampler(nil)}

	r.hub = observations.NewHubWithSampler(r.sampler)

	cfg := &config.Config{SessionSecret: strings.Repeat("fixture", 8), Session: config.DefaultSessionConfig(), Security: config.SecurityConfig{AllowedOrigins: []string{"https://test.invalid"}, WSOriginMode: config.SecurityEnforce}}

	r.api, err = httpapi.New(cfg, logger, httpapi.Dependencies{Sessions: store, LoginLimiter: limiter, Holo: client, Stats: r.hub, Status: observations.NewCollectorWithSampler(r.sampler, "test")})
	require.NoError(t, err)
	startStats(t.Context(), r.hub)
	t.Cleanup(r.Close)

	return r
}

func TestShutdownDrainsOrForceClosesWithinBudget(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(fmt.Sprint("force=", force), func(t *testing.T) {
			release := make(chan struct{})
			store := &stoppingStore{entered: make(chan struct{}), finished: make(chan struct{}), release: release}
			r := stoppingRuntime(t, store)
			server := httptest.NewServer(r.api.Handler())
			t.Cleanup(server.Close)

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/admin/api/auth/session", http.NoBody)
			require.NoError(t, err)
			req.Header.Set("X-Admin-Client-Generation", contract.Generation)
			req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: auth.SignSessionID("fixture", strings.Repeat("fixture", 8)), Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})

			done := make(chan error, 1)

			go func() {
				resp, callErr := server.Client().Do(req)
				if callErr == nil {
					callErr = resp.Body.Close()
				}

				done <- callErr
			}()

			select {
			case <-store.entered:
			case <-time.After(time.Second):
				t.Fatal("request did not enter store")
			}

			if !force {
				close(release)
			}

			ctx, cancel := context.WithTimeout(t.Context(), time.Second)

			defer cancel()

			err = r.shutdown(ctx, server.Config, 30*time.Millisecond)

			if force {
				require.ErrorIs(t, err, context.DeadlineExceeded)
			} else {
				require.NoError(t, err)
			}

			select {
			case <-store.finished:
			default:
				t.Fatal("shutdown left the HTTP handler using store")
			}

			require.Empty(t, r.api.BeginDrain())

			if force {
				require.Error(t, <-done)
			} else {
				require.NoError(t, <-done)
			}
		})
	}
}
