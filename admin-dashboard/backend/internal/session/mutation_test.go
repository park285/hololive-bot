package session

import (
	"context"
	jsonv2 "encoding/json/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const mutationFixtureID = "5ae58f70-51c4-4df2-bf70-683773e40638"

func TestMutationClaimIsAtomicAndSurvivesRefreshRotation(t *testing.T) {
	store, mr := newTestStore(t)

	store.cfg.RotationInterval = 0

	sess, err := store.Create(t.Context())
	require.NoError(t, err)

	var (
		admitted atomic.Int32
		wg       sync.WaitGroup
	)

	errs := make(chan error, 32)

	for range 32 {
		wg.Go(func() {
			ok, claimErr := store.ClaimMutation(t.Context(), sess, mutationFixtureID)
			if ok {
				admitted.Add(1)
			}

			errs <- claimErr
		})
	}

	wg.Wait()
	close(errs)

	for claimErr := range errs {
		require.NoError(t, claimErr)
	}

	require.EqualValues(t, 1, admitted.Load())

	_, err = store.Refresh(t.Context(), sess.ID, false)
	require.NoError(t, err)

	next, rotated, err := store.Rotate(t.Context(), sess.ID)
	require.NoError(t, err)
	require.True(t, rotated)

	claimed, err := store.ClaimMutation(t.Context(), next, mutationFixtureID)
	require.NoError(t, err)
	require.False(t, claimed)
	require.Equal(t, "1", mr.HGet(familyKey(sess.FamilyID), "mutation:"+mutationFixtureID))
	require.LessOrEqual(t, mr.TTL(familyKey(sess.FamilyID)), time.Until(sess.AbsoluteExpiresAt)+time.Second)

	other, err := store.Create(t.Context())
	require.NoError(t, err)

	claimed, err = store.ClaimMutation(t.Context(), other, mutationFixtureID)
	require.NoError(t, err)
	require.True(t, claimed, "new login has a distinct CSRF-bound family")
}

func TestFamilyEvictionCannotRecreateMutationAuthority(t *testing.T) {
	store, mr := newTestStore(t)

	store.cfg.RotationInterval = 0

	sess, err := store.Create(t.Context())
	require.NoError(t, err)

	claimed, err := store.ClaimMutation(t.Context(), sess, mutationFixtureID)
	require.NoError(t, err)
	require.True(t, claimed)
	mr.Del(familyKey(sess.FamilyID))
	require.True(t, mr.Exists(sessionKey(sess.ID)), "simulate LFU eviction of only the family key")

	_, found, err := store.Get(t.Context(), sess.ID)
	require.NoError(t, err)
	require.False(t, found)

	active, err := store.FamilyActive(t.Context(), sess.FamilyID)
	require.NoError(t, err)
	require.False(t, active)

	refresh, err := store.Refresh(t.Context(), sess.ID, false)
	require.NoError(t, err)
	require.Equal(t, RefreshMissing, refresh.Kind)

	_, rotated, err := store.Rotate(t.Context(), sess.ID)
	require.NoError(t, err)
	require.False(t, rotated)

	claimed, err = store.ClaimMutation(t.Context(), sess, mutationFixtureID)
	require.ErrorIs(t, err, ErrFamilyInactive)
	require.False(t, claimed)
	require.False(t, mr.Exists(familyKey(sess.FamilyID)))
}

func TestMutationClaimFailureAndCancellationNeverReleasePriorClaim(t *testing.T) {
	store, mr := newTestStore(t)
	sess, err := store.Create(t.Context())
	require.NoError(t, err)

	claimed, err := store.ClaimMutation(t.Context(), sess, mutationFixtureID)
	require.NoError(t, err)
	require.True(t, claimed)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	claimed, err = store.ClaimMutation(ctx, sess, mutationFixtureID)
	require.Error(t, err)
	require.False(t, claimed)

	claimed, err = store.ClaimMutation(t.Context(), sess, mutationFixtureID)
	require.NoError(t, err)
	require.False(t, claimed)

	mr.SetError("synthetic store failure")

	claimed, err = store.ClaimMutation(t.Context(), sess, mutationFixtureID)
	require.Error(t, err)
	require.False(t, claimed)
}

func TestLegacyTokenCannotAcquireFamilyAuthority(t *testing.T) {
	store, mr := newTestStore(t)
	now := time.Now().UTC()
	legacy := Session{ID: "legacy-token", CreatedAt: now, ExpiresAt: now.Add(time.Minute), AbsoluteExpiresAt: now.Add(time.Hour), LastRotatedAt: now}
	data, err := jsonv2.Marshal(legacy)
	require.NoError(t, err)
	require.NoError(t, mr.Set(sessionKey(legacy.ID), string(data)))
	mr.SetTTL(sessionKey(legacy.ID), time.Minute)

	_, found, err := store.Get(t.Context(), legacy.ID)
	require.NoError(t, err)
	require.False(t, found)

	refresh, err := store.Refresh(t.Context(), legacy.ID, false)
	require.NoError(t, err)
	require.Equal(t, RefreshMissing, refresh.Kind)
	require.False(t, mr.Exists(familyKey(legacy.ID)))
}
