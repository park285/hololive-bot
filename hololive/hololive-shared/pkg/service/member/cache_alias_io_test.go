package member

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedcache "github.com/kapu/hololive-shared/pkg/service/cache"
)

type blockedAliasCache struct {
	sharedcache.KeyValueCache
	entered chan struct{}
	release chan struct{}
	member  domain.Member
}

func (c *blockedAliasCache) Get(ctx context.Context, _ string, destination any) error {
	close(c.entered)
	select {
	case <-c.release:
	case <-ctx.Done():
		return fmt.Errorf("blocked alias read: %w", ctx.Err())
	}
	member, ok := destination.(*domain.Member)
	if !ok {
		return fmt.Errorf("unexpected alias destination %T", destination)
	}
	*member = c.member
	return nil
}

func TestAliasReadDoesNotBlockSnapshotInvalidation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	remote := &blockedAliasCache{
		entered: make(chan struct{}), release: make(chan struct{}),
		member: domain.Member{ID: 1, Name: "Alias"},
	}
	cache := &Cache{cache: remote}
	cache.snapshotGeneration.Store(1)
	cache.allMembersSnapshot.Store(&allMembersState{
		members: []*domain.Member{&remote.member}, generation: 1, hasSuccessful: true,
	})
	readDone := make(chan *domain.Member, 1)
	go func() { readDone <- cache.getAliasFromCache(ctx, "Alias", 1) }()
	select {
	case <-remote.entered:
	case <-ctx.Done():
		t.Fatal("remote read did not start")
	}
	invalidated := make(chan struct{})
	go func() {
		cache.snapshotMu.Lock()
		cache.snapshotGeneration.Add(1)
		cache.allMembersSnapshot.Store(nil)
		cache.snapshotMu.Unlock()
		close(invalidated)
	}()
	select {
	case <-invalidated:
	case <-ctx.Done():
		close(remote.release)
		<-readDone
		<-invalidated
		t.Fatal("snapshot invalidation was blocked by remote I/O")
	}
	close(remote.release)
	select {
	case got := <-readDone:
		if got != nil {
			t.Fatal("remote result from an invalidated generation was accepted")
		}
	case <-ctx.Done():
		t.Fatal("remote read did not finish")
	}
}
