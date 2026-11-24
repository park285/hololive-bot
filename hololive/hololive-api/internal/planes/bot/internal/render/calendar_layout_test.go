package render

import (
	"bytes"
	"image/png"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestCalFontSetClamp(t *testing.T) {
	t.Parallel()

	fs, err := loadCalendarFontSet()
	if err != nil {
		t.Fatalf("loadCalendarFontSet() error = %v", err)
	}

	style := calText{Size: 18, Face: calFaceXBold}

	if got := fs.clamp(style, testShortKoreanName, fs.width(style, testShortKoreanName)+1); got != testShortKoreanName {
		t.Errorf("clamp(fits) = %q, want %q", got, testShortKoreanName)
	}

	for _, s := range []string{
		"우사다 페코라 우사다 페코라 우사다 페코라",
		"시라카미 후부키 白上フブキ 호쇼 마린 宝鐘マリン",
	} {
		limit := fs.width(style, s) / 2
		got := fs.clamp(style, s, limit)

		if got == "" || !strings.HasSuffix(got, "…") || fs.width(style, got) > limit {
			t.Errorf("clamp(%q) = %q, want non-empty ellipsis within %v", s, got, limit)
		}
	}

	spaceCase := "우사다 페코라페코라"
	spaceLimit := fs.width(style, "우사다 ") + fs.width(style, "…")/2

	if got := fs.clamp(style, spaceCase, spaceLimit); strings.Contains(got, " …") {
		t.Errorf("clamp(trailing space) = %q, want trimmed before ellipsis", got)
	}

	if got := fs.clamp(style, testShortKoreanName, 0); got != "" {
		t.Errorf("clamp(0) = %q, want empty", got)
	}

	if got := fs.clamp(style, testShortKoreanName, 1); got != "" {
		t.Errorf("clamp(1) = %q, want empty", got)
	}
}

func sampleSplitCalendarEntries(baseURL string) []domain.CalendarEntry {
	return []domain.CalendarEntry{
		{
			Kind:   domain.CelebrationKindBirthday,
			Day:    7,
			Member: &domain.Member{ShortKoreanName: "제타", NameKo: "베스티아 제타", Units: []string{"holoh3ro"}, Photo: baseURL + "/zeta=s88-c"},
		},
		{
			Kind:    domain.CelebrationKindAnniversary,
			Day:     9,
			Ordinal: 2,
			Member:  &domain.Member{ShortKoreanName: "리오나", NameKo: "이사키 리오나", Units: []string{"FLOW GLOW"}, Photo: baseURL + "/riona=s88-c"},
		},
		{
			Kind:    domain.CelebrationKindAnniversary,
			Day:     9,
			Ordinal: 2,
			Member:  &domain.Member{ShortKoreanName: "니코", NameKo: "코가네이 니코", Units: []string{"FLOW GLOW"}, Photo: baseURL + "/niko=s88-c"},
		},
	}
}

func assertCalendarAlbumBounds(t *testing.T, album [][]byte) {
	t.Helper()

	for _, page := range album {
		img, err := png.Decode(bytes.NewReader(page))
		if err != nil {
			t.Fatalf("png.Decode() error = %v", err)
		}

		if img.Bounds().Dx() != calendarOutputWidth || img.Bounds().Dy() > calendarOutputHeight {
			t.Errorf("bounds = %v, want %dx(<=%d)", img.Bounds(), calendarOutputWidth, calendarOutputHeight)
		}
	}
}

func TestCalendarCardRenderer_SplitsBirthdayAndAnniversaryAndRotatesThemes(t *testing.T) {
	var requests atomic.Int32

	server := newPNGServer(t, &requests)
	defer server.Close()

	store := newTestCalendarImageCache(t)
	renderer := NewCalendarCardRenderer(WithCalendarImageCache(store))

	renderer.variants.intN = func(int) int { return 0 }

	entries := sampleSplitCalendarEntries(server.URL)

	firstAlbum, err := renderer.RenderCalendarImagesContext(t.Context(), 11, 2026, entries)
	if err != nil || len(firstAlbum) != 2 {
		t.Fatalf("first RenderCalendarImagesContext() len=%d, err=%v; want 2 pages", len(firstAlbum), err)
	}

	secondAlbum, err := renderer.RenderCalendarImagesContext(t.Context(), 11, 2026, entries)
	if err != nil || len(secondAlbum) != 2 {
		t.Fatalf("second RenderCalendarImagesContext() len=%d, err=%v; want 2 pages", len(secondAlbum), err)
	}

	if bytes.Equal(firstAlbum[0], secondAlbum[0]) || bytes.Equal(firstAlbum[1], secondAlbum[1]) {
		t.Fatal("consecutive calls returned identical theme pages, want rotating 3-A <-> 3-B")
	}

	thirdAlbum, err := renderer.RenderCalendarImagesContext(t.Context(), 11, 2026, entries)
	if err != nil || !slices.EqualFunc(firstAlbum, thirdAlbum, bytes.Equal) {
		t.Fatalf("third RenderCalendarImagesContext() did not cycle back to cached first theme: err=%v", err)
	}

	assertCalendarAlbumBounds(t, firstAlbum)
	assertCalendarAlbumBounds(t, secondAlbum)

	if got, want := requests.Load(), int32(6); got != want {
		t.Fatalf("photo requests = %d, want %d", got, want)
	}

	freshRenderer := NewCalendarCardRenderer(WithCalendarImageCache(store))

	freshRenderer.variants.intN = func(int) int { return 0 }

	fromShared, err := freshRenderer.RenderCalendarImagesContext(t.Context(), 11, 2026, entries)
	if err != nil || !slices.EqualFunc(firstAlbum, fromShared, bytes.Equal) {
		t.Fatalf("shared Valkey album cache mismatch: err=%v", err)
	}

	if got, want := requests.Load(), int32(6); got != want {
		t.Fatalf("photo requests after shared cache hit = %d, want %d", got, want)
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
