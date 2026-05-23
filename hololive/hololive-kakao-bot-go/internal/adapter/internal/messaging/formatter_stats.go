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

package messaging

import (
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
	"github.com/park285/hololive-bot/shared-go/pkg/stringutil"
)

func (f *ResponseFormatter) FormatStatsTopGainers(periodLabel string, gainers []domain.RankEntry) string {
	trimmedPeriod := stringutil.TrimSpace(periodLabel)
	instruction := fmt.Sprintf("%s %s", DefaultEmoji.Stats, MsgStatsGainersHeader)

	if trimmedPeriod != "" {
		instruction = fmt.Sprintf("%s (%s)", instruction, trimmedPeriod)
	}

	var builder strings.Builder
	builder.WriteString(instruction)
	builder.WriteString("\n\n")

	for _, entry := range gainers {
		fmt.Fprintf(&builder, "%d위. %s\n", entry.Rank, entry.MemberName)
		fmt.Fprintf(&builder, "    +%s명", util.FormatKoreanNumber(entry.Value))

		if entry.CurrentSubscribers > 0 {
			fmt.Fprintf(&builder, " (현재 %s명)", util.FormatKoreanNumber(int64(entry.CurrentSubscribers)))
		}

		builder.WriteString("\n\n")
	}

	content := stringutil.TrimSpace(builder.String())

	return util.ApplyKakaoSeeMorePadding(stringutil.StripLeadingHeader(content, instruction), instruction)
}

func (f *ResponseFormatter) FormatSubscriberCount(memberName string, subscribers uint64) string {
	formattedSubs := util.FormatKoreanNumber(int64(subscribers))

	return fmt.Sprintf("%s %s\n\n%s 현재 구독자: %s명",
		DefaultEmoji.Member,
		memberName,
		DefaultEmoji.Stats,
		formattedSubs,
	)
}

func (f *ResponseFormatter) FormatSubscriberGraph(
	memberName string,
	days int,
	current, change7d, change30d int64,
	sampleCount int,
	updatedAt time.Time,
	pointValues []int64,
) string {
	var builder strings.Builder

	fmt.Fprintf(&builder, "%s %s 구독자 추이 (%d일)\n\n", DefaultEmoji.Stats, memberName, days)
	fmt.Fprintf(&builder, "현재: %s명\n", util.FormatKoreanNumber(current))

	writeSubscriberGraphChange(&builder, "7일", change7d)
	writeSubscriberGraphChange(&builder, "30일", change30d)

	if len(pointValues) >= 2 {
		builder.WriteString("\n추이: ")
		builder.WriteString(generateSparkline(pointValues, 25))
		builder.WriteString("\n")
	}

	fmt.Fprintf(&builder, "\n샘플: %d개 | 업데이트: %s",
		sampleCount,
		updatedAt.Format("01-02 15:04"))

	return builder.String()
}

func writeSubscriberGraphChange(builder *strings.Builder, label string, value int64) {
	if value == 0 {
		return
	}

	sign := "+"

	if value < 0 {
		sign = ""
	}

	fmt.Fprintf(builder, "%s: %s%s명\n", label, sign, util.FormatKoreanNumber(value))
}

func generateSparkline(values []int64, width int) string {
	if len(values) == 0 {
		return ""
	}

	values = downsampleSparklineValues(values, width)

	minVal, maxVal := sparklineMinMax(values)
	sparkChars := []rune(" ▁▂▃▄▅▆▇█")
	rangeVal := maxVal - minVal
	if rangeVal == 0 {
		rangeVal = 1
	}

	var result strings.Builder

	for _, value := range values {
		result.WriteRune(sparkChars[sparklineCharIndex(value, minVal, rangeVal, len(sparkChars))])
	}

	return result.String()
}

func sparklineMinMax(values []int64) (int64, int64) {
	minVal, maxVal := values[0], values[0]
	for _, value := range values {
		minVal = min(minVal, value)
		maxVal = max(maxVal, value)
	}
	return minVal, maxVal
}

func sparklineCharIndex(value, minVal, rangeVal int64, charCount int) int {
	normalized := float64(value-minVal) / float64(rangeVal)
	idx := int(normalized * float64(charCount-1))
	if idx >= charCount {
		return charCount - 1
	}
	return idx
}

func downsampleSparklineValues(values []int64, width int) []int64 {
	if len(values) <= width || width <= 0 {
		return values
	}

	step := float64(len(values)) / float64(width)

	sampled := make([]int64, width)
	for i := range width {
		idx := int(float64(i) * step)
		if idx >= len(values) {
			idx = len(values) - 1
		}

		sampled[i] = values[idx]
	}

	return sampled
}
