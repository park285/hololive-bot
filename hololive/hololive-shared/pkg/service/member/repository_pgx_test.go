package member

import (
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

func newPGXRepository(t *testing.T) (*Repository, *pgxpool.Pool) {
	t.Helper()

	pool := dbtest.NewPool(t)

	return NewMemberRepository(&databasemocks.Client{
		GetPoolFunc: func() *pgxpool.Pool {
			return pool
		},
	}), pool
}

func TestRepositoryPGXMutationsPreserveMemberSemantics(t *testing.T) {
	repository, pool := newPGXRepository(t)

	memberID := assertCreatedMemberDefaults(t, repository, pool)

	assertGraduationTransitions(t, repository, pool, memberID)
	assertAliasMutations(t, repository, pool, memberID)
	assertMutationErrorsOnMissingTargets(t, repository, memberID)
}

func TestRepositoryPGXCreateMemberWithoutAliasesAcceptsAliasMutations(t *testing.T) {
	repository, pool := newPGXRepository(t)
	ctx := t.Context()

	member := &domain.Member{Name: "Alias Less Member"}
	if replayed, err := repository.CreateMemberOrMatch(ctx, member); err != nil || replayed {
		t.Fatalf("CreateMemberOrMatch(without aliases) = %t, %v; want new member", replayed, err)
	}

	var (
		memberID int
		created  string
	)

	if err := pool.QueryRow(ctx, `SELECT id, aliases::text FROM members WHERE slug = $1`, member.Name).Scan(&memberID, &created); err != nil {
		t.Fatalf("query created member aliases: %v", err)
	}

	if created != `{"ja": [], "ko": []}` {
		t.Fatalf("created aliases = %s, want empty ko/ja arrays", created)
	}

	if err := repository.AddAlias(ctx, memberID, "ja", "エイリアス"); err != nil {
		t.Fatalf("AddAlias() error = %v, want nil", err)
	}

	if err := repository.RemoveAlias(ctx, memberID, "ko", "없음"); err != nil {
		t.Fatalf("RemoveAlias(absent) error = %v, want nil", err)
	}

	var mutated string

	if err := pool.QueryRow(ctx, `SELECT aliases::text FROM members WHERE id = $1`, memberID).Scan(&mutated); err != nil {
		t.Fatalf("query mutated aliases: %v", err)
	}

	if mutated != `{"ja": ["エイリアス"], "ko": []}` {
		t.Fatalf("mutated aliases = %s, want ja alias and empty ko", mutated)
	}

	found, err := repository.FindByAlias(ctx, "エイリアス")
	if err != nil {
		t.Fatalf("FindByAlias() error = %v, want nil", err)
	}

	if found == nil || found.ID != memberID || found.Aliases == nil {
		t.Fatalf("FindByAlias() = %+v, want member %d with aliases", found, memberID)
	}

	if !slices.Equal(found.Aliases.Ja, []string{"エイリアス"}) || len(found.Aliases.Ko) != 0 {
		t.Fatalf("FindByAlias() aliases = %+v, want only the added ja alias", found.Aliases)
	}
}

func assertCreatedMemberDefaults(t *testing.T, repository *Repository, pool *pgxpool.Pool) int {
	t.Helper()

	ctx := t.Context()

	member := &domain.Member{
		ChannelID:   "",
		Name:        "Phase Two Slice A",
		NameJa:      "",
		NameKo:      "",
		IsGraduated: false,
		Aliases:     &domain.Aliases{Ko: []string{"페이즈"}, Ja: []string{"フェーズ"}},
	}
	if replayed, err := repository.CreateMemberOrMatch(ctx, member); err != nil || replayed {
		t.Fatalf("CreateMemberOrMatch() = %t, %v; want new member", replayed, err)
	}

	var (
		memberID       int
		channelID      *string
		japaneseName   *string
		koreanName     *string
		status         string
		org            string
		syncSource     string
		aliasesLiteral string
	)

	if err := pool.QueryRow(ctx, `
		SELECT id, channel_id, japanese_name, korean_name, status, org, sync_source, aliases::text
		FROM members
		WHERE slug = $1
	`, member.Name).Scan(&memberID, &channelID, &japaneseName, &koreanName, &status, &org, &syncSource, &aliasesLiteral); err != nil {
		t.Fatalf("query created member: %v", err)
	}

	if channelID != nil || japaneseName != nil || koreanName != nil {
		t.Fatalf("nullable fields = channel:%v ja:%v ko:%v, want nils", channelID, japaneseName, koreanName)
	}

	if status != "active" || org != "Hololive" || syncSource != "manual" {
		t.Fatalf("defaults = status:%q org:%q sync_source:%q, want active/Hololive/manual", status, org, syncSource)
	}

	if !strings.Contains(aliasesLiteral, `"ko": ["페이즈"]`) || !strings.Contains(aliasesLiteral, `"ja": ["フェーズ"]`) {
		t.Fatalf("aliases = %s, want created aliases", aliasesLiteral)
	}

	return memberID
}

func assertGraduationTransitions(t *testing.T, repository *Repository, pool *pgxpool.Pool, memberID int) {
	t.Helper()

	ctx := t.Context()

	if err := repository.SetGraduation(ctx, memberID, true); err != nil {
		t.Fatalf("SetGraduation(true) error = %v, want nil", err)
	}

	assertMemberGraduationState(t, pool, memberID, "graduated", true)

	if err := repository.SetGraduation(ctx, memberID, false); err != nil {
		t.Fatalf("SetGraduation(false) error = %v, want nil", err)
	}

	assertMemberGraduationState(t, pool, memberID, "active", false)

	graduated := &domain.Member{
		Name:        "Phase Two Graduated",
		IsGraduated: true,
		Aliases:     &domain.Aliases{Ko: []string{}, Ja: []string{}},
	}
	if replayed, err := repository.CreateMemberOrMatch(ctx, graduated); err != nil || replayed {
		t.Fatalf("CreateMemberOrMatch(graduated) = %t, %v; want new member", replayed, err)
	}

	var graduatedID int

	if err := pool.QueryRow(ctx, `SELECT id FROM members WHERE slug = $1`, graduated.Name).Scan(&graduatedID); err != nil {
		t.Fatalf("query graduated member id: %v", err)
	}

	assertMemberGraduationState(t, pool, graduatedID, "graduated", true)
}

func assertAliasMutations(t *testing.T, repository *Repository, pool *pgxpool.Pool, memberID int) {
	t.Helper()

	ctx := t.Context()

	if err := repository.AddAlias(ctx, memberID, "ko", "슬라이스"); err != nil {
		t.Fatalf("AddAlias() error = %v, want nil", err)
	}

	if err := repository.AddAlias(ctx, memberID, "ko", "슬라이스"); err != nil {
		t.Fatalf("AddAlias() duplicate error = %v, want nil", err)
	}

	if err := repository.RemoveAlias(ctx, memberID, "ko", "페이즈"); err != nil {
		t.Fatalf("RemoveAlias() error = %v, want nil", err)
	}

	var koAliases []string

	if err := pool.QueryRow(ctx, `
		SELECT ARRAY(SELECT jsonb_array_elements_text(aliases->'ko') ORDER BY 1)
		FROM members
		WHERE id = $1
	`, memberID).Scan(&koAliases); err != nil {
		t.Fatalf("query aliases: %v", err)
	}

	if len(koAliases) != 1 || koAliases[0] != "슬라이스" {
		t.Fatalf("ko aliases = %#v, want only 슬라이스", koAliases)
	}
}

func assertMutationErrorsOnMissingTargets(t *testing.T, repository *Repository, memberID int) {
	t.Helper()

	ctx := t.Context()

	for name, run := range map[string]func() error{
		"AddAlias":          func() error { return repository.AddAlias(ctx, -1, "ko", "없음") },
		"RemoveAlias":       func() error { return repository.RemoveAlias(ctx, -1, "ko", "없음") },
		"SetGraduation":     func() error { return repository.SetGraduation(ctx, -1, true) },
		"UpdateChannelID":   func() error { return repository.UpdateChannelID(ctx, -1, "UC_NONE") },
		"UpdateMemberName":  func() error { return repository.UpdateMemberName(ctx, -1, "Nobody") },
		"InvalidAliasType":  func() error { return repository.AddAlias(ctx, memberID, "en", "phase") },
		"InvalidRemoveType": func() error { return repository.RemoveAlias(ctx, memberID, "en", "phase") },
	} {
		assertMemberMutationError(t, name, run())
	}
}

func assertMemberGraduationState(t *testing.T, pool *pgxpool.Pool, memberID int, wantStatus string, wantGraduated bool) {
	t.Helper()

	var (
		status      string
		isGraduated bool
	)

	if err := pool.QueryRow(t.Context(), `
		SELECT status, is_graduated
		FROM members
		WHERE id = $1
	`, memberID).Scan(&status, &isGraduated); err != nil {
		t.Fatalf("query graduation state: %v", err)
	}

	if status != wantStatus || isGraduated != wantGraduated {
		t.Fatalf("graduation state = status:%q is_graduated:%v, want %s/%v", status, isGraduated, wantStatus, wantGraduated)
	}
}

func assertMemberMutationError(t *testing.T, name string, err error) {
	t.Helper()

	if err == nil {
		t.Fatalf("%s error = nil, want error", name)
	}

	if strings.HasPrefix(name, "Invalid") {
		if !strings.Contains(err.Error(), "invalid alias type") {
			t.Fatalf("%s error = %q, want invalid alias type", name, err)
		}

		return
	}

	if !strings.Contains(err.Error(), "member -1 not found") {
		t.Fatalf("%s error = %q, want member -1 not found", name, err)
	}
}
