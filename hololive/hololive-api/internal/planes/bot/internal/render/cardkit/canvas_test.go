package cardkit

import (
	"image"
	"image/color"
	"math"
	"testing"

	xdraw "golang.org/x/image/draw"
)

func TestDownscaleApproximationStaysCloseToCatmullRom(t *testing.T) {
	t.Parallel()

	src := image.NewRGBA(image.Rect(0, 0, 620, 420))
	for y := range src.Bounds().Dy() {
		for x := range src.Bounds().Dx() {
			stripe := uint8(0)

			if (x/17+y/11)%2 == 0 {
				stripe = 48
			}

			src.SetRGBA(x, y, color.RGBA{
				R: uint8((float64(x)*255)/float64(src.Bounds().Dx())) + stripe/2,
				G: uint8((float64(y) * 255) / float64(src.Bounds().Dy())),
				B: uint8((x+y)%207) + stripe,
				A: 255,
			})
		}
	}

	got := downscaleToWidth(src, 200)
	want := image.NewRGBA(got.Bounds())
	xdraw.CatmullRom.Scale(want, want.Bounds(), src, src.Bounds(), xdraw.Src, nil)

	if delta := meanRGBDelta(got, want); delta > 8 {
		t.Fatalf("mean RGB delta = %.2f, want <= 8", delta)
	}
}

func BenchmarkDownscaleToWidth(b *testing.B) {
	src := image.NewRGBA(image.Rect(0, 0, 2480, 3200))
	for i := range src.Pix {
		src.Pix[i] = uint8(i)
	}

	b.ResetTimer()

	for range b.N {
		_ = downscaleToWidth(src, 1024)
	}
}

func BenchmarkDownscaleCatmullRomBaseline(b *testing.B) {
	src := image.NewRGBA(image.Rect(0, 0, 2480, 3200))
	for i := range src.Pix {
		src.Pix[i] = uint8(i)
	}

	b.ResetTimer()

	for range b.N {
		dst := image.NewRGBA(image.Rect(0, 0, 1024, 3200*1024/2480))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	}
}

func meanRGBDelta(left, right image.Image) float64 {
	bounds := left.Bounds().Intersect(right.Bounds())

	var total float64

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			leftColor := left.At(x, y)
			rightColor := right.At(x, y)

			if leftColor == nil || rightColor == nil {
				return math.Inf(1)
			}

			lr, lg, lb, _ := leftColor.RGBA()
			rr, rg, rb, _ := rightColor.RGBA()

			total += math.Abs(float64(lr>>8) - float64(rr>>8))
			total += math.Abs(float64(lg>>8) - float64(rg>>8))
			total += math.Abs(float64(lb>>8) - float64(rb>>8))
		}
	}

	return total / float64(bounds.Dx()*bounds.Dy()*3)
}
