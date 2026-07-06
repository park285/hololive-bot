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

	"github.com/park285/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (r *Repository) AddAlias(ctx context.Context, memberID int, aliasType, alias string) error {
	if aliasType != "ko" && aliasType != "ja" {
		return fmt.Errorf("invalid alias type: %s (must be 'ko' or 'ja')", aliasType)
	}

	tag, err := r.pool.Exec(ctx, mustSQL("repository_mutation_0037_01.sql"), memberID, aliasType, alias)
	if err != nil {
		return fmt.Errorf("failed to add alias: %w", err)
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
		return fmt.Errorf("failed to remove alias: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("member %d not found", memberID)
	}

	return nil
}

func (r *Repository) SetGraduation(ctx context.Context, memberID int, isGraduated bool) error {
	tag, err := r.pool.Exec(ctx, mustSQL("repository_mutation_0095_03.sql"), memberID, isGraduated)
	if err != nil {
		return fmt.Errorf("failed to update graduation status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("member %d not found", memberID)
	}
	return nil
}

func (r *Repository) UpdateChannelID(ctx context.Context, memberID int, channelID string) error {
	tag, err := r.pool.Exec(ctx, mustSQL("repository_mutation_0111_04.sql"), memberID, channelID)
	if err != nil {
		return fmt.Errorf("failed to update channel ID: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("member %d not found", memberID)
	}
	return nil
}

func (r *Repository) UpdateMemberName(ctx context.Context, memberID int, name string) error {
	tag, err := r.pool.Exec(ctx, mustSQL("repository_mutation_0126_05.sql"), memberID, name)
	if err != nil {
		return fmt.Errorf("failed to update member name: %w", err)
	}
	if tag.RowsAffected() == 0 {
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

	// org/sync_source 기본값 설정 (Task 1 요구사항)
	org := "Hololive" // 기존 API 호환을 위한 기본값
	syncSource := "manual"
	status := "active"
	if member.IsGraduated {
		status = "graduated"
	}

	_, err = r.pool.Exec(ctx, mustSQL("repository_mutation_0175_06.sql"), slug, chIDPtr, member.Name, nameJaPtr, nameKoPtr, status, member.IsGraduated, string(aliasesJSON), org, syncSource)
	if err != nil {
		return fmt.Errorf("failed to create member: %w", err)
	}

	return nil
}
