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
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/park285/shared-go/v2/pkg/kakaoformat"
	"github.com/park285/shared-go/v2/pkg/stringutil"

	"github.com/kapu/hololive-api/internal/templateview"
	"github.com/kapu/hololive-shared/pkg/kakaotext"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/timeutil"
)

type BroadcastHistoryFilter struct {
	MemberName string
	TypeLabel  string
	TopicID    string
	Days       int
	Limit      int
	IncludeAll bool
	Truncated  bool
}

// BroadcastHistoryEntry의 EndedAt은 provider 시각이거나 종료 오차가 확인된 경우에만 채워 길이를 표시한다.
// StartedAt이 없으면 Time을 TimeLabel과 함께 표시하고, 아카이브 확인 실패는 시각 뒤에 안내한다.
type BroadcastHistoryEntry struct {
	VideoID            string
	MemberName         string
	Type               string
	TypeLabel          string
	TopicID            string
	Title              string
	StartedAt          time.Time
	EndedAt            time.Time
	Time               time.Time
	TimeLabel          string
	URL                string
	HasThumbnail       bool
	ArchiveUnavailable bool
}

var broadcastHistoryMembershipTagPattern = regexp.MustCompile(`(?i)(メンバーシップ限定|メンバー限定配信|メンバー限定|メン限|members[\s_-]*only|member[\s_-]*only|membership)`)

const broadcastHistoryTruncatedNotice = "조회 예산에 도달해 일부 결과만 표시했습니다."

func (f *ResponseFormatter) BroadcastHistory(ctx context.Context, filter BroadcastHistoryFilter, entries []BroadcastHistoryEntry) string {
	if len(entries) == 0 {
		return f.BroadcastHistoryEmpty(ctx, filter)
	}

	var b strings.Builder

	fmt.Fprintf(&b, "**방송 이력 %d건**\n", len(entries))

	if line := broadcastHistoryFilterLine(filter); line != "" {
		b.WriteString(line)
		b.WriteByte('\n')
	}

	if filter.Truncated {
		b.WriteString(broadcastHistoryTruncatedNotice)
		b.WriteByte('\n')
	}

	// 머리 문단 뒤에만 빈 줄을 두고, 항목 사이는 구분선 한 줄로 경계를 표시한다.
	b.WriteByte('\n')

	for i := range entries {
		if i > 0 {
			b.WriteString("──────────\n")
		}

		f.writeBroadcastHistoryEntry(ctx, &b, i+1, &entries[i])
	}

	return f.foldSeeMore(strings.TrimRight(b.String(), "\n"), templateview.ShouldFoldItems(len(entries)))
}

func (f *ResponseFormatter) writeBroadcastHistoryEntry(ctx context.Context, b *strings.Builder, index int, entry *BroadcastHistoryEntry) {
	fmt.Fprintf(b, "%d · **[%s] %s**\n", index, broadcastHistoryDisplayLine(entry.TypeLabel), broadcastHistoryDisplayLine(entry.MemberName))
	writeBroadcastHistoryTitle(b, entry.Type, entry.Title)
	writeBroadcastHistoryTime(ctx, f, b, entry)
	writeBroadcastHistoryURL(b, entry.URL)
	writeBroadcastHistoryThumbnail(b, f.Prefix(), entry)
}

func writeBroadcastHistoryTitle(b *strings.Builder, broadcastType, title string) {
	title = broadcastHistoryDisplayTitle(broadcastType, title)
	if title != "" {
		title = strings.Join(strings.Fields(strings.ReplaceAll(title, kakaotext.ZeroWidthSpace, "")), " ")
		if utf8.RuneCountInString(title) > 64 {
			title = stringutil.TruncateString(title, 61)
		}

		fmt.Fprintf(b, "%s\n", kakaotext.MarkdownLiteral(title))
	}
}

func broadcastHistoryDisplayLine(value string) string {
	return kakaotext.MarkdownLiteral(strings.Join(strings.Fields(value), " "))
}

func broadcastHistoryDisplayTitle(broadcastType, title string) string {
	title = strings.TrimSpace(title)

	if broadcastType != "membership" {
		return title
	}

	return trimBroadcastHistoryMembershipTitleTag(title)
}

func trimBroadcastHistoryMembershipTitleTag(title string) string {
	tag, rest, ok := splitLeadingBroadcastHistoryTitleTag(title)
	if !ok {
		return title
	}

	cleanedTag := cleanBroadcastHistoryMembershipTag(tag)

	rest = strings.TrimSpace(rest)

	if cleanedTag == "" {
		return rest
	}

	return strings.TrimSpace("【" + cleanedTag + "】" + rest)
}

func splitLeadingBroadcastHistoryTitleTag(title string) (tag, rest string, ok bool) {
	if !strings.HasPrefix(title, "【") {
		return "", "", false
	}

	tagEnd := strings.Index(title, "】")
	if tagEnd < 0 {
		return "", "", false
	}

	return title[len("【"):tagEnd], title[tagEnd+len("】"):], true
}

