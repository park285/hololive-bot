package schedulecard

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"time"
)

type sopiaPal struct {
	lav, peri, periD, navy, yel, sub, white color.RGBA
}

var sopiaColors = sopiaPal{
	lav: hex(0xCDD0FB), peri: hex(0x8A92F0), periD: hex(0x5F67D6), navy: hex(0x30346E),
	yel: hex(0xFFEE58), sub: hex(0x5A60B8), white: hex(0xFFFFFF),
}

func (p sopiaPal) saturn() saturnStyle {
	return saturnStyle{body: p.yel, band: rgba(0xFFFFFF, 0.85), ring: hex(0xA9AFF8), ink: p.white}
}

func renderSopia(c *canvas, v view) {
	p := sopiaColors
	w, h := c.w, c.h
	c.Fill(rect(0, 0, w, h), p.lav)

	var st shape

	for x := 0.0; x < w; x += 40 {
		st.Add(rect(x, 0, 20, h))
	}

	c.Fill(st, hex(0xD6D9FD))
	c.Glow(w*0.42, h*0.55, 300, 0xFFF7A6, 0.50)
	c.Glow(w*0.66, h+40, 320, 0xFFF7A6, 0.42)
	c.Glow(w, 0, 280, 0xFFFFFF, 0.55)

	vb, tb := sopiaBoxes(c)
	sopiaPanel(c, p, vb, v)
	sopiaTable(c, p, tb, v)
}

func sopiaBoxes(c *canvas) (vb, tb box) {
	w, h := c.w, c.h

	if c.tall() {
		vb = box{28, 28, w - 56, 292}

		return vb, box{28, vb.bottom() + 24, w - 56, h - 28 - vb.bottom() - 24}
	}

	vb = box{28, 28, 376, h - 56}

	return vb, box{vb.right() + 30, 28, w - 28 - vb.right() - 30, h - 56}
}

func (p sopiaPal) sticker(c *canvas, s shape, fill color.RGBA) {
	c.Shadow(s, 0, 2.5, 4, rgba(0x1E2050, 0.40))
	c.Fill(outline(s, 9, false), p.white)
	c.Fill(s, fill)
}

func sopiaSky(c *canvas, in box) {
	sky := in.rrect(37)
	c.Clip(sky, 1, func(l *canvas) {
		l.FillLinear(sky, in.X, in.Y, in.right(), in.bottom(), stop{0, hex(0x20234F)}, stop{1, hex(0x474DAE)})
		l.Glow(in.X+in.W*0.15, in.bottom(), in.W*0.55, 0xB36BE0, 0.34)
		l.Glow(in.right(), in.Y+in.H*0.2, in.W*0.5, 0x5FA8FF, 0.26)

		// 고정 난수로 별을 뿌린다(같은 판형은 항상 같은 하늘).
		seed := uint32(11)
		rnd := func() float64 {
			seed = seed*1664525 + 1013904223

			return float64(seed>>8) / float64(1<<24)
		}

		var dim, bright shape

		for range int(in.W * in.H / 5200) {
			x, y, r := in.X+rnd()*in.W, in.Y+rnd()*in.H, 0.9+rnd()*1.5
			if rnd() < 0.3 {
				bright.Add(circle(x, y, r+0.5))
			} else {
				dim.Add(circle(x, y, r))
			}
		}

		l.Fill(dim, rgba(0xFFFFFF, 0.45))
		l.Fill(bright, rgba(0xFFFFFF, 0.92))
	})
}

