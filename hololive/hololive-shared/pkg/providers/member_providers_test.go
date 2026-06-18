package providers

import (
	"context"
	"testing"
)

func TestProvideMemberServiceAdapter_DetachesCanceledBuildContext(t *testing.T) {
	t.Parallel()

	buildCtx, cancel := context.WithCancel(context.Background())
	cancel()

	adapterCtx := memberAdapterContext(buildCtx)
	if err := adapterCtx.Err(); err != nil {
		t.Fatalf("adapter ctx err = %v, want nil", err)
	}
}
