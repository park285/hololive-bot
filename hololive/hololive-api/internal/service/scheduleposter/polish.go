package scheduleposter

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-shared/pkg/dbx"
)

const MaxMemoChars = 4000

var (
	ErrInvalidKorean = errors.New("schedule poster korean fields are invalid")
	ErrInvalidMemo   = errors.New("schedule poster translation memo is invalid")
)

type SlotKey struct {
	Ordinal     int
	AddedPostID int64
	AddedItem   int
}

func (k SlotKey) matches(slot *Slot) bool {
	if k.AddedPostID != 0 {
		return slot.AddedPostID == k.AddedPostID && slot.AddedItem == k.AddedItem
	}

	return slot.AddedPostID == 0 && slot.Ordinal == k.Ordinal
}

type Korean struct {
	TitleKo string
	WithKo  []string
	PlaceKo string
	TagsKo  []string
	NoteKo  string
}

func (k Korean) validate() error {
	texts := slices.Concat([]string{k.TitleKo, k.PlaceKo, k.NoteKo}, k.WithKo, k.TagsKo)
	required := slices.Concat([]string{k.TitleKo}, k.WithKo, k.TagsKo)

	switch {
	case len(k.WithKo) > maxRowListItems || len(k.TagsKo) > maxRowListItems:
		return fmt.Errorf("%w: list must have at most %d items", ErrInvalidKorean, maxRowListItems)
	case slices.Contains(required, ""):
		return fmt.Errorf("%w: title and list items must not be empty", ErrInvalidKorean)
	case slices.ContainsFunc(texts, func(text string) bool {
		return utf8.RuneCountInString(text) > maxRowTextChars || !utf8.ValidString(text) || strings.ContainsFunc(text, unicode.IsControl)
	}):
		return fmt.Errorf("%w: text must be at most %d characters without control characters", ErrInvalidKorean, maxRowTextChars)
	}

	return nil
}

func (s *Store) PolishSlot(ctx context.Context, posterID int64, seen time.Time, key SlotKey, korean Korean) error {
	if err := korean.validate(); err != nil {
		return fmt.Errorf("polish schedule poster slot: %w", err)
	}

	err := dbx.InPgxTxOptions(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		var (
			schedule           PosterSchedule
			channelID          string
			weekStart, weekEnd *time.Time
			extractedAt        *time.Time
		)

		err := pgxdb.QueryRow(ctx, tx, lockPosterSQL, posterID).Scan(&schedule.PosterID, &channelID, &schedule.Confirmed, &weekStart, &weekEnd,
			&extractedAt, &schedule.PostedAt, &schedule.RowsPending, &schedule.RowsDayShift)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}

		if err != nil {
			return fmt.Errorf("lock poster: %w", err)
		}

		if !schedule.Confirmed || extractedAt == nil {
			return ErrInvalidState
		}

		schedule.ExtractedAt = *extractedAt

		if err := loadPosterRows(ctx, tx, &schedule.PublishedSchedule); err != nil {
			return err
		}

		if !schedule.UpdatedAt().Truncate(time.Second).Equal(seen) {
			return ErrRevisionConflict
		}

		slots := schedule.Slots()

		i := slices.IndexFunc(slots, func(slot Slot) bool { return key.matches(&slot) })
		if i < 0 {
			return ErrRevisionConflict
		}

		return polish(ctx, tx, posterID, &slots[i], korean)
	})
	if err != nil {
		return fmt.Errorf("polish schedule poster slot: %w", err)
	}

	return nil
}

func polish(ctx context.Context, tx pgx.Tx, posterID int64, slot *Slot, korean Korean) error {
	with, tags := append([]string{}, korean.WithKo...), append([]string{}, korean.TagsKo...)
	text := strings.Join(slices.Concat([]string{korean.TitleKo}, with, []string{korean.PlaceKo}, tags, []string{korean.NoteKo}), " ")

	query, target := polishRowSQL, []any{posterID, slot.Ordinal}
	fields := []any{korean.TitleKo, with, korean.PlaceKo, tags, korean.NoteKo, text}

	if change, ok := slot.koreanStep(); ok {
		query, target = polishChangeSQL, []any{change.PostID, change.Item}
		fields = append(fields, change.RecordedAt, posterID)
	}

	tag, err := pgxdb.Exec(ctx, tx, query, append(target, fields...)...)
	if err != nil {
		return fmt.Errorf("write korean fields: %w", err)
	}

	if tag.RowsAffected() != 1 {
		return ErrRevisionConflict
	}

	return nil
}

type Memo struct {
	Body      string
	Revision  int64
	UpdatedAt time.Time
}

func (s *Store) TranslationMemo(ctx context.Context) (Memo, error) {
	var (
		memo      Memo
		updatedAt *time.Time
	)

	if err := pgxdb.QueryRow(ctx, s.pool, memoSQL).Scan(&memo.Body, &memo.Revision, &updatedAt); err != nil {
		return Memo{}, fmt.Errorf("load schedule poster translation memo: %w", err)
	}

	if updatedAt != nil {
		memo.UpdatedAt = *updatedAt
	}

	return memo, nil
}

func (s *Store) SaveTranslationMemo(ctx context.Context, expectedRevision int64, body string) (int64, error) {
	if !ValidMemo(body) {
		return 0, fmt.Errorf("save schedule poster translation memo: %w", ErrInvalidMemo)
	}

	var revision int64

	err := pgxdb.QueryRow(ctx, s.pool, saveMemoSQL, expectedRevision, body).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrRevisionConflict
	}

	if err != nil {
		return 0, fmt.Errorf("save schedule poster translation memo: %w", err)
	}

	return revision, nil
}

func ValidMemo(body string) bool {
	return utf8.ValidString(body) && utf8.RuneCountInString(body) <= MaxMemoChars &&
		!strings.ContainsFunc(body, func(r rune) bool { return r != '\n' && unicode.IsControl(r) })
}
