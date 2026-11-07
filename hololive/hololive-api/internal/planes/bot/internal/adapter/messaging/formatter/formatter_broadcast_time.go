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

// BroadcastTimeSummary는 한 멤버가 기간 안에 끝낸 방송의 합계와 목록이다.
// Partial은 조회 예산이나 합산 상한에 걸려 기간 안 방송을 다 모으지 못했다는 뜻이다.
// Now는 목록 시각에서 연도를 생략할지 정하는 기준이다.
type BroadcastTimeSummary struct {
	MemberName string
	Days       int
	Entries    []BroadcastHistoryEntry
	Partial    bool
	Now        time.Time
}

const broadcastTimePartialNotice = "조회 한도에 도달해 최근 방송 일부만 합산했습니다."

// BroadcastTime은 머리 문단에 기간과 방송 시간 합계를 두고, 방송 목록은 접힌 본문(전체보기)에 둔다.
// 합계는 시작·종료가 확인된 방송만 더하며, 종료를 확인하지 못한 방송은 횟수에만 넣고 그 수를 함께 밝힌다.
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

	// 1건이어도 접는다. 머리 문단의 합계가 요약이고 목록은 전체보기에서 본다.
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

// formatBroadcastTimeTotal은 여러 방송의 합계라 일 단위로 나누지 않고 시간·분으로만 표시한다.
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

// broadcastTimeSpan은 "11/07 15:39~19:38 · 3시간 59분"처럼 표시하고, 시작이 올해가 아니면 연도를 붙인다.
// 종료가 확인되지 않았으면 "11/07 15:39 시작", 시작 시각이 없는 기록은 방송 이력과 같은 대체 시각과 라벨을 쓴다.
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

// BroadcastTimeUsage는 멤버 이름 없이 호출했을 때의 안내다. 예시 명령은 복사할 수 있게 escape만 한다.
func (f *ResponseFormatter) BroadcastTimeUsage() string {
	return "❌ 멤버 이름을 입력해주세요.\n예) " + kakaoformat.EscapeMarkdown(f.Prefix()+"방송시간 페코라")
}
