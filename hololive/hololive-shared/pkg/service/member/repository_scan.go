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
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

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
	officialURL     *string
	units           []string
	chzzkChannelID  *string
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
		&row.birthday, &row.debutDate, &row.officialURL, &row.units, &row.chzzkChannelID,
	)
	if err != nil {
		return row, fmt.Errorf("scan member columns: %w", err)
	}

	return row, nil
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
		&row.birthday, &row.debutDate, &row.officialURL, &row.units, &row.chzzkChannelID,
	)
	if err != nil {
		return row, fmt.Errorf("scan member full columns: %w", err)
	}

	return row, nil
}

func (r *Repository) parseMemberRow(row *memberRow) (*domain.Member, error) {
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
		return nil, fmt.Errorf("scan member: %w", err)
	}

	member.Units = row.units
	if row.chzzkChannelID != nil {
		member.ChzzkChannelID = *row.chzzkChannelID
	}

	if row.officialURL != nil {
		member.OfficialURL = *row.officialURL
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
		return nil, domain.ErrMemberNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("scan member query row: %w", err)
	}

	out, err := r.parseMemberRow(&row)
	if err != nil {
		return nil, fmt.Errorf("parse member row: %w", err)
	}

	return out, nil
}

// rows.Err 오류 접두는 조회 경로별로 기존 문구를 유지합니다.
const (
	memberIterLabel            = "rows iteration error"
	celebrationMemberIterLabel = "celebration member rows iteration"
)

func (r *Repository) collectMembersFromRows(
	rows pgx.Rows,
	iterLabel string,
	rowLabel string,
	scan func(memberRowScanner) (memberRow, error),
) ([]*domain.Member, error) {
	out, err := collectJoinedRows(rows, iterLabel, func(rows pgx.Rows) (*domain.Member, error) {
		row, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("scan %s row: %w", rowLabel, err)
		}

		member, err := r.parseMemberRow(&row)
		if err != nil {
			return nil, fmt.Errorf("parse %s row %q: %w", rowLabel, row.englishName, err)
		}

		return member, nil
	})
	if err != nil {
		return out, fmt.Errorf("collect joined rows: %w", err)
	}

	return out, nil
}

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
	out, err := r.scanMemberWithPhoto(id, channelID, englishName, japaneseName, koreanName, shortKoreanName, isGraduated, aliasesJSON, photo, org, suborg, syncSource, twitchUserID)
	if err != nil {
		return nil, fmt.Errorf("scan member with photo: %w", err)
	}

	return out, nil
}

// scanMemberWithPhoto: DB 조회 결과를 domain.Member로 변환 (photo 포함).

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
		return nil, fmt.Errorf("unmarshal aliases: %w", err)
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
