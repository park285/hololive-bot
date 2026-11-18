package fonts

import (
	_ "embed"
	"errors"
	"fmt"
	"image"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

//go:embed Pretendard-Regular.ttf
var pretendardRegularData []byte

//go:embed NotoSansJP-Regular-subset.ttf
var notoSansJPData []byte

var captionFaceCache sync.Map

type captionFonts struct {
	regular *opentype.Font
	jp      *opentype.Font
}

var loadCaptionFonts = sync.OnceValues(func() (captionFonts, error) {
	regular, err := opentype.Parse(pretendardRegularData)
	if err != nil {
		return captionFonts{}, fmt.Errorf("parse pretendard-regular font: %w", err)
	}

	jp, err := opentype.Parse(notoSansJPData)
	if err != nil {
		return captionFonts{}, fmt.Errorf("parse noto-sans-jp font: %w", err)
	}

	return captionFonts{regular: regular, jp: jp}, nil
})

func CaptionFaceSized(size float64) (font.Face, error) {
	out, err := captionFace(size)
	if err != nil {
		return nil, fmt.Errorf("caption face: %w", err)
	}

	return out, nil
}

func captionFace(size float64) (font.Face, error) {
	if size <= 0 {
		size = 24
	}

	fonts, err := loadCaptionFonts()
	if err != nil {
		return nil, fmt.Errorf("load fonts: %w", err)
	}

	cacheKey := fmt.Sprintf("%.2f", size)
	if face, ok := captionFaceCache.Load(cacheKey); ok {
		if cached, ok := face.(font.Face); ok {
			return cached, nil
		}
	}

	combined, err := buildCaptionFace(fonts, size)
	if err != nil {
		return nil, fmt.Errorf("build caption face: %w", err)
	}

	actual, _ := captionFaceCache.LoadOrStore(cacheKey, combined)
	actualFace, ok := actual.(font.Face)

	if !ok {
		return nil, fmt.Errorf("caption font cache stored %T, want font.Face", actual)
	}

	return actualFace, nil
}

func buildCaptionFace(fonts captionFonts, size float64) (font.Face, error) {
	primary := fonts.regular
	opts := &opentype.FaceOptions{Size: size, DPI: 96, Hinting: font.HintingFull}

	primaryFace, err := opentype.NewFace(primary, opts)
	if err != nil {
		return nil, fmt.Errorf("create caption face size %.2f: %w", size, err)
	}

	jpFace, err := opentype.NewFace(fonts.jp, opts)
	if err != nil {
		return nil, fmt.Errorf("create jp fallback face size %.2f: %w", size, err)
	}

	return &fallbackFace{
		primary:     primaryFace,
		primarySFNT: primary,
		fallback:    jpFace,
	}, nil
}

type fallbackFace struct {
	primary     font.Face
	fallback    font.Face
	primarySFNT *opentype.Font
	buf         sfnt.Buffer
}

func (f *fallbackFace) pick(r rune) font.Face {
	gi, err := f.primarySFNT.GlyphIndex(&f.buf, r)
	if err != nil || gi == 0 {
		return f.fallback
	}

	return f.primary
}

func (f *fallbackFace) Close() error {
	return errors.Join(f.primary.Close(), f.fallback.Close())
}

func (f *fallbackFace) Glyph(dot fixed.Point26_6, r rune) (image.Rectangle, image.Image, image.Point, fixed.Int26_6, bool) {
	return f.pick(r).Glyph(dot, r)
}

func (f *fallbackFace) GlyphBounds(r rune) (fixed.Rectangle26_6, fixed.Int26_6, bool) {
	return f.pick(r).GlyphBounds(r)
}

func (f *fallbackFace) GlyphAdvance(r rune) (fixed.Int26_6, bool) {
	return f.pick(r).GlyphAdvance(r)
}

func (f *fallbackFace) Kern(r0, r1 rune) fixed.Int26_6 {
	first, second := f.pick(r0), f.pick(r1)
	if first == second {
		return first.Kern(r0, r1)
	}

	return 0
}

func (f *fallbackFace) Metrics() font.Metrics {
	return f.primary.Metrics()
}
