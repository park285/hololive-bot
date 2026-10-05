package member

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
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
	c.allMembersSnapshot.Store(newAllMembersState(old, 0, time.Now().Add(-2*time.Minute)))

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

// snapshot을 교체하면 이전 snapshot의 이름·별칭은 더 이상 메모리에서 응답하지 않고, 채널은 새 대표를 가리킨다.
func TestCachePointLookup_SnapshotReplacementDropsPriorEntries(t *testing.T) {
	stale := &domain.Member{
		ID:        1,
		ChannelID: "same-channel",
		Name:      testMemberNameOld,
		Aliases:   &domain.Aliases{Ko: []string{"OldAlias"}},
	}
	current := &domain.Member{ID: 1, ChannelID: "same-channel", Name: testMemberNameNew}
	c := withTestEpochAuthority(&Cache{logger: slog.New(slog.DiscardHandler)})

	if c.storeAllMembersSnapshot(nil, 0, []*domain.Member{stale}) == nil {
		t.Fatal("initial snapshot was not published")
	}

	if got, _ := c.lookupPointInMemory(pointLookupAlias, "OldAlias"); got != stale {
		t.Fatalf("initial alias lookup = %+v, want %+v", got, stale)
	}

	previous, generation := c.allMembersView()
	if c.storeAllMembersSnapshot(previous, generation, []*domain.Member{current}) == nil {
		t.Fatal("replacement snapshot was not published")
	}

	if got, _ := c.lookupPointInMemory(pointLookupName, testMemberNameOld); got != nil {
		t.Fatalf("stale name lookup = %+v, want miss", got)
	}

	if got, _ := c.lookupPointInMemory(pointLookupChannel, "same-channel"); got != current {
		t.Fatalf("channel lookup = %+v, want current member %+v", got, current)
	}

	if got, _ := c.lookupPointInMemory(pointLookupAlias, "OldAlias"); got != nil {
		t.Fatalf("removed alias lookup = %+v, want miss", got)
	}
}
