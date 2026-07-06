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
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (r *Repository) FindByChannelID(ctx context.Context, channelID string) (*domain.Member, error) {
	query := mustSQL("repository_query_0032_01.sql")

	member, err := r.querySingleMember(ctx, query, channelID)
	if err != nil {
		return nil, fmt.Errorf("failed to query member by channel_id: %w", err)
	}
	return member, nil
}

func (r *Repository) FindByName(ctx context.Context, name string) (*domain.Member, error) {
	query := mustSQL("repository_query_0048_02.sql")

	member, err := r.querySingleMember(ctx, query, name)
	if err != nil {
		return nil, fmt.Errorf("failed to query member by name: %w", err)
	}
	return member, nil
}

func (r *Repository) FindByAlias(ctx context.Context, alias string) (*domain.Member, error) {
	query := mustSQL("repository_query_0064_03.sql")

	member, err := r.querySingleMember(ctx, query, alias)
	if err != nil {
		return nil, fmt.Errorf("failed to query member by alias: %w", err)
	}
	return member, nil
}

func (r *Repository) GetAllChannelIDs(ctx context.Context) ([]string, error) {
	query := mustSQL("repository_query_0084_04.sql")

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query channel ids: %w", err)
	}
	defer rows.Close()

	channelIDs := make([]string, 0, 256)
	for rows.Next() {
		var channelID string
		if err := rows.Scan(&channelID); err != nil {
			r.logger.Warn("Failed to scan channel ID", slog.Any("error", err))
			continue
		}
		channelIDs = append(channelIDs, channelID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return channelIDs, nil
}

func (r *Repository) GetAllMembers(ctx context.Context) ([]*domain.Member, error) {
	query := mustSQL("repository_query_0115_05.sql")

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query all members: %w", err)
	}
	defer rows.Close()

	return r.collectAllMembersFromRows(rows)
}

func (r *Repository) GetMembersWithPhoto(ctx context.Context, channelIDs []string) (map[string]*domain.Member, error) {
	if len(channelIDs) == 0 {
		return make(map[string]*domain.Member), nil
	}

	query := mustSQL("repository_query_0136_06.sql")

	rows, err := r.pool.Query(ctx, query, channelIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to query members with photo: %w", err)
	}
	defer rows.Close()

	return r.collectMembersWithPhotoFromRows(rows)
}

func (r *Repository) GetMemberWithPhotoByChannelID(ctx context.Context, channelID string) (*domain.Member, error) {
	query := mustSQL("repository_query_0153_07.sql")

	member, err := r.querySingleMemberWithPhoto(ctx, query, channelID)
	if err != nil {
		return nil, fmt.Errorf("failed to query member by channel_id: %w", err)
	}
	return member, nil
}

// 검색 대상: english_name, korean_name, aliases->>'ko', aliases->>'ja'
func (r *Repository) FindAllByName(ctx context.Context, name string) ([]*domain.Member, error) {
	query := mustSQL("repository_query_0170_08.sql")

	rows, err := r.pool.Query(ctx, query, name)
	if err != nil {
		return nil, fmt.Errorf("failed to query members by name: %w", err)
	}
	defer rows.Close()

	return r.collectMembersByNameFromRows(rows)
}

func (r *Repository) FindByNameAndOrg(ctx context.Context, name, org string) (*domain.Member, error) {
	query := mustSQL("repository_query_0190_09.sql")

	member, err := r.querySingleMember(ctx, query, name, org)
	if err != nil {
		return nil, fmt.Errorf("failed to query member by name and org: %w", err)
	}
	return member, nil
}
