package schedulecard

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"time"
)

type theme func(c *canvas, v view)

var channelThemes = map[string]theme{
	"UCy9mgxB8pn2C4aNK_MPthDQ": renderTsuzuri,
	"UCSjQDxud2HkAO2DVD3lwxmw": renderKyoko,
	"UCROQtXcp2loQEmvpe5rhJzQ": renderSopia,
	"UC8eitCE9Z6EwUCs-VUi1blg": renderMela,
}

func themeFor(channelID string) theme {
	if th, ok := channelThemes[channelID]; ok {
		return th
	}

	return renderFallback
}

type fallbackPal struct {
	accent, soft, tint, ink, sub, rule, white color.RGBA
}

const fallbackHue = 230.0

func newFallbackPal(photo image.Image) fallbackPal {
	hue := fallbackHue

	if photo != nil {
		hue = dominantHue(photo)
	}

	return fallbackPal{
		accent: hsl(hue, 0.62, 0.46),
		soft:   hsl(hue, 0.70, 0.95),
		tint:   hsl(hue, 0.55, 0.90),
		ink:    hsl(hue, 0.28, 0.17),
		sub:    hsl(hue, 0.14, 0.38),
		rule:   hsl(hue, 0.30, 0.88),
		white:  hex(0xFFFFFF),
	}
}

func renderFallback(c *canvas, v view) {
	p := newFallbackPal(v.photo)
	c.Fill(rect(0, 0, c.w, c.h), p.soft)

	var t box

	if c.tall() {
		t = fallbackBand(c, p, v)
	} else {
		t = fallbackColumn(c, p, v)
	}

	fallbackTable(c, p, t, v)
}

func fallbackRing(c *canvas, p fallbackPal, photo image.Image, cx, cy, r float64) {
	c.Shadow(circle(cx, cy, r+7), 0, 8, 14, rgba(0x000000, 0.16))
	c.Fill(circle(cx, cy, r+7), p.white)
	c.Medallion(photo, cx, cy, r, 0, rgb24(p.tint), 0.6)
}

var fallbackPeriodT = txt{Size: 24, Face: faceXBold, Track: 0.6}

func fallbackBand(c *canvas, p fallbackPal, v view) box {
	w := c.w
	c.Fill(rect(0, 0, w, 306), p.tint)
	c.Fill(rect(0, 0, w, 10), p.accent)
	fallbackRing(c, p, v.photo, 152, 160, 104)

	nameW := w - 300 - 40
	nameT := c.f.fitSize(txt{Size: 84, Face: faceXBold, Track: -0.5}, v.name, nameW, 40)
	c.textMid(nameT, c.f.clamp(nameT, v.name, nameW), 300, 132, alignL, p.ink)
	pill(c, 300, 132+nameT.Size*0.5+42, 44, 20, fallbackPeriodT, v.period, p.accent, p.white)

	return fallbackTableBox(c)
}

func fallbackTableBox(c *canvas) box {
	if c.tall() {
		return box{28, 330, c.w - 56, c.h - 28 - 330}
	}

	return box{fallbackColumnW + 28, 28, c.w - 28 - fallbackColumnW - 28, c.h - 56}
}

const fallbackColumnW = 396.0

func fallbackColumn(c *canvas, p fallbackPal, v view) box {
	const (
		vw = fallbackColumnW
		r  = 150.0
	)

	h := c.h
	c.Fill(rect(0, 0, vw, h), p.tint)
	c.Fill(rect(0, 0, 10, h), p.accent)

	cy := h*0.40 - 20
	fallbackRing(c, p, v.photo, vw/2+5, cy, r)

	nameT := c.f.fitSize(txt{Size: 58, Face: faceXBold, Track: -0.3}, v.name, vw-56, 32)
	c.textMid(nameT, c.f.clamp(nameT, v.name, vw-56), vw/2+5, cy+r+76, alignC, p.ink)

	pw := c.f.width(fallbackPeriodT, v.period) + 40
	pill(c, vw/2+5-pw/2, cy+r+76+nameT.Size*0.5+40, 44, 20, fallbackPeriodT, v.period, p.accent, p.white)

	return fallbackTableBox(c)
}

var fallbackRow = rowStyle{
	main: txt{Size: 34, Face: faceXBold}, sub: txt{Size: 24, Face: faceBold},
	time: txt{Size: 32, Face: faceXBold, Track: 0.4}, pill: txt{Size: 25, Face: faceXBold, Track: 0.4},
	capPad: 16, gutter: 24, minSlot: 42, slotGap: 8,
}

const (
	fallbackGap   = 10.0
	fallbackDateW = 118.0
)

