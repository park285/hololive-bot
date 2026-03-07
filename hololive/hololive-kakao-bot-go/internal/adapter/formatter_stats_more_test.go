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

package adapter

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
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

func TestFormatSubscriberGraph(t *testing.T) {
	t.Parallel()

	formatter := NewResponseFormatter("!", nil)
	now := time.Date(2026, 3, 4, 14, 10, 0, 0, time.FixedZone("KST", 9*60*60))
	message := formatter.FormatSubscriberGraph(
		"사쿠라 미코",
		30,
		2050000,
		15000,
		-3000,
		42,
		now,
		[]int64{2000000, 2005000, 2010000, 2023000, 2050000},
	)

	assert.Contains(t, message, "📊 사쿠라 미코 구독자 추이 (30일)")
	assert.Contains(t, message, "현재: 205만명")
	assert.Contains(t, message, "7일: +1만 5000명")
	assert.Contains(t, message, "30일: -3000명")
	assert.Contains(t, message, "추이:")
	assert.Contains(t, message, "샘플: 42개")
	assert.Contains(t, message, "업데이트: 03-04 14:10")
}

func TestWriteSubscriberGraphChange(t *testing.T) {
	t.Parallel()

	var builder strings.Builder
	writeSubscriberGraphChange(&builder, "7일", 0)
	assert.Equal(t, "", builder.String())

	writeSubscriberGraphChange(&builder, "7일", 1234)
	writeSubscriberGraphChange(&builder, "30일", -12)

	output := builder.String()
	assert.Contains(t, output, "7일: +1234명")
	assert.Contains(t, output, "30일: -12명")
}

func TestGenerateSparklineAndDownsample(t *testing.T) {
	t.Parallel()

	t.Run("empty values", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "", generateSparkline(nil, 10))
	})

	t.Run("single flat value", func(t *testing.T) {
		t.Parallel()
		line := generateSparkline([]int64{10, 10, 10}, 10)
		require.NotEmpty(t, line)
		assert.Len(t, []rune(line), 3)
	})

	t.Run("downsample keeps requested width", func(t *testing.T) {
		t.Parallel()
		values := []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
		sampled := downsampleSparklineValues(values, 4)
		require.Len(t, sampled, 4)
		assert.Equal(t, []int64{1, 3, 6, 8}, sampled)
	})

	t.Run("non-positive width keeps original", func(t *testing.T) {
		t.Parallel()
		values := []int64{1, 2, 3}
		assert.Equal(t, values, downsampleSparklineValues(values, 0))
		assert.Equal(t, values, downsampleSparklineValues(values, -1))
	})
}
