package scheduleposter

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-api/internal/service/scheduleposter/cardtemplate"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

type RowStatus string

const (
	RowPlanned   RowStatus = "planned"
	RowTentative RowStatus = "tentative"
	RowCancelled RowStatus = "cancelled" //nolint:misspell // DB CHECK와 추출 지시문 enum의 계약값이 영국식 cancelled라, canceled로 바꾸면 저장과 추출 검증이 어긋난다.
	RowOff       RowStatus = "off"
)

func (s RowStatus) Valid() bool {
	switch s {
	case RowPlanned, RowTentative, RowCancelled, RowOff:
		return true
	default:
		return false
	}
}

const MaxRows = 100

const (
	maxRowTextChars = 1000
	maxRowTZChars   = 40
	maxRowListItems = 40
	maxRowsDayShift = 1
)

var rowTimePattern = regexp.MustCompile(`^([01]?\d|2\d):[0-5]\d$`)

type Term struct {
	Source string `json:"source"`
	Ko     string `json:"ko"`
	Basis  string `json:"basis"`
}

type Row struct {
	Ordinal    int       `json:"ordinal"`
	Day        time.Time `json:"-"`
	Start      string    `json:"start_time"`
	End        string    `json:"end_time"`
	TZ         string    `json:"tz"`
	SourceZone string    `json:"source_zone"`
	Status     RowStatus `json:"status"`
	SourceText string    `json:"source_text"`
	Badges     []string  `json:"badges"`
	TitleKo    string    `json:"title_ko"`
	WithKo     []string  `json:"with_ko"`
	PlaceKo    string    `json:"place_ko"`
	TagsKo     []string  `json:"tags_ko"`
	NoteKo     string    `json:"note_ko"`
	Terms      []Term    `json:"terms"`
	DoubtKo    string    `json:"doubt_ko"`
	Warnings   []string  `json:"warnings"`
	EditedAt   time.Time `json:"-"`
}

type PendingRows struct {
	ID           int64
	ChannelID    string
	ImageURL     string
	ImageSHA256  string
	WeekStart    time.Time
	WeekEnd      time.Time
	RowsRevision int64
}

type PublishedSchedule struct {
	PosterID     int64
	WeekStart    time.Time
	WeekEnd      time.Time
	ExtractedAt  time.Time
	PostedAt     time.Time
	RowsPending  bool
	RowsDayShift int
	Rows         []Row
	Changes      []Change
}

func (s *PublishedSchedule) Slots() []Slot {
	return ApplyChanges(s.Rows, s.Changes)
}

func (s *PublishedSchedule) UpdatedAt() time.Time {
	latest := s.ExtractedAt

	for i := range s.Rows {
		latest = laterOf(latest, s.Rows[i].EditedAt)
	}

	for i := range s.Changes {
		if s.Changes[i].Kind.Applied() {
			latest = laterOf(latest, laterOf(s.Changes[i].RecordedAt, s.Changes[i].EditedAt))
		}
	}

	return latest
}

func laterOf(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}

	return a
}

func (s *Store) PendingRows(ctx context.Context, today time.Time, limit int) ([]PendingRows, error) {
	rows, err := pgxdb.Query(ctx, s.pool, pendingRowsSQL, today, min(max(limit, 1), maxListLimit))
	if err != nil {
		return nil, fmt.Errorf("list pending schedule poster rows: %w", err)
	}

	pending, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (PendingRows, error) {
		var item PendingRows

		return item, row.Scan(&item.ID, &item.ChannelID, &item.ImageURL, &item.ImageSHA256, &item.WeekStart, &item.WeekEnd, &item.RowsRevision)
	})
	if err != nil {
		return nil, fmt.Errorf("list pending schedule poster rows: %w", err)
	}

	return pending, nil
}

func (s *Store) WeekTerms(ctx context.Context, item PendingRows) ([]Term, error) {
	rows, err := pgxdb.Query(ctx, s.pool, weekTermsSQL, item.ID, item.WeekStart, item.WeekEnd)
	if err != nil {
		return nil, fmt.Errorf("list schedule poster week terms: %w", err)
	}

	terms, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Term, error) {
		var term Term

		return term, row.Scan(&term.Source, &term.Ko)
	})
	if err != nil {
		return nil, fmt.Errorf("list schedule poster week terms: %w", err)
	}

	return terms, nil
}

