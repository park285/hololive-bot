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

package util

import (
	"strings"
	"testing"
)

func TestFoldForSeeMore(t *testing.T) {
	t.Parallel()

	longRest := strings.Repeat("가", 300)
	multi := "헤더 라인\n" + longRest

	folded := FoldForSeeMore(multi, KakaoSeeMoreThreshold)
	if !strings.HasPrefix(folded, "헤더 라인\n") {
		t.Fatalf("first line not preserved: %q", folded[:30])
	}
	if got := strings.Count(folded, KakaoZeroWidthSpace); got != KakaoSeeMorePadding {
		t.Errorf("padding count = %d, want %d", got, KakaoSeeMorePadding)
	}
	if !strings.HasSuffix(folded, longRest) {
		t.Error("body after padding not preserved")
	}

	if again := FoldForSeeMore(folded, KakaoSeeMoreThreshold); again != folded {
		t.Error("fold is not idempotent")
	}

	short := "짧은 메시지\n본문"
	if got := FoldForSeeMore(short, KakaoSeeMoreThreshold); got != short {
		t.Errorf("threshold 이하 입력이 변형됨: %q", got)
	}

	single := strings.Repeat("a", 300)
	if got := FoldForSeeMore(single, KakaoSeeMoreThreshold); got != single {
		t.Error("한 줄 입력이 변형됨")
	}

	if got := FoldForSeeMore(multi, 0); got != multi {
		t.Error("threshold<=0 입력이 변형됨")
	}

	blankRest := strings.Repeat("가", 300) + "\n   "
	if got := FoldForSeeMore(blankRest, KakaoSeeMoreThreshold); got != blankRest {
		t.Error("공백 본문 입력이 변형됨")
	}
}
