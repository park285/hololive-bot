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
)

// MaxMemoChars는 migration 026의 body CHECK와 같다.
const MaxMemoChars = 4000

var (
	// ErrInvalidKorean은 고친 한국어 칸이 migration 019·020의 CHECK를 넘거나 제어 문자를 담았다는 뜻이다.
	ErrInvalidKorean = errors.New("schedule poster korean fields are invalid")
	// ErrInvalidMemo는 번역 메모가 MaxMemoChars를 넘거나 줄바꿈 밖의 제어 문자를 담았다는 뜻이다.
	ErrInvalidMemo = errors.New("schedule poster translation memo is invalid")
)

// SlotKey는 Slot 하나를 가리킨다. 포스터 행은 Ordinal로, 글이 더한 칸은 AddedPostID·AddedItem으로 가리킨다.
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

// Korean은 칸의 한국어 다섯 칸이다.
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

// PolishSlot은 확정본 칸 하나의 한국어 다섯 칸을 관리자가 고친 값으로 바꾼다. 값은 그 칸의 한국어를 마지막으로 정한 포스터 행이나
// 글의 변경에 쓰고, 그 행이나 변경의 terms는 고친 글에 남은 표기만 두며 doubt_ko와 warnings는 비운다.
// 인자 seen은 호출자가 읽은 PosterSchedule.UpdatedAt을 초 단위로 자른 값이다. 포스터가 없으면 ErrNotFound, 확정본이 아니거나 행 묶음을
// 저장한 적이 없으면 ErrInvalidState, 그 뒤에 일정이 바뀌었거나 칸이 없으면 ErrRevisionConflict, 값이 한도를 넘으면 ErrInvalidKorean이다.
// 포스터 행을 잠그므로 읽은 행 묶음을 그 사이 RecordRows가 바꾸지 못한다.
func (s *Store) PolishSlot(ctx context.Context, posterID int64, seen time.Time, key SlotKey, korean Korean) error {
	if err := korean.validate(); err != nil {
		return fmt.Errorf("polish schedule poster slot: %w", err)
	}

	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var (
			schedule           PosterSchedule
			channelID          string
			weekStart, weekEnd *time.Time
			extractedAt        *time.Time
		)

		err := tx.QueryRow(ctx, lockPosterSQL, posterID).Scan(&schedule.PosterID, &channelID, &schedule.Confirmed, &weekStart, &weekEnd,
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
	// pgx는 nil slice를 NULL로 보내고 두 열은 NOT NULL이다.
	with, tags := append([]string{}, korean.WithKo...), append([]string{}, korean.TagsKo...)
	text := strings.Join(slices.Concat([]string{korean.TitleKo}, with, []string{korean.PlaceKo}, tags, []string{korean.NoteKo}), " ")

	query, target := polishRowSQL, []any{posterID, slot.Ordinal}
	fields := []any{korean.TitleKo, with, korean.PlaceKo, tags, korean.NoteKo, text}

	if change, ok := slot.koreanStep(); ok {
		query, target = polishChangeSQL, []any{change.PostID, change.Item}
		fields = append(fields, change.RecordedAt, posterID)
	}

	tag, err := tx.Exec(ctx, query, append(target, fields...)...)
	if err != nil {
		return fmt.Errorf("write korean fields: %w", err)
	}

	if tag.RowsAffected() != 1 {
		return ErrRevisionConflict
	}

	return nil
}

// Memo는 번역 지시문에 참고 절로 붙는 관리자 메모다. 저장한 적이 없으면 UpdatedAt이 zero다.
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

	if err := s.pool.QueryRow(ctx, memoSQL).Scan(&memo.Body, &memo.Revision, &updatedAt); err != nil {
		return Memo{}, fmt.Errorf("load schedule poster translation memo: %w", err)
	}

	if updatedAt != nil {
		memo.UpdatedAt = *updatedAt
	}

	return memo, nil
}

// SaveTranslationMemo는 메모 본문을 바꾸고 새 revision을 돌려준다. 인자 expectedRevision이 지금 값과 다르면 ErrRevisionConflict,
// 본문이 ValidMemo를 통과하지 못하면 ErrInvalidMemo다.
func (s *Store) SaveTranslationMemo(ctx context.Context, expectedRevision int64, body string) (int64, error) {
	if !ValidMemo(body) {
		return 0, fmt.Errorf("save schedule poster translation memo: %w", ErrInvalidMemo)
	}

	var revision int64

	err := s.pool.QueryRow(ctx, saveMemoSQL, expectedRevision, body).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrRevisionConflict
	}

	if err != nil {
		return 0, fmt.Errorf("save schedule poster translation memo: %w", err)
	}

	return revision, nil
}

// ValidMemo는 본문이 MaxMemoChars 이하이고 줄바꿈(\n) 밖의 제어 문자가 없는지 본다. 빈 본문은 메모가 없다는 뜻이다.
func ValidMemo(body string) bool {
	return utf8.ValidString(body) && utf8.RuneCountInString(body) <= MaxMemoChars &&
		!strings.ContainsFunc(body, func(r rune) bool { return r != '\n' && unicode.IsControl(r) })
}
