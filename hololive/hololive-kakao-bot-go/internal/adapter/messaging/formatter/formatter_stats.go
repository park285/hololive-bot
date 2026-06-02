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
	"fmt"
	"strings"

	msging "github.com/kapu/hololive-kakao-bot-go/internal/adapter/messaging"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
	"github.com/park285/shared-go/pkg/stringutil"
)

func (f *ResponseFormatter) FormatStatsTopGainers(periodLabel string, gainers []domain.RankEntry) string {
	trimmedPeriod := stringutil.TrimSpace(periodLabel)
	instruction := fmt.Sprintf("%s %s", msging.DefaultEmoji.Stats, msging.MsgStatsGainersHeader)

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
		msging.DefaultEmoji.Member,
		memberName,
		msging.DefaultEmoji.Stats,
		formattedSubs,
	)
}
