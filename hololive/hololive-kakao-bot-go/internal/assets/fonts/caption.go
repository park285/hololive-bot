package fonts

import (
	_ "embed" // 폰트 임베드를 위한 블랭크 임포트
	"fmt"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

//go:embed D2Coding-Ver1.3.2-20180524.ttf
var captionFontData []byte

var (
	captionFontOnce sync.Once
	captionFont     *opentype.Font
	errCaptionFont  error

	captionFaceCache sync.Map
)

// CaptionFace: 기본 크기(24pt)의 측션 폰트 Face를 반환합니다.
func CaptionFace() (font.Face, error) {
	return CaptionFaceSized(24)
}

// CaptionFaceSized: 지정된 크기의 측션 폰트 Face를 반환합니다.
// 생성된 Face는 sync.Map으로 캐시되어 재사용됩니다.
func CaptionFaceSized(size float64) (font.Face, error) {
	if size <= 0 {
		size = 24
	}

	fontData, err := loadCaptionFont()
	if err != nil {
		return nil, err
	}

	cacheKey := fmt.Sprintf("%.2f", size)
	if face, ok := captionFaceCache.Load(cacheKey); ok {
		return face.(font.Face), nil
	}

	face, err := opentype.NewFace(fontData, &opentype.FaceOptions{
		Size:    size,
		DPI:     96,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, fmt.Errorf("create caption font size %.2f: %w", size, err)
	}

	actual, _ := captionFaceCache.LoadOrStore(cacheKey, face)
	return actual.(font.Face), nil
}

func loadCaptionFont() (*opentype.Font, error) {
	captionFontOnce.Do(func() {
		fnt, err := opentype.Parse(captionFontData)
		if err != nil {
			errCaptionFont = fmt.Errorf("parse caption font: %w", err)
			return
		}
		captionFont = fnt
	})
	return captionFont, errCaptionFont
}
