//go:build !race

package schedulecard

import (
	"bytes"
	"cmp"
	"testing"

	"github.com/kapu/hololive-api/internal/service/scheduleposter/cardtemplate"
)

func highlightWeeks(t *testing.T, marked bool) []Card {
	t.Helper()

	title := planned(t, "2026-11-11", at20, "바뀐 방송")
	off := Item{Day: date(t, "2026-11-12"), Status: StatusOff, Title: "휴방"}
	same := planned(t, "2026-11-10", at21, "그대로")
	canceled := Item{Day: date(t, "2026-11-13"), Start: at20, Status: StatusCancelled, Title: "취소한 방송"}
	offBeside := Item{Day: date(t, "2026-11-13"), Status: StatusOff, Title: "휴방"}

	markedTitle, markedOff, markedBeside := title, off, offBeside

	markedTitle.Highlight, markedOff.Highlight, markedBeside.Highlight = marked, marked, marked

	return []Card{
		weekCard(t, same, markedTitle, off),
		weekCard(t, same, title, markedOff),
		weekCard(t, same, canceled, markedBeside),
	}
}

func TestHighlightMarksTheRenderedCardOfEveryDesign(t *testing.T) {
	type design struct {
		name, channelID, variant string
		template                 cardtemplate.ID
	}

	designs := []design{{name: "fallback", template: cardtemplate.Classic}, {name: "orbit", template: cardtemplate.Orbit}}

	for channelID := range channelPosters {
		for _, variant := range Variants(channelID, Format3x4, cardtemplate.Classic) {
			designs = append(designs, design{name: channelID + "/" + variant, channelID: channelID, variant: variant, template: cardtemplate.Classic})
		}
	}

	plain, marked := highlightWeeks(t, false), highlightWeeks(t, true)

	for _, d := range designs {
		render := func(c Card) []byte {
			t.Helper()

			c.ChannelID, c.Variant = cmp.Or(d.channelID, c.ChannelID), d.variant

			pages, err := RenderPages(t.Context(), c, Format3x4, d.template)
			if err != nil || len(pages) != 1 {
				t.Fatalf("%s: RenderPages() = %d pages, %v; want one page", d.name, len(pages), err)
			}

			return pages[0]
		}

		for i, row := range []string{"title", "off", "off beside a canceled stream"} {
			if bytes.Equal(render(plain[i]), render(marked[i])) {
				t.Errorf("%s/%s: highlighted card equals the plain card, want a visible mark", d.name, row)
			}
		}
	}
}
