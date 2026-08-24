package member

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func TestCacheAllMembers_InvalidateDuringFailedReloadDoesNotReturnOldSnapshot(t *testing.T) {
	old := []*domain.Member{{ID: 1, ChannelID: "old-channel", Name: testMemberNameOld}}
	newMembers := []*domain.Member{{ID: 2, ChannelID: "new-channel", Name: testMemberNameNew}}
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})

	var calls atomic.Int64

	c := &Cache{
		logger:      slog.New(slog.DiscardHandler),
		snapshotTTL: time.Minute,
		loadAllMembers: func(context.Context) ([]*domain.Member, error) {
			if calls.Add(1) == 1 {
				close(firstStarted)
				<-releaseFirst

				return nil, errors.New("old generation reload failed")
			}

			return newMembers, nil
		},
	}
	c.allMembersSnapshot.Store(&allMembersState{
		members:       old,
		loadedAt:      time.Now().Add(-2 * time.Minute),
		hasSuccessful: true,
	})

	type result struct {
		members []*domain.Member
		err     error
	}

	done := make(chan result, 1)

	go func() {
		members, err := c.AllMembers(t.Context())
		done <- result{members: members, err: err}
	}()

	<-firstStarted

	if err := c.InvalidateAll(t.Context()); err != nil {
		t.Fatalf("InvalidateAll() error = %v", err)
	}

	close(releaseFirst)

	got := <-done
	if got.err != nil {
		t.Fatalf("AllMembers() error = %v", got.err)
	}

	if len(got.members) != 1 || got.members[0].Name != testMemberNameNew {
		t.Fatalf("AllMembers() members = %+v, want only New", got.members)
	}

	if calls.Load() != 2 {
		t.Fatalf("loader calls = %d, want retry in the new generation", calls.Load())
	}
}

func TestCachePointLookup_RejectsPriorSnapshotValkeyEntries(t *testing.T) {
	stale := domain.Member{
		ID:        1,
		ChannelID: "same-channel",
		Name:      testMemberNameOld,
		Aliases:   &domain.Aliases{Ko: []string{"OldAlias"}},
	}
	current := &domain.Member{ID: 1, ChannelID: "same-channel", Name: testMemberNameNew}
	cacheClient := cachemocks.NewLenientClient()

	cacheClient.GetFunc = func(_ context.Context, _ string, dest any) error {
		member, ok := dest.(*domain.Member)
		if !ok {
			return errors.New("cache destination is not a member")
		}

		*member = stale

		return nil
	}

	c := &Cache{
		cache:  cacheClient,
		logger: slog.New(slog.DiscardHandler),
	}

	if got := c.loadNameFromDistributedCache(t.Context(), testMemberNameOld, 0); got == nil || got.Name != testMemberNameOld {
		t.Fatalf("cold point lookup = %+v, want existing Valkey behavior", got)
	}

	if !c.storeAllMembersSnapshot(nil, 0, []*domain.Member{&stale}) {
		t.Fatal("initial snapshot was not published")
	}

	previous, generation := c.allMembersView()
	if !c.storeAllMembersSnapshot(previous, generation, []*domain.Member{current}) {
		t.Fatal("replacement snapshot was not published")
	}

	_, generation = c.allMembersView()

	if got := c.loadNameFromDistributedCache(t.Context(), testMemberNameOld, generation); got != nil {
		t.Fatalf("stale name lookup = %+v, want miss", got)
	}

	if _, ok := c.byName.Load(testMemberNameOld); ok {
		t.Fatal("stale name was re-pinned in the current generation")
	}

	if got := c.loadChannelFromDistributedCache(t.Context(), "same-channel", generation); got != current {
		t.Fatalf("channel lookup = %+v, want canonical current member %+v", got, current)
	}

	if got := c.getAliasFromCache(t.Context(), "OldAlias", generation); got != nil {
		t.Fatalf("removed alias lookup = %+v, want miss", got)
	}
}