func cleanBroadcastHistoryMembershipTag(tag string) string {
	cleaned := broadcastHistoryMembershipTagPattern.ReplaceAllString(tag, "")
	return strings.Trim(cleaned, " \t\n\r　-_/|:：・,，、")
}

func writeBroadcastHistoryTime(ctx context.Context, f *ResponseFormatter, b *strings.Builder, entry *BroadcastHistoryEntry) {
	fmt.Fprintf(b, "⏰ %s", broadcastHistoryTimeSpan(ctx, f, entry))

	if entry.ArchiveUnavailable {
		b.WriteString(" · 아카이브를 확인할 수 없어 종료 시각 미확인")
	}

	if entry.TopicID != "" {
		fmt.Fprintf(b, " · topic: %s", broadcastHistoryDisplayLine(entry.TopicID))
	}

	b.WriteByte('\n')
}

func writeBroadcastHistoryURL(b *strings.Builder, url string) {
	if url != "" {
		fmt.Fprintf(b, "%s\n", url)
	}
}

func writeBroadcastHistoryThumbnail(b *strings.Builder, prefix string, entry *BroadcastHistoryEntry) {
	if entry.HasThumbnail && entry.VideoID != "" {
		fmt.Fprintf(b, "썸네일: %s\n", kakaoformat.EscapeMarkdown(prefix+"썸네일 "+entry.VideoID))
	}
}

func (f *ResponseFormatter) BroadcastHistoryEmpty(_ context.Context, filter BroadcastHistoryFilter) string {
	var b strings.Builder

	b.WriteString("조건에 맞는 종료된 방송 이력이 없습니다.")

	if line := broadcastHistoryFilterLine(filter); line != "" {
		b.WriteByte('\n')
		b.WriteString(line)
	}

	if filter.Truncated {
		b.WriteByte('\n')
		b.WriteString(broadcastHistoryTruncatedNotice)
	}

	return b.String()
}

func broadcastHistoryFilterLine(filter BroadcastHistoryFilter) string {
	parts := make([]string, 0, 4)

	if filter.MemberName != "" {
		parts = append(parts, "멤버: "+broadcastHistoryDisplayLine(filter.MemberName))
	}

	if filter.TypeLabel != "" {
		parts = append(parts, "타입: "+broadcastHistoryDisplayLine(filter.TypeLabel))
	}

	if filter.TopicID != "" {
		parts = append(parts, "topic: "+broadcastHistoryDisplayLine(filter.TopicID))
	}

	if filter.IncludeAll {
		parts = append(parts, "기간: 전체")
	} else if filter.Days > 0 {
		parts = append(parts, fmt.Sprintf("기간: 최근 %d일", filter.Days))
	}

	if filter.Limit > 0 {
		parts = append(parts, fmt.Sprintf("개수: 최대 %d건", filter.Limit))
	}

	if len(parts) == 0 {
		return ""
	}

	return strings.Join(parts, " / ")
}

const broadcastHistoryTimeLayout = "2006/01/02 15:04"

// broadcastHistoryTimeSpan은 "2026/10/17 20:59~22:29 · 1시간 30분"처럼 시작·종료와 길이를 표시한다.
// 종료가 확인되지 않았으면 "2026/10/17 20:59 시작"만 표시한다.
func broadcastHistoryTimeSpan(ctx context.Context, f *ResponseFormatter, entry *BroadcastHistoryEntry) string {
	if entry.StartedAt.IsZero() {
		text := broadcastHistoryTime(ctx, f, entry.Time)
		if entry.Time.IsZero() || entry.TimeLabel == "" {
			return text
		}

		return text + " " + broadcastHistoryDisplayLine(entry.TimeLabel)
	}

	start := timeutil.FormatKST(entry.StartedAt, broadcastHistoryTimeLayout)
	if entry.EndedAt.IsZero() {
		return start + " 시작"
	}

	return start + "~" + broadcastHistoryEndTime(entry.StartedAt, entry.EndedAt) + " · " + formatBroadcastDuration(entry.EndedAt.Sub(entry.StartedAt))
}

// broadcastHistoryEndTime은 KST 기준으로 시작과 같은 날짜면 시각만, 같은 해면 월/일부터 표시한다.
func broadcastHistoryEndTime(startedAt, endedAt time.Time) string {
	start, end := timeutil.ToKST(startedAt), timeutil.ToKST(endedAt)

	switch {
	case start.Year() == end.Year() && start.YearDay() == end.YearDay():
		return end.Format("15:04")
	case start.Year() == end.Year():
		return end.Format("01/02 15:04")
	default:
		return end.Format(broadcastHistoryTimeLayout)
	}
}

func broadcastHistoryTime(_ context.Context, f *ResponseFormatter, t time.Time) string {
	if t.IsZero() {
		return f.messageStrings.Text(messagestrings.MiscTimeUnknown)
	}

	return timeutil.FormatKST(t, broadcastHistoryTimeLayout)
}
