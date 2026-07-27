package render

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"strings"
	"unicode/utf8"

	"golang.org/x/image/font"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/assets/fonts"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/render/cardkit"
)

const (
	helpCardCanvasWidth   = 1448
	helpCardOutputWidth   = 1080
	helpCardHeaderHeight  = 188
	helpCardBodyPadX      = 88
	helpCardBodyPadTop    = 48
	helpCardBodyPadBottom = 64
	helpCardMaxLines      = 96
	helpCardMaxLineRunes  = 512
	helpCardMaxHeight     = 3072
)

var (
	helpColorBackground = color.RGBA{R: 246, G: 248, B: 252, A: 255}
	helpColorHeader     = color.RGBA{R: 15, G: 23, B: 42, A: 255}
	helpColorAccent     = color.RGBA{R: 14, G: 165, B: 233, A: 255}
	helpColorTitle      = color.RGBA{R: 248, G: 250, B: 252, A: 255}
	helpColorSection    = color.RGBA{R: 3, G: 105, B: 161, A: 255}
	helpColorBody       = color.RGBA{R: 30, G: 41, B: 59, A: 255}
	helpColorRule       = color.RGBA{R: 226, G: 232, B: 240, A: 255}
)

type helpCardFonts struct {
	title   font.Face
	section font.Face
	body    font.Face
}

type helpCardLine struct {
	text   string
	kind   helpCardLineKind
	height int
}

type helpCardLineKind uint8

const (
	helpLineBlank helpCardLineKind = iota
	helpLineSection
	helpLineBody
)

func renderHelpCard(ctx context.Context, text string) ([]byte, error) {
	fontMu.Lock()
	defer fontMu.Unlock()

	faces, err := loadHelpCardFonts()
	if err != nil {
		return nil, err
	}

	title, lines, err := layoutHelpCard(ctx, text, faces)
	if err != nil {
		return nil, err
	}

	canvasHeight := helpCardHeaderHeight + helpCardBodyPadTop + helpCardBodyPadBottom
	for _, line := range lines {
		canvasHeight += line.height
	}
	if canvasHeight > helpCardMaxHeight {
		return nil, fmt.Errorf("help card height %d exceeds %d", canvasHeight, helpCardMaxHeight)
	}

	canvas := cardkit.NewCanvas(helpCardCanvasWidth, canvasHeight, helpColorBackground)
	drawHelpHeader(canvas, faces.title, title)
	if err := drawHelpBody(ctx, canvas, faces, lines); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return cardkit.EncodePNG(canvas, helpCardOutputWidth)
}

func loadHelpCardFonts() (helpCardFonts, error) {
	title, err := fonts.CaptionBoldFaceSized(52)
	if err != nil {
		return helpCardFonts{}, fmt.Errorf("load help title font: %w", err)
	}
	section, err := fonts.CaptionBoldFaceSized(32)
	if err != nil {
		return helpCardFonts{}, fmt.Errorf("load help section font: %w", err)
	}
	body, err := fonts.CaptionFaceSized(27)
	if err != nil {
		return helpCardFonts{}, fmt.Errorf("load help body font: %w", err)
	}
	return helpCardFonts{title: title, section: section, body: body}, nil
}

func layoutHelpCard(
	ctx context.Context,
	text string,
	faces helpCardFonts,
) (string, []helpCardLine, error) {
	source := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(source) == 0 || len(source) > helpCardMaxLines {
		return "", nil, fmt.Errorf("help source line count %d is outside 1..%d", len(source), helpCardMaxLines)
	}

	title := strings.TrimSpace(source[0])
	if title == "" {
		return "", nil, fmt.Errorf("help title is empty")
	}
	title = cardkit.DropUncoveredRunes(faces.title, title)
	title = cardkit.ClampToWidth(faces.title, title, helpCardCanvasWidth-2*helpCardBodyPadX)
	if title == "" {
		return "", nil, fmt.Errorf("help title cannot be rendered")
	}

	lines := make([]helpCardLine, 0, len(source))
	for _, raw := range source[1:] {
		if err := ctx.Err(); err != nil {
			return "", nil, err
		}
		laidOut, err := layoutHelpSourceLine(raw, faces)
		if err != nil {
			return "", nil, err
		}
		if len(lines)+len(laidOut) > helpCardMaxLines {
			return "", nil, fmt.Errorf("help rendered line count exceeds %d", helpCardMaxLines)
		}
		lines = append(lines, laidOut...)
	}
	return title, trimTrailingHelpBlanks(lines), nil
}

