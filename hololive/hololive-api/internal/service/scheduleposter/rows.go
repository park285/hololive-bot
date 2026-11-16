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
)

// RowStatus는 포스터가 그 칸에 대해 말한 상태이며 migration 019의 CHECK와 같은 값이다.
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

// MaxRows는 migration 019의 ordinal CHECK와 같다.
const MaxRows = 100

const (
	maxRowTextChars = 1000
	maxRowTZChars   = 40
	maxRowListItems = 40
	// 하루 밀기 값의 상한이며 migration 019의 rows_day_shift CHECK와 같다.
	maxRowsDayShift = 1
)

var rowTimePattern = regexp.MustCompile(`^([01]?\d|2\d):[0-5]\d$`)

// Term은 번역이 정한 이름 표기 하나다. Basis는 official_ko, translated, transliterated 가운데 하나다.
type Term struct {
	Source string `json:"source"`
	Ko     string `json:"ko"`
	Basis  string `json:"basis"`
}

// Row는 포스터의 일정 한 칸이다. Day·Start·End는 JST 날짜와 "HH:MM"(심야 표기는 29:59까지)이고 시각이 없으면 빈 문자열이다.
// JST가 아닌 포스터의 행은 JST로 바꿔 TZ를 "JST"로, SourceZone을 포스터에 적힌 시간대로 둔다. JST 행의 SourceZone은 비어 있다.
// PublishedRows는 Terms, DoubtKo, Warnings를 비우고, PosterRows는 Terms만 비운다.
type Row struct {
	// Ordinal은 RecordRows가 묶음 안 순서대로 1부터 매긴다.
	Ordinal int `json:"ordinal"`
	// Day는 encoding/json/v2가 format 태그를 거부해(Go 1.27.4) RecordRows가 DateOnly 문자열로 따로 넣는다.
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
	// Warnings는 구조 검사가 이 행에서 찾은 경고다. 저장을 막지 않는다.
	Warnings []string `json:"warnings"`
	// EditedAt은 관리자가 한국어 다섯 칸을 마지막으로 고친 시각이며 고친 적이 없으면 zero다. PolishSlot만 쓴다.
	EditedAt time.Time `json:"-"`
}

// PendingRows는 일정표 행을 뽑을 확정본이다. 그 사이 거절·주 범위 수정·재추출 예약으로 RowsRevision이 오르면 결과를 기록하지 않는다.
type PendingRows struct {
	ID        int64
	ChannelID string
	ImageURL  string
	// ImageSHA256은 썸네일 후보만 채운다.
	ImageSHA256  string
	WeekStart    time.Time
	WeekEnd      time.Time
	RowsRevision int64
}

// PublishedSchedule은 !스케쥴이 보여 줄 확정본의 행 묶음이다.
type PublishedSchedule struct {
	PosterID    int64
	WeekStart   time.Time
	WeekEnd     time.Time
	ExtractedAt time.Time
	PostedAt    time.Time
	// RowsPending은 재추출이 예약돼 행 묶음이 바뀔 예정이라는 뜻이다. 그동안 새 글을 대응하지 않는다.
	RowsPending bool
	// RowsDayShift는 행 묶음의 JST 날짜가 주 범위보다 며칠 밀렸는지(-1..1)다. 게시 범위와 카드 창은 주 범위를 이만큼 민다.
	RowsDayShift int
	Rows         []Row
	// Changes는 현재 행 묶음에 대응한 글의 변경 가운데 거절(rejected)을 뺀 것이다. 같음(same)은 Applied가 아니다.
	Changes []Change
}

// Slots는 행 묶음에 변경을 적용한 현재 일정이다.
func (s *PublishedSchedule) Slots() []Slot {
	return ApplyChanges(s.Rows, s.Changes)
}

// UpdatedAt은 행 묶음을 뽑은 시각, Applied인 변경을 기록한 시각, 관리자가 행이나 Applied인 변경의 한국어를 고친 시각 가운데 가장 늦은 값이다.
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

