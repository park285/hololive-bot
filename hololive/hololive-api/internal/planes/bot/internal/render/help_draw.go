package render

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"strings"

	"golang.org/x/image/font"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/render/cardkit"
)

const (
	helpCardFrameX          = 16
	helpCardFrameY          = 12
	helpCardFrameInset      = 6
	helpCardFrameRadius     = 34
	helpCardContentX        = 56
	helpCardContentWidth    = 1336
	helpCardCommandX        = 88
	helpCardDescriptionX    = 500
	helpCardColumnDividerX  = 468
	helpCardTableHeaderY    = 178
	helpCardTableHeaderH    = 54
	helpCardTableBodyY      = 246
	helpCardFooterBaselineY = 1028
)

var (
	helpColorOuter        = color.RGBA{R: 255, G: 250, B: 243, A: 255}
	helpColorBorder       = color.RGBA{R: 57, G: 141, B: 204, A: 255}
	helpColorSurface      = color.RGBA{R: 255, G: 252, B: 248, A: 255}
	helpColorTitle        = color.RGBA{R: 24, G: 63, B: 92, A: 255}
	helpColorMuted        = color.RGBA{R: 107, G: 135, B: 153, A: 255}
	helpColorHeader       = color.RGBA{R: 234, G: 245, B: 251, A: 255}
	helpColorSection      = color.RGBA{R: 30, G: 95, B: 138, A: 255}
	helpColorCommand      = color.RGBA{R: 30, G: 95, B: 138, A: 255}
	helpColorDescription  = color.RGBA{R: 52, G: 77, B: 95, A: 255}
	helpColorRule         = color.RGBA{R: 213, G: 232, B: 243, A: 255}
	helpColorRowPrimary   = color.RGBA{R: 255, G: 252, B: 248, A: 255}
	helpColorRowAlternate = color.RGBA{R: 247, G: 251, B: 253, A: 255}
	helpColorBadge        = color.RGBA{R: 226, G: 241, B: 250, A: 255}
)

func renderHelpCards(ctx context.Context, text string) ([][]byte, error) {
	fontMu.Lock()
	defer fontMu.Unlock()

	faces, err := loadHelpCardFonts()
	if err != nil {
		return nil, err
	}
	document, err := parseHelpCardDocument(ctx, text, &faces)
	if err != nil {
		return nil, err
	}
	pages, err := paginateHelpDocument(document)
	if err != nil {
		return nil, err
	}

	images := make([][]byte, 0, len(pages))
	for _, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		imageData, err := renderHelpPage(ctx, page, &faces)
		if err != nil {
			return nil, err
		}
		images = append(images, imageData)
	}
	return images, nil
}

func renderHelpPage(ctx context.Context, page helpCardPage, faces *helpCardFonts) ([]byte, error) {
	if page.contentHeight > helpCardMaxContentH {
		return nil, fmt.Errorf("help card content height %d exceeds %d", page.contentHeight, helpCardMaxContentH)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	canvas := newHelpCardCanvas()
	drawHelpFrame(canvas)
	drawHelpHeader(canvas, faces, page)
	if err := drawHelpTable(ctx, canvas, faces, page); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return cardkit.EncodePNG(canvas, helpCardOutputWidth)
}

func newHelpCardCanvas() *image.RGBA {
	return cardkit.NewCanvas(helpCardCanvasWidth, helpCardCanvasHeight, helpColorOuter)
}

func drawHelpFrame(canvas *image.RGBA) {
	outer := image.Rect(
		helpCardFrameX,
		helpCardFrameY,
		helpCardCanvasWidth-helpCardFrameX,
		helpCardCanvasHeight-helpCardFrameY,
	)
	cardkit.FillRoundedRect(canvas, outer, helpCardFrameRadius, helpColorBorder)
	inner := image.Rect(
		helpCardFrameX+helpCardFrameInset,
		helpCardFrameY+helpCardFrameInset,
		helpCardCanvasWidth-helpCardFrameX-helpCardFrameInset,
		helpCardCanvasHeight-helpCardFrameY-helpCardFrameInset,
	)
	cardkit.FillRoundedRect(canvas, inner, helpCardFrameRadius-helpCardFrameInset, helpColorSurface)
}

func drawHelpHeader(canvas *image.RGBA, faces *helpCardFonts, page helpCardPage) {
	cardkit.DrawText(canvas, faces.title, 64, 96, helpColorTitle, page.title)
	cardkit.DrawText(canvas, faces.subtitle, 67, 136, helpColorMuted, page.subtitle)
	cardkit.FillRoundedRect(canvas, image.Rect(64, 151, 1384, 157), 3, helpColorBorder)
	drawHelpPageBadge(canvas, faces.header, page.subtitle)
}

func drawHelpPageBadge(canvas *image.RGBA, face font.Face, subtitle string) {
	label := "명령어 안내"
	if fraction := pageFraction(subtitle); fraction != "" {
		label += " " + fraction
	}
	width := cardkit.MeasureText(face, label) + 44
	right := 1378
	cardkit.FillRoundedRect(canvas, image.Rect(right-width, 54, right, 104), 25, helpColorBadge)
	cardkit.DrawText(canvas, face, right-width+22, 88, helpColorCommand, label)
}

func pageFraction(subtitle string) string {
	for _, field := range strings.Fields(subtitle) {
		parts := strings.Split(field, "/")
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			return field
		}
	}
	return ""
}

