package fonts

import (
	"testing"

	"golang.org/x/image/font"
)

func TestCaptionFaceSized_CoversKoreanAndJapanese(t *testing.T) {
	face, err := CaptionFaceSized(24)
	if err != nil {
		t.Fatalf("CaptionFaceSized: %v", err)
	}

	covered := "가힣한글ABCdef012 도쿄東京渋谷鏡雫兎猫森竜鈴星空海月白黒歌姫あいうえおンゲソラ・ー"
	for _, r := range covered {
		if _, ok := face.GlyphAdvance(r); !ok {
			t.Errorf("rune %q(%U): 결합 face에 글리프 없음", r, r)
		}
	}

	d := &font.Drawer{Face: face}
	if w := d.MeasureString("시라카미 후부키 (白上フブキ)").Ceil(); w <= 0 {
		t.Errorf("혼합 문자열 측정 폭 = %d, want > 0", w)
	}
}
