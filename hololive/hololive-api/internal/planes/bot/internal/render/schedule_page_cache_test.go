package render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/render/schedulecard"
	"github.com/kapu/hololive-api/internal/service/scheduleposter/cardtemplate"
)

func templateTestPages(template cardtemplate.ID) [][]byte {
	pages := [][]byte{slices.Concat(pngSignature, []byte(template))}
	if template == cardtemplate.Orbit {
		pages = append(pages, slices.Concat(pngSignature, []byte("두 번째")), slices.Concat(pngSignature, []byte("세 번째")))
	}

	return pages
}

func TestScheduleCardRendererCachesEachTemplateAsOneBundle(t *testing.T) {
	store := newTestCalendarImageCache(t)
	member := scheduleCardMember()
	schedule := scheduleCardSchedule(1)
	draws := 0

	for range 2 {
		renderer := NewScheduleCardRenderer(store)

		renderer.draw = func(_ context.Context, _ schedulecard.Card, _ schedulecard.Format, template cardtemplate.ID) ([][]byte, error) {
			draws++

			return templateTestPages(template), nil
		}

		for _, selected := range []cardtemplate.ID{cardtemplate.Classic, cardtemplate.Orbit, cardtemplate.Classic} {
			pages, err := renderer.RenderScheduleCards(t.Context(), member, schedule, selected)
			if err != nil || !slices.EqualFunc(pages, templateTestPages(selected), bytes.Equal) {
				t.Fatalf("template %q: pages = %q, err = %v", selected, pages, err)
			}

			for i := range pages {
				pages[i][0] = 0
				pages[i] = nil
			}
		}

		if pages, err := renderer.RenderScheduleCards(t.Context(), member, schedule, "unknown"); err == nil || pages != nil {
			t.Fatalf("unknown template with warm cache: pages = %q, err = %v", pages, err)
		}
	}

	if draws != 2 {
		t.Fatalf("draws = %d, want one per effective template across both renderers", draws)
	}
}

func TestScheduleCardRendererCoalescesOnlyMatchingTemplates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})

		var draws atomic.Int32

		renderer := newScheduleCardRenderer(schedulecard.Format3x4, 4, func(_ context.Context, _ schedulecard.Card, _ schedulecard.Format, template cardtemplate.ID) ([][]byte, error) {
			draws.Add(1)
			<-release

			return templateTestPages(template), nil
		})

		member, schedule := scheduleCardMember(), scheduleCardSchedule(1)
		requests := []cardtemplate.ID{cardtemplate.Classic, cardtemplate.Classic, cardtemplate.Orbit}
		results := make([][][]byte, len(requests))

		for i, template := range requests {
			go func() {
				pages, err := renderer.RenderScheduleCards(t.Context(), member, schedule, template)
				if err != nil {
					t.Errorf("render template %q: %v", template, err)
				}

				results[i] = pages
			}()
		}

		synctest.Wait()
		close(release)
		synctest.Wait()

		if draws.Load() != 2 {
			t.Fatalf("concurrent draws = %d, want one classic and one orbit draw", draws.Load())
		}

		for i, template := range []cardtemplate.ID{cardtemplate.Classic, cardtemplate.Classic, cardtemplate.Orbit} {
			pages := results[i]
			if !slices.EqualFunc(pages, templateTestPages(template), bytes.Equal) {
				t.Fatalf("request %q: pages = %q, want independent %s pages", requests[i], pages, template)
			}

			pages[0][0] = 0
			pages[0] = nil
		}
	})
}

func TestScheduleCardRendererReturnsOversizedBundleWithoutCaching(t *testing.T) {
	store := newTestCalendarImageCache(t)
	member, schedule := scheduleCardMember(), scheduleCardSchedule(1)
	page := make([]byte, sharedPNGCacheMaxBytes/2+1)
	copy(page, pngSignature)

	draws := 0
	renderer := NewScheduleCardRenderer(store)

	renderer.draw = func(context.Context, schedulecard.Card, schedulecard.Format, cardtemplate.ID) ([][]byte, error) {
		draws++

		return [][]byte{page, page}, nil
	}

	for range 2 {
		pages, err := renderer.RenderScheduleCards(t.Context(), member, schedule, cardtemplate.Orbit)
		if err != nil || len(pages) != 2 || !bytes.Equal(pages[0], page) || !bytes.Equal(pages[1], page) {
			t.Fatalf("oversized bundle: page count = %d, err = %v", len(pages), err)
		}
	}

	if draws != 2 {
		t.Fatalf("draws = %d, want a redraw with neither cache retaining the oversized bundle", draws)
	}

	key := newScheduleCardKey(member, &schedule, schedulecard.Format3x4, cardtemplate.Orbit, "")
	sharedKey, _ := key.shared()

	if _, found, err := store.Get(t.Context(), sharedKey); err != nil || found {
		t.Fatalf("oversized shared entry: found = %v, err = %v", found, err)
	}
}

