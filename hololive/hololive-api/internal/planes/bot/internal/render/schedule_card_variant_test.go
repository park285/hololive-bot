package render

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/render/schedulecard"
	"github.com/kapu/hololive-api/internal/service/scheduleposter/cardtemplate"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func kyokoMember() *domain.Member {
	return &domain.Member{ChannelID: kyokoChannel, Name: "Hakuto Kyoko", NameKo: "햐쿠토 쿄코", ShortKoreanName: "쿄코"}
}

func variantPage(variant string) []byte {
	return slices.Concat(pngSignature, []byte("variant="+variant))
}

type variantDrawer struct {
	drawn []string
}

func (d *variantDrawer) draw(_ context.Context, card schedulecard.Card, _ schedulecard.Format, _ cardtemplate.ID) ([][]byte, error) {
	d.drawn = append(d.drawn, card.Variant)

	return [][]byte{variantPage(card.Variant)}, nil
}

func TestScheduleCardRendererNeverServesTheSameDesignTwiceInARow(t *testing.T) {
	pool := schedulecard.Variants(kyokoChannel, schedulecard.Format3x4, cardtemplate.Classic)
	if len(pool) < 2 {
		t.Fatalf("kyoko pool = %q, want several designs", pool)
	}

	drawer := &variantDrawer{}
	renderer := newScheduleCardRenderer(schedulecard.Format3x4, 32, drawer.draw)

	renderer.variants.intN = func(int) int { return 0 }

	member, schedule := kyokoMember(), scheduleCardSchedule(1)

	served := make([]string, 0, 6)

	for range cap(served) {
		pages, err := renderer.RenderScheduleCards(t.Context(), member, schedule, cardtemplate.Classic)
		if err != nil || len(pages) != 1 {
			t.Fatalf("RenderScheduleCards() = %q, %v; want one page", pages, err)
		}

		i := slices.IndexFunc(pool, func(v string) bool { return bytes.Equal(pages[0], variantPage(v)) })
		if i < 0 {
			t.Fatalf("page = %q, want one of %q", pages[0], pool)
		}

		if len(served) > 0 && served[len(served)-1] == pool[i] {
			t.Fatalf("served = %q then %q again, want a different design", served, pool[i])
		}

		served = append(served, pool[i])
	}

	if !slices.Equal(drawer.drawn, []string{pool[0], pool[1]}) {
		t.Fatalf("drawn = %q, want each alternating design drawn once and then served from cache", drawer.drawn)
	}
}

func TestScheduleCardRendererKeepsOneDesignOutsidePosterPool(t *testing.T) {
	for _, tc := range []struct {
		name     string
		format   schedulecard.Format
		member   *domain.Member
		template cardtemplate.ID
	}{
		{"orbit template", schedulecard.Format3x4, kyokoMember(), cardtemplate.Orbit},
		{"member without posters", schedulecard.Format3x4, scheduleCardMember(), cardtemplate.Classic},
		{"landscape format", schedulecard.Format16x9, kyokoMember(), cardtemplate.Classic},
	} {
		t.Run(tc.name, func(t *testing.T) {
			drawer := &variantDrawer{}
			renderer := newScheduleCardRenderer(tc.format, 4, drawer.draw)

			for range 3 {
				pages, err := renderer.RenderScheduleCards(t.Context(), tc.member, scheduleCardSchedule(1), tc.template)
				if err != nil || !slices.EqualFunc(pages, [][]byte{variantPage("")}, bytes.Equal) {
					t.Fatalf("RenderScheduleCards() = %q, %v; want the single design", pages, err)
				}
			}

			if !slices.Equal(drawer.drawn, []string{""}) {
				t.Fatalf("drawn = %q, want one draw without a variant", drawer.drawn)
			}
		})
	}
}

func TestScheduleCardSharedCacheKeepsVariantsApart(t *testing.T) {
	store := newTestCalendarImageCache(t)
	pool := schedulecard.Variants(kyokoChannel, schedulecard.Format3x4, cardtemplate.Classic)
	member, schedule := kyokoMember(), scheduleCardSchedule(1)
	drawer := &variantDrawer{}

	for _, variant := range []string{pool[1], pool[1], pool[2]} {
		renderer := NewScheduleCardRenderer(store)

		renderer.draw = drawer.draw
		renderer.variants.intN = func(int) int { return slices.Index(pool, variant) }

		pages, err := renderer.RenderScheduleCards(t.Context(), member, schedule, cardtemplate.Classic)
		if err != nil || !slices.EqualFunc(pages, [][]byte{variantPage(variant)}, bytes.Equal) {
			t.Fatalf("variant %q: pages = %q, err = %v", variant, pages, err)
		}
	}

	if !slices.Equal(drawer.drawn, []string{pool[1], pool[2]}) {
		t.Fatalf("drawn = %q, want a fresh renderer to reuse the shared bundle of the same design only", drawer.drawn)
	}
}

func TestVariantPickerRemembersEachChannelSeparately(t *testing.T) {
	picker := newVariantPicker()

	picker.intN = func(int) int { return 0 }

	pool := []string{"a", "b"}

	got := []string{picker.pick("x", pool), picker.pick("y", pool), picker.pick("x", pool), picker.pick("y", nil), picker.pick("z", pool[:1]), picker.pick("z", pool[:1])}
	if want := []string{"a", "a", "b", "", "a", "a"}; !slices.Equal(got, want) {
		t.Fatalf("picks = %q, want %q", got, want)
	}
}

func TestVariantPickerCoversThePoolWithoutRepeats(t *testing.T) {
	picker := newVariantPicker()
	pool := []string{"original", "a", "b", "c"}
	seen := make(map[string]int)
	last := ""

	for range 400 {
		v := picker.pick(kyokoChannel, pool)
		if v == last || !slices.Contains(pool, v) {
			t.Fatalf("pick = %q after %q, want a different design from %q", v, last, pool)
		}

		seen[v]++

		last = v
	}

	if len(seen) != len(pool) {
		t.Fatalf("seen = %v, want every design of %q", seen, pool)
	}
}
