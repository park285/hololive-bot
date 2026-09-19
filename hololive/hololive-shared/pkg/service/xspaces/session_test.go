package xspaces

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

func TestSessionCandidateValidationAndFencing(t *testing.T) {
	pool := dbtest.NewPool(t)
	store, err := NewStore(pool, bytes.Repeat([]byte{1}, 32))
	require.NoError(t, err)

	ctx := t.Context()
	cookies := Cookies{AuthToken: strings.Repeat("a", 40), CSRFToken: strings.Repeat("b", 64)}
	status, err := store.Status(ctx)
	require.NoError(t, err)
	require.Equal(t, "unconfigured", status.State)

	accepted, err := store.Submit(ctx, cookies, "0")
	require.NoError(t, err)
	require.True(t, accepted)

	accepted, err = store.Submit(ctx, cookies, "0")
	require.NoError(t, err)
	require.False(t, accepted)

	snapshot, err := store.Snapshot(ctx)
	require.NoError(t, err)
	require.Nil(t, snapshot.Active)
	require.Equal(t, cookies, *snapshot.Candidate)

	promoted, err := store.ResolveCandidate(ctx, snapshot.Revision, "")
	require.NoError(t, err)
	require.True(t, promoted)

	verifyCandidateReplacement(t, store, cookies)
}

func verifyCandidateReplacement(t *testing.T, store *Store, cookies Cookies) {
	t.Helper()

	ctx := t.Context()

	// 잘못된 새 세션을 제출해도 기존 활성 인증을 보존한다.
	newCookies := Cookies{AuthToken: strings.Repeat("c", 40), CSRFToken: strings.Repeat("d", 64)}

	accepted, err := store.Submit(ctx, newCookies, "1")
	require.NoError(t, err)
	require.True(t, accepted)

	rejected, err := store.ResolveCandidate(ctx, 2, "authentication")
	require.NoError(t, err)
	require.True(t, rejected)

	snapshot, err := store.Snapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, cookies, *snapshot.Active)
	require.Nil(t, snapshot.Candidate)

	accepted, err = store.Submit(ctx, newCookies, "2")
	require.NoError(t, err)
	require.True(t, accepted)

	promoted, err := store.ResolveCandidate(ctx, 2, "")
	require.NoError(t, err)
	require.False(t, promoted)

	promoted, err = store.ResolveCandidate(ctx, 3, "")
	require.NoError(t, err)
	require.True(t, promoted)

	current, err := store.Observe(ctx, 1, "authentication", time.Now())
	require.NoError(t, err)
	require.False(t, current)

	status, err := store.Status(ctx)
	require.NoError(t, err)
	require.Equal(t, "connected", status.State)

	encoded, err := jsonv2.Marshal(status)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), newCookies.AuthToken)
	require.NotContains(t, string(encoded), newCookies.CSRFToken)

	var sealed []byte

	require.NoError(t, store.pool.QueryRow(ctx, `SELECT active_ciphertext FROM x_space_session WHERE id=1`).Scan(&sealed))
	require.NotContains(t, string(sealed), newCookies.AuthToken)

	if len(sealed) == 0 {
		t.Fatal("stored session ciphertext is empty")
	}

	sealed[len(sealed)-1] ^= 1

	_, err = store.open(sealed)
	require.Error(t, err)
}

func TestSessionCipherNoncesAndSafeErrors(t *testing.T) {
	pool := dbtest.NewPool(t)
	store, err := NewStore(pool, bytes.Repeat([]byte{2}, 32))
	require.NoError(t, err)

	cookies := Cookies{AuthToken: strings.Repeat("a", 40), CSRFToken: strings.Repeat("b", 64)}
	a, err := store.seal(cookies)
	require.NoError(t, err)

	b, err := store.seal(cookies)
	require.NoError(t, err)
	require.NotEqual(t, a, b)

	decoded, err := store.open(a)
	require.NoError(t, err)
	require.Equal(t, cookies, *decoded)
	require.False(t, ValidErrorCode("auth_token=secret"))

	_, err = store.Submit(t.Context(), cookies, "01")
	require.Error(t, err)

	accepted, err := store.Submit(t.Context(), cookies, "5")
	require.NoError(t, err)
	require.False(t, accepted)
}