func layoutHelpSourceLine(raw string, faces helpCardFonts) ([]helpCardLine, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return []helpCardLine{{kind: helpLineBlank, height: 22}}, nil
	}
	if utf8.RuneCountInString(trimmed) > helpCardMaxLineRunes {
		return nil, fmt.Errorf("help line exceeds %d runes", helpCardMaxLineRunes)
	}

	kind := helpLineBody
	face := faces.body
	height := 44
	if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
		kind = helpLineSection
		face = faces.section
		height = 58
	}

	trimmed = cardkit.DropUncoveredRunes(face, trimmed)
	wrapped := wrapHelpLine(face, trimmed, helpCardCanvasWidth-2*helpCardBodyPadX)
	if len(wrapped) == 0 {
		return nil, fmt.Errorf("help line cannot be rendered")
	}

	lines := make([]helpCardLine, 0, len(wrapped))
	for _, line := range wrapped {
		lines = append(lines, helpCardLine{text: line, kind: kind, height: height})
	}
	return lines, nil
}

func wrapHelpLine(face font.Face, text string, maxWidth int) []string {
	if text == "" {
		return nil
	}
	if cardkit.MeasureText(face, text) <= maxWidth {
		return []string{text}
	}

	runes := []rune(text)
	lines := make([]string, 0, 2)
	for len(runes) > 0 {
		end := fittingHelpPrefix(face, runes, maxWidth)
		if end < len(runes) {
			end = preferredHelpBreak(runes, end)
		}

		line := strings.TrimSpace(string(runes[:end]))
		if line != "" {
			lines = append(lines, line)
		}
		runes = runes[end:]
		for len(runes) > 0 && runes[0] == ' ' {
			runes = runes[1:]
		}
	}
	return lines
}

func fittingHelpPrefix(face font.Face, runes []rune, maxWidth int) int {
	low, high := 1, len(runes)
	for low < high {
		mid := low + (high-low+1)/2
		if cardkit.MeasureText(face, strings.TrimSpace(string(runes[:mid]))) <= maxWidth {
			low = mid
		} else {
			high = mid - 1
		}
	}
	return low
}

func preferredHelpBreak(runes []rune, fitting int) int {
	for index := fitting - 1; index > 0; index-- {
		if runes[index] == ' ' {
			return index
		}
	}
	return fitting
}

func trimTrailingHelpBlanks(lines []helpCardLine) []helpCardLine {
	for len(lines) > 0 && lines[len(lines)-1].kind == helpLineBlank {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func drawHelpHeader(canvas *image.RGBA, face font.Face, title string) {
	cardkit.FillRect(canvas, image.Rect(0, 0, helpCardCanvasWidth, helpCardHeaderHeight), helpColorHeader)
	cardkit.FillRect(canvas, image.Rect(0, helpCardHeaderHeight-8, helpCardCanvasWidth, helpCardHeaderHeight), helpColorAccent)
	cardkit.DrawText(canvas, face, helpCardBodyPadX, 116, helpColorTitle, title)
}

func drawHelpBody(
	ctx context.Context,
	canvas *image.RGBA,
	faces helpCardFonts,
	lines []helpCardLine,
) error {
	y := helpCardHeaderHeight + helpCardBodyPadTop
	for _, line := range lines {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch line.kind {
		case helpLineBlank:
			y += line.height
		case helpLineSection:
			cardkit.DrawText(canvas, faces.section, helpCardBodyPadX, y+38, helpColorSection, line.text)
			y += line.height
		case helpLineBody:
			cardkit.DrawText(canvas, faces.body, helpCardBodyPadX+20, y+31, helpColorBody, line.text)
			y += line.height
			cardkit.FillRect(
				canvas,
				image.Rect(helpCardBodyPadX, y-1, helpCardCanvasWidth-helpCardBodyPadX, y),
				helpColorRule,
			)
		}
	}
	return nil
}
