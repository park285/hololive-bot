package adapter

import (
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
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

	minVal, maxVal := values[0], values[0]
	for _, value := range values {
		if value < minVal {
			minVal = value
		}
		if value > maxVal {
			maxVal = value
		}
	}

	sparkChars := []rune(" ▁▂▃▄▅▆▇█")
	rangeVal := maxVal - minVal
	if rangeVal == 0 {
		rangeVal = 1
	}

	var result strings.Builder
	for _, value := range values {
		normalized := float64(value-minVal) / float64(rangeVal)
		idx := int(normalized * float64(len(sparkChars)-1))
		if idx >= len(sparkChars) {
			idx = len(sparkChars) - 1
		}
		result.WriteRune(sparkChars[idx])
	}

	return result.String()
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
