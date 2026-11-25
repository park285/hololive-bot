//go:build !race

package render

import (
	"bytes"
	"image/png"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestCalendarCardRenderer_RenderCalendarImages_EmptyEntries(t *testing.T) {
	t.Parallel()

	r := NewCalendarCardRenderer()

	pages, err := r.RenderCalendarImagesWithTheme(t.Context(), 6, 2026, CalendarThemePrismDaylight, nil)
	if err != nil {
		t.Fatalf("RenderCalendarImagesWithTheme() error = %v", err)
	}

	if len(pages) != 1 {
		t.Fatalf("len(pages) = %d, want 1", len(pages))
	}

	assertValidPNG(t, pages[0])
}

func TestCalendarCardRenderer_RenderCalendarImages_WithEntries(t *testing.T) {
	t.Parallel()

	r := NewCalendarCardRenderer()
	entries := []domain.CalendarEntry{
		{Kind: domain.CelebrationKindBirthday, Member: &domain.Member{ShortKoreanName: testShortKoreanName}, Day: 15},
		{Kind: domain.CelebrationKindAnniversary, Member: &domain.Member{ShortKoreanName: "미코"}, Day: 15, Ordinal: 3},
		{Kind: domain.CelebrationKindBirthday, Member: &domain.Member{NameKo: "스이세이"}, Day: 22},
	}

	pages, err := r.RenderCalendarImagesWithTheme(t.Context(), 6, 2026, CalendarThemePrismDaylight, entries)
	if err != nil {
		t.Fatalf("RenderCalendarImagesWithTheme() error = %v", err)
	}

	if len(pages) != 2 {
		t.Fatalf("len(pages) = %d, want 2 (birthday + anniversary)", len(pages))
	}

	for _, data := range pages {
		assertValidPNG(t, data)

		img, decErr := png.Decode(bytes.NewReader(data))
		if decErr != nil {
			t.Fatalf("png.Decode() error = %v", decErr)
		}

		bounds := img.Bounds()
		if bounds.Dx() != calendarOutputWidth {
			t.Errorf("width = %d, want %d", bounds.Dx(), calendarOutputWidth)
		}

		if bounds.Dy() > calendarOutputHeight {
			t.Errorf("height = %d, want <= %d", bounds.Dy(), calendarOutputHeight)
		}
	}
}

func TestCalendarCardRenderer_RenderCalendarImages_UsesTransportFriendlyCanvas(t *testing.T) {
	t.Parallel()

	r := NewCalendarCardRenderer()

	pages, err := r.RenderCalendarImagesWithTheme(t.Context(), 6, 2026, CalendarThemePrismDaylight, []domain.CalendarEntry{
		{Kind: domain.CelebrationKindBirthday, Member: &domain.Member{ShortKoreanName: testShortKoreanName}, Day: 15},
	})
	if err != nil {
		t.Fatalf("RenderCalendarImagesWithTheme() error = %v", err)
	}

	if len(pages) != 1 {
		t.Fatalf("len(pages) = %d, want 1", len(pages))
	}

	img, decErr := png.Decode(bytes.NewReader(pages[0]))
	if decErr != nil {
		t.Fatalf("png.Decode() error = %v", decErr)
	}

	if got, want := img.Bounds().Dx(), calendarOutputWidth; got != want {
		t.Fatalf("output width = %d, want %d (kakao display width)", got, want)
	}
}

func TestCalendarCardRenderer_RenderCalendarImages_NilMember(t *testing.T) {
	t.Parallel()

	r := NewCalendarCardRenderer()
	entries := []domain.CalendarEntry{
		{Kind: domain.CelebrationKindBirthday, Member: nil, Day: 1},
	}

	pages, err := r.RenderCalendarImagesWithTheme(t.Context(), 1, 2026, CalendarThemePrismDaylight, entries)
	if err != nil {
		t.Fatalf("RenderCalendarImagesWithTheme() error = %v", err)
	}

	if len(pages) != 1 {
		t.Fatalf("len(pages) = %d, want 1", len(pages))
	}

	assertValidPNG(t, pages[0])
}

func TestCalendarCardRenderer_CompactsBusyMonthIntoSingleImage(t *testing.T) {
	t.Parallel()

	r := NewCalendarCardRenderer()
	entries := make([]domain.CalendarEntry, 200)

	for i := range entries {
		entries[i] = domain.CalendarEntry{
			Kind:   domain.CelebrationKindBirthday,
			Member: &domain.Member{Name: "Test"},
			Day:    (i / 8) + 1,
		}
	}

	pages, err := r.RenderCalendarImagesWithTheme(t.Context(), 6, 2026, CalendarThemePrismDaylight, entries)
	if err != nil {
		t.Fatalf("RenderCalendarImagesWithTheme() error = %v", err)
	}

	if len(pages) != 1 {
		t.Fatalf("len(pages) = %d, want 1", len(pages))
	}

	img, decErr := png.Decode(bytes.NewReader(pages[0]))
	if decErr != nil {
		t.Fatalf("png.Decode() error = %v", decErr)
	}

	if img.Bounds().Dx() != calendarOutputWidth {
		t.Errorf("width = %d, want %d", img.Bounds().Dx(), calendarOutputWidth)
	}

	if img.Bounds().Dy() > calendarOutputHeight {
		t.Errorf("height %d exceeds single-image budget %d", img.Bounds().Dy(), calendarOutputHeight)
	}
}

func TestCalendarCardRenderer_SixWeekBusyMonthFitsHeightBudget(t *testing.T) {
	t.Parallel()

	renderer := NewCalendarCardRenderer()
	entries := []domain.CalendarEntry{
		{Kind: domain.CelebrationKindAnniversary, Day: 1, Ordinal: 4, Member: &domain.Member{ShortKoreanName: "멤버A", Units: []string{"유닛1"}}},
		{Kind: domain.CelebrationKindAnniversary, Day: 1, Ordinal: 4, Member: &domain.Member{ShortKoreanName: "멤버B", Units: []string{"유닛1"}}},
		{Kind: domain.CelebrationKindAnniversary, Day: 15, Ordinal: 3, Member: &domain.Member{ShortKoreanName: "멤버C", Units: []string{"유닛2"}}},
		{Kind: domain.CelebrationKindAnniversary, Day: 15, Ordinal: 3, Member: &domain.Member{ShortKoreanName: "멤버D", Units: []string{"유닛2"}}},
		{Kind: domain.CelebrationKindAnniversary, Day: 31, Ordinal: 5, Member: &domain.Member{ShortKoreanName: "멤버E", Units: []string{"유닛3"}}},
	}

	for _, themeID := range calendarThemePool {
		pages, err := renderer.RenderCalendarImagesWithTheme(t.Context(), 5, 2026, themeID, entries)
		if err != nil || len(pages) != 1 {
			t.Fatalf("RenderCalendarImagesWithTheme(%q) len=%d, err=%v; want 1 page", themeID, len(pages), err)
		}

		assertCalendarAlbumBounds(t, pages)
	}
}
