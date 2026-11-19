package formatter

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/park285/shared-go/v2/pkg/kakaoformat"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/timeutil"
)

type BroadcastTimeSummary struct {
	MemberName string
	Days       int
	Entries    []BroadcastHistoryEntry
	Partial    bool
	Now        time.Time
}

const broadcastTimePartialNotice = "조회 한도에 도달해 최근 방송 일부만 합산했습니다."

func (f *ResponseFormatter) BroadcastTime(ctx context.Context, summary *BroadcastTimeSummary) string {
	name := broadcastHistoryDisplayLine(summary.MemberName)

	if len(summary.Entries) == 0 {
		message := fmt.Sprintf("📅 %s: 최근 %d일 이내 종료된 방송이 없습니다.", name, summary.Days)

		if summary.Partial {
			message += "\n" + broadcastTimePartialNotice
		}

		return message
	}

	var b strings.Builder

	richText := replyFormat(ctx) == domain.TemplateFormatRichText
	if richText && name != "" {
		fmt.Fprintf(&b, "📅 **%s** 방송시간 · 최근 %d일\n", name, summary.Days)
	} else {
		fmt.Fprintf(&b, "📅 %s 방송시간 · 최근 %d일\n", name, summary.Days)
	}

	b.WriteString(broadcastTimeTotalLine(summary.Entries))
	b.WriteByte('\n')

	if summary.Partial {
		b.WriteString(broadcastTimePartialNotice)
		b.WriteByte('\n')
	}

	b.WriteByte('\n')

	for i := range summary.Entries {
		if !richText {
			fmt.Fprintf(&b, "%d · ", i+1)
		}

		b.WriteString(broadcastTimeSpan(ctx, f, &summary.Entries[i], summary.Now))

		if label := broadcastHistoryDisplayLine(summary.Entries[i].TypeLabel); label != "" {
			b.WriteString(" · " + label)
		}

		b.WriteByte('\n')
	}

	return f.foldSeeMore(strings.TrimRight(b.String(), "\n"), true)
}

func broadcastTimeTotalLine(entries []BroadcastHistoryEntry) string {
	var (
		total   time.Duration
		unknown int
	)

	for i := range entries {
		if entries[i].StartedAt.IsZero() || entries[i].EndedAt.IsZero() {
			unknown++

			continue
		}

		total += entries[i].EndedAt.Sub(entries[i].StartedAt)
	}

	line := fmt.Sprintf("합계 %s · %d회", formatBroadcastTimeTotal(total), len(entries))

	if unknown > 0 {
		line += fmt.Sprintf(" (종료 미확인 %d회 제외)", unknown)
	}

	return line
}

func formatBroadcastTimeTotal(d time.Duration) string {
	minutes := int64(d / time.Minute)
	hours, mins := minutes/60, minutes%60

	switch {
	case hours == 0:
		return fmt.Sprintf("%d분", mins)
	case mins == 0:
		return fmt.Sprintf("%d시간", hours)
	default:
		return fmt.Sprintf("%d시간 %d분", hours, mins)
	}
}

func broadcastTimeSpan(ctx context.Context, f *ResponseFormatter, entry *BroadcastHistoryEntry, now time.Time) string {
	if entry.StartedAt.IsZero() {
		if entry.Time.IsZero() {
			return broadcastHistoryTime(ctx, f, entry.Time)
		}

		text := broadcastTimeDate(entry.Time, now)
		if entry.TimeLabel == "" {
			return text
		}

		return text + " " + broadcastHistoryDisplayLine(entry.TimeLabel)
	}

	start := broadcastTimeDate(entry.StartedAt, now)
	if entry.EndedAt.IsZero() {
		return start + " 시작"
	}

	return start + "~" + broadcastHistoryEndTime(entry.StartedAt, entry.EndedAt) + " · " + formatBroadcastDuration(entry.EndedAt.Sub(entry.StartedAt))
}

func broadcastTimeDate(t, now time.Time) string {
	if timeutil.ToKST(t).Year() == timeutil.ToKST(now).Year() {
		return timeutil.FormatKST(t, "01/02 15:04")
	}

	return timeutil.FormatKST(t, broadcastHistoryTimeLayout)
}

func (f *ResponseFormatter) BroadcastTimeUsage() string {
	return "❌ 멤버 이름을 입력해주세요.\n예) " + kakaoformat.EscapeMarkdown(f.Prefix()+"방송시간 페코라")
}
