package render

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/assets/fonts"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/render/cardkit"
)

func TestClampToWidth(t *testing.T) {
	fontMu.Lock()
	defer fontMu.Unlock()

	face, err := fonts.CaptionFaceSized(22 * scaleFactor)
	if err != nil {
		t.Fatalf("CaptionFaceSized() error = %v", err)
	}

	t.Run("fits unchanged", func(t *testing.T) {
		s := "페코라"
		limit := cardkit.MeasureText(face, s) + 1
		if got := cardkit.ClampToWidth(face, s, limit); got != s {
			t.Errorf("cardkit.ClampToWidth() = %q, want unchanged %q", got, s)
		}
	})

	t.Run("overflow clamps with ellipsis", func(t *testing.T) {
		s := "우사다 페코라 우사다 페코라 우사다 페코라"
		limit := cardkit.MeasureText(face, s) / 2
		got := cardkit.ClampToWidth(face, s, limit)
		if !strings.HasSuffix(got, "…") {
			t.Fatalf("cardkit.ClampToWidth() = %q, want ellipsis suffix", got)
		}
		if w := cardkit.MeasureText(face, got); w > limit {
			t.Errorf("clamped width = %d, want <= %d", w, limit)
		}
		if !strings.HasPrefix(s, strings.TrimSuffix(got, "…")) {
			t.Errorf("clamped %q is not a prefix of source %q", got, s)
		}
	})

	t.Run("mixed KR JP overflow", func(t *testing.T) {
		s := "시라카미 후부키 白上フブキ 호쇼 마린 宝鐘マリン"
		limit := cardkit.MeasureText(face, s) / 3
		got := cardkit.ClampToWidth(face, s, limit)
		if got == "" || !strings.HasSuffix(got, "…") {
			t.Fatalf("cardkit.ClampToWidth() = %q, want non-empty with ellipsis", got)
		}
		if w := cardkit.MeasureText(face, got); w > limit {
			t.Errorf("clamped width = %d, want <= %d", w, limit)
		}
	})

	t.Run("trailing space trimmed before ellipsis", func(t *testing.T) {
		s := "우사다 페코라페코라"
		limit := cardkit.MeasureText(face, "우사다 ") + cardkit.MeasureText(face, "…")/2
		got := cardkit.ClampToWidth(face, s, limit)
		if strings.Contains(got, " …") {
			t.Errorf("cardkit.ClampToWidth() = %q, want no space before ellipsis", got)
		}
	})

	t.Run("non-positive limit", func(t *testing.T) {
		if got := cardkit.ClampToWidth(face, "페코라", 0); got != "" {
			t.Errorf("cardkit.ClampToWidth(0) = %q, want empty", got)
		}
	})

	t.Run("limit below single rune", func(t *testing.T) {
		if got := cardkit.ClampToWidth(face, "페코라", 1); got != "" {
			t.Errorf("cardkit.ClampToWidth(1) = %q, want empty", got)
		}
	})
}
