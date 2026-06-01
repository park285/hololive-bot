package render

import (
	"image"
	"image/color"
	stddraw "image/draw"
	"math"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

const (
	scaleFactor     = 5
	canvasWidth     = 620 * scaleFactor
	maxCanvasPixels = 48_000_000
	maxCanvasH      = min(4000*scaleFactor, maxCanvasPixels/canvasWidth)
	paddingX        = 28 * scaleFactor
	paddingY        = 20 * scaleFactor
	headerH         = 82 * scaleFactor
	dateSectGap     = 12 * scaleFactor
	dateHeaderH     = 34 * scaleFactor
	entryRowH       = 104 * scaleFactor
	entryIndent     = 20 * scaleFactor
	separatorH      = 1 * scaleFactor
	avatarSize      = 90 * scaleFactor
	avatarGap       = 18 * scaleFactor
	badgePadX       = 12 * scaleFactor
	badgePadY       = 5 * scaleFactor
	badgeH          = 32 * scaleFactor
	badgeRadius     = 9 * scaleFactor
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
)

func drawText(img *image.RGBA, face font.Face, x, y int, col color.Color, text string) {
	defer func() {
		_ = recover()
	}()
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(col),
		Face: face,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(text)
}

func measureText(face font.Face, text string) int {
	defer func() {
		_ = recover()
	}()
	d := &font.Drawer{Face: face}
	return d.MeasureString(text).Ceil()
}

func fillRect(img *image.RGBA, rect image.Rectangle, col color.Color) {
	stddraw.Draw(img, rect, image.NewUniform(col), image.Point{}, stddraw.Src)
}

func fillRoundedRect(img *image.RGBA, rect image.Rectangle, radius int, col color.Color) {
	u := image.NewUniform(col)
	stddraw.Draw(img, image.Rect(rect.Min.X+radius, rect.Min.Y, rect.Max.X-radius, rect.Max.Y), u, image.Point{}, stddraw.Src)
	stddraw.Draw(img, image.Rect(rect.Min.X, rect.Min.Y+radius, rect.Max.X, rect.Max.Y-radius), u, image.Point{}, stddraw.Src)
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
	c := color.RGBAModel.Convert(col).(color.RGBA)
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

func drawCircularImage(dst *image.RGBA, src image.Image, cx, cy, r int, bgCol color.RGBA) {
	avatar := sharpenAvatar(resizeAvatarSource(src, r*2))
	fr := float64(r)

	for dy := -r; dy < r; dy++ {
		for dx := -r; dx < r; dx++ {
			dist := math.Sqrt(float64(dx*dx + dy*dy))
			if dist > fr+0.5 {
				continue
			}
			c := avatar.RGBAAt(dx+r, dy+r)
			c = applyEdgeBlend(c, bgCol, dist, fr)
			dst.Set(cx+dx, cy+dy, c)
		}
	}
}

func resizeAvatarSource(src image.Image, size int) *image.RGBA {
	bounds := src.Bounds()
	side := min(bounds.Dx(), bounds.Dy())
	cropMinX := bounds.Min.X + (bounds.Dx()-side)/2
	cropMinY := bounds.Min.Y + (bounds.Dy()-side)/2
	srcRect := image.Rect(cropMinX, cropMinY, cropMinX+side, cropMinY+side)
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, srcRect, xdraw.Src, nil)
	return dst
}

func sharpenAvatar(src *image.RGBA) *image.RGBA {
	bounds := src.Bounds()
	if bounds.Dx() < 3 || bounds.Dy() < 3 {
		return src
	}

	dst := image.NewRGBA(bounds)
	copy(dst.Pix, src.Pix)
	for y := bounds.Min.Y + 1; y < bounds.Max.Y-1; y++ {
		for x := bounds.Min.X + 1; x < bounds.Max.X-1; x++ {
			center := src.RGBAAt(x, y)
			left := src.RGBAAt(x-1, y)
			right := src.RGBAAt(x+1, y)
			up := src.RGBAAt(x, y-1)
			down := src.RGBAAt(x, y+1)
			dst.SetRGBA(x, y, color.RGBA{
				R: sharpenChannel(center.R, left.R, right.R, up.R, down.R),
				G: sharpenChannel(center.G, left.G, right.G, up.G, down.G),
				B: sharpenChannel(center.B, left.B, right.B, up.B, down.B),
				A: center.A,
			})
		}
	}
	return dst
}

func applyEdgeBlend(c, bgCol color.RGBA, dist, fr float64) color.RGBA {
	if dist > fr-0.5 {
		return blendRGBA(c, bgCol, fr+0.5-dist)
	}
	return c
}

const avatarSharpenAmount = 0.32

func sharpenChannel(center, left, right, up, down uint8) uint8 {
	neighborAvg := float64(int(left)+int(right)+int(up)+int(down)) / 4
	value := float64(center) + avatarSharpenAmount*(float64(center)-neighborAvg)
	return uint8(clampI(int(math.Round(value)), 0, 255))
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
