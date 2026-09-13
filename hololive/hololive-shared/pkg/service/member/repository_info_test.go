package member

import (
	"errors"
	"slices"
	"testing"
	"time"
)

func TestRepositoryMemberInfoPreservesSharedChannelIdentity(t *testing.T) {
	repository, pool := newPGXRepository(t)
	ctx := t.Context()

	const sharedChannel = "UC-info-shared"

	_, err := pool.Exec(ctx, `INSERT INTO members(slug,channel_id,english_name,org,sync_source,aliases,units,birthday,debut_date,official_link)
 VALUES ('info-a',$1,'Info A','Hololive','manual','{}',ARRAY['AREA15'],NULL,DATE '2025-10-15','https://example.com/a'),
 ('info-b',$1,'Info B','Hololive','manual','{}',ARRAY['Gen 1','GAMERS'],DATE '2000-02-29',DATE '2025-12-30','https://example.com/b')`, sharedChannel)
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"Info A", "Info B"} {
		member, findErr := repository.FindByName(ctx, name)
		if findErr != nil {
			t.Fatal(findErr)
		}

		if member.Name != name || member.ChannelID != sharedChannel || member.DebutDate == nil || member.OfficialURL == "" || len(member.Units) == 0 {
			t.Fatalf("incomplete info: %+v", member)
		}
	}

	member, err := repository.FindByName(ctx, "Info B")
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(member.Units, []string{"Gen 1", "GAMERS"}) || member.Birthday.Format("01-02") != "02-29" {
		t.Fatalf("lost dates/units: %+v", member)
	}

	assertMemberInfoSnapshot(t, repository, sharedChannel)
	assertMemberInfoAnniversary(t, repository)
}

func assertMemberInfoSnapshot(t *testing.T, repository *Repository, sharedChannel string) {
	t.Helper()

	ctx := t.Context()

	all, err := repository.GetAllMembers(ctx)
	if err != nil {
		t.Fatal(err)
	}

	found := 0

	for _, member := range all {
		if member.ChannelID == sharedChannel {
			found++

			if len(member.Units) == 0 || member.OfficialURL == "" {
				t.Fatalf("snapshot dropped info: %+v", member)
			}
		}
	}

	if found != 2 {
		t.Fatalf("shared identities = %d", found)
	}
}

func assertMemberInfoAnniversary(t *testing.T, repository *Repository) {
	t.Helper()

	ctx := t.Context()

	calendar, err := repository.FindMembersWithCelebrationsInMonth(ctx, int(time.December), 2026)
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range calendar {
		if entry.Member.Name == "Info B" {
			if entry.Ordinal != 1 || len(entry.Member.Units) != 2 {
				t.Fatalf("calendar lost info: %+v", entry)
			}

			return
		}
	}

	t.Fatal("anniversary member absent")
}

func TestMemberAliasLookupTreatsWildcardsLiterally(t *testing.T) {
	repo, pool := newPGXRepository(t)
	ctx := t.Context()

	_, err := pool.Exec(ctx, `INSERT INTO members(slug,english_name,korean_name,org,sync_source,aliases) VALUES ('wild-normal','Normal','가','Hololive','manual','{}')`)
	if err != nil {
		t.Fatal(err)
	}

	for _, query := range []string{"%", "_", "Nor%"} {
		if _, lookupErr := repo.FindByAlias(ctx, query); !errors.Is(lookupErr, ErrMemberNotFound) {
			t.Errorf("query %q error=%v", query, lookupErr)
		}
	}

	_, err = pool.Exec(ctx, `INSERT INTO members(slug,english_name,org,sync_source,aliases) VALUES ('wild-literal','100%_literal','Hololive','manual','{}')`)
	if err != nil {
		t.Fatal(err)
	}

	got, err := repo.FindByAlias(ctx, "100%_LITERAL")
	if err != nil {
		t.Fatal(err)
	}

	if got.Name != "100%_literal" {
		t.Fatalf("literal lookup=%+v", got)
	}
}
