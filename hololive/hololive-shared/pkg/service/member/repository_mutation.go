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
	"context"
	"fmt"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (r *Repository) AddAlias(ctx context.Context, memberID int, aliasType string, alias string) error {
	if aliasType != "ko" && aliasType != "ja" {
		return fmt.Errorf("invalid alias type: %s (must be 'ko' or 'ja')", aliasType)
	}

	path := fmt.Sprintf("{%s}", aliasType)
	result := r.gormDB.WithContext(ctx).
		Model(&Model{}).
		Where("id = ?", memberID).
		Update("aliases", gorm.Expr(`
			jsonb_set(
				COALESCE(aliases::jsonb, '{}'::jsonb),
				CAST(? AS text[]),
				CASE
					WHEN jsonb_exists(COALESCE(aliases::jsonb -> ?, '[]'::jsonb), CAST(? AS text)) THEN COALESCE(aliases::jsonb -> ?, '[]'::jsonb)
					ELSE COALESCE(aliases::jsonb -> ?, '[]'::jsonb) || jsonb_build_array(?)
				END,
				true
			)
		`, path, aliasType, alias, aliasType, aliasType, alias))
	if result.Error != nil {
		return fmt.Errorf("failed to add alias: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("member %d not found", memberID)
	}

	return nil
}

func (r *Repository) RemoveAlias(ctx context.Context, memberID int, aliasType string, alias string) error {
	if aliasType != "ko" && aliasType != "ja" {
		return fmt.Errorf("invalid alias type: %s (must be 'ko' or 'ja')", aliasType)
	}

	path := fmt.Sprintf("{%s}", aliasType)
	result := r.gormDB.WithContext(ctx).
		Model(&Model{}).
		Where("id = ?", memberID).
		Update("aliases", gorm.Expr(`
			jsonb_set(
				COALESCE(aliases::jsonb, '{}'::jsonb),
				CAST(? AS text[]),
				COALESCE(
					(
						SELECT jsonb_agg(elem)
						FROM jsonb_array_elements(COALESCE(aliases::jsonb -> ?, '[]'::jsonb)) AS elem
						WHERE elem <> to_jsonb(CAST(? AS text))
					),
					'[]'::jsonb
				),
				true
			)
		`, path, aliasType, alias))
	if result.Error != nil {
		return fmt.Errorf("failed to remove alias: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("member %d not found", memberID)
	}

	return nil
}

func (r *Repository) SetGraduation(ctx context.Context, memberID int, isGraduated bool) error {
	result := r.gormDB.WithContext(ctx).
		Model(&Model{}).
		Where("id = ?", memberID).
		Update("is_graduated", isGraduated)
	if result.Error != nil {
		return fmt.Errorf("failed to update graduation status: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("member %d not found", memberID)
	}
	return nil
}

func (r *Repository) UpdateChannelID(ctx context.Context, memberID int, channelID string) error {
	result := r.gormDB.WithContext(ctx).
		Model(&Model{}).
		Where("id = ?", memberID).
		Update("channel_id", channelID)
	if result.Error != nil {
		return fmt.Errorf("failed to update channel ID: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("member %d not found", memberID)
	}
	return nil
}

func (r *Repository) UpdateMemberName(ctx context.Context, memberID int, name string) error {
	result := r.gormDB.WithContext(ctx).
		Model(&Model{}).
		Where("id = ?", memberID).
		Update("english_name", name)
	if result.Error != nil {
		return fmt.Errorf("failed to update member name: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("member %d not found", memberID)
	}
	return nil
}

func (r *Repository) CreateMember(ctx context.Context, member *domain.Member) error {
	aliasesJSON, err := json.Marshal(member.Aliases)
	if err != nil {
		return fmt.Errorf("failed to marshal aliases: %w", err)
	}

	// domain.Member가 Slug를 노출하지 않으므로 Name을 Slug로 사용함
	slug := member.Name

	chID := member.ChannelID
	var chIDPtr *string
	if chID != "" {
		chIDPtr = &chID
	}

	var nameJaPtr *string
	if member.NameJa != "" {
		val := member.NameJa
		nameJaPtr = &val
	}

	var nameKoPtr *string
	if member.NameKo != "" {
		val := member.NameKo
		nameKoPtr = &val
	}

	// Add default values for org/sync_source (Task 1 requirement)
	org := "Hololive" // 기존 API 호환을 위한 기본값
	syncSource := "manual"

	m := Model{
		Slug:         slug,
		ChannelID:    chIDPtr,
		EnglishName:  member.Name,
		JapaneseName: nameJaPtr,
		KoreanName:   nameKoPtr,
		Status:       "active",
		IsGraduated:  member.IsGraduated,
		Aliases:      aliasesJSON,
		Org:          org,
		Suborg:       nil,
		SyncSource:   syncSource,
	}

	if err := r.gormDB.WithContext(ctx).Create(&m).Error; err != nil {
		return fmt.Errorf("failed to create member: %w", err)
	}

	return nil
}
