package member

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const sharedReviewChannel = "UC-shared-owner"

func seedSharedReviewMembers(t *testing.T) (*Repository, *domain.Member) {
	t.Helper()

	repo, pool := newPGXRepository(t)

	_, err := pool.Exec(t.Context(), `INSERT INTO members(slug,channel_id,english_name,short_korean_name,org,sync_source,aliases)
 VALUES ('review-owner',$1,'holoAN owner','홀로아나','Hololive','manual','{}'),
 ('review-person',$1,'A Person','개인','Hololive','manual','{"ko":["개인"],"ja":[]}')`, sharedReviewChannel)
	if err != nil {
		t.Fatal(err)
	}

	owner, err := repo.FindByChannelID(t.Context(), sharedReviewChannel)
	if err != nil {
		t.Fatal(err)
	}

	if owner.Name != "holoAN owner" {
		t.Fatalf("SQL representative=%s", owner.Name)
	}

	return repo, owner
}

func TestSharedChannelRepresentativeSurvivesIndividualLookups(t *testing.T) {
	repo, owner := seedSharedReviewMembers(t)
	ctx := t.Context()

	cache := withTestEpochAuthority(newMemberCache(repo, slog.New(slog.DiscardHandler), CacheConfig{}))

	if err := cache.WarmUpCache(ctx); err != nil {
		t.Fatal(err)
	}

	person, err := cache.FindByAlias(ctx, "개인")
	if err != nil {
		t.Fatal(err)
	}

	if person.Name != "A Person" {
		t.Fatalf("individual lookup=%s", person.Name)
	}

	got, err := cache.GetByChannelID(ctx, sharedReviewChannel)
	if err != nil {
		t.Fatal(err)
	}

	if got.ID != owner.ID || got.ShortKoreanName != "홀로아나" {
		t.Fatalf("channel/alarm identity changed: %+v", got)
	}
}

func TestPhotoQueriesUseTheChannelRepresentative(t *testing.T) {
	repo, owner := seedSharedReviewMembers(t)

	photo, err := repo.GetMemberWithPhotoByChannelID(t.Context(), sharedReviewChannel)
	if err != nil {
		t.Fatal(err)
	}

	if photo.ID != owner.ID {
		t.Fatalf("photo representative=%d want=%d", photo.ID, owner.ID)
	}

	photos, err := repo.GetMembersWithPhoto(t.Context(), []string{sharedReviewChannel})
	if err != nil {
		t.Fatal(err)
	}

	if photos[sharedReviewChannel] == nil || photos[sharedReviewChannel].ID != owner.ID {
		t.Fatal("batch photo representative differs")
	}
}

// snapshot 없이 개인 멤버를 이름·별칭으로 먼저 조회해도 공유 채널 index는 SQL 채널 대표만 채운다.
func TestColdIndividualLookupsDoNotClaimSharedChannel(t *testing.T) {
	repo, owner := seedSharedReviewMembers(t)
	ctx := t.Context()

	cache := withTestEpochAuthority(newMemberCache(repo, slog.New(slog.DiscardHandler), CacheConfig{}))

	if person, err := cache.GetByName(ctx, "A Person"); err != nil || person.Name != "A Person" {
		t.Fatalf("GetByName() = %+v, %v", person, err)
	}

	if person, err := cache.FindByAlias(ctx, "개인"); err != nil || person.Name != "A Person" {
		t.Fatalf("FindByAlias() = %+v, %v", person, err)
	}

	got, err := cache.GetByChannelID(ctx, sharedReviewChannel)
	if err != nil {
		t.Fatal(err)
	}

	if got.ID != owner.ID {
		t.Fatalf("channel lookup = %+v, want representative %d", got, owner.ID)
	}
}

// pointLookupExpectations는 repository가 돌려준 조회 결과를 cache 조회와 비교할 기준으로 담는다.
type pointLookupExpectations struct {
	aliases     []string
	channels    []string
	wantAlias   map[string]int
	wantChannel map[string]int
}