func sopiaPanel(c *canvas, p sopiaPal, vb box, v view) {
	c.Shadow(vb.rrect(46), 0, 7, 14, rgba(0x5C63C8, 0.40))
	c.Fill(vb.rrect(46), p.peri)
	c.Fill(vb.inset(4).rrect(42), p.white)
	sopiaSky(c, vb.inset(9))

	if c.tall() {
		r := vb.H/2 - 34
		pcx := vb.X + 40 + r
		sopiaPorthole(c, p, v.photo, pcx, vb.cy(), r)

		nx := pcx + r + 44
		nw := vb.right() - 150 - nx
		sopiaName(c, p, v, nx+nw/2, vb.cy()-30, nw, 92)
		drawRocket(c, vb.right()-88, vb.cy()+6, 1.18, deg(30), p.periD)
		p.sticker(c, puffyStar(vb.right()-150, vb.Y+52, 15, 7.5, deg(-10)), p.yel)
		c.Fill(sparkle(vb.right()-40, vb.bottom()-48, 9), p.white)

		return
	}

	r := min(vb.W/2-46, 146)
	pcy := vb.Y + 46 + r
	sopiaPorthole(c, p, v.photo, vb.cx(), pcy, r)

	ny := pcy + r + 76
	sopiaName(c, p, v, vb.cx(), ny, vb.W-48, 66)

	top := ny + 112
	rest := vb.bottom() - 22 - top
	s := min(1.7, rest/176)

	drawRocket(c, vb.cx()+8, top+rest*0.5, s, deg(32), p.periD)
	drawSaturn(c, vb.X+62, vb.bottom()-70, 22, deg(-18), p.saturn())
	p.sticker(c, puffyStar(vb.right()-58, top+rest*0.2, 17, 8.5, deg(12)), p.yel)
	c.Fill(sparkle(vb.right()-46, vb.bottom()-64, 10), p.white)
	c.Fill(sparkle(vb.X+48, top+rest*0.22, 7), rgba(0xFFFFFF, 0.9))
}

func sopiaPorthole(c *canvas, p sopiaPal, photo image.Image, cx, cy, r float64) {
	c.Shadow(circle(cx, cy, r+17), 0, 5, 10, rgba(0x0E1030, 0.55))
	c.Fill(circle(cx, cy, r+17), p.peri)
	c.Fill(circle(cx, cy, r+12), p.white)
	c.Fill(circle(cx, cy, r+5), p.periD)
	c.Medallion(photo, cx, cy, r, 0, 0xC3C7FB, 0.58)

	c.Shadow(circle(cx-r*0.80, cy-r*0.72, r*0.30), 0, 3, 5, rgba(0x0E1030, 0.45))
	drawSaturn(c, cx-r*0.80, cy-r*0.72, r*0.27, deg(-20), p.saturn())
	p.sticker(c, puffyStar(cx+r*0.86, cy-r*0.62, r*0.17, r*0.085, deg(14)), p.yel)
	p.sticker(c, puffyStar(cx+r*0.70, cy+r*0.84, r*0.11, r*0.055, deg(-10)), p.yel)
}

func sopiaName(c *canvas, p sopiaPal, v view, cx, cy, maxW, maxSize float64) {
	nameT := c.f.fitSize(txt{Size: maxSize, Face: faceRound, Track: 1}, v.name, maxW-18, 34)
	ns := xf(c.textMidShape(nameT, c.f.clamp(nameT, v.name, maxW-18)), move(cx, cy))
	c.Shadow(outline(ns, 17, false), 0, 3, 5, rgba(0x0E1030, 0.5))
	c.Fill(outline(ns, 17, false), p.white)
	c.Fill(outline(ns, 9, false), p.navy)
	c.Fill(ns, p.yel)

	perT := txt{Size: 25, Face: faceRound, Track: 1.4}
	pw := c.f.width(perT, v.period) + 84
	py := cy + nameT.Size*0.5 + 42
	c.Fill(rrect(cx-pw/2-3, py-25, pw+6, 50, 25), p.white)
	c.Fill(rrect(cx-pw/2, py-22, pw, 44, 22), p.navy)
	c.textMid(perT, v.period, cx, py, alignC, p.white)
	c.Fill(sparkle(cx-pw/2+24, py, 8.5), p.yel)
	c.Fill(sparkle(cx+pw/2-24, py, 8.5), p.yel)
}

var sopiaRow = rowStyle{
	main: txt{Size: 40, Face: faceRound}, sub: txt{Size: 27, Face: faceRound},
	time: txt{Size: 36, Face: faceRound, Track: 0.6}, pill: txt{Size: 28, Face: faceRound, Track: 0.4},
	capPad: 17, gutter: 24, minSlot: 46, slotGap: 8,
}

const sopiaBadgeR = 46.0

const sopiaGap = 12.0

func sopiaTimeX(t box) float64 { return t.X + 2*sopiaBadgeR + 34 }

func sopiaSpec(c *canvas, t box) tableSpec {
	return tableSpec{
		innerW: t.right() - 28 - sopiaTimeX(t), avail: t.H - 34, gap: sopiaGap, minRow: 2*sopiaBadgeR - 2, pad: 12, kMax: c.kMax(),
	}
}

