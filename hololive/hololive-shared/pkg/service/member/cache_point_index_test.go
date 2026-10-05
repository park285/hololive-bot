package member

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestPointIndexKeepsSmallestIDChannelRepresentative(t *testing.T) {
	members := []*domain.Member{
		nil,
		{ID: 2, ChannelID: "shared", Name: "Second"},
		{ID: 1, ChannelID: "shared", Name: "First"},
		{ID: 3, ChannelID: "persisted", Name: "Persisted"},
	}
	snapshot, index := newMemberSnapshotIndex(members)

	if len(snapshot) != 3 {
		t.Fatalf("snapshot len = %d, want nil member removed", len(snapshot))
	}

	if index.channelRepresentatives["shared"] != members[2] {
		t.Fatal("shared channel must preserve the smallest persistent ID representative")
	}

	if index.channelRepresentatives["persisted"] != members[3] {
		t.Fatal("single-member channel must be its own representative")
	}

	if want := []string{"shared", "shared", "persisted"}; !slices.Equal(index.channelIDs, want) {
		t.Fatalf("channel IDs = %v, want repository-equivalent %v", index.channelIDs, want)
	}
}

// snapshot 별칭 조회는 repository_query_0064_03.sql과 같은 규칙을 따른다: 공식 이름은 대소문자 무시, 명시 별칭은
// 정확히 일치, 여러 명이 맞으면 가장 작은 ID.
func TestSnapshotAliasLookupFollowsRepositoryRules(t *testing.T) {
	sigma := &domain.Member{ID: 5, Name: "Sigma", NameJa: "Σ", Aliases: &domain.Aliases{Ko: []string{"ExactAlias", "공유"}}}
	other := &domain.Member{ID: 3, Name: "Other", Aliases: &domain.Aliases{Ja: []string{"공유"}}}
	cache := &Cache{}
	cache.snapshotGeneration.Store(7)
	cache.allMembersSnapshot.Store(newAllMembersState([]*domain.Member{sigma, other}, 7, time.Now()))

	cases := []struct {
		alias string
		want  *domain.Member
	}{
		{alias: "σ", want: sigma},
		{alias: "SIGMA", want: sigma},
		{alias: "ExactAlias", want: sigma},
		{alias: "exactalias", want: nil},
		{alias: "공유", want: other},
		{alias: " Sigma", want: nil},
		{alias: "missing", want: nil},
	}

	for _, tc := range cases {
		got, generation := cache.lookupPointInMemory(pointLookupAlias, tc.alias)
		if got != tc.want || generation != 7 {
			t.Fatalf("snapshot alias %q = %+v@%d, want %+v in generation 7", tc.alias, got, generation, tc.want)
		}
	}
}

// 이름과 명시 별칭이 서로 다른 멤버를 가리키면 SQL처럼 둘 중 영속 ID가 작은 쪽이 이긴다.
func TestSnapshotAliasOwnerComparesNameAndExplicitAlias(t *testing.T) {
	named := &domain.Member{ID: 9, Name: testMemberPekora}
	aliased := &domain.Member{ID: 4, Name: "Other", Aliases: &domain.Aliases{Ko: []string{testMemberPekora}}}
	_, index := newMemberSnapshotIndex([]*domain.Member{named, aliased})

	if got := index.aliasOwner(testMemberPekora); got != aliased {
		t.Fatalf("aliasOwner = %+v, want smaller-ID explicit alias owner", got)
	}

	if got := index.aliasOwner("PEKORA"); got != named {
		t.Fatalf("aliasOwner(case-folded) = %+v, want name owner because explicit aliases are exact", got)
	}
}

func TestSnapshotAliasLookupIgnoresSnapshotFromOtherGeneration(t *testing.T) {
	member := &domain.Member{ID: 1, Name: "Sigma"}
	cache := &Cache{}
	cache.snapshotGeneration.Store(7)
	cache.allMembersSnapshot.Store(newAllMembersState([]*domain.Member{member}, 6, time.Now()))

	if got, _ := cache.lookupPointInMemory(pointLookupAlias, "Sigma"); got != nil {
		t.Fatalf("alias served from generation 6 snapshot in generation 7: %+v", got)
	}
}

