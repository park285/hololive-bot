package dispatch

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func TestDispatcherWiresClaimManagerAndSendEngine(t *testing.T) {
	t.Parallel()

	dispatcher := NewDispatcher(nil, cachemocks.NewLenientClient(), &testSender{failRoom: map[string]bool{}}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), &Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		DeliveryParallelism: 2,
	})

	require.NotNil(t, dispatcher.claim)
	require.NotNil(t, dispatcher.send)
	require.Same(t, dispatcher.status, dispatcher.claim.status)
	require.Same(t, dispatcher.grouper, dispatcher.claim.grouper)
	require.Same(t, dispatcher.send, dispatcher.claim.executor)
	require.Same(t, dispatcher.claim, dispatcher.send.claims)
}
