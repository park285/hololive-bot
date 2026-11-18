package member

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestCache_LoadAllMembers_ReturnsError(t *testing.T) {
	finder := &Cache{}

	_, err := finder.LoadAllMembers(t.Context())
	if err == nil {
		t.Fatal("LoadAllMembers() error = nil, want non-nil")
	}

	if got := err.Error(); got != "member repository is nil" {
		t.Fatalf("LoadAllMembers() error = %q, want %q", got, "member repository is nil")
	}
}

func TestCache_BypassHonorsCallerCancellation(t *testing.T) {
	cache := newEpochTestCache(&fakeMemberEpochAuthority{epoch: 1})
	cache.authorityHealthy.Store(false)

	started := make(chan struct{})

	cache.loadAllMembers = func(ctx context.Context) ([]*domain.Member, error) {
		close(started)
		<-ctx.Done()

		return nil, ctx.Err()
	}

	finder := cache
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() {
		_, err := finder.LoadAllMembers(ctx)
		done <- err
	}()

	<-started
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("bypass error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("bypass load ignored caller cancellation")
	}
}

func TestCache_BypassDoesNotPublishSnapshot(t *testing.T) {
	cache := newEpochTestCache(&fakeMemberEpochAuthority{epoch: 1})
	cache.authorityHealthy.Store(false)

	cache.loadAllMembers = func(context.Context) ([]*domain.Member, error) {
		return []*domain.Member{
			{ID: 1, Name: testMemberMiko},
			{ID: 2, Name: testMemberPekora},
		}, nil
	}

	members, err := cache.LoadAllMembers(t.Context())
	if err != nil || len(members) != 2 {
		t.Fatalf("bypass LoadAllMembers() = %+v, %v; want both members", members, err)
	}

	if cache.allMembersSnapshot.Load() != nil {
		t.Fatal("bypass published a snapshot while epoch authority was uncertain")
	}
}
