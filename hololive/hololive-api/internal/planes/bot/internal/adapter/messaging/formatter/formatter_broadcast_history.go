package formatter

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/park285/shared-go/core/stringutil"
	"github.com/park285/shared-go/v2/pkg/kakaoformat"

	"github.com/kapu/hololive-api/internal/templateview"
	"github.com/kapu/hololive-shared/pkg/domain"
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
	Truncated  bool
}

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

	richText := replyFormat(ctx) == domain.TemplateFormatRichText
	if richText {
		fmt.Fprintf(&b, "**방송 이력 %d건**\n", len(entries))
	} else {
		fmt.Fprintf(&b, "방송 이력 %d건\n", len(entries))
	}

	if line := broadcastHistoryFilterLine(filter); line != "" {
		b.WriteString(line)
		b.WriteByte('\n')
	}

	if filter.Truncated {
		b.WriteString(broadcastHistoryTruncatedNotice)
		b.WriteByte('\n')
	}

	b.WriteByte('\n')

	for i := range entries {
		if i > 0 {
			b.WriteString("──────────\n")
		}

		f.writeBroadcastHistoryEntry(ctx, &b, i+1, richText, &entries[i])
	}

	return f.foldSeeMore(strings.TrimRight(b.String(), "\n"), templateview.ShouldFoldItems(len(entries)))
}

func (f *ResponseFormatter) writeBroadcastHistoryEntry(ctx context.Context, b *strings.Builder, index int, richText bool, entry *BroadcastHistoryEntry) {
	typeLabel, memberName := broadcastHistoryDisplayLine(entry.TypeLabel), broadcastHistoryDisplayLine(entry.MemberName)

	if richText {
		fmt.Fprintf(b, "**[%s] %s**\n", typeLabel, memberName)
	} else {
		fmt.Fprintf(b, "%d · [%s] %s\n", index, typeLabel, memberName)
	}

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

	if filter.Days > 0 {
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
