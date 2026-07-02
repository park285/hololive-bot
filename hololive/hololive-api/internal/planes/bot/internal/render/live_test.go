package render

import (
	"bytes"
	"image/png"
	"strings"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/assets/fonts"
)

func TestLiveCardRenderer_RenderLiveImages_EmptyEntries(t *testing.T) {
	t.Parallel()

	if _, err := NewLiveCardRenderer().RenderLiveImages(nil); err == nil {
		t.Fatal("RenderLiveImages(nil) error = nil, want error")
	}
}

func TestLiveCardRenderer_RenderLiveImages_SinglePage(t *testing.T) {
	t.Parallel()

	entries := []LiveCardEntry{
		{Name: "페코라", Title: "【マイクラ】건축 방송"},
		{Name: "마린", Title: "노래 연습", Chzzk: true},
	}

	pages, err := NewLiveCardRenderer().RenderLiveImages(entries)
	if err != nil {
		t.Fatalf("RenderLiveImages() error = %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("len(pages) = %d, want 1", len(pages))
	}
	assertValidPNG(t, pages[0])

	img, decErr := png.Decode(bytes.NewReader(pages[0]))
	if decErr != nil {
		t.Fatalf("png.Decode() error = %v", decErr)
	}
	if img.Bounds().Dx() != calendarOutputWidth {
		t.Errorf("width = %d, want %d", img.Bounds().Dx(), calendarOutputWidth)
	}
}

func TestLiveCardRenderer_RenderLiveImages_PaginatesAndCaps(t *testing.T) {
	t.Parallel()

	entries := make([]LiveCardEntry, 100)
	for i := range entries {
		entries[i] = LiveCardEntry{Name: "멤버", Title: "방송"}
	}

	pages, err := NewLiveCardRenderer().RenderLiveImages(entries)
	if err != nil {
		t.Fatalf("RenderLiveImages() error = %v", err)
	}
	if len(pages) != calendarMaxPages {
		t.Fatalf("len(pages) = %d, want cap %d", len(pages), calendarMaxPages)
	}
	for _, page := range pages {
		assertValidPNG(t, page)
	}
}

func TestPaginateLiveEntries(t *testing.T) {
	t.Parallel()

	m := newLiveMetrics()

	entries := make([]LiveCardEntry, 20)
	pages, omitted := paginateLiveEntries(&m, entries)
	if omitted != 0 {
		t.Fatalf("omitted = %d, want 0", omitted)
	}
	total := 0
	for _, page := range pages {
		if len(page) == 0 {
			t.Fatal("empty page produced")
		}
		total += len(page)
	}
	if total != 20 {
		t.Fatalf("total across pages = %d, want 20", total)
	}

	many := make([]LiveCardEntry, 200)
	capped, omitted := paginateLiveEntries(&m, many)
	if len(capped) != calendarMaxPages {
		t.Fatalf("len(capped) = %d, want %d", len(capped), calendarMaxPages)
	}
	rendered := 0
	for _, page := range capped {
		rendered += len(page)
	}
	if rendered+omitted != 200 {
		t.Fatalf("rendered %d + omitted %d != 200", rendered, omitted)
	}
	if omitted == 0 {
		t.Fatal("omitted = 0, want > 0")
	}
}

func TestDropUncoveredRunes(t *testing.T) {
	fontMu.Lock()
	defer fontMu.Unlock()

	face, err := fonts.CaptionFaceSized(22 * scaleFactor)
	if err != nil {
		t.Fatalf("CaptionFaceSized() error = %v", err)
	}

	got := dropUncoveredRunes(face, "🎮페코라 東京 ライブ🔴")
	if strings.ContainsRune(got, '🎮') || strings.ContainsRune(got, '🔴') {
		t.Errorf("dropUncoveredRunes() = %q, want emoji removed", got)
	}
	for _, keep := range []string{"페코라", "東京", "ライブ"} {
		if !strings.Contains(got, keep) {
			t.Errorf("dropUncoveredRunes() = %q, want %q kept", got, keep)
		}
	}
}
