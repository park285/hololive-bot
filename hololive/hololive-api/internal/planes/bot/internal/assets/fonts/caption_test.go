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

func TestCaptionFaceSized_RoutesKanaToFallback(t *testing.T) {
	if err := loadFonts(); err != nil {
		t.Fatalf("loadFonts: %v", err)
	}

	gi, err := pretendardRegular.GlyphIndex(nil, 'ぶ')
	if err != nil {
		t.Fatalf("primary GlyphIndex: %v", err)
	}
	if gi != 0 {
		t.Skip("Pretendard가 가나를 직접 커버 — fallback 라우팅 검증 불필요")
	}

	face, err := CaptionFaceSized(24)
	if err != nil {
		t.Fatalf("CaptionFaceSized: %v", err)
	}
	if _, ok := face.GlyphAdvance('ぶ'); !ok {
		t.Error("가나가 primary에 없는데 fallback으로도 렌더되지 않음")
	}
}

func TestCaptionBoldFaceSized_Loads(t *testing.T) {
	face, err := CaptionBoldFaceSized(30)
	if err != nil {
		t.Fatalf("CaptionBoldFaceSized: %v", err)
	}
	if _, ok := face.GlyphAdvance('가'); !ok {
		t.Error("SemiBold face에 한글 글리프 없음")
	}

	regular, err := CaptionFaceSized(30)
	if err != nil {
		t.Fatalf("CaptionFaceSized: %v", err)
	}
	if regular == face {
		t.Error("regular와 semibold가 동일 face 인스턴스")
	}
}
