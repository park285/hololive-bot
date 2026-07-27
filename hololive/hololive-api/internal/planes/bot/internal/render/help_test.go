package render

import (
	"bytes"
	"context"
	"image/png"
	"strings"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/render/cardkit"
)

func TestHelpCardRendererRendersBoundedPNGAndReturnsOwnedCopies(t *testing.T) {
	renderer := NewHelpCardRenderer()
	text := "홀로라이브 봇 명령어\n\n[방송]\n  !라이브 - 방송 중 목록\n  !예정 - 예정 방송 목록\n\n[기타]\n  !도움말 - 도움말"

	first, err := renderer.RenderHelpImage(t.Context(), text)
	if err != nil {
		t.Fatalf("RenderHelpImage() error = %v", err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(first))
	if err != nil {
		t.Fatalf("decode PNG config: %v", err)
	}
	if config.Width != helpCardOutputWidth {
		t.Fatalf("PNG width = %d, want %d", config.Width, helpCardOutputWidth)
	}
	if config.Height <= 0 || config.Height > helpCardMaxHeight {
		t.Fatalf("PNG height = %d, want 1..%d", config.Height, helpCardMaxHeight)
	}
	if len(first) > helpCardMaxPNGBytes {
		t.Fatalf("PNG size = %d, want <= %d", len(first), helpCardMaxPNGBytes)
	}

	first[0] ^= 0xff
	second, err := renderer.RenderHelpImage(t.Context(), text)
	if err != nil {
		t.Fatalf("second RenderHelpImage() error = %v", err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("cached PNG must be returned as an owned copy")
	}
	if _, err := png.DecodeConfig(bytes.NewReader(second)); err != nil {
		t.Fatalf("cached PNG is invalid: %v", err)
	}
}

func TestHelpCardRendererRejectsCanceledAndOversizedInput(t *testing.T) {
	renderer := NewHelpCardRenderer()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := renderer.RenderHelpImage(ctx, "도움말"); err == nil {
		t.Fatal("RenderHelpImage() accepted a canceled context")
	}

	oversized := "도움말\n" + strings.Repeat("가", helpCardMaxTextBytes)
	if _, err := renderer.RenderHelpImage(t.Context(), oversized); err == nil {
		t.Fatal("RenderHelpImage() accepted oversized text")
	}
}

func TestWrapHelpLineKeepsEveryRune(t *testing.T) {
	fontMu.Lock()
	defer fontMu.Unlock()

	faces, err := loadHelpCardFonts()
	if err != nil {
		t.Fatalf("loadHelpCardFonts() error = %v", err)
	}

	text := "방송이력 카테고리 게임 기간 개수 필터를 함께 사용할 수 있습니다"
	maxWidth := cardkit.MeasureText(faces.body, "방송이력 카테고리 게임")
	lines := wrapHelpLine(faces.body, text, maxWidth)
	if len(lines) < 2 {
		t.Fatalf("wrapped line count = %d, want at least 2", len(lines))
	}
	if strings.Join(lines, " ") != text {
		t.Fatalf("wrapped text = %q, want %q", strings.Join(lines, " "), text)
	}
}
