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

package summarizer

import (
	"fmt"
	"strings"
	"time"

	json "github.com/park285/shared-go/pkg/json"

	sharedmodel "github.com/kapu/hololive-llm-sched/internal/model"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type eventForPrompt struct {
	Title     string `json:"title"`
	DateStr   string `json:"date"`
	Members   string `json:"members,omitempty"`
	EventType string `json:"type"`
	Link      string `json:"link"`
}

const maxEventNoteRunes = 30

func buildUserPrompt(events []domain.MajorEvent, summaryType SummaryType, periodKey string, searchContext ...string) string {
	promptEvents := projectPromptEvents(events)
	eventsJSON := marshalPromptJSON(promptEvents, "[]")

	now := time.Now().In(kst)
	todayStr := fmt.Sprintf("%d년 %d월 %d일 (%s요일)",
		now.Year(), now.Month(), now.Day(), sharedmodel.WeekdayKR[now.Weekday()])

	var periodDesc string
	switch summaryType {
	case SummaryTypeWeekly:
		periodDesc = fmt.Sprintf("다음 주(%s~) 예정된 홀로라이브 행사", periodKey)
	case SummaryTypeMonthly:
		periodDesc = fmt.Sprintf("이번 달(%s) 예정된 홀로라이브 행사", periodKey)
	}

	base := fmt.Sprintf(`오늘 날짜: %s

%s %d건을 요약해주세요.

행사 목록:
%s`, todayStr, periodDesc, len(events), string(eventsJSON))

	if len(searchContext) > 0 && searchContext[0] != "" {
		return fmt.Sprintf(`%s

<web_search_context>
아래는 외부 검색으로 수집된 참고 자료입니다. 입력 행사 목록에 없는 공식 이벤트가 있다면 discovered_events에 추가하세요.
비공식 출처이거나 입력 행사와 중복되면 무시하세요.

%s
</web_search_context>`, base, searchContext[0])
	}

	return base
}

func marshalPromptJSON(value any, fallback string) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		return []byte(fallback)
	}
	return data
}

func projectPromptEvents(events []domain.MajorEvent) []eventForPrompt {
	promptEvents := make([]eventForPrompt, 0, len(events))
	for i := range events {
		e := &events[i]
		promptEvents = append(promptEvents, eventForPrompt{
			Title:     e.Title,
			DateStr:   formatEventDateForPrompt(e.EventStartDate, e.EventEndDate),
			Members:   joinMembers(e.Members),
			EventType: string(e.Type),
			Link:      e.Link,
		})
	}
	return promptEvents
}

func truncateNote(s string) string {
	runes := []rune(s)
	if len(runes) <= maxEventNoteRunes {
		return s
	}
	return string(runes[:maxEventNoteRunes]) + "…"
}

func normalizeNotes(resp *summaryResponse) {
	for i := range resp.Highlights {
		resp.Highlights[i].Note = truncateNote(resp.Highlights[i].Note)
	}
	for i := range resp.OngoingEvents {
		resp.OngoingEvents[i].Note = truncateNote(resp.OngoingEvents[i].Note)
	}
	for i := range resp.DiscoveredEvents {
		resp.DiscoveredEvents[i].Note = truncateNote(resp.DiscoveredEvents[i].Note)
	}
}

func assembleSummaryText(resp *summaryResponse) string {
	if resp == nil {
		return ""
	}
	if len(resp.Highlights) == 0 && len(resp.OngoingEvents) == 0 && len(resp.DiscoveredEvents) == 0 {
		return ""
	}

	normalizeNotes(resp)

	var sb strings.Builder
	writeHighlights(&sb, resp.Highlights)

	if len(resp.OngoingEvents) > 0 {
		appendSection(&sb, "[기간 행사]\n")
		writeOngoingEvents(&sb, resp.OngoingEvents)
	}

	if len(resp.DiscoveredEvents) > 0 {
		appendSection(&sb, "[추가 발견]\n")
		writeDiscoveredEvents(&sb, resp.DiscoveredEvents)
	}

	return sb.String()
}

func appendSection(sb *strings.Builder, header string) {
	if sb.Len() > 0 {
		sb.WriteString("\n\n")
	}
	sb.WriteString(header)
}

func writeHighlights(sb *strings.Builder, highlights []eventHighlight) {
	for i, h := range highlights {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		writeHighlight(sb, &h)
	}
}

func writeHighlight(sb *strings.Builder, h *eventHighlight) {
	sb.WriteString(h.Date)
	sb.WriteByte(' ')
	sb.WriteString(h.Name)
	if h.Members != "" {
		sb.WriteString(" (")
		sb.WriteString(h.Members)
		sb.WriteByte(')')
	}
	writeNoteAndLink(sb, h.Note, h.Link)
}

func writeOngoingEvents(sb *strings.Builder, events []ongoingEvent) {
	for i, o := range events {
		if i > 0 {
			sb.WriteByte('\n')
		}
		writeOngoingEvent(sb, o)
	}
}

func writeOngoingEvent(sb *strings.Builder, o ongoingEvent) {
	if o.Date != "" {
		sb.WriteString(o.Date)
		sb.WriteByte(' ')
	}
	sb.WriteString(o.Name)
	writeNoteAndLink(sb, o.Note, o.Link)
}

func writeNoteAndLink(sb *strings.Builder, note, link string) {
	if note != "" {
		sb.WriteString("\n- ")
		sb.WriteString(note)
	}
	if link != "" {
		sb.WriteByte('\n')
		sb.WriteString(link)
	}
}

func writeDiscoveredEvents(sb *strings.Builder, events []discoveredEvent) {
	for i, d := range events {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(d.Date)
		sb.WriteByte(' ')
		sb.WriteString(d.Name)
		if d.Note != "" {
			sb.WriteString("\n- ")
			sb.WriteString(d.Note)
		}
		if d.Source != "" {
			sb.WriteString("\n출처: ")
			sb.WriteString(d.Source)
		}
	}
}

func formatEventDateForPrompt(start, end *time.Time) string {
	if start == nil {
		return "TBA"
	}
	format := func(t time.Time) string {
		return fmt.Sprintf("%d년 %d월 %d일", t.Year(), t.Month(), t.Day())
	}
	if end == nil || start.Equal(*end) {
		return format(*start)
	}
	return fmt.Sprintf("%s ~ %s", format(*start), format(*end))
}

func joinMembers(members []string) string {
	if len(members) == 0 {
		return ""
	}
	if len(members) == 1 {
		return members[0]
	}

	totalLen := 0
	for _, m := range members {
		totalLen += len(m)
	}
	totalLen += (len(members) - 1) * 2

	buf := make([]byte, 0, totalLen)
	buf = append(buf, members[0]...)
	for i := 1; i < len(members); i++ {
		buf = append(buf, ',', ' ')
		buf = append(buf, members[i]...)
	}
	return string(buf)
}