func sopiaTable(c *canvas, p sopiaPal, t box, v view) {
	const gap = sopiaGap

	bcx := t.X + sopiaBadgeR + 4
	sx := bcx + 14
	sw := t.right() - sx
	timeX := sopiaTimeX(t)
	fit, days := fitWeek(c.f, v.days, sopiaRow, sopiaSpec(c, t))
	rowY := t.Y

	for i, d := range days {
		rh, y := fit.heights[i], rowY

		rowY += rh + gap

		cy := y + rh/2

		sopiaRowFrame(c, p, box{sx, y, sw, rh}, d.off() || len(d.slots) == 0)
		sopiaBadge(c, p, bcx, cy, d)

		switch {
		case len(d.slots) == 0:
			c.Fill(rrect(timeX, cy-2, 30, 4, 2), p.peri)
		case d.off():
			offT := txt{Size: 38 * fit.k, Face: faceRound, Track: 4}
			layoutOff(c.f, d.slots[0], offT, sopiaRow.sub.scaled(fit.k), t.right()-28-timeX).draw(c, timeX, cy, p.periD, p.sub)
		default:
			drawTableSlots(c, sopiaRow, &fit, i, timeX, y+(rh-fit.totals[i])/2, slotPaint{
				timeInk: p.navy, unsureInk: p.periD, ink: p.navy, sub: p.sub,
				changedFill: p.yel, changedInk: p.navy, changedRing: p.periD,
				capH: 44, dashOn: 6, dashOff: 4.5, dashW: 2.4,
			})
		}
	}

	p.sticker(c, puffyStar(t.X+16, t.bottom()-12, 12, 6, deg(8)), p.yel)
	c.textMid(txt{Size: 19, Face: faceBold, Track: 0.2}, v.updated, t.right()-4, t.bottom()-11, alignR, p.navy)
}

func sopiaRowFrame(c *canvas, p sopiaPal, r box, quiet bool) {
	halo := rrect(r.X-6, r.Y-6, r.W+12, r.H+12, 30)
	c.Fill(halo, rgba(0xFFFFFF, 0.50))
	c.Stroke(halo, 1.6, rgba(0x8A92F0, 0.60))

	fill := p.white

	if quiet {
		fill = hex(0xE9EBFE)
	}

	slotShape := r.rrect(24)
	c.Fill(slotShape, fill)
	c.Stroke(slotShape, 2.6, p.peri)

	node := func(x, y float64, col color.RGBA) {
		c.Fill(circle(x, y, 6.6), p.peri)
		c.Fill(circle(x, y, 4.2), col)
	}

	node(r.X+r.W*0.36, r.Y-6, p.yel)
	node(r.X+r.W*0.88, r.bottom()+6, hex(0xE4E6FF))
}

func sopiaBadge(c *canvas, p sopiaPal, bcx, cy float64, d day) {
	sat := d.date.Weekday() == time.Saturday
	// 고리 앞쪽 호가 날짜 숫자 아래를 지나도록 중심을 내린다.
	ringM := place(bcx, cy+8, 1, deg(-13))
	ring := func(a0, a1 float64) {
		var s shape

		s.Arc(0, 0, 64, 25, a0, a1)

		s = xf(s, ringM)
		c.StrokeButt(s, 11, p.white)
		c.StrokeButt(s, 6.5, p.yel)
	}

	if sat {
		ring(math.Pi, 2*math.Pi)
	}

	sc := scallop(bcx, cy, sopiaBadgeR, 3, 12)
	c.Shadow(sc, 0, 2.5, 4, rgba(0x5C63C8, 0.30))
	c.Fill(scallop(bcx, cy, sopiaBadgeR+3.4, 3, 12), p.peri)
	c.Fill(sc, p.white)

	inner := circle(bcx, cy, sopiaBadgeR-7.5)
	c.Clip(inner, 1, func(l *canvas) {
		l.Fill(inner, p.yel)
		checker(l, box{bcx - 48, cy - 48, 96, 96}, 8, rgba(0xFFFFFF, 0.36))
	})
	c.StrokeButt(dash(circle(bcx, cy, sopiaBadgeR-12.5), 3, 3.4), 1.3, rgba(0xFFFFFF, 0.95))
	c.textMid(txt{Size: 15, Face: faceXBold, Track: 1.3}, dowEN[weekday(d.date)], bcx, cy-15, alignC, p.navy)
	c.textMid(txt{Size: 30, Face: faceRound, Track: 0.4}, fmt.Sprintf("%02d", d.date.Day()), bcx, cy+10, alignC, p.navy)

	if sat {
		ring(0, math.Pi)
	}
}
