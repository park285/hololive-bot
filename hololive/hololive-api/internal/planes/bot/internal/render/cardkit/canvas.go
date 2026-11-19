package cardkit

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
)

func NewCanvas(width, height int, background color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	FillRect(img, img.Bounds(), background)

	return img
}

func EncodePNG(img *image.RGBA, outputWidth int) ([]byte, error) {
	out := downscaleToWidth(img, outputWidth)

	var buf bytes.Buffer

	if err := png.Encode(&buf, out); err != nil {
		return nil, fmt.Errorf("encode card png: %w", err)
	}

	return buf.Bytes(), nil
}

func downscaleToWidth(src *image.RGBA, outputWidth int) image.Image {
	b := src.Bounds()
	if b.Dx() <= outputWidth {
		return src
	}

	nh := b.Dy() * outputWidth / b.Dx()
	dst := image.NewRGBA(image.Rect(0, 0, outputWidth, nh))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, xdraw.Src, nil)

	return dst
}

type BadgeStyle struct {
	Face         font.Face
	Background   color.RGBA
	Text         color.RGBA
	PadX, PadY   int
	Height       int
	Radius       int
	BaselineLift int
}

func BadgeRightAligned(img *image.RGBA, rightX, y int, text string, s BadgeStyle) (badgeLeft int) {
	bw := MeasureText(s.Face, text)
	bx := rightX - bw - s.PadX*2
	FillRoundedRect(img, image.Rect(bx, y, bx+bw+s.PadX*2, y+s.Height), s.Radius, s.Background)
	DrawText(img, s.Face, bx+s.PadX, y+s.Height-s.PadY-s.BaselineLift, s.Text, text)

	return bx
}