// PendingRows의 today는 JST 날짜다.
func (s *Store) PendingRows(ctx context.Context, today time.Time, limit int) ([]PendingRows, error) {
	rows, err := s.pool.Query(ctx, pendingRowsSQL, today, min(max(limit, 1), maxListLimit))
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

// WeekTerms는 같은 주를 덮는 다른 확정본이 먼저 정한 표기를 저장한 순서대로 돌려준다. 같은 원문이 여러 번 나올 수 있다.
func (s *Store) WeekTerms(ctx context.Context, item PendingRows) ([]Term, error) {
	rows, err := s.pool.Query(ctx, weekTermsSQL, item.ID, item.WeekStart, item.WeekEnd)
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

// RecordRows는 이전 행 묶음과 그 하루 밀기 값(PublishedSchedule.RowsDayShift)을 새 값으로 바꾼다. 묶음이 ValidateRows를 통과하지
// 못하거나 dayShift가 -1..1 밖이면 오류이고, 그 사이 거절·주 범위 수정·재추출 예약으로 대상이 달라졌으면 아무것도 바꾸지 않고 false를 돌려준다.
func (s *Store) RecordRows(ctx context.Context, item PendingRows, rows []Row, dayShift int) (bool, error) {
	if dayShift < -maxRowsDayShift || dayShift > maxRowsDayShift {
		return false, fmt.Errorf("record schedule poster rows: day shift %d is outside -%d..%d", dayShift, maxRowsDayShift, maxRowsDayShift)
	}

	payload, err := encodeRows(rows)
	if err != nil {
		return false, fmt.Errorf("record schedule poster rows: %w", err)
	}

	recorded := false

	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, markErr := tx.Exec(ctx, recordRowsSQL, item.ID, item.RowsRevision, dayShift)
		if markErr != nil {
			return fmt.Errorf("mark rows extracted: %w", markErr)
		}

		if tag.RowsAffected() != 1 {
			return nil
		}

		if deleteErr := deleteRows(ctx, tx, item.ID); deleteErr != nil {
			return deleteErr
		}

		if _, insertErr := tx.Exec(ctx, insertRowsSQL, item.ID, payload); insertErr != nil {
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

// ValidateRows는 migration 019의 CHECK와 같은 조건을 DB에 가기 전에 검사한다. 글 길이는 char_length처럼 문자 수로 세고,
// jsonb와 text가 받지 못하는 잘못된 UTF-8과 NUL 문자, DateOnly로 네 자리 연도가 되지 않는 날짜도 거부한다.
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

// RecordRowsFailure는 이전 행 묶음을 그대로 둔다. 실패 사유 reason은 관리자에게 보이는 분류다.
func (s *Store) RecordRowsFailure(ctx context.Context, item PendingRows, reason string) (bool, error) {
	tag, err := s.pool.Exec(ctx, recordRowsFailureSQL, item.ID, item.RowsRevision, reason)
	if err != nil {
		return false, fmt.Errorf("record schedule poster rows failure: %w", err)
	}

	return tag.RowsAffected() == 1, nil
}

// PublishedRows의 day는 JST 날짜다. 같은 멤버에서 queries/published_poster.sql의 규칙(오늘을 덮는 주, 없으면 7일 안에 시작하는 주)으로
// 확정본 하나를 고르고, 행을 저장한 적이 없으면 false다.
// 행과 그 묶음의 반영 변경은 한 snapshot에서 읽어, 묶음이 바뀌는 중에도 다른 묶음의 변경이 섞이지 않는다.
func (s *Store) PublishedRows(ctx context.Context, channelID string, day time.Time) (PublishedSchedule, bool, error) {
	var schedule PublishedSchedule

	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		rows, queryErr := tx.Query(ctx, publishedRowsSQL, channelID, day)
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
				&item.TitleKo, &item.WithKo, &item.PlaceKo, &item.TagsKo, &item.NoteKo, &editedAt)
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
		return PublishedSchedule{}, false, fmt.Errorf("load published schedule poster rows: %w", err)
	}

	if len(schedule.Rows) == 0 {
		return PublishedSchedule{}, false, nil
	}

	return schedule, true, nil
}

// PosterSchedule은 포스터 하나의 행 묶음과 그 포스터가 게시되는지다. 확정본이 아니거나 묶음을 저장한 적이 없으면
// Rows와 Changes가 비고 ExtractedAt은 zero value다.
type PosterSchedule struct {
	PublishedSchedule

	Confirmed bool
	// TodayPosterID는 같은 멤버로 그 날짜에 PublishedRows가 고르는 확정본이며 없으면 0이다. 확정본이 아니면 0이다.
	TodayPosterID int64
}

// PosterRows의 day는 JST 날짜다. 포스터가 없으면 ErrNotFound다. 포스터, 게시 대상, 행과 반영 변경은 한 snapshot에서 읽는다.
func (s *Store) PosterRows(ctx context.Context, id int64, day time.Time) (PosterSchedule, error) {
	var schedule PosterSchedule

	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var (
			channelID          string
			weekStart, weekEnd *time.Time
			extractedAt        *time.Time
		)

		err := tx.QueryRow(ctx, posterScheduleSQL, id).Scan(&schedule.PosterID, &channelID, &schedule.Confirmed, &weekStart, &weekEnd,
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

		// 확정본은 migration 010의 CHECK가 주 범위를 요구한다.
		if weekStart != nil && weekEnd != nil {
			schedule.WeekStart, schedule.WeekEnd = *weekStart, *weekEnd
		}

		if err = tx.QueryRow(ctx, publishedPosterSQL, channelID, day).Scan(&schedule.TodayPosterID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
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
	rows, err := tx.Query(ctx, posterRowsSQL, schedule.PosterID)
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
