// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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

//go:embed Pretendard-SemiBold.ttf
var pretendardSemiBoldData []byte

//go:embed NotoSansJP-Regular-subset.ttf
var notoSansJPData []byte

var (
	fontsOnce sync.Once
	fontsErr  error

	pretendardRegular  *opentype.Font
	pretendardSemiBold *opentype.Font
	notoSansJP         *opentype.Font

	captionFaceCache sync.Map
)

// 반환되는 Face는 내부 sfnt.Buffer와 래스터 상태를 공유해 동시 사용에 안전하지 않다 —
// 호출자는 render.fontMu처럼 렌더 전 구간을 단일 뮤텍스로 직렬화해야 한다(기존 계약).
func CaptionFaceSized(size float64) (font.Face, error) {
	return captionFace("regular", size)
}

func CaptionBoldFaceSized(size float64) (font.Face, error) {
	return captionFace("semibold", size)
}

func captionFace(weight string, size float64) (font.Face, error) {
	if size <= 0 {
		size = 24
	}
	if err := loadFonts(); err != nil {
		return nil, err
	}

	cacheKey := fmt.Sprintf("%s:%.2f", weight, size)
	if face, ok := captionFaceCache.Load(cacheKey); ok {
		if cached, ok := face.(font.Face); ok {
			return cached, nil
		}
	}

	combined, err := buildCaptionFace(weight, size)
	if err != nil {
		return nil, err
	}

	actual, _ := captionFaceCache.LoadOrStore(cacheKey, combined)
	actualFace, ok := actual.(font.Face)
	if !ok {
		return nil, fmt.Errorf("caption font cache stored %T, want font.Face", actual)
	}
	return actualFace, nil
}

func buildCaptionFace(weight string, size float64) (font.Face, error) {
	primary := pretendardRegular
	if weight == "semibold" {
		primary = pretendardSemiBold
	}

	opts := &opentype.FaceOptions{Size: size, DPI: 96, Hinting: font.HintingFull}

	primaryFace, err := opentype.NewFace(primary, opts)
	if err != nil {
		return nil, fmt.Errorf("create %s caption face size %.2f: %w", weight, size, err)
	}
	jpFace, err := opentype.NewFace(notoSansJP, opts)
	if err != nil {
		return nil, fmt.Errorf("create jp fallback face size %.2f: %w", size, err)
	}

	return &fallbackFace{
		primary:     primaryFace,
		primarySFNT: primary,
		fallback:    jpFace,
	}, nil
}

func loadFonts() error {
	fontsOnce.Do(func() {
		parse := func(name string, data []byte) *opentype.Font {
			if fontsErr != nil {
				return nil
			}
			parsed, err := opentype.Parse(data)
			if err != nil {
				fontsErr = fmt.Errorf("parse %s font: %w", name, err)
				return nil
			}
			return parsed
		}
		pretendardRegular = parse("pretendard-regular", pretendardRegularData)
		pretendardSemiBold = parse("pretendard-semibold", pretendardSemiBoldData)
		notoSansJP = parse("noto-sans-jp", notoSansJPData)
	})
	return fontsErr
}

// fallbackFace는 rune 단위로 primary(Pretendard) cmap에 글리프가 없을 때만
// NotoSansJP로 라우팅한다(일본어 한자·가나 두부 방지).
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
