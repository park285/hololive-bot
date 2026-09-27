package member

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestPointIndexMatchesIdentityScan(t *testing.T) {
	members := []*domain.Member{
		nil,
		{ID: 2, ChannelID: "shared", Name: "Second"},
		{ID: 1, ChannelID: "shared", Name: "First"},
		{ID: 3, ChannelID: "persisted", Name: "Persisted"},
		{ID: 1, ChannelID: "other", Name: "Duplicate identity"},
	}
	index := buildMemberPointIndex(members)

	for _, cached := range append(members[1:], &domain.Member{ID: 99}) {
		var want []*domain.Member

		for _, current := range members {
			if current != nil && samePointMemberIdentity(current, cached) {
				want = append(want, current)
			}
		}

		got := index.byID[cached.ID]
		if len(got) != len(want) {
			t.Fatalf("id=%d: got %d want %d", cached.ID, len(got), len(want))
		}

		for i := range got {
			if got[i] != want[i] {
				t.Fatal("identity bucket changed original member order")
			}
		}
	}

	if index.representatives["shared"] != members[2] {
		t.Fatal("shared channel must preserve the smallest persistent ID representative")
	}
}

// ID 없는 멤버를 채널·이름으로 대신 식별하던 호환 체인을 지웠다. 같은 채널·이름이라도 ID가 없으면 인덱싱하지 않고
// 소유를 인정하지 않는다.
func TestPointIndexRejectsMembersWithoutID(t *testing.T) {
	persisted := &domain.Member{ID: 3, ChannelID: "legacy", Name: "Persisted"}
	withoutID := &domain.Member{ChannelID: "legacy", Name: "Persisted"}
	index := buildMemberPointIndex([]*domain.Member{persisted, withoutID})

	if got := index.byID[0]; len(got) != 0 {
		t.Fatalf("members without ID must not be indexed, got %d", len(got))
	}

	if samePointMemberIdentity(persisted, withoutID) || samePointMemberIdentity(withoutID, withoutID) {
		t.Fatal("channel or name must not stand in for a missing member ID")
	}

	cache := &Cache{}
	cache.allMembersSnapshot.Store(&allMembersState{members: []*domain.Member{persisted, withoutID}, generation: 1, hasSuccessful: true})

	if cache.snapshotOwnedNameMemberLocked("Persisted", withoutID, 1) != nil {
		t.Fatal("cached member without ID must not be owned by the snapshot")
	}
}

func TestPointIndexConcurrentInitialization(t *testing.T) {
	snapshot := &allMembersState{members: []*domain.Member{{ID: 1, Name: "a"}}}

	var group sync.WaitGroup

	results := make(chan *memberPointIndex, 64)

	for range cap(results) {
		group.Go(func() {
			results <- snapshot.pointLookup()
		})
	}

	group.Wait()
	close(results)

	for index := range results {
		if index != snapshot.pointLookup() {
			t.Fatal("snapshot published more than one point index")
		}
	}
}

func TestPointOwnershipStillChecksGenerationAndAlias(t *testing.T) {
	member := &domain.Member{ID: 1, Name: "Sigma", NameJa: "Σ", Aliases: &domain.Aliases{Ko: []string{"ExactAlias"}}}
	cache := &Cache{}
	cache.allMembersSnapshot.Store(&allMembersState{members: []*domain.Member{member}, generation: 7, hasSuccessful: true})

	if cache.snapshotOwnedAliasMemberLocked("σ", member, 7) != member {
		t.Fatal("official name EqualFold matching changed")
	}

	if cache.snapshotOwnedAliasMemberLocked("exactalias", member, 7) != nil {
		t.Fatal("explicit alias must remain case-sensitive")
	}

	if cache.snapshotOwnedAliasMemberLocked("ExactAlias", member, 6) != nil {
		t.Fatal("old generations must be rejected")
	}

	if cache.snapshotOwnedNameMemberLocked("Other", member, 7) != nil {
		t.Fatal("identity lookup must still validate the requested name")
	}
}

func BenchmarkPointOwnershipIndex(b *testing.B) {
	for _, n := range []int{256, 1024, 4096} {
		members := make([]*domain.Member, n)
		for i := range members {
			members[i] = &domain.Member{ID: i + 1, ChannelID: fmt.Sprintf("UC-%d", i), Name: fmt.Sprintf("Member-%d", i)}
		}

		cache := &Cache{}
		snapshot := &allMembersState{members: members, generation: 1, hasSuccessful: true}
		snapshot.pointLookup()
		cache.allMembersSnapshot.Store(snapshot)

		cached := members[n-1]
		b.Run(fmt.Sprintf("members-%d", n), func(b *testing.B) {
			b.ReportAllocs()

			for b.Loop() {
				if cache.snapshotOwnedNameMemberLocked(cached.Name, cached, 1) != cached {
					b.Fatal("unexpected owner")
				}
			}
		})
	}
}

func TestPointIndexIsPreparedBeforePublishAndReusedForStaleSnapshot(t *testing.T) {
	cache := &Cache{}
	if !cache.storeAllMembersSnapshot(nil, 0, []*domain.Member{{ID: 1, Name: "a", ChannelID: "channel"}}) {
		t.Fatal("failed to publish initial snapshot")
	}

	original := cache.allMembersSnapshot.Load()
	if original.pointIndex == nil {
		t.Fatal("runtime snapshot must initialize the index before publication")
	}

	if !cache.deferAllMembersSnapshotReload(original, original.generation, errors.New("load failure")) {
		t.Fatal("failed to schedule retry")
	}

	deferred := cache.allMembersSnapshot.Load()
	if deferred == original || deferred.pointLookup() != original.pointLookup() {
		t.Fatal("stale retry metadata must reuse the immutable member index")
	}
}
