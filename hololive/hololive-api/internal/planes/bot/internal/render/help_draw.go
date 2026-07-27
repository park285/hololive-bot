package render

import (
	"context"
	"fmt"
	"image"
	"image/color"

	"golang.org/x/image/font"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/render/cardkit"
)

const (
	helpCardPanelX       = 48
	helpCardPanelWidth   = 1352
	helpCardContentX     = 72
	helpCardContentWidth = 1304
	helpCardCommandX     = 112
	helpCardDescriptionX = 510
)

var (
	helpColorBackgroundStart = color.RGBA{R: 15, G: 23, B: 42, A: 255}
	helpColorBackgroundEnd   = color.RGBA{R: 23, G: 37, B: 84, A: 255}
	helpColorTopOrb          = color.RGBA{R: 22, G: 45, B: 81, A: 255}
	helpColorBottomOrb       = color.RGBA{R: 39, G: 43, B: 94, A: 255}
	helpColorPanelShadow     = color.RGBA{R: 8, G: 15, B: 32, A: 255}
	helpColorPanel           = color.RGBA{R: 238, G: 242, B: 255, A: 255}
	helpColorRowPrimary      = color.RGBA{R: 255, G: 255, B: 255, A: 255}
	helpColorRowAlternate    = color.RGBA{R: 246, G: 248, B: 252, A: 255}
	helpColorTitle           = color.RGBA{R: 248, G: 250, B: 252, A: 255}
	helpColorMuted           = color.RGBA{R: 203, G: 213, B: 225, A: 255}
	helpColorCommand         = color.RGBA{R: 15, G: 23, B: 42, A: 255}
	helpColorDescription     = color.RGBA{R: 51, G: 65, B: 85, A: 255}
	helpColorSection         = color.RGBA{R: 30, G: 64, B: 175, A: 255}
)

func renderHelpCards(ctx context.Context, text string) ([][]byte, error) {
	fontMu.Lock()
	defer fontMu.Unlock()

	faces, err := loadHelpCardFonts()
	if err != nil {
		return nil, err
	}
	document, err := parseHelpCardDocument(ctx, text, faces)
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
		imageData, err := renderHelpPage(ctx, page, faces)
		if err != nil {
			return nil, err
		}
		images = append(images, imageData)
	}
	return images, nil
}

func renderHelpPage(ctx context.Context, page helpCardPage, faces helpCardFonts) ([]byte, error) {
	if page.canvasHeight > helpCardMaxHeight {
		return nil, fmt.Errorf("help card height %d exceeds %d", page.canvasHeight, helpCardMaxHeight)
	}
	canvas, err := newHelpCardCanvas(ctx, page.canvasHeight)
	if err != nil {
		return nil, err
	}
	drawHelpDecorations(canvas, page)
	drawHelpHeader(canvas, faces, page)
	if err := drawHelpPanel(ctx, canvas, faces, page); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return cardkit.EncodePNG(canvas, helpCardOutputWidth)
}

func newHelpCardCanvas(ctx context.Context, height int) (*image.RGBA, error) {
	canvas := image.NewRGBA(image.Rect(0, 0, helpCardCanvasWidth, height))
	for y := 0; y < height; y++ {
		if y%64 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		for x := 0; x < helpCardCanvasWidth; x++ {
			position := float64(x)/float64(helpCardCanvasWidth-1) + float64(y)/float64(max(height-1, 1))
			pixel := blendHelpColor(helpColorBackgroundStart, helpColorBackgroundEnd, position/2)
			offset := y*canvas.Stride + x*4
			canvas.Pix[offset] = pixel.R
			canvas.Pix[offset+1] = pixel.G
			canvas.Pix[offset+2] = pixel.B
			canvas.Pix[offset+3] = 255
		}
	}
	return canvas, nil
}

func blendHelpColor(start, end color.RGBA, position float64) color.RGBA {
	blend := func(a, b uint8) uint8 {
		return uint8(float64(a) + (float64(b)-float64(a))*position)
	}
	return color.RGBA{R: blend(start.R, end.R), G: blend(start.G, end.G), B: blend(start.B, end.B), A: 255}
}

func drawHelpDecorations(canvas *image.RGBA, page helpCardPage) {
	cardkit.FillCircle(canvas, 1320, 86, 180, helpColorTopOrb)
	cardkit.FillCircle(canvas, 1180, page.canvasHeight-76, 220, helpColorBottomOrb)
	shadow := image.Rect(
		helpCardPanelX+8,
		helpCardPanelY+14,
		helpCardPanelX+helpCardPanelWidth+8,
		helpCardPanelY+page.panelHeight+14,
	)
	cardkit.FillRoundedRect(canvas, shadow, 34, helpColorPanelShadow)
	panel := image.Rect(
		helpCardPanelX,
		helpCardPanelY,
		helpCardPanelX+helpCardPanelWidth,
		helpCardPanelY+page.panelHeight,
	)
	cardkit.FillRoundedRect(canvas, panel, 34, helpColorPanel)
}

func drawHelpHeader(canvas *image.RGBA, faces helpCardFonts, page helpCardPage) {
	cardkit.DrawText(canvas, faces.title, 72, 96, helpColorTitle, page.title)
	cardkit.DrawText(canvas, faces.subtitle, 76, 146, helpColorMuted, page.subtitle)
}

func drawHelpPanel(ctx context.Context, canvas *image.RGBA, faces helpCardFonts, page helpCardPage) error {
	cardkit.DrawText(canvas, faces.header, helpCardCommandX, 226, helpColorDescription, "명령어")
	cardkit.DrawText(canvas, faces.header, helpCardDescriptionX, 226, helpColorDescription, "설명")

	y := helpCardPanelY + helpCardPanelHeaderH
	alternate := false
	for _, section := range page.sections {
		if err := ctx.Err(); err != nil {
			return err
		}
		cardkit.DrawText(canvas, faces.section, helpCardCommandX, y+36, helpColorSection, section.label)
		y += 54
		for _, row := range section.rows {
			drawHelpCommandRow(canvas, faces, row, y, alternate)
			y += row.height
			alternate = !alternate
		}
	}

	cardkit.DrawText(
		canvas,
		faces.footer,
		72,
		page.canvasHeight-42,
		helpColorMuted,
		"이미지 전송이 불가능한 경우 같은 내용을 텍스트로 안내합니다.",
	)
	return nil
}

func drawHelpCommandRow(canvas *image.RGBA, faces helpCardFonts, row helpCardRow, y int, alternate bool) {
	background := helpColorRowPrimary
	if alternate {
		background = helpColorRowAlternate
	}
	rowBottom := y + row.height - 6
	cardkit.FillRoundedRect(
		canvas,
		image.Rect(helpCardContentX, y, helpCardContentX+helpCardContentWidth, rowBottom),
		20,
		background,
	)

	contentHeight := row.height - 6
	drawHelpTextLines(canvas, faces.command, helpCardCommandX, y, contentHeight, 34, helpColorCommand, row.commandLines)
	drawHelpTextLines(
		canvas,
		faces.description,
		helpCardDescriptionX,
		y,
		contentHeight,
		34,
		helpColorDescription,
		row.descriptionLines,
	)
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
	baseline := y + (rowHeight-blockHeight)/2 + lineHeight - 5
	for _, line := range lines {
		cardkit.DrawText(canvas, face, x, baseline, textColor, line)
		baseline += lineHeight
	}
}
