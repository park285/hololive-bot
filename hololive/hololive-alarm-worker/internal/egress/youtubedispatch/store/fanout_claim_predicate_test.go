package store

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestFanoutClaimDeclaresPendingPartialIndexPredicate(t *testing.T) {
	if string(domain.OutboxStatusPending) != "PENDING" {
		t.Fatal("fanout claim SQL must track the canonical pending status")
	}

	claim, update, found := strings.Cut(mustSQL("fanout_claim.sql"), "), updated AS (")
	if !found {
		t.Fatal("fanout claim must retain the atomic claim/update statement")
	}

	for _, predicate := range []string{
		"outbox.status = $1",
		"outbox.status = 'PENDING'",
		"FOR UPDATE OF outbox SKIP LOCKED",
		"ORDER BY outbox.next_attempt_at, outbox.created_at, outbox.id",
		"LIMIT $5",
	} {
		if !strings.Contains(claim, predicate) {
			t.Errorf("fanout claim is missing %q", predicate)
		}
	}

	if !strings.Contains(update, "outbox.status = $1") {
		t.Error("fanout update must preserve the bound status guard")
	}
}
