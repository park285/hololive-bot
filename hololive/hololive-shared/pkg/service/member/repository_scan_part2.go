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
	jsonv2 "encoding/json/v2"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (r *Repository) scanMemberWithPhoto(
	id int,
	channelID *string,
	englishName string,
	japaneseName *string,
	koreanName *string,
	shortKoreanName *string,
	isGraduated bool,
	aliasesJSON []byte,
	photo *string,
	org string,
	suborg *string,
	syncSource string,
	twitchUserID *string,
) (*domain.Member, error) {
	var aliases domain.Aliases

	if err := jsonv2.Unmarshal(aliasesJSON, &aliases); err != nil {
		return nil, fmt.Errorf("failed to unmarshal aliases: %w", err)
	}

	member := &domain.Member{
		ID:          id,
		Name:        englishName,
		Aliases:     &aliases,
		IsGraduated: isGraduated,
		Org:         org,
		SyncSource:  syncSource,
	}

	if channelID != nil {
		member.ChannelID = *channelID
	}

	if japaneseName != nil {
		member.NameJa = *japaneseName
	}

	if koreanName != nil {
		member.NameKo = *koreanName
	}

	if shortKoreanName != nil {
		member.ShortKoreanName = *shortKoreanName
	}

	if photo != nil {
		member.Photo = *photo
	}

	if suborg != nil {
		member.Suborg = *suborg
	}

	if twitchUserID != nil {
		member.TwitchUserID = *twitchUserID
	}

	return member, nil
}

func (r *Repository) collectMembersByNameFromRows(rows pgx.Rows) ([]*domain.Member, error) {
	out, err := collectJoinedRows(rows, "rows iteration error", func(rows pgx.Rows) (*domain.Member, error) {
		row, err := scanMemberQueryRow(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan member row: %w", err)
		}

		member, err := r.parseMemberRow(&row)
		if err != nil {
			return nil, fmt.Errorf("failed to parse member row %q: %w", row.englishName, err)
		}

		return member, nil
	})
	if err != nil {
		return out, fmt.Errorf("collect joined rows: %w", err)
	}

	return out, nil
}
