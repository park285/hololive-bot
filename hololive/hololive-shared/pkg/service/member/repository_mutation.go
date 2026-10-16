// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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

	tag, err := r.pool.Exec(ctx, mustSQL("repository_mutation_0037_01.sql"), memberID, aliasType, alias)
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

	tag, err := r.pool.Exec(ctx, mustSQL("repository_mutation_0066_02.sql"), memberID, aliasType, alias)
	if err != nil {
		return fmt.Errorf("remove alias: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("member %d not found", memberID)
	}

	return nil
}

func (r *Repository) SetGraduation(ctx context.Context, memberID int, isGraduated bool) error {
	tag, err := r.pool.Exec(ctx, mustSQL("repository_mutation_0095_03.sql"), memberID, isGraduated, graduationStatus(isGraduated))
	if err != nil {
		return fmt.Errorf("update graduation status: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("member %d not found", memberID)
	}

	return nil
}

func (r *Repository) UpdateChannelID(ctx context.Context, memberID int, channelID string) error {
	tag, err := r.pool.Exec(ctx, mustSQL("repository_mutation_0111_04.sql"), memberID, channelID)
	if err != nil {
		return fmt.Errorf("update channel ID: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("member %d not found", memberID)
	}

	return nil
}

func (r *Repository) UpdateMemberName(ctx context.Context, memberID int, name string) error {
	tag, err := r.pool.Exec(ctx, mustSQL("repository_mutation_0126_05.sql"), memberID, name)
	if err != nil {
		return fmt.Errorf("update member name: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("member %d not found", memberID)
	}

	return nil
}

// ErrCreateConflict는 같은 slug의 저장 내용이 생성 요청과 다름을 나타낸다.
var ErrCreateConflict = errors.New("member already exists with different values")

// CreateMember는 멤버 기본 정보와 채널·기수·공식 링크·날짜를 저장한다. 캐시 갱신은 호출자가 수행한다.
func (r *Repository) CreateMember(ctx context.Context, member *domain.Member) error {
	_, err := r.createMember(ctx, member, false)
	return err
}

// CreateMemberOrMatch는 slug 유일성 충돌일 때만 모든 저장 필드가 같은 정본을 확인한다.
// 일치하면 삽입을 재시도하지 않고 기존 ID와 replayed=true를 반환한다.
func (r *Repository) CreateMemberOrMatch(ctx context.Context, member *domain.Member) (bool, error) {
	return r.createMember(ctx, member, true)
}

func (r *Repository) createMember(ctx context.Context, member *domain.Member, allowReplay bool) (bool, error) {
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

	err = r.pool.QueryRow(ctx, mustSQL("repository_mutation_0175_06.sql"), args...).Scan(&member.ID)
	if err == nil {
		return false, nil
	}

	pgErr, unique := errors.AsType[*pgconn.PgError](err)
	if !allowReplay || !unique || pgErr.Code != "23505" || pgErr.ConstraintName != "idx_members_slug" {
		return false, fmt.Errorf("create member: %w", err)
	}

	err = r.pool.QueryRow(ctx, mustSQL("match_created_member.sql"), args...).Scan(&member.ID)
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