func newPointLookupExpectations(t *testing.T, repo *Repository, aliases, channels []string) *pointLookupExpectations {
	t.Helper()

	ctx := t.Context()
	expectations := &pointLookupExpectations{
		aliases:     aliases,
		channels:    channels,
		wantAlias:   make(map[string]int, len(aliases)),
		wantChannel: make(map[string]int, len(channels)),
	}

	for _, alias := range aliases {
		want, err := repo.FindByAlias(ctx, alias)
		if err != nil {
			t.Fatalf("repository alias %q: %v", alias, err)
		}

		expectations.wantAlias[alias] = want.ID
	}

	for _, channelID := range channels {
		want, err := repo.FindByChannelID(ctx, channelID)
		if err != nil {
			t.Fatal(err)
		}

		expectations.wantChannel[channelID] = want.ID
	}

	return expectations
}

func (e *pointLookupExpectations) assert(t *testing.T, label string, cache *Cache) {
	t.Helper()

	ctx := t.Context()

	for _, alias := range e.aliases {
		got, err := cache.FindByAlias(ctx, alias)
		if err != nil || got.ID != e.wantAlias[alias] {
			t.Fatalf("%s FindByAlias(%q) = %+v, %v; want ID %d", label, alias, got, err, e.wantAlias[alias])
		}
	}

	for _, channelID := range e.channels {
		got, err := cache.GetByChannelID(ctx, channelID)
		if err != nil || got.ID != e.wantChannel[channelID] {
			t.Fatalf("%s GetByChannelID(%q) = %+v, %v; want ID %d", label, channelID, got, err, e.wantChannel[channelID])
		}
	}

	got, err := cache.GetByName(ctx, "Twin Name")
	if err != nil || got.Name != "Twin Name" {
		t.Fatalf("%s GetByName(Twin Name) = %+v, %v", label, got, err)
	}

	if _, err := cache.FindByAlias(ctx, "없는별칭"); !errors.Is(err, ErrMemberNotFound) {
		t.Fatalf("%s FindByAlias(missing) error = %v, want ErrMemberNotFound", label, err)
	}
}

// 채널·이름·별칭 조회는 snapshot 적재 전(PostgreSQL)과 적재 후(프로세스 snapshot)에 같은 멤버를 돌려준다. Warm 조회는
// members 행을 지운 뒤에 수행해 snapshot에서 응답했음을 확인한다.
func TestPointLookupsMatchRepositoryWithAndWithoutSnapshot(t *testing.T) {
	repo, _ := seedSharedReviewMembers(t)
	ctx := t.Context()

	_, err := repo.pool.Exec(ctx, `INSERT INTO members(slug,channel_id,english_name,korean_name,org,sync_source,aliases)
 VALUES ('dup-holo','UC-dup-holo','Twin Name','쌍둥이','Hololive','manual','{"ko":["쌍둥"],"ja":[]}'),
 ('dup-other','UC-dup-other','Twin Name','쌍둥이','Nijisanji','manual','{"ko":["쌍둥"],"ja":["ツイン"]}')`)
	if err != nil {
		t.Fatal(err)
	}

	expectations := newPointLookupExpectations(t, repo,
		[]string{"개인", "a person", "HOLOAN OWNER", "쌍둥", "쌍둥이", "twin name", "ツイン"},
		[]string{sharedReviewChannel, "UC-dup-holo", "UC-dup-other"},
	)

	if expectations.wantAlias["쌍둥"] == expectations.wantAlias["ツイン"] {
		t.Fatal("fixture must resolve the shared alias and the org-specific alias to different members")
	}

	expectations.assert(t, "cold", withTestEpochAuthority(newMemberCache(repo, slog.New(slog.DiscardHandler), CacheConfig{})))

	warm := withTestEpochAuthority(newMemberCache(repo, slog.New(slog.DiscardHandler), CacheConfig{}))
	if err := warm.WarmUpCache(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.pool.Exec(ctx, `DELETE FROM members`); err != nil {
		t.Fatal(err)
	}

	expectations.assert(t, "warm", warm)
}