func TestDeferredSnapshotReusesPublishedIndex(t *testing.T) {
	cache := &Cache{}
	if cache.storeAllMembersSnapshot(nil, 0, []*domain.Member{{ID: 1, Name: "a", ChannelID: "channel"}}) == nil {
		t.Fatal("failed to publish initial snapshot")
	}

	original := cache.allMembersSnapshot.Load()
	if original.index == nil {
		t.Fatal("runtime snapshot must build the index before publication")
	}

	if !cache.deferAllMembersSnapshotReload(original, original.generation, errors.New("load failure")) {
		t.Fatal("failed to schedule retry")
	}

	deferred := cache.allMembersSnapshot.Load()
	if deferred == original || deferred.index != original.index {
		t.Fatal("stale retry metadata must reuse the immutable member index")
	}
}

// foldKey는 strings.EqualFold와 같은 동치 관계여야 한다. ToLower로 바꾸면 Kelvin 기호·long s·그리스 시그마에서 갈라진다.
func TestFoldKeyMatchesEqualFold(t *testing.T) {
	corpus := []string{
		"", "a", "A", "k", "K", "\u212a", "s", "S", "\u017f", "σ", "Σ", "ς", "ß", "ẞ", "ǅ", "ǆ", "Ǆ",
		testMemberPekora, "PEKORA", "pekora", "Pe\u212aora", "미코", "みこ", "ミコ", "İ", "i", "ı", "I",
		"\xff", "\xfe", "\ufffd", "a\xff", "A\ufffd", "MiKo ", testMemberMikoSlug,
	}

	for _, left := range corpus {
		for _, right := range corpus {
			want := strings.EqualFold(left, right)
			if got := foldKey(left) == foldKey(right); got != want {
				t.Errorf("foldKey(%q)==foldKey(%q) = %v, EqualFold = %v", left, right, got, want)
			}
		}
	}
}

// 다건 조회 색인은 예전 전체 순회(앞뒤 공백 제거 + EqualFold, snapshot 순서, 멤버당 한 번)와 같은 결과를 내야 한다.
func TestSnapshotSearchIndexMatchesLinearScan(t *testing.T) {
	members := []*domain.Member{
		{ID: 1, Name: testMemberMiko, NameJa: "みこ", NameKo: "미코", Aliases: &domain.Aliases{Ko: []string{"미코", " 엘리트 "}, Ja: []string{"みこち"}}},
		{ID: 2, Name: "miko ", NameKo: "MIKO", Aliases: &domain.Aliases{Ko: []string{"엘리트"}}},
		{ID: 3, Name: "Pe\u212aora", Aliases: &domain.Aliases{Ja: []string{"PEKO", "peko"}}},
		nil,
		{ID: 4, Name: "Kanata", NameKo: " ", Aliases: &domain.Aliases{Ko: []string{"", "  "}}},
		{ID: 5, Name: "Ollie", Aliases: &domain.Aliases{Ko: []string{"\u017fora"}}},
	}
	queries := []string{
		testMemberMikoSlug, " MIKO ", "미코", "みこ", "엘리트", " 엘리트", "pekora", "PEKORA", "peko", "Peko", "SORA", "sora",
		"Kanata", "missing", "\t", "", "みこち",
	}

	snapshot, index := newMemberSnapshotIndex(members)

	for _, query := range queries {
		needle := strings.TrimSpace(query)

		wantNames := []*domain.Member{}
		wantAliases := []*domain.Member{}

		for _, member := range snapshot {
			if needle == "" {
				break
			}

			if linearFoldMatch(needle, member.Name, member.NameJa, member.NameKo) {
				wantNames = append(wantNames, member)
			}

			if linearFoldMatch(needle, member.GetAllAliases()...) {
				wantAliases = append(wantAliases, member)
			}
		}

		gotNames, gotAliases := []*domain.Member{}, []*domain.Member{}

		if key, ok := searchKey(query); ok {
			gotNames = index.membersByName(key)
			gotAliases = index.membersByAlias(key)
		}

		if !sameMembers(gotNames, wantNames) {
			t.Errorf("names(%q) = %v, want %v", query, memberIDs(gotNames), memberIDs(wantNames))
		}

		if !sameMembers(gotAliases, wantAliases) {
			t.Errorf("aliases(%q) = %v, want %v", query, memberIDs(gotAliases), memberIDs(wantAliases))
		}
	}
}

func linearFoldMatch(needle string, values ...string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), needle) {
			return true
		}
	}

	return false
}

func sameMembers(got, want []*domain.Member) bool {
	return slices.Equal(got, want) || (len(got) == 0 && len(want) == 0)
}

func memberIDs(members []*domain.Member) []int {
	ids := make([]int, 0, len(members))
	for _, member := range members {
		ids = append(ids, member.ID)
	}

	return ids
}
