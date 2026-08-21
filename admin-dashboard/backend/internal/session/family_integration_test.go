package session

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSessionFamilyLeaseTracksCreateRotateAndDelete(t *testing.T) {
	store, mr := newTestStore(t)
	ctx := context.Background()
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

	rotated, err := store.Rotate(ctx, old.ID)
	require.NoError(t, err)
	require.NotNil(t, rotated)
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

func TestConcurrentRotationConvergesOnSingleWinner(t *testing.T) {
	store, mr := newTestStore(t)
	ctx := context.Background()
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

	const callers = 24
	results := make(chan *Session, callers)
	errs := make(chan error, callers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range callers {
		wg.Go(func() {
			<-start
			rotated, err := store.Rotate(ctx, old.ID)
			results <- rotated
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
	for rotated := range results {
		require.NotNil(t, rotated)
		require.Equal(t, old.FamilyID, rotated.FamilyID)
		if winnerID == "" {
			winnerID = rotated.ID
		}
		require.Equal(t, winnerID, rotated.ID)
	}
	require.NotEmpty(t, winnerID)

	marker, err := store.Get(ctx, old.ID)
	require.NoError(t, err)
	require.NotNil(t, marker)
	require.NotNil(t, marker.RotatedTo)
	require.Equal(t, winnerID, *marker.RotatedTo)
	familyCurrent, err := mr.Get(familyKey(old.FamilyID))
	require.NoError(t, err)
	require.Equal(t, winnerID, familyCurrent)

	winner, err := store.Get(ctx, winnerID)
	require.NoError(t, err)
	require.NotNil(t, winner)
	require.Equal(t, old.FamilyID, winner.FamilyID)
	require.Equal(t, old.CreatedAt.Unix(), winner.CreatedAt.Unix())
	require.Equal(t, old.AbsoluteExpiresAt.Unix(), winner.AbsoluteExpiresAt.Unix())
}

func TestLegacySessionGetsStableFamilyOnRefresh(t *testing.T) {
	store, mr := newTestStore(t)
	ctx := context.Background()
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
