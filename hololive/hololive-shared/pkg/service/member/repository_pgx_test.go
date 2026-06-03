package member

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

func newPGXRepository(t *testing.T) (*Repository, *pgxpool.Pool) {
	t.Helper()

	pool := dbtest.NewPool(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewMemberRepository(&databasemocks.Client{
		GetPoolFunc: func() *pgxpool.Pool {
			return pool
		},
	}, logger), pool
}

func TestRepositoryPGXMutationsPreserveMemberSemantics(t *testing.T) {
	ctx := context.Background()
	repository, pool := newPGXRepository(t)

	member := &domain.Member{
		ChannelID:   "",
		Name:        "Phase Two Slice A",
		NameJa:      "",
		NameKo:      "",
		IsGraduated: false,
		Aliases:     &domain.Aliases{Ko: []string{"페이즈"}, Ja: []string{"フェーズ"}},
	}
	if err := repository.CreateMember(ctx, member); err != nil {
		t.Fatalf("CreateMember() error = %v, want nil", err)
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

	for name, run := range map[string]func() error{
		"AddAlias":          func() error { return repository.AddAlias(ctx, -1, "ko", "없음") },
		"RemoveAlias":       func() error { return repository.RemoveAlias(ctx, -1, "ko", "없음") },
		"SetGraduation":     func() error { return repository.SetGraduation(ctx, -1, true) },
		"UpdateChannelID":   func() error { return repository.UpdateChannelID(ctx, -1, "UC_NONE") },
		"UpdateMemberName":  func() error { return repository.UpdateMemberName(ctx, -1, "Nobody") },
		"InvalidAliasType":  func() error { return repository.AddAlias(ctx, memberID, "en", "phase") },
		"InvalidRemoveType": func() error { return repository.RemoveAlias(ctx, memberID, "en", "phase") },
	} {
		err := run()
		if err == nil {
			t.Fatalf("%s error = nil, want error", name)
		}
		if strings.HasPrefix(name, "Invalid") {
			if !strings.Contains(err.Error(), "invalid alias type") {
				t.Fatalf("%s error = %q, want invalid alias type", name, err)
			}
			continue
		}
		if !strings.Contains(err.Error(), "member -1 not found") {
			t.Fatalf("%s error = %q, want member -1 not found", name, err)
		}
	}
}

func TestRepositoryPGXPhotoOperationsPreserveSemantics(t *testing.T) {
	ctx := context.Background()
	repository, pool := newPGXRepository(t)

	now := time.Now().Add(-2 * time.Hour)
	fresh := time.Now()
	if _, err := pool.Exec(ctx, `
		INSERT INTO members (slug, channel_id, english_name, japanese_name, korean_name, status, is_graduated, aliases, org, sync_source, photo, photo_updated_at)
		VALUES
			('pgx-photo-null', 'UC_PGX_NULL', 'PGX Photo Null', NULL, NULL, 'active', false, '{"ko":[],"ja":[]}', 'Hololive', 'manual', NULL, NULL),
			('pgx-photo-stale', 'UC_PGX_STALE', 'PGX Photo Stale', NULL, NULL, 'active', false, '{"ko":[],"ja":[]}', 'Hololive', 'manual', 'old', $1),
			('pgx-photo-fresh', 'UC_PGX_FRESH', 'PGX Photo Fresh', NULL, NULL, 'active', false, '{"ko":[],"ja":[]}', 'Hololive', 'manual', 'fresh', $2),
			('pgx-photo-no-channel', NULL, 'PGX Photo No Channel', NULL, NULL, 'active', false, '{"ko":[],"ja":[]}', 'Hololive', 'manual', NULL, NULL)
	`, now, fresh); err != nil {
		t.Fatalf("insert photo fixtures: %v", err)
	}

	if got, err := repository.GetPhotoByChannelID(ctx, "UC_PGX_NULL"); err != nil || got != "" {
		t.Fatalf("GetPhotoByChannelID(null) = %q, %v; want empty nil", got, err)
	}
	if got, err := repository.GetPhotoByChannelID(ctx, "UC_PGX_MISSING"); err != nil || got != "" {
		t.Fatalf("GetPhotoByChannelID(missing) = %q, %v; want empty nil", got, err)
	}

	if err := repository.UpdatePhoto(ctx, "UC_PGX_NULL", "https://example.com/photo=s1024"); err != nil {
		t.Fatalf("UpdatePhoto() error = %v, want nil", err)
	}
	got, err := repository.GetPhotoByChannelID(ctx, "UC_PGX_NULL")
	if err != nil || got != "https://example.com/photo=s1024" {
		t.Fatalf("GetPhotoByChannelID(updated) = %q, %v; want updated photo nil", got, err)
	}

	needingSync, err := repository.GetMembersNeedingPhotoSync(ctx, time.Hour)
	if err != nil {
		t.Fatalf("GetMembersNeedingPhotoSync() error = %v, want nil", err)
	}
	if !containsString(needingSync, "UC_PGX_STALE") || containsString(needingSync, "UC_PGX_FRESH") {
		t.Fatalf("needing sync = %#v, want stale and not fresh", needingSync)
	}
}

func containsString(values []string, want string) bool {
	return slices.Contains(values, want)
}
