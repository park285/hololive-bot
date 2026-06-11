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
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type memberRow struct {
	id              int
	slug            string
	channelID       *string
	englishName     string
	japaneseName    *string
	koreanName      *string
	shortKoreanName *string
	status          string
	isGraduated     bool
	aliasesJSON     []byte
	photo           *string
	org             string
	suborg          *string
	syncSource      string
	twitchUserID    *string
	birthday        *time.Time
	debutDate       *time.Time
}

type memberRowScanner interface {
	Scan(dest ...any) error
}

func scanMemberQueryRow(scanner memberRowScanner) (memberRow, error) {
	var row memberRow
	err := scanner.Scan(
		&row.id,
		&row.slug,
		&row.channelID,
		&row.englishName,
		&row.japaneseName,
		&row.koreanName,
		&row.shortKoreanName,
		&row.status,
		&row.isGraduated,
		&row.aliasesJSON,
		&row.org,
		&row.suborg,
		&row.syncSource,
		&row.twitchUserID,
	)
	return row, err
}

func scanMemberFullRow(scanner memberRowScanner) (memberRow, error) {
	var row memberRow
	err := scanner.Scan(
		&row.id,
		&row.slug,
		&row.channelID,
		&row.englishName,
		&row.japaneseName,
		&row.koreanName,
		&row.shortKoreanName,
		&row.status,
		&row.isGraduated,
		&row.aliasesJSON,
		&row.photo,
		&row.org,
		&row.suborg,
		&row.syncSource,
		&row.twitchUserID,
	)
	return row, err
}

func scanMemberPhotoQueryRow(scanner memberRowScanner) (memberRow, error) {
	var row memberRow
	err := scanner.Scan(
		&row.id,
		&row.channelID,
		&row.englishName,
		&row.japaneseName,
		&row.koreanName,
		&row.shortKoreanName,
		&row.isGraduated,
		&row.aliasesJSON,
		&row.photo,
		&row.org,
		&row.suborg,
		&row.syncSource,
		&row.twitchUserID,
	)
	return row, err
}

func (r *Repository) parseMemberRow(row memberRow) (*domain.Member, error) {
	member, err := r.scanMember(
		row.id,
		row.slug,
		row.channelID,
		row.englishName,
		row.japaneseName,
		row.koreanName,
		row.shortKoreanName,
		row.status,
		row.isGraduated,
		row.aliasesJSON,
		row.photo,
		row.org,
		row.suborg,
		row.syncSource,
		row.twitchUserID,
	)
	if err != nil {
		return nil, err
	}
	if row.birthday != nil {
		member.Birthday = row.birthday
	}
	if row.debutDate != nil {
		member.DebutDate = row.debutDate
	}
	return member, nil
}

func (r *Repository) parseMemberPhotoRow(row memberRow) (*domain.Member, error) {
	member, err := r.scanMemberWithPhoto(
		row.id,
		row.channelID,
		row.englishName,
		row.japaneseName,
		row.koreanName,
		row.shortKoreanName,
		row.isGraduated,
		row.aliasesJSON,
		row.photo,
		row.org,
		row.suborg,
		row.syncSource,
		row.twitchUserID,
	)
	if err != nil {
		return nil, err
	}
	if row.birthday != nil {
		member.Birthday = row.birthday
	}
	if row.debutDate != nil {
		member.DebutDate = row.debutDate
	}
	return member, nil
}

func (r *Repository) querySingleMember(ctx context.Context, query string, args ...any) (*domain.Member, error) {
	row, err := scanMemberQueryRow(r.pool.QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return r.parseMemberRow(row)
}

func (r *Repository) querySingleMemberWithPhoto(ctx context.Context, query string, args ...any) (*domain.Member, error) {
	row, err := scanMemberPhotoQueryRow(r.pool.QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return r.parseMemberPhotoRow(row)
}

func (r *Repository) collectAllMembersFromRows(rows pgx.Rows) ([]*domain.Member, error) {
	return collectJoinedRows(rows, "rows iteration error", func(rows pgxRows) (*domain.Member, error) {
		row, err := scanMemberFullRow(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan member row: %w", err)
		}
		member, err := r.parseMemberRow(row)
		if err != nil {
			return nil, fmt.Errorf("failed to parse member row %q: %w", row.englishName, err)
		}
		return member, nil
	})
}

type photoMemberRow struct {
	channelID *string
	member    *domain.Member
}

func (r *Repository) collectMembersWithPhotoFromRows(rows pgx.Rows) (map[string]*domain.Member, error) {
	collected, err := collectJoinedRows(rows, "rows iteration error", func(rows pgxRows) (photoMemberRow, error) {
		channelID, member, scanErr := r.collectMemberWithPhotoRow(rows)
		if scanErr != nil {
			return photoMemberRow{}, scanErr
		}
		return photoMemberRow{channelID: channelID, member: member}, nil
	})

	result := make(map[string]*domain.Member)
	for _, row := range collected {
		if row.channelID != nil {
			result[*row.channelID] = row.member
		}
	}

	return result, err
}

func (r *Repository) collectMemberWithPhotoRow(rows pgx.Rows) (*string, *domain.Member, error) {
	row, err := scanMemberPhotoQueryRow(rows)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to scan member row: %w", err)
	}

	member, err := r.parseMemberPhotoRow(row)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse member row %q: %w", row.englishName, err)
	}

	return row.channelID, member, nil
}

// scanMember: DB 조회 결과를 domain.Member로 변환함
func (r *Repository) scanMember(
	id int,
	_ string,
	channelID *string,
	englishName string,
	japaneseName *string,
	koreanName *string,
	shortKoreanName *string,
	_ string,
	isGraduated bool,
	aliasesJSON []byte,
	photo *string,
	org string,
	suborg *string,
	syncSource string,
	twitchUserID *string,
) (*domain.Member, error) {
	return r.scanMemberWithPhoto(id, channelID, englishName, japaneseName, koreanName, shortKoreanName, isGraduated, aliasesJSON, photo, org, suborg, syncSource, twitchUserID)
}

// scanMemberWithPhoto: DB 조회 결과를 domain.Member로 변환 (photo 포함)
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
	if err := json.Unmarshal(aliasesJSON, &aliases); err != nil {
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
	return collectJoinedRows(rows, "rows iteration error", func(rows pgxRows) (*domain.Member, error) {
		row, err := scanMemberQueryRow(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan member row: %w", err)
		}
		member, err := r.parseMemberRow(row)
		if err != nil {
			return nil, fmt.Errorf("failed to parse member row %q: %w", row.englishName, err)
		}
		return member, nil
	})
}
