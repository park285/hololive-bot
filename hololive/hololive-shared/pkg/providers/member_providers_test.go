package providers

import (
	"context"
	"testing"
)

type providerMemberAdapterContextKey struct{}

func TestProvideMemberServiceAdapter_DetachesCancellationAndPreservesValues(t *testing.T) {
	t.Parallel()

	parent := context.WithValue(context.Background(), providerMemberAdapterContextKey{}, "build-value")
	buildCtx, cancel := context.WithCancel(parent)
	cancel()

	adapterCtx := memberAdapterContext(buildCtx)
	if err := adapterCtx.Err(); err != nil {
		t.Fatalf("adapter ctx err = %v, want nil", err)
	}
	if got := adapterCtx.Value(providerMemberAdapterContextKey{}); got != "build-value" {
		t.Fatalf("adapter ctx value = %v, want build-value", got)
	}
}
