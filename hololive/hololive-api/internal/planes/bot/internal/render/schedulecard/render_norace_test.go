//go:build !race

package schedulecard

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"
	"testing"
)

func decodeCard(t *testing.T, raw []byte, f Format) image.Image {
	t.Helper()

	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("png.Decode() error = %v", err)
	}

	w, h, _ := f.size()
	if b := img.Bounds(); b.Dx() != w || b.Dy() != h {
		t.Fatalf("size = %v, want %dx%d", b.Size(), w, h)
	}

	return img
}

func TestRenderThemesAndFormats(t *testing.T) {
	formats := []Format{Format16x9, Format4x3, Format3x4}
	photo := image.NewRGBA(image.Rect(0, 0, 64, 64))
	draw.Draw(photo, photo.Bounds(), image.NewUniform(color.RGBA{R: 200, G: 80, B: 160, A: 255}), image.Point{}, draw.Src)

	for _, id := range themeChannels {
		for _, f := range formats {
			t.Run(id+"/"+string(f), func(t *testing.T) {
				card := weekCard(t, sampleWeek(t)...)

				card.ChannelID = id
				card.Photo = photo

				raw, err := Render(card, f)
				if err != nil {
					t.Fatalf("Render() error = %v", err)
				}

				decodeCard(t, raw, f)
			})
		}
	}
}

func TestRenderChangeMarks(t *testing.T) {
	for _, id := range themeChannels {
		for i, f := range []Format{Format16x9, Format4x3, Format3x4} {
			t.Run(id+"/"+string(f), func(t *testing.T) {
				card := weekCard(t, changeItems(t, 4+i)...)

				card.ChannelID = id

				raw, err := Render(card, f)
				if err != nil {
					t.Fatalf("Render() error = %v", err)
				}

				decodeCard(t, raw, f)
			})
		}
	}
}

func TestRenderFoldedConvertedItems(t *testing.T) {
	items := append(sampleWeek(t),
		Item{Day: date(t, "2026-11-08"), Start: "28:30", Status: StatusTentative, Title: "앞 주 심야", Converted: true},
		Item{Day: date(t, "2026-11-16"), Start: "29:00", Status: StatusPlanned, Title: "다음 주 심야 콜라보", Converted: true},
	)

	for _, id := range themeChannels {
		for _, f := range []Format{Format16x9, Format4x3, Format3x4} {
			t.Run(id+"/"+string(f), func(t *testing.T) {
				card := weekCard(t, items...)

				card.ChannelID = id

				raw, err := Render(card, f)
				if err != nil {
					t.Fatalf("Render() error = %v", err)
				}

				decodeCard(t, raw, f)
			})
		}
	}
}

func hostileItems(t *testing.T) []Item {
	t.Helper()

	long := strings.Repeat("아주아주 긴 제목 ", 2000)
	items := make([]Item, 0, 70)

	items = append(items,
		Item{Day: date(t, "2026-11-09"), Start: at20, Status: StatusPlanned, Title: long, With: []string{long, "\x1b[31m빨강", ""}, Place: long, Note: long, Tags: []string{long, "\u202eevil", "EN"}},
		Item{Day: date(t, "2026-11-10"), Status: StatusPlanned},
		Item{Day: date(t, "2026-11-11"), Status: StatusOff, Title: "\x00\x01"},
		Item{Day: date(t, "2026-11-15"), Status: StatusOff, Title: "휴방 (" + long + ")", Note: long, Tags: []string{"EN"}},
	)

	for i := range 60 {
		items = append(items, Item{Day: date(t, "2026-11-12"), Start: at21, Status: StatusPlanned, Title: "릴레이 " + strings.Repeat("방송 ", i)})
	}

	for i := range 6 {
		items = append(items, Item{Day: date(t, "2026-11-13").AddDate(0, 0, i%2), Start: "12:00", Status: StatusTentative, Title: "하루 여러 건"})
	}

	return items
}

func TestRenderHostileText(t *testing.T) {
	formats := []Format{Format3x4, Format16x9, Format4x3}

	for i, id := range themeChannels {
		f := formats[i%len(formats)]
		t.Run(id+"/"+string(f), func(t *testing.T) {
			card := weekCard(t, hostileItems(t)...)

			card.ChannelID = id
			card.MemberName = strings.Repeat("코세키 비쥬 Koseki Bijou ", 20)

			raw, err := Render(card, f)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}

			decodeCard(t, raw, f)
		})
	}
}
