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

package formatter

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
	"github.com/stretchr/testify/assert"
)

func TestFormatStatsTopGainers(t *testing.T) {
	t.Parallel()

	formatter := NewResponseFormatter("!", nil)
	gainers := []domain.RankEntry{
		{Rank: 1, MemberName: "사쿠라 미코", Value: 12345, CurrentSubscribers: 2000000},
		{Rank: 2, MemberName: "시라카미 후부키", Value: 2100, CurrentSubscribers: 0},
	}

	message := formatter.FormatStatsTopGainers("주간", gainers)

	assert.Contains(t, message, "📊 구독자 증가 순위 (주간)")
	assert.Contains(t, message, "1위. 사쿠라 미코")
	assert.Contains(t, message, "+1만 2345명")
	assert.Contains(t, message, "현재 200만명")
	assert.Contains(t, message, "2위. 시라카미 후부키")
	assert.Equal(t, util.KakaoSeeMorePadding, strings.Count(message, util.KakaoZeroWidthSpace))
}

func TestFormatSubscriberCount(t *testing.T) {
	t.Parallel()

	formatter := NewResponseFormatter("!", nil)
	message := formatter.FormatSubscriberCount("호시마치 스이세이", 2050000)

	assert.Contains(t, message, "📘 호시마치 스이세이")
	assert.Contains(t, message, "📊 현재 구독자: 205만명")
}