func (s *Store) RecordRows(ctx context.Context, item PendingRows, rows []Row, dayShift int) (bool, error) {
	if dayShift < -maxRowsDayShift || dayShift > maxRowsDayShift {
		return false, fmt.Errorf("record schedule poster rows: day shift %d is outside -%d..%d", dayShift, maxRowsDayShift, maxRowsDayShift)
	}

	payload, err := encodeRows(rows)
	if err != nil {
		return false, fmt.Errorf("record schedule poster rows: %w", err)
	}

	recorded := false

	err = dbx.InPgxTxOptions(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		tag, markErr := pgxdb.Exec(ctx, tx, recordRowsSQL, item.ID, item.RowsRevision, dayShift)
		if markErr != nil {
			return fmt.Errorf("mark rows extracted: %w", markErr)
		}

		if tag.RowsAffected() != 1 {
			return nil
		}

		if deleteErr := deleteRows(ctx, tx, item.ID); deleteErr != nil {
			return deleteErr
		}

		if _, insertErr := pgxdb.Exec(ctx, tx, insertRowsSQL, item.ID, payload); insertErr != nil {
			return fmt.Errorf("insert rows: %w", insertErr)
		}

		recorded = true

		return nil
	})
	if err != nil {
		return false, fmt.Errorf("record schedule poster rows: %w", err)
	}

	return recorded, nil
}

func ValidateRows(rows []Row) error {
	if len(rows) == 0 || len(rows) > MaxRows {
		return fmt.Errorf("validate schedule poster rows: rows must be 1..%d, got %d", MaxRows, len(rows))
	}

	for i := range rows {
		if err := rows[i].validate(); err != nil {
			return fmt.Errorf("validate schedule poster rows: row %d: %w", i+1, err)
		}
	}

	return nil
}

func (r *Row) validate() error {
	if !r.Status.Valid() {
		return fmt.Errorf("invalid status %q", r.Status)
	}

	if r.Day.IsZero() || r.Day.Year() < 1 || r.Day.Year() > 9999 {
		return fmt.Errorf("day %q must be a date in years 1..9999", r.Day.Format(time.DateOnly))
	}

	for _, clock := range [...]string{r.Start, r.End} {
		if clock != "" && !rowTimePattern.MatchString(clock) {
			return fmt.Errorf("time %q is not H:MM or HH:MM up to 29:59", clock)
		}
	}

	for _, field := range []struct {
		name     string
		value    string
		min, max int
	}{
		{"source_text", r.SourceText, 1, maxRowTextChars},
		{"title_ko", r.TitleKo, 1, maxRowTextChars},
		{"place_ko", r.PlaceKo, 0, maxRowTextChars},
		{"note_ko", r.NoteKo, 0, maxRowTextChars},
		{"doubt_ko", r.DoubtKo, 0, maxRowTextChars},
		{"tz", r.TZ, 0, maxRowTZChars},
		{"source_zone", r.SourceZone, 0, maxRowTZChars},
	} {
		if n := utf8.RuneCountInString(field.value); n < field.min || n > field.max {
			return fmt.Errorf("%s must be %d..%d characters, got %d", field.name, field.min, field.max, n)
		}
	}

	for _, list := range [...][]string{r.Badges, r.WithKo, r.TagsKo, r.Warnings} {
		if len(list) > maxRowListItems {
			return fmt.Errorf("list must have at most %d items, got %d", maxRowListItems, len(list))
		}
	}

	if !r.storable() {
		return errors.New("text must be valid UTF-8 without NUL")
	}

	return nil
}

func (r *Row) storable() bool {
	texts := []string{r.Start, r.End, r.TZ, r.SourceZone, r.SourceText, r.TitleKo, r.PlaceKo, r.NoteKo, r.DoubtKo}
	for _, list := range [][]string{r.Badges, r.WithKo, r.TagsKo, r.Warnings} {
		texts = append(texts, list...)
	}

	for _, term := range r.Terms {
		texts = append(texts, term.Source, term.Ko, term.Basis)
	}

	for _, text := range texts {
		if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
			return false
		}
	}

	return true
}

type rowRecord struct {
	Row

	Day string `json:"day"`
}

func encodeRows(rows []Row) (string, error) {
	if err := ValidateRows(rows); err != nil {
		return "", err
	}

	numbered := make([]rowRecord, len(rows))

	for i := range rows {
		numbered[i] = rowRecord{Row: rows[i], Day: rows[i].Day.Format(time.DateOnly)}
		numbered[i].Ordinal = i + 1
	}

	payload, err := jsonv2.Marshal(numbered)
	if err != nil {
		return "", fmt.Errorf("encode rows: %w", err)
	}

	return string(payload), nil
}

func (s *Store) RecordRowsFailure(ctx context.Context, item PendingRows, reason string) (bool, error) {
	tag, err := pgxdb.Exec(ctx, s.pool, recordRowsFailureSQL, item.ID, item.RowsRevision, reason)
	if err != nil {
		return false, fmt.Errorf("record schedule poster rows failure: %w", err)
	}

	return tag.RowsAffected() == 1, nil
}

