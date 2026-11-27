package durability

import (
	"fmt"
	"sync"
	"testing"

	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const orderingKeyAlpha = "room:alpha"

type concurrentInboxOutcome struct {
	name    string
	err     error
	applied bool
}

func TestInboxOrderingHeadStaysConsistentUnderConcurrentAdmissionAndSettlement(t *testing.T) {
	store := newTestStore(t)
	repo := NewInboxRepository(store)
	ctx := t.Context()

	first := inboxMessage("message:ordering-first", "room-1", orderingKeyAlpha)
	admitOne(ctx, t, repo, first)

	claim, err := repo.Claim(ctx, testClaimToken, durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, claim)

	const admissions = 8

	start := make(chan struct{})
	outcomes := make(chan concurrentInboxOutcome, admissions+1)

	var wg sync.WaitGroup

	for i := range admissions {
		wg.Go(func() {
			<-start

			messageID := fmt.Sprintf("message:ordering-%02d", i)
			admitted, admitErr := repo.AdmitResult(ctx, inboxMessage(messageID, "room-1", orderingKeyAlpha))

			outcomes <- concurrentInboxOutcome{name: messageID, err: admitErr, applied: admitted == workercontract.AdmissionAccepted}
		})
	}

	wg.Go(func() {
		<-start

		applied, completeErr := repo.Complete(ctx, first.MessageID, testClaimToken)
		outcomes <- concurrentInboxOutcome{name: "complete " + first.MessageID, err: completeErr, applied: applied}
	})

	close(start)
	wg.Wait()
	close(outcomes)

	for outcome := range outcomes {
		require.NoError(t, outcome.err, outcome.name)
		require.True(t, outcome.applied, outcome.name)
	}

	var oldestActive string

	require.NoError(t, store.QueryRow(ctx, `SELECT message_id FROM bot_webhook_inbox
		WHERE ordering_key = ?1 AND status IN ('pending', 'processing', 'retry')
		ORDER BY id LIMIT 1`, orderingKeyAlpha).Scan(&oldestActive))

	head, ok := webhookHead(ctx, t, store, orderingKeyAlpha)
	require.True(t, ok, "active messages must keep an ordering head")
	assert.Equal(t, oldestActive, head, "the head must be the oldest active message after concurrent admission and settlement")

	var heads int

	require.NoError(t, store.QueryRow(ctx, "SELECT count(*) FROM bot_webhook_heads").Scan(&heads))
	assert.Equal(t, 1, heads)
}
