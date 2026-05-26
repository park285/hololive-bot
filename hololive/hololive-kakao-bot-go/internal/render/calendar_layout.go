package render

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

const (
	scaleFactor = 2
	canvasWidth = 620 * scaleFactor
	maxCanvasH  = 4000 * scaleFactor
	paddingX    = 28 * scaleFactor
	paddingY    = 20 * scaleFactor
	headerH     = 70 * scaleFactor
	dateSectGap = 12 * scaleFactor
	dateHeaderH = 28 * scaleFactor
	entryRowH   = 58 * scaleFactor
	entryIndent = 20 * scaleFactor
	separatorH  = 1 * scaleFactor
	avatarSize  = 44 * scaleFactor
	avatarGap   = 14 * scaleFactor
	badgePadX   = 10 * scaleFactor
	badgePadY   = 4 * scaleFactor
	badgeH      = 24 * scaleFactor
	badgeRadius = 7 * scaleFactor
)

var (
	colWhite      = color.RGBA{R: 255, G: 255, B: 255, A: 255}
	colSlate100   = color.RGBA{R: 241, G: 245, B: 249, A: 255}
	colSlate200   = color.RGBA{R: 226, G: 232, B: 240, A: 255}
	colSlate500   = color.RGBA{R: 100, G: 116, B: 139, A: 255}
	colSlate800   = color.RGBA{R: 30, G: 41, B: 59, A: 255}
	colAmber50    = color.RGBA{R: 255, G: 251, B: 235, A: 255}
	colAmber600   = color.RGBA{R: 217, G: 119, B: 6, A: 255}
	colEmerald50  = color.RGBA{R: 236, G: 253, B: 245, A: 255}
	colEmerald600 = color.RGBA{R: 5, G: 150, B: 105, A: 255}
	colSky400     = color.RGBA{R: 56, G: 189, B: 248, A: 255}
)

func drawText(img *image.RGBA, face font.Face, x, y int, col color.Color, text string) {
	defer func() { recover() }()
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(col),
		Face: face,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(text)
}

func measureText(face font.Face, text string) int {
	defer func() { recover() }()
	d := &font.Drawer{Face: face}
	return d.MeasureString(text).Ceil()
}

func fillRect(img *image.RGBA, rect image.Rectangle, col color.Color) {
	draw.Draw(img, rect, image.NewUniform(col), image.Point{}, draw.Src)
}

func fillRoundedRect(img *image.RGBA, rect image.Rectangle, radius int, col color.Color) {
	u := image.NewUniform(col)
	draw.Draw(img, image.Rect(rect.Min.X+radius, rect.Min.Y, rect.Max.X-radius, rect.Max.Y), u, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(rect.Min.X, rect.Min.Y+radius, rect.Max.X, rect.Max.Y-radius), u, image.Point{}, draw.Src)
	for _, c := range [][2]int{
		{rect.Min.X + radius, rect.Min.Y + radius},
		{rect.Max.X - radius - 1, rect.Min.Y + radius},
		{rect.Min.X + radius, rect.Max.Y - radius - 1},
		{rect.Max.X - radius - 1, rect.Max.Y - radius - 1},
	} {
		fillCircle(img, c[0], c[1], radius, col)
	}
}

func fillCircle(img *image.RGBA, cx, cy, r int, col color.Color) {
	u := image.NewUniform(col)
	r2 := r * r
	for dy := -r; dy <= r; dy++ {
		dx := int(math.Sqrt(float64(r2 - dy*dy)))
		draw.Draw(img, image.Rect(cx-dx, cy+dy, cx+dx+1, cy+dy+1), u, image.Point{}, draw.Src)
	}
}

func drawCircularImage(dst *image.RGBA, src image.Image, cx, cy, r int, bgCol color.RGBA) {
	bounds := src.Bounds()
	srcW := float64(bounds.Dx())
	srcH := float64(bounds.Dy())
	diameter := float64(r * 2)
	fr := float64(r)

	for dy := -r; dy < r; dy++ {
		for dx := -r; dx < r; dx++ {
			dist := math.Sqrt(float64(dx*dx + dy*dy))
			if dist > fr+0.5 {
				continue
			}
			sfx := (float64(dx+r) + 0.5) * srcW / diameter
			sfy := (float64(dy+r) + 0.5) * srcH / diameter
			c := bilinearSample(src, bounds, sfx, sfy)
			if dist > fr-0.5 {
				a := fr + 0.5 - dist
				c = blendRGBA(c, bgCol, a)
			}
			dst.Set(cx+dx, cy+dy, c)
		}
	}
}

func bilinearSample(src image.Image, bounds image.Rectangle, fx, fy float64) color.RGBA {
	x0 := clampI(int(math.Floor(fx)), 0, bounds.Dx()-1) + bounds.Min.X
	y0 := clampI(int(math.Floor(fy)), 0, bounds.Dy()-1) + bounds.Min.Y
	x1 := clampI(int(math.Floor(fx))+1, 0, bounds.Dx()-1) + bounds.Min.X
	y1 := clampI(int(math.Floor(fy))+1, 0, bounds.Dy()-1) + bounds.Min.Y
	xf := fx - math.Floor(fx)
	yf := fy - math.Floor(fy)

	r00, g00, b00, _ := src.At(x0, y0).RGBA()
	r10, g10, b10, _ := src.At(x1, y0).RGBA()
	r01, g01, b01, _ := src.At(x0, y1).RGBA()
	r11, g11, b11, _ := src.At(x1, y1).RGBA()

	lerp := func(a, b, c, d uint32) uint8 {
		top := float64(a)*(1-xf) + float64(b)*xf
		bot := float64(c)*(1-xf) + float64(d)*xf
		return uint8((top*(1-yf) + bot*yf) / 256)
	}
	return color.RGBA{R: lerp(r00, r10, r01, r11), G: lerp(g00, g10, g01, g11), B: lerp(b00, b10, b01, b11), A: 255}
}

func blendRGBA(fg, bg color.RGBA, a float64) color.RGBA {
	if a < 0 {
		a = 0
	}
	if a > 1 {
		a = 1
	}
	return color.RGBA{
		R: uint8(float64(fg.R)*a + float64(bg.R)*(1-a)),
		G: uint8(float64(fg.G)*a + float64(bg.G)*(1-a)),
		B: uint8(float64(fg.B)*a + float64(bg.B)*(1-a)),
		A: 255,
	}
}

func clampI(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
