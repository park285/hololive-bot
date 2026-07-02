package cardkit

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"testing"
)

func TestDrawCircularImageAvoidsLegacySoftBilinearDownsample(t *testing.T) {
	t.Parallel()

	src := detailedAvatarSource()
	r := 225
	size := r*2 + 8
	got := image.NewRGBA(image.Rect(0, 0, size, size))
	legacy := image.NewRGBA(image.Rect(0, 0, size, size))
	cx, cy := size/2, size/2
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}

	DrawCircularImage(got, src, cx, cy, r, white)
	drawLegacyBilinearCircularImage(legacy, src, cx, cy, r, white)

	if bytes.Equal(got.Pix, legacy.Pix) {
		t.Fatal("avatar renderer still matches legacy bilinear downsample")
	}
	if gotEnergy, legacyEnergy := avatarEdgeEnergy(got), avatarEdgeEnergy(legacy); gotEnergy <= legacyEnergy {
		t.Fatalf("avatar edge energy = %.2f, want more than legacy %.2f", gotEnergy, legacyEnergy)
	}
}

func detailedAvatarSource() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 512, 512))
	for y := range 512 {
		for x := range 512 {
			base := uint8(96 + (x+y)%72)
			if (x/10+y/14)%2 == 0 {
				base = uint8(min(255, int(base)+80))
			}
			if x > 156 && x < 196 && y > 150 && y < 196 {
				base = 20
			}
			if x > 316 && x < 356 && y > 150 && y < 196 {
				base = 20
			}
			if y > 315 && y < 330 && x > 190 && x < 322 {
				base = 240
			}
			img.SetRGBA(x, y, color.RGBA{
				R: base,
				G: uint8(max(0, int(base)-26)),
				B: uint8(min(255, int(base)+18)),
				A: 255,
			})
		}
	}
	return img
}

func drawLegacyBilinearCircularImage(dst *image.RGBA, src image.Image, cx, cy, r int, bgCol color.RGBA) {
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
			c := legacyBilinearSample(src, bounds, sfx, sfy)
			c = applyEdgeBlend(c, bgCol, dist, fr)
			dst.Set(cx+dx, cy+dy, c)
		}
	}
}

func legacyBilinearSample(src image.Image, bounds image.Rectangle, fx, fy float64) color.RGBA {
	x0 := legacyClamp(int(math.Floor(fx)), bounds.Dx()-1) + bounds.Min.X
	y0 := legacyClamp(int(math.Floor(fy)), bounds.Dy()-1) + bounds.Min.Y
	x1 := legacyClamp(int(math.Floor(fx))+1, bounds.Dx()-1) + bounds.Min.X
	y1 := legacyClamp(int(math.Floor(fy))+1, bounds.Dy()-1) + bounds.Min.Y
	xf := fx - math.Floor(fx)
	yf := fy - math.Floor(fy)

	r00, g00, b00 := legacySampleRGB(src, x0, y0)
	r10, g10, b10 := legacySampleRGB(src, x1, y0)
	r01, g01, b01 := legacySampleRGB(src, x0, y1)
	r11, g11, b11 := legacySampleRGB(src, x1, y1)

	lerp := func(a, b, c, d uint32) uint8 {
		top := float64(a)*(1-xf) + float64(b)*xf
		bot := float64(c)*(1-xf) + float64(d)*xf
		return uint8((top*(1-yf) + bot*yf) / 256)
	}
	return color.RGBA{R: lerp(r00, r10, r01, r11), G: lerp(g00, g10, g01, g11), B: lerp(b00, b10, b01, b11), A: 255}
}

func legacyClamp(v, hi int) int {
	if v < 0 {
		return 0
	}
	if v > hi {
		return hi
	}
	return v
}

func legacySampleRGB(src image.Image, x, y int) (red, green, blue uint32) {
	c := src.At(x, y)
	if c == nil {
		return 0, 0, 0
	}
	r, g, b, _ := c.RGBA()
	return r, g, b
}

func avatarEdgeEnergy(img *image.RGBA) float64 {
	bounds := img.Bounds()
	var total float64
	var count int
	for y := bounds.Min.Y + 1; y < bounds.Max.Y-1; y++ {
		for x := bounds.Min.X + 1; x < bounds.Max.X-1; x++ {
			c := img.RGBAAt(x, y)
			if c.A == 0 {
				continue
			}
			right := img.RGBAAt(x+1, y)
			down := img.RGBAAt(x, y+1)
			total += luminanceDelta(c, right) + luminanceDelta(c, down)
			count += 2
		}
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func luminanceDelta(a, b color.RGBA) float64 {
	la := 0.2126*float64(a.R) + 0.7152*float64(a.G) + 0.0722*float64(a.B)
	lb := 0.2126*float64(b.R) + 0.7152*float64(b.G) + 0.0722*float64(b.B)
	return math.Abs(la - lb)
}
