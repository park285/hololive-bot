package cardkit

import (
	"image"
	"image/color"
	stddraw "image/draw"
	"math"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

func DrawText(img *image.RGBA, face font.Face, x, y int, col color.Color, text string) {
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(col),
		Face: face,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(text)
}

func DrawCenteredText(img *image.RGBA, face font.Face, cx, y int, col color.Color, text string) {
	w := MeasureText(face, text)
	DrawText(img, face, cx-w/2, y, col, text)
}

func MeasureText(face font.Face, text string) int {
	d := &font.Drawer{Face: face}
	return d.MeasureString(text).Ceil()
}

func ClampToWidth(face font.Face, s string, maxPx int) string {
	if maxPx <= 0 {
		return ""
	}
	if MeasureText(face, s) <= maxPx {
		return s
	}
	const ellipsis = "…"
	runes := []rune(s)
	for len(runes) > 0 {
		runes = runes[:len(runes)-1]
		trimmed := strings.TrimRight(string(runes), " ")
		if trimmed == "" {
			break
		}
		if MeasureText(face, trimmed+ellipsis) <= maxPx {
			return trimmed + ellipsis
		}
	}
	return ""
}

// 임베드 폰트 밖의 rune(이모지 등)은 두부(notdef 박스)로 그려진다 —
// 그리기 전에 커버되지 않는 rune을 떨군다.
func DropUncoveredRunes(face font.Face, s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if _, ok := face.GlyphAdvance(r); ok {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func FillRect(img *image.RGBA, rect image.Rectangle, col color.Color) {
	stddraw.Draw(img, rect, image.NewUniform(col), image.Point{}, stddraw.Src)
}

func FillRoundedRect(img *image.RGBA, rect image.Rectangle, radius int, col color.Color) {
	u := image.NewUniform(col)
	stddraw.Draw(img, image.Rect(rect.Min.X+radius, rect.Min.Y, rect.Max.X-radius, rect.Max.Y), u, image.Point{}, stddraw.Src)
	stddraw.Draw(img, image.Rect(rect.Min.X, rect.Min.Y+radius, rect.Max.X, rect.Max.Y-radius), u, image.Point{}, stddraw.Src)
	for _, c := range [][2]int{
		{rect.Min.X + radius, rect.Min.Y + radius},
		{rect.Max.X - radius - 1, rect.Min.Y + radius},
		{rect.Min.X + radius, rect.Max.Y - radius - 1},
		{rect.Max.X - radius - 1, rect.Max.Y - radius - 1},
	} {
		FillCircle(img, c[0], c[1], radius, col)
	}
}

func FillCircle(img *image.RGBA, cx, cy, r int, col color.Color) {
	c, ok := color.RGBAModel.Convert(col).(color.RGBA)
	if !ok {
		c = color.RGBA{}
	}
	fcx, fcy, fr := float64(cx)+0.5, float64(cy)+0.5, float64(r)
	for y := cy - r - 1; y <= cy+r+1; y++ {
		for x := cx - r - 1; x <= cx+r+1; x++ {
			cov := fr + 0.5 - math.Hypot(float64(x)+0.5-fcx, float64(y)+0.5-fcy)
			blendCoveragePixel(img, x, y, c, cov)
		}
	}
}

func blendCoveragePixel(img *image.RGBA, x, y int, c color.RGBA, cov float64) {
	if cov <= 0 {
		return
	}
	if cov >= 1 {
		img.SetRGBA(x, y, c)
		return
	}
	img.SetRGBA(x, y, blendRGBA(c, img.RGBAAt(x, y), cov))
}

func blendRGBA(fg, bg color.RGBA, a float64) color.RGBA {
	if a < 0 {
		a = 0
	}
	if a > 1 {
		a = 1
	}
	return color.RGBA{
		R: clampChannel(float64(fg.R)*a + float64(bg.R)*(1-a)),
		G: clampChannel(float64(fg.G)*a + float64(bg.G)*(1-a)),
		B: clampChannel(float64(fg.B)*a + float64(bg.B)*(1-a)),
		A: 255,
	}
}

func clampChannel(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}
