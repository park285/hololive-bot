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

// scanMemberRow는 멤버 조회 SQL(get_all_members·find_by_*·기념일 조회)의 공통 열 순서를 읽는다.
func scanMemberRow(scanner memberRowScanner) (memberRow, error) {
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
		return row, fmt.Errorf("scan member columns: %w", err)
	}

	return row, nil
}

// member는 NULL 열을 빈 값으로 둔 domain.Member를 만든다.
func (row *memberRow) member() (*domain.Member, error) {
	var aliases domain.Aliases

	if err := jsonv2.Unmarshal(row.aliasesJSON, &aliases); err != nil {
		return nil, fmt.Errorf("unmarshal aliases: %w", err)
	}

	return &domain.Member{
		ID:              row.id,
		ChannelID:       nullableText(row.channelID),
		Name:            row.englishName,
		Aliases:         &aliases,
		NameJa:          nullableText(row.japaneseName),
		NameKo:          nullableText(row.koreanName),
		ShortKoreanName: nullableText(row.shortKoreanName),
		IsGraduated:     row.isGraduated,
		Photo:           nullableText(row.photo),
		Units:           row.units,
		OfficialURL:     nullableText(row.officialURL),
		Org:             row.org,
		Suborg:          nullableText(row.suborg),
		SyncSource:      row.syncSource,
		ChzzkChannelID:  nullableText(row.chzzkChannelID),
		TwitchUserID:    nullableText(row.twitchUserID),
		Birthday:        row.birthday,
		DebutDate:       row.debutDate,
	}, nil
}

func nullableText(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

func (r *Repository) querySingleMember(ctx context.Context, query string, args ...any) (*domain.Member, error) {
	row, err := scanMemberRow(r.pool.QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrMemberNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("scan member query row: %w", err)
	}

	out, err := row.member()
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

// collectMembersFromRows는 rowLabel로 행 단위 scan/parse 오류를, iterLabel로 rows.Err 오류를 구분해 감쌉니다.
// 한 행이 실패해도 나머지 행은 계속 수집하고, 오류는 errors.Join으로 함께 반환합니다.
func collectMembersFromRows(rows pgx.Rows, iterLabel, rowLabel string) ([]*domain.Member, error) {
	out, err := collectJoinedRows(rows, iterLabel, func(rows pgx.Rows) (*domain.Member, error) {
		row, err := scanMemberRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan %s row: %w", rowLabel, err)
		}

		member, err := row.member()
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
