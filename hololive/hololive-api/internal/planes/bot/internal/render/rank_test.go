package render

import (
	"bytes"
	"context"
	"image/png"
	"log/slog"
	"strings"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/assets/fonts"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/render/cardkit"
	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

func rankFixture() []RankCardEntry {
	return []RankCardEntry{
		{Rank: 1, Name: "페코라", Delta: "+1.2만", Total: "260만"},
		{Rank: 2, Name: "마린", Delta: "+9,800", Total: "330만"},
		{Rank: 3, Name: "스이세이", Delta: "+7,200", Total: "250만"},
		{Rank: 4, Name: "후부키", Delta: "+5,100", Total: "245만"},
	}
}

func TestRankCardRenderer_RenderRankImage(t *testing.T) {
	t.Parallel()

	img, err := NewRankCardRenderer().RenderRankImage("주간", rankFixture())
	if err != nil {
		t.Fatalf("RenderRankImage() error = %v", err)
	}
	assertValidPNG(t, img)

	decoded, decErr := png.Decode(bytes.NewReader(img))
	if decErr != nil {
		t.Fatalf("png.Decode() error = %v", decErr)
	}
	if decoded.Bounds().Dx() != calendarOutputWidth {
		t.Errorf("width = %d, want %d", decoded.Bounds().Dx(), calendarOutputWidth)
	}
}

func TestRankCardRenderer_RenderRankImage_EmptyErrors(t *testing.T) {
	t.Parallel()

	if _, err := NewRankCardRenderer().RenderRankImage("주간", nil); err == nil {
		t.Fatal("RenderRankImage(empty) error = nil, want error")
	}
}

func TestRankCardRenderer_CachesByContent(t *testing.T) {
	t.Parallel()

	r := NewRankCardRenderer()
	entries := rankFixture()

	first, err := r.RenderRankImage("주간", entries)
	if err != nil {
		t.Fatalf("first render: %v", err)
	}
	first[0] = 0

	second, err := r.RenderRankImage("주간", entries)
	if err != nil {
		t.Fatalf("second render: %v", err)
	}
	assertValidPNG(t, second)

	if rankCardCacheKey("주간", entries) == rankCardCacheKey("월간", entries) {
		t.Fatal("cache key must include period label")
	}
	changed := append([]RankCardEntry{}, entries...)
	changed[0].Delta = "+2.0만"
	if rankCardCacheKey("주간", changed) == rankCardCacheKey("주간", entries) {
		t.Fatal("cache key must change when entries change")
	}
}

func TestRankStrings_SeededRowsMatchFallbackLiterals(t *testing.T) {
	store := messagestrings.NewStore(dbtest.NewPool(t), slog.Default())
	if err := store.Load(context.Background()); err != nil {
		t.Fatalf("load message_strings: %v", err)
	}

	cases := []struct {
		key      string
		fallback string
	}{
		{"header", "구독자 증가 순위"},
		{"summary", "%s · 상위 %d"},
		{"total", "구독자 %s"},
	}
	for _, c := range cases {
		if got := store.Get(messagestrings.NamespaceRankCard, c.key); got != c.fallback {
			t.Errorf("seeded rankcard/%s = %q, want %q", c.key, got, c.fallback)
		}
	}

	m := newRankMetrics()
	if got := m.rankSummaryText("주간", 10); got != "주간 · 상위 10" {
		t.Errorf("nil-store summary = %q", got)
	}
	if got := m.rankTotalText("260만"); got != "구독자 260만" {
		t.Errorf("nil-store total = %q", got)
	}
}

func TestDropUncoveredRunes(t *testing.T) {
	fontMu.Lock()
	defer fontMu.Unlock()

	face, err := fonts.CaptionFaceSized(22 * scaleFactor)
	if err != nil {
		t.Fatalf("CaptionFaceSized() error = %v", err)
	}

	got := cardkit.DropUncoveredRunes(face, "🎮페코라 東京 ライブ🔴")
	if strings.ContainsRune(got, '🎮') || strings.ContainsRune(got, '🔴') {
		t.Errorf("dropUncoveredRunes() = %q, want emoji removed", got)
	}
	for _, keep := range []string{"페코라", "東京", "ライブ"} {
		if !strings.Contains(got, keep) {
			t.Errorf("dropUncoveredRunes() = %q, want %q kept", got, keep)
		}
	}
}
