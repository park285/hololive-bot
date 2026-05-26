package member

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func scanCelebrationMemberRow(scanner memberRowScanner) (memberRow, error) {
	var row memberRow
	err := scanner.Scan(
		&row.id, &row.slug, &row.channelID,
		&row.englishName, &row.japaneseName, &row.koreanName, &row.shortKoreanName,
		&row.status, &row.isGraduated, &row.aliasesJSON, &row.photo,
		&row.org, &row.suborg, &row.syncSource, &row.twitchUserID,
		&row.birthday, &row.debutDate,
	)
	return row, err
}

const celebrationMemberColumns = `id, slug, channel_id, english_name, japanese_name, korean_name, short_korean_name,
	status, is_graduated, aliases, photo, org, suborg, sync_source, twitch_user_id,
	birthday, debut_date`

func (r *Repository) FindMembersWithBirthdayOn(ctx context.Context, month, day int) ([]*domain.Member, error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM members
		WHERE EXTRACT(MONTH FROM birthday) = $1
		  AND EXTRACT(DAY FROM birthday) = $2
		  AND status = 'active'
	`, celebrationMemberColumns)

	rows, err := r.pool.Query(ctx, query, month, day)
	if err != nil {
		return nil, fmt.Errorf("find members with birthday: %w", err)
	}
	defer rows.Close()

	return r.collectCelebrationMembersFromRows(rows)
}

func (r *Repository) FindMembersWithAnniversaryOn(ctx context.Context, month, day, referenceYear int) ([]*domain.Member, error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM members
		WHERE EXTRACT(MONTH FROM debut_date) = $1
		  AND EXTRACT(DAY FROM debut_date) = $2
		  AND EXTRACT(YEAR FROM debut_date) < $3
		  AND status = 'active'
	`, celebrationMemberColumns)

	rows, err := r.pool.Query(ctx, query, month, day, referenceYear)
	if err != nil {
		return nil, fmt.Errorf("find members with anniversary: %w", err)
	}
	defer rows.Close()

	return r.collectCelebrationMembersFromRows(rows)
}

func (r *Repository) collectCelebrationMembersFromRows(rows pgx.Rows) ([]*domain.Member, error) {
	var (
		members []*domain.Member
		rowErrs []error
	)
	for rows.Next() {
		row, err := scanCelebrationMemberRow(rows)
		if err != nil {
			rowErrs = append(rowErrs, fmt.Errorf("scan celebration member row: %w", err))
			continue
		}
		member, err := r.parseMemberRow(row)
		if err != nil {
			rowErrs = append(rowErrs, fmt.Errorf("parse celebration member row %q: %w", row.englishName, err))
			continue
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		rowErrs = append(rowErrs, fmt.Errorf("celebration member rows iteration: %w", err))
	}
	if len(rowErrs) > 0 {
		return members, errors.Join(rowErrs...)
	}
	return members, nil
}
