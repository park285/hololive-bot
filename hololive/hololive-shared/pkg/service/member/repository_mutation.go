package member

import (
	"cmp"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (r *Repository) AddAlias(ctx context.Context, memberID int, aliasType, alias string) error {
	if aliasType != "ko" && aliasType != "ja" {
		return fmt.Errorf("invalid alias type: %s (must be 'ko' or 'ja')", aliasType)
	}

	tag, err := r.pool.Exec(ctx, addAliasSQL, memberID, aliasType, alias)
	if err != nil {
		return fmt.Errorf("add alias: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("member %d not found", memberID)
	}

	return nil
}

func (r *Repository) RemoveAlias(ctx context.Context, memberID int, aliasType, alias string) error {
	if aliasType != "ko" && aliasType != "ja" {
		return fmt.Errorf("invalid alias type: %s (must be 'ko' or 'ja')", aliasType)
	}

	tag, err := r.pool.Exec(ctx, removeAliasSQL, memberID, aliasType, alias)
	if err != nil {
		return fmt.Errorf("remove alias: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("member %d not found", memberID)
	}

	return nil
}

func (r *Repository) SetGraduation(ctx context.Context, memberID int, isGraduated bool) error {
	tag, err := r.pool.Exec(ctx, setGraduationSQL, memberID, isGraduated, graduationStatus(isGraduated))
	if err != nil {
		return fmt.Errorf("update graduation status: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("member %d not found", memberID)
	}

	return nil
}

func (r *Repository) UpdateChannelID(ctx context.Context, memberID int, channelID string) error {
	tag, err := r.pool.Exec(ctx, updateChannelIDSQL, memberID, channelID)
	if err != nil {
		return fmt.Errorf("update channel ID: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("member %d not found", memberID)
	}

	return nil
}

func (r *Repository) UpdateMemberName(ctx context.Context, memberID int, name string) error {
	tag, err := r.pool.Exec(ctx, updateMemberNameSQL, memberID, name)
	if err != nil {
		return fmt.Errorf("update member name: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("member %d not found", memberID)
	}

	return nil
}

var ErrCreateConflict = errors.New("member already exists with different values")

func (r *Repository) CreateMemberOrMatch(ctx context.Context, member *domain.Member) (bool, error) {
	if member == nil {
		return false, errors.New("member is required")
	}

	aliasesJSON, err := jsonv2.Marshal(cmp.Or(member.Aliases, &domain.Aliases{}))
	if err != nil {
		return false, fmt.Errorf("marshal aliases: %w", err)
	}

	args := []any{
		member.Name, member.ChannelID, member.Name, member.NameJa, member.NameKo,
		graduationStatus(member.IsGraduated), member.IsGraduated, string(aliasesJSON), member.GetOrg(), "manual",
		member.Units, member.OfficialURL, member.Birthday, member.DebutDate,
		member.ShortKoreanName, member.ChzzkChannelID, member.TwitchUserID,
	}

	err = r.pool.QueryRow(ctx, createMemberSQL, args...).Scan(&member.ID)
	if err == nil {
		return false, nil
	}

	pgErr, unique := errors.AsType[*pgconn.PgError](err)
	if !unique || pgErr.Code != "23505" || pgErr.ConstraintName != "idx_members_slug" {
		return false, fmt.Errorf("create member: %w", err)
	}

	err = r.pool.QueryRow(ctx, matchCreatedMemberSQL, args...).Scan(&member.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrCreateConflict
	}

	if err != nil {
		return false, fmt.Errorf("match created member: %w", err)
	}

	return true, nil
}

func graduationStatus(graduated bool) string {
	if graduated {
		return "graduated"
	}

	return "active"
}
