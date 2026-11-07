package formatter

import (
	"context"
	"fmt"
	"strings"

	"github.com/park285/shared-go/v2/pkg/kakaoformat"

	"github.com/kapu/hololive-api/internal/templateview"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// BroadcastTime은 한 멤버의 최근 종료 방송을 한 줄에 하나씩 "시작~종료 · 길이 · 타입"으로 보여 준다.
// 2건 이상이면 목록을 접으므로 가장 최근 방송을 머리 문단의 "최근:" 줄에도 둔다. 조회 기간은 handler가 정하는 최근 1년이다.
func (f *ResponseFormatter) BroadcastTime(ctx context.Context, memberName string, entries []BroadcastHistoryEntry, truncated bool) string {
	name := broadcastHistoryDisplayLine(memberName)

	if len(entries) == 0 {
		message := "📅 " + name + ": 최근 1년 이내 종료된 방송이 없습니다."

		if truncated {
			message += "\n" + broadcastHistoryTruncatedNotice
		}

		return message
	}

	var b strings.Builder

	richText := replyFormat(ctx) == domain.TemplateFormatRichText
	if richText && name != "" {
		fmt.Fprintf(&b, "📅 **%s** 방송시간 · 최근 %d개\n", name, len(entries))
	} else {
		fmt.Fprintf(&b, "📅 %s 방송시간 · 최근 %d개\n", name, len(entries))
	}

	if len(entries) > 1 {
		fmt.Fprintf(&b, "최근: %s\n", broadcastHistoryTimeSpan(ctx, f, &entries[0]))
	}

	if truncated {
		b.WriteString(broadcastHistoryTruncatedNotice)
		b.WriteByte('\n')
	}

	b.WriteByte('\n')

	for i := range entries {
		if !richText {
			fmt.Fprintf(&b, "%d · ", i+1)
		}

		b.WriteString(broadcastHistoryTimeSpan(ctx, f, &entries[i]))

		if label := broadcastHistoryDisplayLine(entries[i].TypeLabel); label != "" {
			b.WriteString(" · " + label)
		}

		b.WriteByte('\n')
	}

	return f.foldSeeMore(strings.TrimRight(b.String(), "\n"), templateview.ShouldFoldItems(len(entries)))
}

// BroadcastTimeUsage는 멤버 이름 없이 호출했을 때의 안내다. 예시 명령은 복사할 수 있게 escape만 한다.
func (f *ResponseFormatter) BroadcastTimeUsage() string {
	return "❌ 멤버 이름을 입력해주세요.\n예) " + kakaoformat.EscapeMarkdown(f.Prefix()+"방송시간 페코라")
}
