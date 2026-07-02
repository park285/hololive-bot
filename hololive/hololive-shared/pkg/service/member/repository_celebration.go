package member

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (r *Repository) FindMembersWithCelebrationsInMonth(ctx context.Context, month, referenceYear int) ([]domain.CalendarEntry, error) {
	query := fmt.Sprintf(`
		SELECT %s, 'birthday' AS celebration_kind,
			EXTRACT(DAY FROM birthday)::int AS celebration_day
		FROM members
		WHERE EXTRACT(MONTH FROM birthday) = $1
		  AND status = 'active'
		UNION ALL
		SELECT %s, 'anniversary' AS celebration_kind,
			EXTRACT(DAY FROM debut_date)::int AS celebration_day
		FROM members
		WHERE EXTRACT(MONTH FROM debut_date) = $1
		  AND EXTRACT(YEAR FROM debut_date) < $2
		  AND status = 'active'
		ORDER BY celebration_day, celebration_kind, id
	`, celebrationMemberColumns, celebrationMemberColumns)

	rows, err := r.pool.Query(ctx, query, month, referenceYear)
	if err != nil {
		return nil, fmt.Errorf("find members with celebrations in month %d: %w", month, err)
	}
	defer rows.Close()

	return r.collectCalendarEntriesFromRows(rows, referenceYear)
}

func scanCalendarRow(scanner memberRowScanner, kindStr *string, day *int) (memberRow, error) {
	var row memberRow
	err := scanner.Scan(
		&row.id, &row.slug, &row.channelID,
		&row.englishName, &row.japaneseName, &row.koreanName, &row.shortKoreanName,
		&row.status, &row.isGraduated, &row.aliasesJSON, &row.photo,
		&row.org, &row.suborg, &row.syncSource, &row.twitchUserID,
		&row.birthday, &row.debutDate,
		kindStr, day,
	)
	return row, err
}

func (r *Repository) collectCalendarEntriesFromRows(rows pgx.Rows, referenceYear int) ([]domain.CalendarEntry, error) {
	return collectJoinedRows(rows, "calendar rows iteration", func(rows pgxRows) (domain.CalendarEntry, error) {
		var (
			kindStr string
			day     int
		)
		row, err := scanCalendarRow(rows, &kindStr, &day)
		if err != nil {
			return domain.CalendarEntry{}, fmt.Errorf("scan calendar row: %w", err)
		}
		member, err := r.parseMemberRow(&row)
		if err != nil {
			return domain.CalendarEntry{}, fmt.Errorf("parse calendar member row %q: %w", row.englishName, err)
		}
		return buildCalendarEntry(kindStr, member, day, referenceYear), nil
	})
}

func buildCalendarEntry(kindStr string, member *domain.Member, day, referenceYear int) domain.CalendarEntry {
	entry := domain.CalendarEntry{Kind: domain.CelebrationKind(kindStr), Member: member, Day: day}
	if entry.Kind == domain.CelebrationKindAnniversary && member.DebutDate != nil {
		entry.Ordinal = referenceYear - member.DebutDate.Year()
	}
	return entry
}

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
	return collectJoinedRows(rows, "celebration member rows iteration", func(rows pgxRows) (*domain.Member, error) {
		row, err := scanCelebrationMemberRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan celebration member row: %w", err)
		}
		member, err := r.parseMemberRow(&row)
		if err != nil {
			return nil, fmt.Errorf("parse celebration member row %q: %w", row.englishName, err)
		}
		return member, nil
	})
}
