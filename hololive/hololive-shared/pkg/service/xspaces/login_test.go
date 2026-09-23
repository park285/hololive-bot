package xspaces

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

func newLoginStore(t *testing.T) *Store {
	t.Helper()

	store, err := NewStore(dbtest.NewPool(t), bytes.Repeat([]byte{3}, 32))
	require.NoError(t, err)

	return store
}

func loginCookies() Cookies {
	return Cookies{AuthToken: strings.Repeat("a", 40), CSRFToken: strings.Repeat("b", 64)}
}

func TestLoginClaimConcurrentAndInterrupted(t *testing.T) {
	store := newLoginStore(t)

	var wg sync.WaitGroup

	ids := make(chan int64, 4)
	errs := make(chan error, 4)

	for range 4 {
		wg.Go(func() { id, err := store.ClaimLogin(t.Context(), 1); ids <- id; errs <- err })
	}

	wg.Wait()
	close(ids)
	close(errs)

	claimed := 0

	for id := range ids {
		if id > 0 {
			claimed++
		}
	}

	for err := range errs {
		require.NoError(t, err)
	}

	require.Equal(t, 1, claimed)

	_, err := store.pool.Exec(t.Context(), `UPDATE x_space_login_attempts SET started_at=now()-interval '6 minutes'`)
	require.NoError(t, err)

	status, err := store.LoginStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, "outcome_unknown", status.State)
	require.NoError(t, store.ReconcileLogin(t.Context()))

	id, err := store.ClaimLogin(t.Context(), 1)
	require.NoError(t, err)
	require.Zero(t, id)
}

func TestLoginCandidateValidationAndRecoveryBudget(t *testing.T) {
	store := newLoginStore(t)
	cookies := loginCookies()

	for attempt := range 3 {
		id, err := store.ClaimLogin(t.Context(), 1)
		require.NoError(t, err)
		require.Positive(t, id)
		require.NoError(t, store.FinishLogin(t.Context(), id, &cookies, ""))

		snapshot, err := store.Snapshot(t.Context())
		require.NoError(t, err)
		require.NotNil(t, snapshot.Candidate)

		if attempt == 0 {
			require.Nil(t, snapshot.Active)
		}

		promoted, err := store.ResolveCandidate(t.Context(), snapshot.Revision, "")
		require.NoError(t, err)
		require.True(t, promoted)
		require.NoError(t, store.ReconcileLogin(t.Context()))

		status, err := store.LoginStatus(t.Context())
		require.NoError(t, err)
		require.Equal(t, "connected", status.State)

		_, err = store.Observe(t.Context(), snapshot.Revision, "authentication", time.Now())
		require.NoError(t, err)

		blocked, err := store.ClaimLogin(t.Context(), 1)
		require.NoError(t, err)
		require.Zero(t, blocked)

		_, err = store.pool.Exec(t.Context(), `UPDATE x_space_login_attempts SET started_at=started_at-interval '2 hours'`)
		require.NoError(t, err)
	}

	blocked, err := store.ClaimLogin(t.Context(), 1)
	require.NoError(t, err)
	require.Zero(t, blocked)
}

func TestLoginManualCandidateIsNeverOverwritten(t *testing.T) {
	store := newLoginStore(t)
	id, err := store.ClaimLogin(t.Context(), 1)
	require.NoError(t, err)

	manual := Cookies{AuthToken: strings.Repeat("m", 40), CSRFToken: strings.Repeat("n", 64)}
	accepted, err := store.Submit(t.Context(), manual, "0")
	require.NoError(t, err)
	require.True(t, accepted)

	auto := loginCookies()
	require.NoError(t, store.FinishLogin(t.Context(), id, &auto, ""))

	snapshot, err := store.Snapshot(t.Context())
	require.NoError(t, err)
	require.Equal(t, manual, *snapshot.Candidate)

	status, err := store.LoginStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, "manual_override", status.State)
}

func TestLoginFailureStopsUntilExplicitNewConfiguration(t *testing.T) {
	for _, code := range []string{"additional_authentication", "login_rejected", "browser_failed", "outcome_unknown"} {
		t.Run(code, func(t *testing.T) {
			store := newLoginStore(t)
			id, err := store.ClaimLogin(t.Context(), 1)
			require.NoError(t, err)
			require.NoError(t, store.FinishLogin(t.Context(), id, nil, code))

			_, err = store.pool.Exec(t.Context(), `UPDATE x_space_login_attempts SET started_at=now()-interval '2 hours'`)
			require.NoError(t, err)

			blocked, err := store.ClaimLogin(t.Context(), 1)
			require.NoError(t, err)
			require.Zero(t, blocked)

			next, err := store.ClaimLogin(t.Context(), 2)
			require.NoError(t, err)
			require.Positive(t, next)
		})
	}
}

func TestLoginCandidateRejectionDoesNotDestroyActiveSession(t *testing.T) {
	store := newLoginStore(t)
	cookies := loginCookies()
	accepted, err := store.Submit(t.Context(), cookies, "0")
	require.NoError(t, err)
	require.True(t, accepted)

	_, err = store.ResolveCandidate(t.Context(), 1, "")
	require.NoError(t, err)

	id, err := store.ClaimLogin(t.Context(), 1)
	require.NoError(t, err)
	require.NoError(t, store.FinishLogin(t.Context(), id, &cookies, ""))

	_, err = store.ResolveCandidate(t.Context(), 2, "authentication")
	require.NoError(t, err)
	require.NoError(t, store.ReconcileLogin(t.Context()))

	status, err := store.LoginStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, "login_required", status.State)

	snapshot, err := store.Snapshot(t.Context())
	require.NoError(t, err)
	require.Equal(t, cookies, *snapshot.Active)
}
