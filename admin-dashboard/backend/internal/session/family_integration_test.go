package session

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSessionFamilyLeaseTracksCreateRotateAndDelete(t *testing.T) {
	store, mr := newTestStore(t)
	ctx := t.Context()
	now := time.Now().UTC()
	old := Session{
		ID:                "family-source",
		FamilyID:          "family-source",
		CreatedAt:         now.Add(-time.Hour),
		ExpiresAt:         now.Add(30 * time.Minute),
		AbsoluteExpiresAt: now.Add(7 * time.Hour),
		LastRotatedAt:     now.Add(-time.Hour),
	}
	seedSession(t, mr, &old)
	require.NoError(t, mr.Set(familyKey(old.FamilyID), old.ID))
	mr.SetTTL(familyKey(old.FamilyID), time.Hour)

	active, err := store.FamilyActive(ctx, old.FamilyID)
	require.NoError(t, err)
	require.True(t, active)

	rotated, ok, err := store.Rotate(ctx, old.ID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, old.FamilyID, rotated.FamilyID)

	familyCurrent, err := mr.Get(familyKey(old.FamilyID))
	require.NoError(t, err)
	require.Equal(t, rotated.ID, familyCurrent)

	// Deleting the grace-period marker must not revoke the authoritative token.
	require.NoError(t, store.Delete(ctx, old.ID))

	active, err = store.FamilyActive(ctx, old.FamilyID)
	require.NoError(t, err)
	require.True(t, active)

	require.NoError(t, store.Delete(ctx, rotated.ID))

	active, err = store.FamilyActive(ctx, old.FamilyID)
	require.NoError(t, err)
	require.False(t, active)
}

type rotateAttempt struct {
	sess Session
	ok   bool
}

func concurrentRotationWinner(t *testing.T, store *Store, old *Session) string {
	t.Helper()

	const callers = 24

	ctx := t.Context()
	results := make(chan rotateAttempt, callers)
	errs := make(chan error, callers)
	start := make(chan struct{})

	var wg sync.WaitGroup

	for range callers {
		wg.Go(func() {
			<-start

			rotated, ok, err := store.Rotate(ctx, old.ID)
			results <- rotateAttempt{sess: rotated, ok: ok}

			errs <- err
		})
	}

	close(start)
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	winnerID := ""

	for attempt := range results {
		require.True(t, attempt.ok)
		require.Equal(t, old.FamilyID, attempt.sess.FamilyID)

		if winnerID == "" {
			winnerID = attempt.sess.ID
		}

		require.Equal(t, winnerID, attempt.sess.ID)
	}

	require.NotEmpty(t, winnerID)

	return winnerID
}

func TestConcurrentRotationConvergesOnSingleWinner(t *testing.T) {
	store, mr := newTestStore(t)
	ctx := t.Context()
	now := time.Now().UTC()
	old := Session{
		ID:                "concurrent-source",
		FamilyID:          "stable-family",
		CreatedAt:         now.Add(-time.Hour),
		ExpiresAt:         now.Add(30 * time.Minute),
		AbsoluteExpiresAt: now.Add(7 * time.Hour),
		LastRotatedAt:     now.Add(-time.Hour),
	}
	seedSession(t, mr, &old)
	require.NoError(t, mr.Set(familyKey(old.FamilyID), old.ID))
	mr.SetTTL(familyKey(old.FamilyID), time.Hour)

	winnerID := concurrentRotationWinner(t, store, &old)

	marker, markerOK, err := store.Get(ctx, old.ID)
	require.NoError(t, err)
	require.True(t, markerOK)
	require.NotNil(t, marker.RotatedTo)
	require.Equal(t, winnerID, *marker.RotatedTo)

	familyCurrent, err := mr.Get(familyKey(old.FamilyID))
	require.NoError(t, err)
	require.Equal(t, winnerID, familyCurrent)

	winner, winnerOK, err := store.Get(ctx, winnerID)
	require.NoError(t, err)
	require.True(t, winnerOK)
	require.Equal(t, old.FamilyID, winner.FamilyID)
	require.Equal(t, old.CreatedAt.Unix(), winner.CreatedAt.Unix())
	require.Equal(t, old.AbsoluteExpiresAt.Unix(), winner.AbsoluteExpiresAt.Unix())
}

func TestLegacySessionGetsStableFamilyOnRefresh(t *testing.T) {
	store, mr := newTestStore(t)
	ctx := t.Context()
	now := time.Now().UTC()
	legacy := Session{
		ID:                "legacy-session",
		CreatedAt:         now.Add(-time.Minute),
		ExpiresAt:         now.Add(20 * time.Minute),
		AbsoluteExpiresAt: now.Add(7 * time.Hour),
		LastRotatedAt:     now,
	}
	seedSession(t, mr, &legacy)

	result, err := store.Refresh(ctx, legacy.ID, false)
	require.NoError(t, err)
	require.Equal(t, RefreshRefreshed, result.Kind)
	require.NotNil(t, result.Session)
	require.Equal(t, legacy.ID, result.Session.FamilyID)

	familyCurrent, err := mr.Get(familyKey(legacy.ID))
	require.NoError(t, err)
	require.Equal(t, legacy.ID, familyCurrent)
}

func TestRevokeFamilySerializesWithRotation(t *testing.T) {
	store, _ := newTestStore(t)

	store.cfg.RotationInterval = 0

	other, err := store.Create(t.Context())
	require.NoError(t, err)

	for range 24 {
		original, createErr := store.Create(t.Context())
		require.NoError(t, createErr)

		start := make(chan struct{})
		rotated := make(chan Session, 1)
		errs := make(chan error, 2)

		var wg sync.WaitGroup

		wg.Go(func() {
			<-start

			next, _, rotateErr := store.Rotate(t.Context(), original.ID)
			rotated <- next

			errs <- rotateErr
		})
		wg.Go(func() {
			<-start

			errs <- store.RevokeFamily(t.Context(), original.FamilyID)
		})
		close(start)
		wg.Wait()
		require.NoError(t, <-errs)
		require.NoError(t, <-errs)

		for _, id := range []string{original.ID, (<-rotated).ID} {
			_, found, getErr := store.Get(t.Context(), id)
			require.NoError(t, getErr)
			require.False(t, found)

			_, ok, rotateErr := store.Rotate(t.Context(), id)
			require.NoError(t, rotateErr)
			require.False(t, ok, "a late rotation cannot recreate a revoked family")
		}

		active, activeErr := store.FamilyActive(t.Context(), original.FamilyID)
		require.NoError(t, activeErr)
		require.False(t, active)
		require.NoError(t, store.RevokeFamily(t.Context(), original.FamilyID))
	}

	_, found, err := store.Get(t.Context(), other.ID)
	require.NoError(t, err)
	require.True(t, found, "revocation must remain family scoped")
}
