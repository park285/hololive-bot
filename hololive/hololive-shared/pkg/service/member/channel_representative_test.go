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
 VALUES ('review-owner',$1,'holoAN owner','홀로아나','Hololive','manual','{"ko":[],"ja":[]}'),
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

	if _, err := cache.FindByAlias(ctx, "없는별칭"); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Fatalf("%s FindByAlias(missing) error = %v, want domain.ErrMemberNotFound", label, err)
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

// 어댑터 단건 조회는 미존재를 domain.ErrMemberNotFound로, PostgreSQL 실패를 그와 다른 오류로 돌려준다. 미존재는
// 캐시하지 않으므로 나중에 생긴 행은 곧바로 보인다.
func TestServiceAdapterSeparatesNotFoundFromRepositoryFailure(t *testing.T) {
	repo, pool := newPGXRepository(t)
	ctx := t.Context()
	adapter := NewMemberServiceAdapter(withTestEpochAuthority(newMemberCache(repo, slog.New(slog.DiscardHandler), CacheConfig{})))

	_, channelErr := adapter.FindMemberByChannelID(ctx, "UC-missing")
	_, nameErr := adapter.FindMemberByName(ctx, "Missing")
	_, aliasErr := adapter.FindMemberByAlias(ctx, "없음")

	for label, err := range map[string]error{"channel": channelErr, "name": nameErr, "alias": aliasErr} {
		if !errors.Is(err, domain.ErrMemberNotFound) {
			t.Fatalf("%s missing error = %v, want domain.ErrMemberNotFound", label, err)
		}
	}

	if _, err := pool.Exec(ctx, `INSERT INTO members(slug,channel_id,english_name,org,sync_source,aliases)
 VALUES ('late-row','UC-missing','Missing','Hololive','manual','{"ko":["없음"],"ja":[]}')`); err != nil {
		t.Fatal(err)
	}

	if got, err := adapter.FindMemberByChannelID(ctx, "UC-missing"); err != nil || got.Name != "Missing" {
		t.Fatalf("late row lookup = %+v, %v; want negative result not cached", got, err)
	}

	pool.Close()

	_, failure := adapter.FindMemberByAlias(ctx, "없음")
	if failure == nil || errors.Is(failure, domain.ErrMemberNotFound) {
		t.Fatalf("closed pool alias error = %v, want repository failure distinct from not-found", failure)
	}
}

// 같은 english_name이 여럿이면 repository(SQL ORDER BY id), cold cache, epoch 우회, warm snapshot 모두 가장 작은 영속 ID를
// 돌려준다. 먼저 넣은 행을 갱신해 heap 순서가 ID 순서와 달라지게 만든다.
func TestDuplicateNameLookupsAgreeOnSmallestID(t *testing.T) {
	repo, pool := newPGXRepository(t)
	ctx := t.Context()

	if _, err := pool.Exec(ctx, `INSERT INTO members(slug,channel_id,english_name,org,sync_source,aliases)
 VALUES ('dup-first','UC-dup-first','Dup Name','Hololive','manual','{"ko":[],"ja":[]}'),
 ('dup-second','UC-dup-second','Dup Name','Nijisanji','manual','{"ko":[],"ja":[]}')`); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `UPDATE members SET short_korean_name = '갱신' WHERE slug = 'dup-first'`); err != nil {
		t.Fatal(err)
	}

	var wantID int

	if err := pool.QueryRow(ctx, `SELECT min(id) FROM members WHERE english_name = 'Dup Name'`).Scan(&wantID); err != nil {
		t.Fatal(err)
	}

	assertName := func(label string, got *domain.Member, err error) {
		t.Helper()

		if err != nil || got.ID != wantID {
			t.Fatalf("%s GetByName(Dup Name) = %+v, %v; want smallest ID %d", label, got, err, wantID)
		}
	}

	got, err := repo.FindByName(ctx, "Dup Name")
	assertName("repository", got, err)

	cold := withTestEpochAuthority(newMemberCache(repo, slog.New(slog.DiscardHandler), CacheConfig{}))

	got, err = cold.GetByName(ctx, "Dup Name")
	assertName("cold", got, err)

	bypass := withTestEpochAuthority(newMemberCache(repo, slog.New(slog.DiscardHandler), CacheConfig{}))
	bypass.authorityHealthy.Store(false)

	got, err = bypass.GetByName(ctx, "Dup Name")
	assertName("bypass", got, err)

	warm := withTestEpochAuthority(newMemberCache(repo, slog.New(slog.DiscardHandler), CacheConfig{}))
	if warmErr := warm.WarmUpCache(ctx); warmErr != nil {
		t.Fatal(warmErr)
	}

	if _, deleteErr := pool.Exec(ctx, `DELETE FROM members`); deleteErr != nil {
		t.Fatal(deleteErr)
	}

	got, err = warm.GetByName(ctx, "Dup Name")
	assertName("warm snapshot", got, err)
}
