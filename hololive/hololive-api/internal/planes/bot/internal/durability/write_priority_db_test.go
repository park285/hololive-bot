package durability

import (
	"context"
	"database/sql"
	"testing"
	"testing/synctest"

	"github.com/park285/shared-go/sqlite/sqlitedb"
	"github.com/stretchr/testify/require"
)

type nonceOutcome struct {
	duplicate bool
	err       error
}

func TestPriorityWriteReservesTheNonceBeforeAQueuedWrite(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := newTestStore(t)
		nonces := NewWebhookNonceStore(store)

		held := make(chan struct{})
		unblock := make(chan struct{})
		holder := make(chan error, 1)

		go func() {
			holder <- store.write(t.Context(), func(*sql.Tx, int64) error {
				close(held)
				<-unblock

				return nil
			})
		}()

		<-held

		reserve := func(ctx context.Context, outcome chan<- nonceOutcome) {
			duplicate, err := nonces.IsDuplicate(ctx, "settlement", WebhookNonceReplayWindow)
			outcome <- nonceOutcome{duplicate: duplicate, err: err}
		}

		normal := make(chan nonceOutcome, 1)
		priority := make(chan nonceOutcome, 1)

		go reserve(t.Context(), normal)

		synctest.Wait()

		go reserve(sqlitedb.WithWritePriority(t.Context()), priority)

		synctest.Wait()
		close(unblock)
		require.NoError(t, <-holder)

		first := <-priority
		require.NoError(t, first.err)
		require.False(t, first.duplicate, "the priority write must reserve the nonce before the queued normal write")

		second := <-normal
		require.NoError(t, second.err)
		require.True(t, second.duplicate)
	})
}
