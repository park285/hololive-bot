package member

import (
	"errors"
	"sync"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestPointIndexKeepsSmallestIDChannelRepresentative(t *testing.T) {
	members := []*domain.Member{
		nil,
		{ID: 2, ChannelID: "shared", Name: "Second"},
		{ID: 1, ChannelID: "shared", Name: "First"},
		{ID: 3, ChannelID: "persisted", Name: "Persisted"},
	}
	index := buildMemberPointIndex(members)

	if index.representatives["shared"] != members[2] {
		t.Fatal("shared channel must preserve the smallest persistent ID representative")
	}

	if index.representatives["persisted"] != members[3] {
		t.Fatal("single-member channel must be its own representative")
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

// snapshot 별칭 조회는 repository_query_0064_03.sql과 같은 규칙을 따른다: 공식 이름은 대소문자 무시, 명시 별칭은
// 정확히 일치, 여러 명이 맞으면 가장 작은 ID.
func TestSnapshotAliasLookupFollowsRepositoryRules(t *testing.T) {
	sigma := &domain.Member{ID: 5, Name: "Sigma", NameJa: "Σ", Aliases: &domain.Aliases{Ko: []string{"ExactAlias", "공유"}}}
	other := &domain.Member{ID: 3, Name: "Other", Aliases: &domain.Aliases{Ja: []string{"공유"}}}
	cache := &Cache{}
	cache.snapshotGeneration.Store(7)
	cache.allMembersSnapshot.Store(&allMembersState{members: []*domain.Member{sigma, other}, generation: 7, hasSuccessful: true})

	cases := []struct {
		alias string
		want  *domain.Member
	}{
		{alias: "σ", want: sigma},
		{alias: "SIGMA", want: sigma},
		{alias: "ExactAlias", want: sigma},
		{alias: "exactalias", want: nil},
		{alias: "공유", want: other},
		{alias: "missing", want: nil},
	}

	for _, tc := range cases {
		// snapshot miss는 PostgreSQL로 넘어가므로 여기서는 snapshot 판정만 본다.
		if tc.want == nil {
			if got, generation := cache.findAliasInSnapshot(tc.alias); got != nil || generation != 7 {
				t.Fatalf("snapshot alias %q = %+v@%d, want miss in generation 7", tc.alias, got, generation)
			}

			continue
		}

		got, err := cache.FindByAlias(t.Context(), tc.alias)
		if err != nil || got != tc.want {
			t.Fatalf("FindByAlias(%q) = %+v, %v; want %+v", tc.alias, got, err, tc.want)
		}
	}
}

func TestSnapshotAliasLookupIgnoresSnapshotFromOtherGeneration(t *testing.T) {
	member := &domain.Member{ID: 1, Name: "Sigma"}
	cache := &Cache{}
	cache.snapshotGeneration.Store(7)
	cache.allMembersSnapshot.Store(&allMembersState{members: []*domain.Member{member}, generation: 6, hasSuccessful: true})

	if got, _ := cache.findAliasInSnapshot("Sigma"); got != nil {
		t.Fatalf("alias served from generation 6 snapshot in generation 7: %+v", got)
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
