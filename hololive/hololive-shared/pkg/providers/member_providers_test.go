package providers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
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

type failingMemberSnapshot struct {
	err error
}

func (s failingMemberSnapshot) AllMembers(context.Context) ([]*domain.Member, error) {
	return nil, s.err
}

func TestInitializeMemberDatabaseFromSnapshot_SkipsDestructiveInitAfterColdFailure(t *testing.T) {
	wantErr := errors.New("database unavailable")
	var initializeCalls atomic.Int64
	cacheClient := cachemocks.NewLenientClient()
	cacheClient.InitializeMemberDatabaseFunc = func(context.Context, map[string]string) error {
		initializeCalls.Add(1)
		return nil
	}

	err := initializeMemberDatabaseFromSnapshot(
		context.Background(),
		failingMemberSnapshot{err: wantErr},
		cacheClient,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("initializeMemberDatabaseFromSnapshot() error = %v, want nil", err)
	}
	if initializeCalls.Load() != 0 {
		t.Fatalf("InitializeMemberDatabase() calls = %d, want 0 after cold scan failure", initializeCalls.Load())
	}
}
