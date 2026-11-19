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

const (
	memberIterLabel            = "rows iteration error"
	celebrationMemberIterLabel = "celebration member rows iteration"
)

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
