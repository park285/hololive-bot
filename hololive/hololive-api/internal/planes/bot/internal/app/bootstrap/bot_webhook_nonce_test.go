package bootstrap

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/park285/iris-client-go/v6/webhook"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability/durabilitytest"
)

func TestWebhookNonceSharedAcrossHandlersWithoutCache(t *testing.T) {
	const body = `{"messageId":"nonce-test","text":"hello","room":"room-1","sender":"tester","userId":"user-1"}`

	store := durabilitytest.Open(t).Store
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
		handler := newDurableTestHandler(t, admitter, store)

		wg.Go(func() {
			<-start

			statuses[i] = send(handler)
		})
	}

	close(start)
	wg.Wait()
	require.ElementsMatch(t, []int{http.StatusOK, http.StatusUnauthorized}, statuses[:])
	require.Equal(t, http.StatusUnauthorized, send(newDurableTestHandler(t, admitter, store)))
	require.Len(t, admitter.messages, 1)
}

func TestWebhookNonceUnavailableRejectsBeforeAdmission(t *testing.T) {
	const body = `{"messageId":"no-ledger","text":"hello","room":"room-1","sender":"tester","userId":"user-1"}`

	admitter := &recordingWebhookAdmitter{messages: make(chan *webhook.Message, 1)}
	store, err := durability.OpenStore(t.Context(), durability.StoreOptions(t.TempDir()+"/durable/bot-durable.db", nil))
	require.NoError(t, err)
	require.NoError(t, store.Close(t.Context()))

	handler := newDurableTestHandler(t, admitter, store)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, newSignedBotWebhookTestRequest(t, "no-ledger", body))
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Empty(t, admitter.messages)
}
