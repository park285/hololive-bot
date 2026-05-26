package formatter

import (
	"context"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestCelebrationCalendar(t *testing.T) {
	t.Parallel()

	f := NewResponseFormatter("!", nil)

	t.Run("empty entries", func(t *testing.T) {
		t.Parallel()

		result := f.CelebrationCalendar(context.Background(), 6, 2026, nil)
		if !strings.Contains(result, "등록된 기념일이 없습니다") {
			t.Error("empty entries should contain empty message")
		}
		if !strings.Contains(result, "6월") {
			t.Error("should contain month")
		}
	})

	t.Run("birthday entry without ordinal", func(t *testing.T) {
		t.Parallel()

		entries := []domain.CalendarEntry{
			{
				Kind:   domain.CelebrationKindBirthday,
				Member: &domain.Member{ShortKoreanName: "페코라"},
				Day:    15,
			},
		}

		result := f.CelebrationCalendar(context.Background(), 6, 2026, entries)
		if !strings.Contains(result, "페코라 생일") {
			t.Error("should contain birthday text")
		}
		if strings.Contains(result, "주년") {
			t.Error("birthday should not contain 주년")
		}
	})

	t.Run("anniversary entry with ordinal", func(t *testing.T) {
		t.Parallel()

		entries := []domain.CalendarEntry{
			{
				Kind:    domain.CelebrationKindAnniversary,
				Member:  &domain.Member{ShortKoreanName: "미코"},
				Day:     5,
				Ordinal: 3,
			},
		}

		result := f.CelebrationCalendar(context.Background(), 6, 2026, entries)
		if !strings.Contains(result, "미코 데뷔 3주년") {
			t.Errorf("should contain anniversary text, got: %s", result)
		}
	})

	t.Run("multiple entries grouped by day", func(t *testing.T) {
		t.Parallel()

		entries := []domain.CalendarEntry{
			{Kind: domain.CelebrationKindBirthday, Member: &domain.Member{NameKo: "라미"}, Day: 10},
			{Kind: domain.CelebrationKindAnniversary, Member: &domain.Member{NameKo: "라미"}, Day: 10, Ordinal: 2},
			{Kind: domain.CelebrationKindBirthday, Member: &domain.Member{ShortKoreanName: "보탄"}, Day: 20},
		}

		result := f.CelebrationCalendar(context.Background(), 6, 2026, entries)
		if !strings.Contains(result, "3건") {
			t.Error("should show count")
		}
		if !strings.Contains(result, "6월 10일") {
			t.Error("should contain day header for day 10")
		}
		if !strings.Contains(result, "6월 20일") {
			t.Error("should contain day header for day 20")
		}
	})

	t.Run("member display name fallback", func(t *testing.T) {
		t.Parallel()

		entries := []domain.CalendarEntry{
			{Kind: domain.CelebrationKindBirthday, Member: &domain.Member{Name: "Pekora"}, Day: 1},
		}

		result := f.CelebrationCalendar(context.Background(), 1, 2026, entries)
		if !strings.Contains(result, "Pekora") {
			t.Error("should fall back to Name")
		}
	})

	t.Run("nil member", func(t *testing.T) {
		t.Parallel()

		entries := []domain.CalendarEntry{
			{Kind: domain.CelebrationKindBirthday, Member: nil, Day: 1},
		}

		result := f.CelebrationCalendar(context.Background(), 1, 2026, entries)
		if !strings.Contains(result, "알 수 없음") {
			t.Error("nil member should show fallback name")
		}
	})
}