func drawHelpTable(ctx context.Context, canvas *image.RGBA, faces *helpCardFonts, page helpCardPage) error {
	cardkit.FillRoundedRect(
		canvas,
		image.Rect(helpCardContentX, helpCardTableHeaderY, helpCardContentX+helpCardContentWidth, helpCardTableHeaderY+helpCardTableHeaderH),
		16,
		helpColorHeader,
	)
	cardkit.DrawText(canvas, faces.header, helpCardCommandX, 212, helpColorCommand, "명령어")
	cardkit.DrawText(canvas, faces.header, helpCardDescriptionX, 212, helpColorCommand, "설명")
	cardkit.FillRect(canvas, image.Rect(helpCardColumnDividerX, 185, helpCardColumnDividerX+2, 225), helpColorRule)

	y := helpCardTableBodyY
	alternate := false
	for _, section := range page.sections {
		if err := ctx.Err(); err != nil {
			return err
		}
		cardkit.DrawText(canvas, faces.section, helpCardCommandX, y+29, helpColorSection, section.label)
		y += helpCardSectionHeight
		for _, row := range section.rows {
			drawHelpCommandRow(canvas, faces, row, y, alternate)
			y += row.height
			alternate = !alternate
		}
	}

	cardkit.DrawText(
		canvas,
		faces.footer,
		64,
		helpCardFooterBaselineY,
		helpColorMuted,
		"이미지 전송이 불가능한 경우 같은 내용을 텍스트로 안내합니다.",
	)
	return nil
}

func drawHelpCommandRow(canvas *image.RGBA, faces *helpCardFonts, row helpCardRow, y int, alternate bool) {
	background := helpColorRowPrimary
	if alternate {
		background = helpColorRowAlternate
	}
	rowBottom := y + row.height - 4
	cardkit.FillRoundedRect(
		canvas,
		image.Rect(helpCardContentX, y, helpCardContentX+helpCardContentWidth, rowBottom),
		14,
		background,
	)
	cardkit.FillRect(canvas, image.Rect(helpCardColumnDividerX, y+8, helpCardColumnDividerX+2, rowBottom-8), helpColorRule)
	drawHelpTextLines(canvas, faces.command, helpCardCommandX, y, row.height-4, helpCardTextLineHeight, helpColorCommand, row.commandLines)
	drawHelpTextLines(
		canvas,
		faces.description,
		helpCardDescriptionX,
		y,
		row.height-4,
		helpCardTextLineHeight,
		helpColorDescription,
		row.descriptionLines,
	)
	cardkit.FillRect(canvas, image.Rect(72, rowBottom+1, 1376, rowBottom+3), helpColorRule)
}

func drawHelpTextLines(
	canvas *image.RGBA,
	face font.Face,
	x int,
	y int,
	rowHeight int,
	lineHeight int,
	textColor color.Color,
	lines []string,
) {
	if len(lines) == 0 {
		return
	}
	blockHeight := len(lines) * lineHeight
	baseline := y + (rowHeight-blockHeight)/2 + lineHeight - 4
	for _, line := range lines {
		cardkit.DrawText(canvas, face, x, baseline, textColor, line)
		baseline += lineHeight
	}
}
