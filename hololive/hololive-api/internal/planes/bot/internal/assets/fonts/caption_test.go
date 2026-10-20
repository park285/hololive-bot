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