func fallbackSpec(c *canvas, t box) tableSpec {
	return tableSpec{
		innerW: t.right() - 26 - (t.X + fallbackDateW + 26), avail: t.H - 34, gap: fallbackGap, minRow: 78, pad: 13, kMax: c.kMax(),
	}
}

func fallbackTable(c *canvas, p fallbackPal, t box, v view) {
	const (
		gap   = fallbackGap
		dateW = fallbackDateW
	)

	timeX := t.X + dateW + 26
	fit, days := fitWeek(c.f, v.days, fallbackRow, fallbackSpec(c, t))
	rowY := t.Y

	for i, d := range days {
		rh, y := fit.heights[i], rowY

		rowY += rh + gap

		cy := y + rh/2
		row := rrect(t.X, y, t.W, rh, 20)
		c.Shadow(row, 0, 3, 7, rgba(0x000000, 0.07))
		c.Fill(row, p.white)
		fallbackDate(c, p, d, t.X, cy)
		c.Fill(rrect(t.X+dateW, y+18, 3, rh-36, 1.5), p.rule)

		switch {
		case len(d.slots) == 0:
			c.Fill(rrect(timeX, cy-2, 28, 4, 2), p.rule)
		case d.off():
			offT := txt{Size: 34 * fit.k, Face: faceXBold, Track: 3}
			layoutOff(c.f, d.slots[0], offT, fallbackRow.sub.scaled(fit.k), t.right()-26-timeX).draw(c, timeX, cy, p.sub, p.sub)
		default:
			drawTableSlots(c, fallbackRow, &fit, i, timeX, y+(rh-fit.totals[i])/2, slotPaint{
				timeInk: p.ink, unsureInk: p.accent, ink: p.ink, sub: p.sub,
				changedFill: p.accent, changedInk: p.white,
				capH: 40, dashOn: 6, dashOff: 4.5, dashW: 2.2,
			})
		}
	}

	c.textMid(txt{Size: 19, Face: faceBold, Track: 0.2}, v.updated, t.right()-2, t.bottom()-11, alignR, p.sub)
}

func fallbackDate(c *canvas, p fallbackPal, d day, x, cy float64) {
	dc := p.ink

	switch d.date.Weekday() {
	case time.Saturday:
		dc = hex(0x2F6FD6)
	case time.Sunday:
		dc = hex(0xD9446B)
	case time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday:
	}

	c.textMid(txt{Size: 38, Face: faceXBold, Track: -0.5}, fmt.Sprintf("%02d", d.date.Day()), x+24, cy, alignL, dc)
	c.textMid(txt{Size: 20, Face: faceBold, Track: 1}, dowKO[weekday(d.date)], x+80, cy+6, alignL, dc)
}

func rgb24(c color.RGBA) uint32 { return uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B) }

func dominantHue(img image.Image) float64 {
	var hist [36]float64

	b := img.Bounds()
	step := max(1, b.Dx()/96, b.Dy()/96)

	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			px := img.At(x, y)
			if px == nil {
				continue
			}

			r, g, bl, _ := px.RGBA()
			h, s, v := rgbToHSV(float64(r)/65535, float64(g)/65535, float64(bl)/65535)

			if s < 0.35 || v < 0.35 {
				continue
			}

			w := s * v

			if h > 10 && h < 50 {
				w *= 0.35
			}

			hist[int(h/10)%36] += w
		}
	}

	best := 0

	for i := range hist {
		if hist[i]+hist[(i+1)%36] > hist[best]+hist[(best+1)%36] {
			best = i
		}
	}

	return float64(best)*10 + 10
}

func rgbToHSV(r, g, b float64) (h, s, v float64) {
	mx, mn := max(r, g, b), min(r, g, b)

	v = mx

	if mx == 0 || mx == mn {
		return 0, 0, v
	}

	d := mx - mn

	s = d / mx

	switch mx {
	case r:
		h = math.Mod((g-b)/d, 6)
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}

	h *= 60
	if h < 0 {
		h += 360
	}

	return h, s, v
}

func hsl(h, s, l float64) color.RGBA {
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2

	var r, g, b float64

	switch {
	case h < 60:
		r, g = c, x
	case h < 120:
		r, g = x, c
	case h < 180:
		g, b = c, x
	case h < 240:
		g, b = x, c
	case h < 300:
		r, b = x, c
	default:
		r, b = c, x
	}

	return color.RGBA{R: uint8((r+m)*255 + 0.5), G: uint8((g+m)*255 + 0.5), B: uint8((b+m)*255 + 0.5), A: 255}
}