func changeCacheUint64(offset int, value uint64) func([]byte) []byte {
	return func(b []byte) []byte {
		binary.BigEndian.PutUint64(b[offset:], value)

		return b
	}
}

func TestScheduleCardRendererRedrawsInvalidSharedBundle(t *testing.T) {
	store := newTestCalendarImageCache(t)
	member, schedule := scheduleCardMember(), scheduleCardSchedule(1)
	template := cardtemplate.Orbit
	key := newScheduleCardKey(member, &schedule, schedulecard.Format3x4, template, "")
	sharedKey, hash := key.shared()
	pages := templateTestPages(template)
	record := encodeSchedulePages(hash, pages)

	for _, tc := range []struct {
		name    string
		corrupt func([]byte) []byte
	}{
		{"wrong input", func(b []byte) []byte { b[0] ^= 1; return b }},
		{"truncated", func(b []byte) []byte { return b[:len(b)-1] }},
		{"missing page", changeCacheUint64(sha256.Size+8, 4)},
		{"unbounded count", changeCacheUint64(sha256.Size+8, ^uint64(0))},
		{"excess pages", changeCacheUint64(sha256.Size+8, 1)},
		{"oversized length", changeCacheUint64(sha256.Size+16, ^uint64(0))},
		{"invalid last PNG", func(b []byte) []byte { b[len(b)-len(pages[2])] = 0; return b }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := store.Set(t.Context(), sharedKey, tc.corrupt(bytes.Clone(record)), scheduleCardCacheTTL); err != nil {
				t.Fatal(err)
			}

			draws := 0
			renderer := NewScheduleCardRenderer(store)

			renderer.draw = func(context.Context, schedulecard.Card, schedulecard.Format, cardtemplate.ID) ([][]byte, error) {
				draws++

				return templateTestPages(template), nil
			}

			got, err := renderer.RenderScheduleCards(t.Context(), member, schedule, template)
			if err != nil || !slices.EqualFunc(got, pages, bytes.Equal) || draws != 1 {
				t.Fatalf("pages = %q, err = %v, draws = %d; want a fresh complete bundle", got, err, draws)
			}
		})
	}
}

func TestScheduleCardRendererDoesNotReturnPartialDraw(t *testing.T) {
	member, schedule := scheduleCardMember(), scheduleCardSchedule(1)
	draws := 0
	renderer := newScheduleCardRenderer(schedulecard.Format3x4, 4, func(_ context.Context, _ schedulecard.Card, _ schedulecard.Format, template cardtemplate.ID) ([][]byte, error) {
		draws++

		pages := templateTestPages(template)

		if draws == 1 {
			pages[1] = nil
		}

		return pages, nil
	})

	if pages, err := renderer.RenderScheduleCards(t.Context(), member, schedule, cardtemplate.Orbit); err == nil || pages != nil {
		t.Fatalf("incomplete draw: pages = %q, err = %v", pages, err)
	}

	pages, err := renderer.RenderScheduleCards(t.Context(), member, schedule, cardtemplate.Orbit)
	if err != nil || !slices.EqualFunc(pages, templateTestPages(cardtemplate.Orbit), bytes.Equal) || draws != 2 {
		t.Fatalf("next render: pages = %q, err = %v, draws = %d", pages, err, draws)
	}
}

func TestScheduleRenderBudgetStopsSharedWorkAndDoesNotCache(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewScheduleCardRenderer(nil)

		r.draw = func(ctx context.Context, _ schedulecard.Card, _ schedulecard.Format, _ cardtemplate.ID) ([][]byte, error) {
			<-ctx.Done()

			return nil, context.Cause(ctx)
		}

		pages, err := r.RenderScheduleCards(t.Context(), scheduleCardMember(), scheduleCardSchedule(1), cardtemplate.Orbit)

		if !errors.Is(err, schedulecard.ErrRenderBudget) || pages != nil {
			t.Fatalf("pages=%d err=%v", len(pages), err)
		}
	})
}

func TestScheduleAlbumRejectsCombinedBytesWithoutReturningPartialPages(t *testing.T) {
	page := make([]byte, 4<<20)
	copy(page, pngSignature)

	pages := slices.Repeat([][]byte{page}, schedulecard.MaxPages)
	if err := validateScheduleAlbum(pages); !errors.Is(err, schedulecard.ErrRenderBudget) {
		t.Fatalf("32 MiB album: %v", err)
	}

	if err := validateScheduleAlbum(pages[:7]); err != nil {
		t.Fatalf("28 MiB album: %v", err)
	}
}
