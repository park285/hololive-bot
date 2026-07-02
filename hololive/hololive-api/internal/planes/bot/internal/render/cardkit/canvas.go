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

// 고해상도 캔버스를 표시폭(outputWidth)으로 다운스케일(SSAA) 후 PNG 인코딩한다.
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
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Src, nil)
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

// badgeLeft는 장식용이 아니다 — 호출자가 이 값으로 이름 폭 예산을 잘라야 배지와 겹치지 않는다.
func BadgeRightAligned(img *image.RGBA, rightX, y int, text string, s BadgeStyle) (badgeLeft int) {
	bw := MeasureText(s.Face, text)
	bx := rightX - bw - s.PadX*2
	FillRoundedRect(img, image.Rect(bx, y, bx+bw+s.PadX*2, y+s.Height), s.Radius, s.Background)
	DrawText(img, s.Face, bx+s.PadX, y+s.Height-s.PadY-s.BaselineLift, s.Text, text)
	return bx
}
