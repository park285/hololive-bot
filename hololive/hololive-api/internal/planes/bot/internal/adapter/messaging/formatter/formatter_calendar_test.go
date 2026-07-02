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
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/stretchr/testify/assert"
)

const cmdCalendarBody = `{{if eq .Count 0}}📅 {{.Year}}년 {{.Month}}월 등록된 기념일이 없습니다.{{else}}📅 {{.Year}}년 {{.Month}}월 기념일 ({{.Count}})
{{range $i, $day := .Days}}{{if $i}}
{{end}}
[{{printf "%02d/%02d" $day.Month $day.Day}}]
{{range $day.Entries}}{{if .IsBirthday}}  🎂 {{.Name}} 생일{{else}}  🎉 {{.Name}} 데뷔 {{.Years}}주년{{end}}
{{end}}{{end}}{{end}}`

func newCalendarTestFormatter(t *testing.T) *ResponseFormatter {
	t.Helper()

	renderer := setupFormatterTestRenderer(t, map[domain.TemplateKey]string{
		domain.TemplateKeyCmdCalendar: cmdCalendarBody,
	})
	return NewResponseFormatter("!", renderer)
}

func TestCelebrationCalendar(t *testing.T) {
	t.Parallel()

	f := newCalendarTestFormatter(t)

	t.Run("empty entries", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t,
			"📅 2026년 6월 등록된 기념일이 없습니다.",
			f.CelebrationCalendar(t.Context(), 6, 2026, nil))
	})

	t.Run("birthday entry without ordinal", func(t *testing.T) {
		t.Parallel()

		entries := []domain.CalendarEntry{
			{Kind: domain.CelebrationKindBirthday, Member: &domain.Member{ShortKoreanName: "페코라"}, Day: 15},
		}

		assert.Equal(t,
			"📅 2026년 6월 기념일 (1)\n\n[06/15]\n  🎂 페코라 생일",
			f.CelebrationCalendar(t.Context(), 6, 2026, entries))
	})

	t.Run("anniversary entry with ordinal", func(t *testing.T) {
		t.Parallel()

		entries := []domain.CalendarEntry{
			{Kind: domain.CelebrationKindAnniversary, Member: &domain.Member{ShortKoreanName: "미코"}, Day: 5, Ordinal: 3},
		}

		assert.Equal(t,
			"📅 2026년 6월 기념일 (1)\n\n[06/05]\n  🎉 미코 데뷔 3주년",
			f.CelebrationCalendar(t.Context(), 6, 2026, entries))
	})

	t.Run("multiple entries grouped by day", func(t *testing.T) {
		t.Parallel()

		entries := []domain.CalendarEntry{
			{Kind: domain.CelebrationKindBirthday, Member: &domain.Member{NameKo: "라미"}, Day: 10},
			{Kind: domain.CelebrationKindAnniversary, Member: &domain.Member{NameKo: "라미"}, Day: 10, Ordinal: 2},
			{Kind: domain.CelebrationKindBirthday, Member: &domain.Member{ShortKoreanName: "보탄"}, Day: 20},
		}

		assert.Equal(t,
			"📅 2026년 6월 기념일 (3)\n\n[06/10]\n  🎂 라미 생일\n  🎉 라미 데뷔 2주년\n\n\n[06/20]\n  🎂 보탄 생일",
			f.CelebrationCalendar(t.Context(), 6, 2026, entries))
	})

	t.Run("member display name fallback", func(t *testing.T) {
		t.Parallel()

		entries := []domain.CalendarEntry{
			{Kind: domain.CelebrationKindBirthday, Member: &domain.Member{Name: "Pekora"}, Day: 1},
		}

		assert.Equal(t,
			"📅 2026년 1월 기념일 (1)\n\n[01/01]\n  🎂 Pekora 생일",
			f.CelebrationCalendar(t.Context(), 1, 2026, entries))
	})

	t.Run("nil member", func(t *testing.T) {
		t.Parallel()

		entries := []domain.CalendarEntry{
			{Kind: domain.CelebrationKindBirthday, Member: nil, Day: 1},
		}

		assert.Equal(t,
			"📅 2026년 1월 등록된 기념일이 없습니다.",
			f.CelebrationCalendar(t.Context(), 1, 2026, entries))
	})
}

func TestCelebrationCalendar_Fallback(t *testing.T) {
	t.Parallel()

	formatter := NewResponseFormatter("!", setupFormatterTestRenderer(t, map[domain.TemplateKey]string{}))

	assert.Equal(t,
		messagestrings.FallbackSentinel,
		formatter.CelebrationCalendar(t.Context(), 6, 2026, nil))
}
