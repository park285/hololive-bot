package render

import (
	"bytes"
	"image"
	"image/color"
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

func TestCalendarThemePool_ContainsSevenLightAndDarkThemes(t *testing.T) {
	t.Parallel()

	want := []string{
		CalendarThemePrismDaylight,
		CalendarThemeShirokamiRisograph,
		CalendarThemeSakuraSoraPress,
		CalendarThemeMuseumArchive,
		CalendarThemeCreamBromide,
		CalendarThemeKurokamiRisograph,
		CalendarThemeHoloStage,
	}

	if !slices.Equal(calendarThemePool, want) {
		t.Fatalf("calendarThemePool = %v, want %v", calendarThemePool, want)
	}
}

func TestCalendarCardRenderer_SplitsBirthdayAndAnniversaryWithMixedThemes(t *testing.T) {
	var requests atomic.Int32

	server := newPNGServer(t, &requests)
	defer server.Close()

	store := newTestCalendarImageCache(t)
	renderer := NewCalendarCardRenderer(WithCalendarImageCache(store))
	entries := sampleSplitCalendarEntries(server.URL)

	bdayRef := make(map[string][]byte, len(calendarThemePool))
	annivRef := make(map[string][]byte, len(calendarThemePool))

	var wantColdPhotoFetches int32

	for _, themeID := range calendarThemePool {
		album, err := renderer.RenderCalendarImagesWithTheme(t.Context(), 11, 2026, themeID, entries)
		if err != nil || len(album) != 2 {
			t.Fatalf("RenderCalendarImagesWithTheme(%q) len=%d, err=%v; want 2 pages", themeID, len(album), err)
		}

		assertCalendarAlbumBounds(t, album)

		bdayRef[themeID] = album[0]
		annivRef[themeID] = album[1]

		wantColdPhotoFetches += 3
	}

	if got := requests.Load(); got != wantColdPhotoFetches {
		t.Fatalf("cold photo requests = %d, want %d", got, wantColdPhotoFetches)
	}

	mixedSequence := [4][2]string{
		{CalendarThemeShirokamiRisograph, CalendarThemeKurokamiRisograph},
		{CalendarThemeCreamBromide, CalendarThemeSakuraSoraPress},
		{CalendarThemePrismDaylight, CalendarThemeMuseumArchive},
		{CalendarThemeKurokamiRisograph, CalendarThemeHoloStage},
	}

	freshRenderer := NewCalendarCardRenderer(WithCalendarImageCache(store))

	var seq stepPicker

	freshRenderer.variants.intN = seq.next

	for callIdx, pair := range mixedSequence {
		seq.setTargets(freshRenderer.variants, "2026-11:birthday", pair[0], "2026-11:anniversary", pair[1])

		album, err := freshRenderer.RenderCalendarImagesContext(t.Context(), 11, 2026, entries)
		if err != nil || len(album) != 2 {
			t.Fatalf("call %d RenderCalendarImagesContext() len=%d, err=%v; want 2", callIdx, len(album), err)
		}

		if !bytes.Equal(album[0], bdayRef[pair[0]]) || !bytes.Equal(album[1], annivRef[pair[1]]) {
			t.Fatalf("call %d album pages did not match expected pair %v", callIdx, pair)
		}
	}

	if got := requests.Load(); got != wantColdPhotoFetches {
		t.Fatalf("photo requests after mixed shared-cache combinations = %d, want %d", got, wantColdPhotoFetches)
	}
}

func TestCalendarCardRenderer_NeverRepeatsImmediatePreviousPageTheme(t *testing.T) {
	t.Parallel()

	renderer := NewCalendarCardRenderer()

	renderer.variants.intN = func(n int) int { return n - 1 }

	entries := []domain.CalendarEntry{
		{Kind: domain.CelebrationKindBirthday, Day: 7, Member: &domain.Member{ShortKoreanName: "제타"}},
		{Kind: domain.CelebrationKindAnniversary, Day: 9, Ordinal: 2, Member: &domain.Member{ShortKoreanName: "리오나"}},
	}

	var prevBday, prevAnniv []byte

	for step := range 8 {
		album, err := renderer.RenderCalendarImagesContext(t.Context(), 11, 2026, entries)
		if err != nil || len(album) != 2 {
			t.Fatalf("step %d RenderCalendarImagesContext() len=%d, err=%v", step, len(album), err)
		}

		if step > 0 && (bytes.Equal(album[0], prevBday) || bytes.Equal(album[1], prevAnniv)) {
			t.Fatalf("step %d repeated immediately preceding page theme", step)
		}

		prevBday = album[0]
		prevAnniv = album[1]
	}
}

type stepPicker struct {
	picks []int
	idx   int
}

func (s *stepPicker) setTargets(v *variantPicker, k1, t1, k2, t2 string) {
	c1 := slices.DeleteFunc(slices.Clone(calendarThemePool), func(x string) bool { return x == v.last[k1] })
	c2 := slices.DeleteFunc(slices.Clone(calendarThemePool), func(x string) bool { return x == v.last[k2] })
	i1 := max(0, slices.Index(c1, t1))
	i2 := max(0, slices.Index(c2, t2))

	s.picks = []int{i1, i2}
	s.idx = 0
}

func (s *stepPicker) next(n int) int {
	if s.idx >= len(s.picks) {
		return 0
	}

	val := s.picks[s.idx]
	s.idx++

	return min(val, max(0, n-1))
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

func TestCalendarCardRenderer_AllThemesRenderEmptyAndOverflowMonths(t *testing.T) {
	t.Parallel()

	renderer := NewCalendarCardRenderer()
	overflowEntries := make([]domain.CalendarEntry, 48)

	for i := range overflowEntries {
		overflowEntries[i] = domain.CalendarEntry{
			Kind:   domain.CelebrationKindBirthday,
			Day:    (i / 6) + 1,
			Member: &domain.Member{ShortKoreanName: "멤버", NameKo: "테스트 멤버", Units: []string{"홀로라이브"}},
		}
	}

	for _, themeID := range calendarThemePool {
		emptyPages, err := renderer.RenderCalendarImagesWithTheme(t.Context(), 2, 2026, themeID, nil)
		if err != nil || len(emptyPages) != 1 {
			t.Fatalf("empty RenderCalendarImagesWithTheme(%q) len=%d, err=%v", themeID, len(emptyPages), err)
		}

		assertCalendarAlbumBounds(t, emptyPages)

		busyPages, err := renderer.RenderCalendarImagesWithTheme(t.Context(), 11, 2026, themeID, overflowEntries)
		if err != nil || len(busyPages) != 1 {
			t.Fatalf("overflow RenderCalendarImagesWithTheme(%q) len=%d, err=%v", themeID, len(busyPages), err)
		}

		assertCalendarAlbumBounds(t, busyPages)
	}
}

func TestCalCircularSafeRadius_DetectsWhiteCornerMatte(t *testing.T) {
	t.Parallel()

	img := image.NewRGBA(image.Rect(0, 0, 100, 100))

	for y := range 100 {
		for x := range 100 {
			dx, dy := float64(x)-50, float64(y)-50
			if dx*dx+dy*dy <= 46*46 {
				img.SetRGBA(x, y, color.RGBA{R: 40, G: 90, B: 180, A: 255})
			} else {
				img.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
			}
		}
	}

	hasMatte, safeR := calCircularSafeRadius(img)
	if !hasMatte || safeR < 38 || safeR > 47 {
		t.Fatalf("calCircularSafeRadius() = (%v, %v), want (true, ~44)", hasMatte, safeR)
	}
}
