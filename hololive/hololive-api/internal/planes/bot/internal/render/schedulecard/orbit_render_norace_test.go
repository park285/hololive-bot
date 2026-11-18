//go:build !race

package schedulecard

import (
	"bytes"
	"testing"

	"github.com/kapu/hololive-api/internal/service/scheduleposter/cardtemplate"
)

func TestRenderPagesEncodesEachOrbitPage(t *testing.T) {
	card := orbitFeaturedWeek(t, true)

	card.ChannelID = "UC-other"
	card.MemberName = "다른 멤버"

	pages, err := RenderPages(t.Context(), card, Format3x4, cardtemplate.Orbit)

	if err != nil || len(pages) != 2 {
		t.Fatalf("RenderPages(orbit) returned %d pages, err %v", len(pages), err)
	}

	for _, page := range pages {
		decodeCard(t, page, Format3x4)
	}

	if bytes.Equal(pages[0], pages[1]) {
		t.Fatal("distinct pages encoded as identical images")
	}
}

func TestRenderPagesClassicRemainsSinglePNG(t *testing.T) {
	card := orbitFeaturedWeek(t, true)
	pages, err := RenderPages(t.Context(), card, Format3x4, cardtemplate.Classic)

	if err != nil || len(pages) != 1 {
		t.Fatalf("RenderPages(classic) returned %d pages, err %v", len(pages), err)
	}

	decodeCard(t, pages[0], Format3x4)
}
