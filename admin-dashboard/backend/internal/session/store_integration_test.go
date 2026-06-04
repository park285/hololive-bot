package session

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/park285/shared-go/pkg/json"
	"github.com/stretchr/testify/require"

	"github.com/kapu/admin-dashboard/internal/config"
)

func newTestStore(t *testing.T) (*Store, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	store, err := NewStoreWithOptions(context.Background(), mr.Addr(), config.DefaultSessionConfig(), Options{DisableCache: true, ForceSingleClient: true})
	require.NoError(t, err)
	t.Cleanup(store.Close)
	return store, mr
}

func seedSession(t *testing.T, mr *miniredis.Miniredis, sess Session) {
	t.Helper()
	data, err := json.Marshal(sess)
	require.NoError(t, err)
	mr.Set(sessionKey(sess.ID), string(data))
	mr.SetTTL(sessionKey(sess.ID), time.Hour)
}

func TestStoreCreateGetRoundTrip(t *testing.T) {
	store, mr := newTestStore(t)
	ctx := context.Background()

	created, err := store.Create(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	require.True(t, mr.Exists(sessionKey(created.ID)))

	loaded, err := store.Get(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.Equal(t, created.ID, loaded.ID)
	require.WithinDuration(t, created.AbsoluteExpiresAt, loaded.AbsoluteExpiresAt, time.Second)
}

func TestStoreGetMissing(t *testing.T) {
	store, _ := newTestStore(t)

	loaded, err := store.Get(context.Background(), "does-not-exist")
	require.NoError(t, err)
	require.Nil(t, loaded)
}

func TestStoreGetDropsAbsolutelyExpired(t *testing.T) {
	store, mr := newTestStore(t)
	now := time.Now().UTC()
	expired := Session{
		ID:                "expired-session",
		CreatedAt:         now.Add(-9 * time.Hour),
		ExpiresAt:         now.Add(time.Minute),
		AbsoluteExpiresAt: now.Add(-time.Minute),
		LastRotatedAt:     now.Add(-9 * time.Hour),
	}
	seedSession(t, mr, expired)

	loaded, err := store.Get(context.Background(), expired.ID)
	require.NoError(t, err)
	require.Nil(t, loaded)
	require.False(t, mr.Exists(sessionKey(expired.ID)))
}

func TestStoreRefreshExtendsSession(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	created, err := store.Create(ctx)
	require.NoError(t, err)

	result, err := store.Refresh(ctx, created.ID, false)
	require.NoError(t, err)
	require.Equal(t, RefreshRefreshed, result.Kind)
	require.NotNil(t, result.Session)
	require.False(t, result.Session.ExpiresAt.Before(created.ExpiresAt))
}

func TestStoreRefreshIdleShortens(t *testing.T) {
	store, mr := newTestStore(t)
	ctx := context.Background()

	created, err := store.Create(ctx)
	require.NoError(t, err)

	result, err := store.Refresh(ctx, created.ID, true)
	require.NoError(t, err)
	require.Equal(t, RefreshIdleShortened, result.Kind)

	ttl := mr.TTL(sessionKey(created.ID))
	require.LessOrEqual(t, ttl, config.DefaultSessionConfig().IdleSessionTTL+time.Second)
}

func TestStoreRefreshMissing(t *testing.T) {
	store, _ := newTestStore(t)

	result, err := store.Refresh(context.Background(), "missing", false)
	require.NoError(t, err)
	require.Equal(t, RefreshMissing, result.Kind)
}

func TestStoreRotateBeforeIntervalIsNoop(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	created, err := store.Create(ctx)
	require.NoError(t, err)

	rotated, err := store.Rotate(ctx, created.ID)
	require.NoError(t, err)
	require.Nil(t, rotated)
}

func TestStoreRotateAfterIntervalCreatesReplacement(t *testing.T) {
	store, mr := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	old := Session{
		ID:                "rotate-me",
		CreatedAt:         now.Add(-time.Hour),
		ExpiresAt:         now.Add(30 * time.Minute),
		AbsoluteExpiresAt: now.Add(7 * time.Hour),
		LastRotatedAt:     now.Add(-time.Hour),
	}
	seedSession(t, mr, old)

	rotated, err := store.Rotate(ctx, old.ID)
	require.NoError(t, err)
	require.NotNil(t, rotated)
	require.NotEqual(t, old.ID, rotated.ID)
	require.Equal(t, old.AbsoluteExpiresAt.Unix(), rotated.AbsoluteExpiresAt.Unix())

	marker, err := store.Get(ctx, old.ID)
	require.NoError(t, err)
	require.NotNil(t, marker)
	require.NotNil(t, marker.RotatedTo)
	require.Equal(t, rotated.ID, *marker.RotatedTo)

	result, err := store.Refresh(ctx, old.ID, false)
	require.NoError(t, err)
	require.Equal(t, RefreshRotated, result.Kind)
	require.Equal(t, rotated.ID, result.Session.ID)
}

func TestStoreDelete(t *testing.T) {
	store, mr := newTestStore(t)
	ctx := context.Background()

	created, err := store.Create(ctx)
	require.NoError(t, err)
	require.NoError(t, store.Delete(ctx, created.ID))
	require.False(t, mr.Exists(sessionKey(created.ID)))
}