func (s *Store) PublishedRows(ctx context.Context, channelID string, day time.Time) (PublishedSchedule, bool, error) {
	schedule, _, found, err := s.PublishedMemberRows(ctx, 0, channelID, day)

	return schedule, found, err
}

func (s *Store) PublishedMemberRows(ctx context.Context, memberID int, channelID string, day time.Time) (PublishedSchedule, cardtemplate.ID, bool, error) {
	var (
		schedule PublishedSchedule
		template cardtemplate.ID
	)

	err := dbx.InPgxTxOptions(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		rows, queryErr := pgxdb.Query(ctx, tx, publishedRowsSQL, channelID, day, memberID, cardtemplate.Classic)
		if queryErr != nil {
			return fmt.Errorf("load rows: %w", queryErr)
		}

		collected, collectErr := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Row, error) {
			var (
				item     Row
				editedAt *time.Time
			)

			scanErr := row.Scan(&schedule.PosterID, &schedule.WeekStart, &schedule.WeekEnd, &schedule.ExtractedAt,
				&schedule.PostedAt, &schedule.RowsPending, &schedule.RowsDayShift,
				&item.Ordinal, &item.Day, &item.Start, &item.End, &item.TZ, &item.SourceZone, &item.Status, &item.SourceText, &item.Badges,
				&item.TitleKo, &item.WithKo, &item.PlaceKo, &item.TagsKo, &item.NoteKo, &editedAt, &template)
			if editedAt != nil {
				item.EditedAt = *editedAt
			}

			return item, scanErr
		})
		if collectErr != nil {
			return fmt.Errorf("load rows: %w", collectErr)
		}

		if len(collected) == 0 {
			return nil
		}

		schedule.Rows = collected

		changes, changesErr := publishedChanges(ctx, tx, &schedule)
		if changesErr != nil {
			return changesErr
		}

		schedule.Changes = changes

		return nil
	})
	if err != nil {
		return PublishedSchedule{}, "", false, fmt.Errorf("load published schedule poster rows: %w", err)
	}

	if len(schedule.Rows) == 0 {
		return PublishedSchedule{}, "", false, nil
	}

	return schedule, template, true, nil
}

type PosterSchedule struct {
	PublishedSchedule

	Confirmed     bool
	TodayPosterID int64
}

func (s *Store) PosterRows(ctx context.Context, id int64, day time.Time) (PosterSchedule, error) {
	var schedule PosterSchedule

	err := dbx.InPgxTxOptions(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var (
			channelID          string
			weekStart, weekEnd *time.Time
			extractedAt        *time.Time
		)

		err := pgxdb.QueryRow(ctx, tx, posterScheduleSQL, id).Scan(&schedule.PosterID, &channelID, &schedule.Confirmed, &weekStart, &weekEnd,
			&extractedAt, &schedule.PostedAt, &schedule.RowsPending, &schedule.RowsDayShift)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}

		if err != nil {
			return fmt.Errorf("load poster: %w", err)
		}

		if !schedule.Confirmed {
			return nil
		}

		if weekStart != nil && weekEnd != nil {
			schedule.WeekStart, schedule.WeekEnd = *weekStart, *weekEnd
		}

		if err = pgxdb.QueryRow(ctx, tx, publishedPosterSQL, channelID, day).Scan(&schedule.TodayPosterID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("load published poster: %w", err)
		}

		if extractedAt == nil {
			return nil
		}

		schedule.ExtractedAt = *extractedAt

		return loadPosterRows(ctx, tx, &schedule.PublishedSchedule)
	})
	if err != nil {
		return PosterSchedule{}, fmt.Errorf("load schedule poster rows: %w", err)
	}

	return schedule, nil
}

func loadPosterRows(ctx context.Context, tx pgx.Tx, schedule *PublishedSchedule) error {
	rows, err := pgxdb.Query(ctx, tx, posterRowsSQL, schedule.PosterID)
	if err != nil {
		return fmt.Errorf("load rows: %w", err)
	}

	collected, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Row, error) {
		var (
			item     Row
			editedAt *time.Time
		)

		scanErr := row.Scan(&item.Ordinal, &item.Day, &item.Start, &item.End, &item.TZ, &item.SourceZone, &item.Status, &item.SourceText,
			&item.Badges, &item.TitleKo, &item.WithKo, &item.PlaceKo, &item.TagsKo, &item.NoteKo, &item.DoubtKo, &item.Warnings, &editedAt)
		if editedAt != nil {
			item.EditedAt = *editedAt
		}

		return item, scanErr
	})
	if err != nil {
		return fmt.Errorf("load rows: %w", err)
	}

	changes, err := publishedChanges(ctx, tx, schedule)
	if err != nil {
		return err
	}

	schedule.Rows, schedule.Changes = collected, changes

	return nil
}
