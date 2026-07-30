package store

import (
	"math/rand"
	"testing"
	"time"
)

func benchDeliveryLockTokens(n int) []LockToken {
	//nolint:gosec // G404: benchmark fixture needs deterministic pseudo-random values, not cryptographic randomness.
	rng := rand.New(rand.NewSource(0x5eed))
	base := time.Date(2026, time.June, 12, 12, 0, 0, 0, time.UTC)
	tokens := make([]LockToken, 0, n)
	for range n {
		lockedAt := base.Add(time.Duration(rng.Intn(1_000_000)) * time.Microsecond)
		tokens = append(tokens, NewLockToken(rng.Int63n(1<<40)+1, &lockedAt))
	}
	return tokens
}

func BenchmarkDeliveryBatchUpdateArgs(b *testing.B) {
	tokens := benchDeliveryLockTokens(50)
	b.ReportAllocs()
	for b.Loop() {
		ids, lockedAts := deliveryLockTokenArrays(tokens)
		if len(ids) != len(tokens) || len(lockedAts) != len(tokens) {
			b.Fatalf("deliveryLockTokenArrays built %d ids / %d lockedAts, want %d", len(ids), len(lockedAts), len(tokens))
		}
	}
}
