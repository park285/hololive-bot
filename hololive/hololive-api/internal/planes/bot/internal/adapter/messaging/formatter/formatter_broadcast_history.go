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

	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/util"
)

type BroadcastHistoryFilter struct {
	MemberName string
	TypeLabel  string
	TopicID    string
	Days       int
	Limit      int
	IncludeAll bool
}

type BroadcastHistoryEntry struct {
	VideoID      string
	MemberName   string
	Type         string
	TypeLabel    string
	TopicID      string
	Title        string
	Time         time.Time
	URL          string
	HasThumbnail bool
}

var broadcastHistoryMembershipTagPattern = regexp.MustCompile(`(?i)(メンバーシップ限定|メンバー限定配信|メンバー限定|メン限|members[\s_-]*only|member[\s_-]*only|membership)`)

func (f *ResponseFormatter) BroadcastHistory(ctx context.Context, filter BroadcastHistoryFilter, entries []BroadcastHistoryEntry) string {
	if len(entries) == 0 {
		return f.BroadcastHistoryEmpty(ctx, filter)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "방송 이력 %d건\n", len(entries))
	if line := broadcastHistoryFilterLine(filter); line != "" {
		b.WriteString(line)
		b.WriteByte('\n')
	}

	for i := range entries {
		if i > 0 {
			b.WriteByte('\n')
		}
		f.writeBroadcastHistoryEntry(ctx, &b, i+1, &entries[i])
	}

	return f.foldSeeMore(strings.TrimRight(b.String(), "\n"))
}

func (f *ResponseFormatter) writeBroadcastHistoryEntry(ctx context.Context, b *strings.Builder, index int, entry *BroadcastHistoryEntry) {
	fmt.Fprintf(b, "%d. [%s] %s\n", index, entry.TypeLabel, entry.MemberName)
	writeBroadcastHistoryTitle(b, entry.Type, entry.Title)
	writeBroadcastHistoryTime(ctx, f, b, entry)
	writeBroadcastHistoryURL(b, entry.URL)
	writeBroadcastHistoryThumbnail(b, f.Prefix(), entry)
}

func writeBroadcastHistoryTitle(b *strings.Builder, broadcastType, title string) {
	title = broadcastHistoryDisplayTitle(broadcastType, title)
	if title != "" {
		fmt.Fprintf(b, "   %s\n", title)
	}
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
	fmt.Fprintf(b, "   %s", broadcastHistoryTime(ctx, f, entry.Time))
	if entry.TopicID != "" {
		fmt.Fprintf(b, " | topic: %s", entry.TopicID)
	}
	b.WriteByte('\n')
}

func writeBroadcastHistoryURL(b *strings.Builder, url string) {
	if url != "" {
		fmt.Fprintf(b, "   %s\n", url)
	}
}

func writeBroadcastHistoryThumbnail(b *strings.Builder, prefix string, entry *BroadcastHistoryEntry) {
	if entry.HasThumbnail && entry.VideoID != "" {
		fmt.Fprintf(b, "   %s썸네일 %s\n", prefix, entry.VideoID)
	}
}

func (f *ResponseFormatter) BroadcastHistoryEmpty(ctx context.Context, filter BroadcastHistoryFilter) string {
	var b strings.Builder
	b.WriteString("조건에 맞는 종료된 방송 이력이 없습니다.")
	if line := broadcastHistoryFilterLine(filter); line != "" {
		b.WriteByte('\n')
		b.WriteString(line)
	}
	return f.foldSeeMore(b.String())
}

func broadcastHistoryFilterLine(filter BroadcastHistoryFilter) string {
	parts := make([]string, 0, 4)
	if filter.MemberName != "" {
		parts = append(parts, "멤버: "+filter.MemberName)
	}
	if filter.TypeLabel != "" {
		parts = append(parts, "타입: "+filter.TypeLabel)
	}
	if filter.TopicID != "" {
		parts = append(parts, "topic: "+filter.TopicID)
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

func broadcastHistoryTime(ctx context.Context, f *ResponseFormatter, t time.Time) string {
	if t.IsZero() {
		return f.messageStrings.GetContext(ctx, messagestrings.NamespaceMisc, "time_unknown")
	}
	return util.FormatKST(t, "2006/01/02 15:04")
}
