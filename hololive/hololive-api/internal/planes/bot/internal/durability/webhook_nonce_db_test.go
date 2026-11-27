package durability

import (
	"crypto/sha256"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWebhookNonceExpiryAndBoundedPrune(t *testing.T) {
	const key = "signed-request"

	ctx := t.Context()
	store := newTestStore(t)

	var clock atomic.Int64

	clock.Store(time.Now().UnixMilli())

	store.now = func() time.Time { return time.UnixMilli(clock.Load()) }

	nonces := NewWebhookNonceStore(store)
	digest := sha256.Sum256([]byte(key))

	duplicate, err := nonces.IsDuplicate(ctx, key, WebhookNonceReplayWindow)
	require.NoError(t, err)
	require.False(t, duplicate)

	var expiry int64

	require.NoError(t, store.QueryRow(ctx, `SELECT expires_at FROM bot_webhook_nonces WHERE digest = ?1`, digest[:]).Scan(&expiry))
	require.Equal(t, clock.Load()+WebhookNonceReplayWindow.Milliseconds(), expiry)

	_, err = store.Exec(ctx, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 70)
		INSERT INTO bot_webhook_nonces (digest, expires_at)
		SELECT unhex(printf('%064x', i)), ?1 FROM n`, clock.Load()-time.Minute.Milliseconds())
	require.NoError(t, err)

	duplicate, err = nonces.IsDuplicate(ctx, key, WebhookNonceReplayWindow)
	require.NoError(t, err)
	require.True(t, duplicate)

	var (
		unchanged int64
		expired   int
	)

	require.NoError(t, store.QueryRow(ctx, `SELECT expires_at FROM bot_webhook_nonces WHERE digest = ?1`, digest[:]).Scan(&unchanged))
	require.Equal(t, expiry, unchanged)
	require.NoError(t, store.QueryRow(ctx, `SELECT count(*) FROM bot_webhook_nonces WHERE expires_at <= ?1`, clock.Load()).Scan(&expired))
	require.Equal(t, 6, expired)

	clock.Store(expiry)

	duplicate, err = nonces.IsDuplicate(ctx, key, WebhookNonceReplayWindow)
	require.NoError(t, err)
	require.False(t, duplicate)

	duplicate, err = nonces.IsDuplicate(ctx, key, WebhookNonceReplayWindow)
	require.NoError(t, err)
	require.True(t, duplicate)

	_, err = nonces.IsDuplicate(ctx, key, time.Minute)
	require.ErrorIs(t, err, ErrInvalidArgument)
}
