package bootstrap

import (
	"crypto/sha256"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v5/webhook"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

func TestWebhookNonceSharedAcrossHandlersWithoutCache(t *testing.T) {
	const body = `{"messageId":"nonce-test","text":"hello","room":"room-1","sender":"tester","userId":"user-1"}`

	pool := dbtest.NewPool(t)
	admitter := &recordingWebhookAdmitter{messages: make(chan *webhook.Message, 3)}
	signed := newSignedBotWebhookTestRequest(t, "nonce-test", body)
	send := func(handler *webhook.Handler) int {
		req := signed.Clone(t.Context())

		req.Body = io.NopCloser(strings.NewReader(body))

		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		return recorder.Code
	}

	start := make(chan struct{})

	var (
		wg       sync.WaitGroup
		statuses [2]int
	)

	for i := range statuses {
		handler := newDurableTestHandler(t, admitter, pool)

		wg.Go(func() {
			<-start

			statuses[i] = send(handler)
		})
	}

	close(start)
	wg.Wait()
	require.ElementsMatch(t, []int{http.StatusOK, http.StatusUnauthorized}, statuses[:])
	require.Equal(t, http.StatusUnauthorized, send(newDurableTestHandler(t, admitter, pool)))
	require.Len(t, admitter.messages, 1)
}

func TestWebhookNonceUnavailableRejectsBeforeAdmission(t *testing.T) {
	const body = `{"messageId":"no-ledger","text":"hello","room":"room-1","sender":"tester","userId":"user-1"}`

	admitter := &recordingWebhookAdmitter{messages: make(chan *webhook.Message, 1)}
	handler := newDurableTestHandler(t, admitter, dbtest.NewBlankPool(t))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, newSignedBotWebhookTestRequest(t, "no-ledger", body))
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Empty(t, admitter.messages)
}

func TestWebhookNonceExpiryAndBoundedPrune(t *testing.T) {
	const (
		key = "signed-request"
		ttl = 10 * time.Minute
	)

	ctx := t.Context()
	pool := dbtest.NewPool(t)
	store := postgresNonceStore{pool: pool}
	digest := sha256.Sum256([]byte(key))

	var before time.Time

	require.NoError(t, pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&before))

	duplicate, err := store.IsDuplicate(ctx, key, ttl)
	require.NoError(t, err)
	require.False(t, duplicate)

	var expiry, after time.Time

	require.NoError(t, pool.QueryRow(ctx, `SELECT expires_at, clock_timestamp() FROM public.bot_webhook_nonces WHERE digest=$1`, digest[:]).Scan(&expiry, &after))
	require.False(t, expiry.Before(before.Add(ttl)))
	require.False(t, expiry.After(after.Add(ttl)))

	_, err = pool.Exec(ctx, `INSERT INTO public.bot_webhook_nonces
		SELECT sha256(int4send(n)), clock_timestamp() - interval '1 minute' FROM generate_series(1,70) AS n`)
	require.NoError(t, err)

	duplicate, err = store.IsDuplicate(ctx, key, ttl)
	require.NoError(t, err)
	require.True(t, duplicate)

	var (
		unchanged time.Time
		expired   int
	)

	require.NoError(t, pool.QueryRow(ctx, `SELECT expires_at FROM public.bot_webhook_nonces WHERE digest=$1`, digest[:]).Scan(&unchanged))
	require.True(t, expiry.Equal(unchanged))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM public.bot_webhook_nonces WHERE expires_at <= clock_timestamp()`).Scan(&expired))
	require.Equal(t, 6, expired)

	_, err = pool.Exec(ctx, `UPDATE public.bot_webhook_nonces SET expires_at=clock_timestamp() WHERE digest=$1`, digest[:])
	require.NoError(t, err)

	duplicate, err = store.IsDuplicate(ctx, key, ttl)
	require.NoError(t, err)
	require.False(t, duplicate)

	duplicate, err = store.IsDuplicate(ctx, key, ttl)
	require.NoError(t, err)
	require.True(t, duplicate)
}
