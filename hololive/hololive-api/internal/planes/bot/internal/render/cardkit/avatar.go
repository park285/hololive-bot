package cardkit

import (
	"image"
	"image/color"
	"math"
	"unicode/utf8"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
)

type AvatarStyle struct {
	Ring       color.RGBA
	RingWidth  int
	Accent     color.RGBA
	Background color.RGBA
	Initials   font.Face
	TextColor  color.RGBA
	// 이니셜 baseline을 원 중심에서 아래로 내리는 오프셋(글리프 광학 중심 보정)
	InitialDrop int
}

func AvatarCircle(img *image.RGBA, cx, cy, r int, photo image.Image, name string, style AvatarStyle) {
	FillCircle(img, cx, cy, r+style.RingWidth, style.Ring)

	if photo != nil {
		DrawCircularImage(img, photo, cx, cy, r, style.Background)
		return
	}

	FillCircle(img, cx, cy, r, style.Accent)
	initial := FirstRune(DropUncoveredRunes(style.Initials, name))
	if initial == "" {
		return
	}
	iw := MeasureText(style.Initials, initial)
	DrawText(img, style.Initials, cx-iw/2, cy+style.InitialDrop, style.TextColor, initial)
}

func FirstRune(s string) string {
	if s == "" {
		return ""
	}
	r, _ := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return ""
	}
	return string(r)
}

func DrawCircularImage(dst *image.RGBA, src image.Image, cx, cy, r int, bgCol color.RGBA) {
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

const avatarSharpenAmount = 0.32

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

func sharpenChannel(center, left, right, up, down uint8) uint8 {
	neighborAvg := float64(int(left)+int(right)+int(up)+int(down)) / 4
	return clampChannel(math.Round(float64(center) + avatarSharpenAmount*(float64(center)-neighborAvg)))
}

func applyEdgeBlend(c, bgCol color.RGBA, dist, fr float64) color.RGBA {
	if dist > fr-0.5 {
		return blendRGBA(c, bgCol, fr+0.5-dist)
	}
	return c
}
