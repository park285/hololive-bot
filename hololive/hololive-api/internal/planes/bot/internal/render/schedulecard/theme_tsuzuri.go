package schedulecard

import (
	"fmt"
	"image"
	"image/color"
	"time"
)

type tsuzuriPal struct {
	bg, page, grid, line, burgundy, ink, sub, red, ribbon, rose, white color.RGBA
}

var tsuzuriColors = tsuzuriPal{
	bg: hex(0xF7D3D6), page: hex(0xFFFAF6), grid: hex(0xFAEBE7), line: hex(0xF1C2C8),
	burgundy: hex(0x8A2D3B), ink: hex(0x4B3836), sub: hex(0x86655F),
	red: hex(0xD64A5B), ribbon: hex(0xC8394D), rose: hex(0xF2A0AC), white: hex(0xFFFFFF),
}

func renderTsuzuri(c *canvas, v view) {
	const px = tsuzuriPageX

	p := tsuzuriColors
	w, h := c.w, c.h
	c.Fill(rect(0, 0, w, h), p.bg)
	c.Clip(rect(0, 0, w, h), 1, func(l *canvas) {
		dots(l, box{0, 0, w, h}, 38, 3.4, rgba(0xFFFFFF, 0.40))
	})

	page := rrect(px, 20, w-px-20, h-40, 24)
	c.Shadow(page, 0, 5, 14, rgba(0xB8606A, 0.30))
	c.Fill(page, p.page)
	c.Clip(page, 1, func(l *canvas) {
		var g shape

		for x := px + 24; x < w-20; x += 24 {
			g.Add(rect(x, 20, 1, h-40))
		}

		for y := 44.0; y < h-20; y += 24 {
			g.Add(rect(px, y, w-px-20, 1))
		}

		l.Fill(g, p.grid)
	})
	c.Stroke(rrect(px+10, 30, w-px-40, h-60, 16), 1.6, p.line)
	tsuzuriRings(c, px, 20, h-20)

	vb, tb := tsuzuriBoxes(c)

	if c.tall() {
		c.StrokeButt(dash(line(vb.X, vb.bottom()+2, vb.right(), vb.bottom()+2), 7, 7), 2, p.line)
		tsuzuriBand(c, p, vb, v)
	} else {
		c.StrokeButt(dash(line(vb.right()+19, 62, vb.right()+19, h-62), 7, 7), 2, p.line)
		tsuzuriColumn(c, p, vb, v)
	}

	tsuzuriTable(c, p, tb, v)
}

const tsuzuriPageX = 46.0

func tsuzuriBoxes(c *canvas) (vb, tb box) {
	const px = tsuzuriPageX

	w, h := c.w, c.h

	if c.tall() {
		vb = box{px + 62, 40, w - px - 62 - 44, 296}

		return vb, box{vb.X, vb.bottom() + 12, vb.W, h - 44 - vb.bottom() - 12}
	}

	vb = box{px + 58, 44, 372, h - 88}

	return vb, box{vb.right() + 38, 44, w - 46 - vb.right() - 38, h - 88}
}

func tsuzuriRings(c *canvas, x, y0, y1 float64) {
	n := max(1, int((y1-y0)/96))
	step := (y1 - y0) / float64(n)

	for i := range n {
		y := y0 + step*(float64(i)+0.5)
		c.Fill(ellipse(x+27, y, 8.5, 7.5), hex(0xE3B7B3))

		for _, dy := range []float64{-5.5, 5.5} {
			wire := rrect(x-24, y+dy-3.2, 56, 6.4, 3.2)
			c.Shadow(wire, 0, 1.6, 2, rgba(0x7A4B50, 0.28))
			c.FillLinear(wire, 0, y+dy-3.2, 0, y+dy+3.2, stop{0, hex(0xEFD7B8)}, stop{0.5, hex(0xCFA57C)}, stop{1, hex(0xB0855E)})
		}
	}
}

func tsuzuriColumn(c *canvas, p tsuzuriPal, vb box, v view) {
	const nameH = 150.0

	sw := min(vb.W-24, 348)
	sh := sw * 1.13
	doodle := min(250, vb.H-sh-nameH-60)
	gap := (vb.H - sh - nameH - doodle) / 3

	top := vb.Y + gap*0.7
	tsuzuriStamp(c, p, v.photo, vb.cx(), top+sh/2, sw)
	tsuzuriName(c, p, v, vb.cx(), top+sh+gap+46, vb.W, 72)
	tsuzuriDoodle(c, p, vb.cx(), top+sh+gap+nameH+gap*0.9+doodle/2, doodle)
}

func tsuzuriBand(c *canvas, p tsuzuriPal, vb box, v view) {
	const sw = 228.0

	tsuzuriStamp(c, p, v.photo, vb.X+sw/2+14, vb.cy(), sw)

	nx := vb.X + sw + 44
	nw := vb.right() - nx - 110
	tsuzuriName(c, p, v, nx+nw/2, vb.Y+112, nw, 96)

	postmark(c, vb.right()-52, vb.Y+62, 40, rgba(0x8A2D3B, 0.24))
	drawEnvelope(c, vb.right()-56, vb.bottom()-62, 78, deg(10), envelopeStyle{p.white, hex(0xE08C99), p.red, 2.2})
	c.Fill(heart(vb.right()-112, vb.bottom()-110, 8), p.rose)
}

func tsuzuriName(c *canvas, p tsuzuriPal, v view, cx, cy, maxW, maxSize float64) {
	nameT := c.f.fitSize(txt{Size: maxSize, Face: faceHand}, v.name, maxW, 34)
	c.textMid(nameT, c.f.clamp(nameT, v.name, maxW), cx, cy, alignC, p.burgundy)

	nw := min(maxW, c.f.width(nameT, v.name))
	uy := cy + nameT.Size*0.56
	c.StrokeButt(dash(line(cx-nw/2+18, uy, cx+nw/2-18, uy), 7, 6), 2.4, p.rose)
	c.Fill(heart(cx-nw/2+2, uy, 7), p.red)
	c.Fill(heart(cx+nw/2-2, uy, 7), p.red)

	perT := txt{Size: 24, Face: faceXBold, Track: 0.8}
	pw := c.f.width(perT, v.period) + 52
	ry := uy + 44

	for _, sg := range []float64{-1, 1} {
		ex := cx + sg*pw/2
		c.Fill(poly(ex-sg*14, ry-13, ex+sg*30, ry-13, ex+sg*18, ry+9, ex+sg*30, ry+31, ex-sg*14, ry+31), hex(0xE98593))
		c.Fill(poly(ex-sg*14, ry+22, ex, ry+22, ex-sg*14, ry+31), p.burgundy)
	}

	band := rrect(cx-pw/2, ry-22, pw, 44, 4)
	c.Shadow(band, 0, 2, 4, rgba(0x8A2D3B, 0.20))
	c.Fill(band, p.ribbon)
	c.StrokeButt(dash(line(cx-pw/2+8, ry-16, cx+pw/2-8, ry-16), 4, 4), 1.2, rgba(0xFFFFFF, 0.55))
	c.StrokeButt(dash(line(cx-pw/2+8, ry+16, cx+pw/2-8, ry+16), 4, 4), 1.2, rgba(0xFFFFFF, 0.55))
	c.textMid(perT, v.period, cx, ry, alignC, p.white)
}

const tsuzuriTilt = -3.5

func tsuzuriStamp(c *canvas, p tsuzuriPal, photo image.Image, cx, cy, w float64) {
	h := w * 1.13
	m := place(cx, cy, 1, deg(tsuzuriTilt))
	nx, ny := int(w/23), int(h/23)

	body := xf(perforated(w, h, w/float64(nx)*0.27, nx, ny), m)
	c.Shadow(body, 2, 8, 12, rgba(0x8A2D3B, 0.26))
	c.Fill(body, p.white)

	in := w * 0.075
	c.Fill(xf(rrect(-w/2+in, -h/2+in, w-2*in, h-2*in, 4), m), hex(0xFCE3E6))
	c.Stroke(xf(rrect(-w/2+in+7, -h/2+in+7, w-2*in-14, h-2*in-14, 2), m), 1.4, rgba(0xD64A5B, 0.55))

	r := w * 0.35
	pc := m.Pt(0, -h*0.055)
	c.Fill(scallop(pc.X, pc.Y, r+11, 3.2, 28), p.white)
	c.Fill(circle(pc.X, pc.Y, r+4), p.red)
	c.Medallion(photo, pc.X, pc.Y, r, deg(tsuzuriTilt), 0xF9CDD3, 0.62)

	foot := m.Pt(0, h/2-in-w*0.085)
	drawEnvelope(c, foot.X, foot.Y, w*0.11, deg(tsuzuriTilt), envelopeStyle{p.white, p.red, p.red, 1.6})

	for _, dx := range []float64{-0.13, 0.13} {
		hp := m.Pt(dx*w, h/2-in-w*0.085)
		c.Fill(heart(hp.X, hp.Y, w*0.02), p.red)
	}

	for _, dx := range []float64{-0.21, 0.21} {
		hp := m.Pt(dx*w, h/2-in-w*0.085)
		c.Fill(heart(hp.X, hp.Y, w*0.014), p.rose)
	}

	a := m.Pt(-w*0.42, -h*0.47)
	washi(c, place(a.X, a.Y, 1, deg(tsuzuriTilt-30)), w*0.40, w*0.105, hex(0xF9BCC7), polka(rgba(0xFFFFFF, 0.85), w*0.04, w*0.0095))

	b := m.Pt(w*0.44, h*0.43)
	washi(c, place(b.X, b.Y, 1, deg(tsuzuriTilt-36)), w*0.34, w*0.10, hex(0xD6ECC0), gingham(rgba(0xFFFFFF, 0.55), w*0.036, w*0.017))
}

func polka(col color.RGBA, step, r float64) func(l *canvas, m affine) {
	return func(l *canvas, m affine) {
		var s shape

		for j := -3; j <= 3; j++ {
			for i := -14; i <= 14; i++ {
				s.Add(circle((float64(i)+0.5*float64(j&1))*step, float64(j)*step*0.86, r))
			}
		}

		l.Fill(xf(s, m), col)
	}
}

func gingham(col color.RGBA, step, t float64) func(l *canvas, m affine) {
	return func(l *canvas, m affine) {
		var s shape

		for i := -14; i <= 14; i++ {
			s.Add(rect(float64(i)*step-t/2, -60, t, 120))
		}

		l.Fill(xf(s, m), col)

		s = shape{}

		for j := -4; j <= 4; j++ {
			s.Add(rect(-200, float64(j)*step-t/2, 400, t))
		}

		l.Fill(xf(s, m), col)
	}
}

func tsuzuriDoodle(c *canvas, p tsuzuriPal, cx, cy, size float64) {
	postmark(c, cx-size*0.40, cy+size*0.12, size*0.27, rgba(0x8A2D3B, 0.24))

	ew := size * 0.66
	env := xf(rrect(-ew/2, -ew*0.34, ew, ew*0.68, ew*0.06), place(cx+size*0.10, cy+size*0.04, 1, deg(-8)))
	c.Shadow(env, 2, 6, 9, rgba(0x8A2D3B, 0.20))
	drawEnvelope(c, cx+size*0.10, cy+size*0.04, ew, deg(-8), envelopeStyle{p.white, hex(0xE08C99), p.red, 2.6})

	for _, h := range [][3]float64{{0.50, -0.34, 0.058}, {0.36, -0.44, 0.040}, {0.58, -0.16, 0.034}, {-0.44, -0.30, 0.036}} {
		c.Fill(heart(cx+size*h[0], cy+size*h[1], size*h[2]), p.rose)
	}
}

var tsuzuriRow = rowStyle{
	main: txt{Size: 49, Face: faceHand}, sub: txt{Size: 32, Face: faceHand},
	time: txt{Size: 31, Face: faceXBold, Track: 0.2}, pill: txt{Size: 25, Face: faceXBold, Track: 0.2},
	capPad: 15, gutter: 22, minSlot: 46, slotGap: 14,
}

const tsuzuriDateW = 104.0

func tsuzuriSpec(c *canvas, t box) tableSpec {
	return tableSpec{innerW: t.W - tsuzuriDateW - 12 - 14, avail: t.H - 34, minRow: 66, pad: 9, kMax: c.kMax()}
}

func tsuzuriTable(c *canvas, p tsuzuriPal, t box, v view) {
	const dateW = tsuzuriDateW

	st := tsuzuriRow
	fit, days := fitWeek(c.f, v.days, st, tsuzuriSpec(c, t))
	timeX := t.X + dateW + 12
	y := t.Y

	for i, d := range days {
		rh := fit.heights[i]
		if i > 0 {
			c.StrokeButt(dash(line(t.X+6, y, t.right()-6, y), 3, 5), 1.8, p.line)
		}

		tsuzuriDate(c, p, d, t.X+dateW/2, y+rh/2, min(1, rh/104))

		switch {
		case len(d.slots) == 0:
			c.Fill(rrect(timeX, y+rh/2-1.5, 28, 3, 1.5), p.line)
		case d.off():
			c.Fill(heart(timeX+10, y+rh/2, 8), p.rose)
			layoutOff(c.f, d.slots[0], st.main.scaled(fit.k), st.sub.scaled(fit.k), t.right()-10-timeX-30).draw(c, timeX+30, y+rh/2, p.sub, p.sub)
		default:
			drawTableSlots(c, st, &fit, i, timeX, y+(rh-fit.totals[i])/2, slotPaint{
				timeInk: p.burgundy, unsureInk: p.burgundy, ink: p.ink, sub: p.sub,
				changedFill: p.ribbon, changedInk: p.white,
				capH: 42, dashOn: 6, dashOff: 5, dashW: 2.2,
				separator: func(gy float64) {
					c.StrokeButt(dash(line(timeX, gy, t.right()-10, gy), 2, 6), 1.4, p.line)
				},
			})
		}

		y += rh
	}

	c.textMid(txt{Size: 19, Face: faceBold, Track: 0.2}, v.updated, t.right()-4, t.bottom()-11, alignR, p.sub)
}

func tsuzuriDate(c *canvas, p tsuzuriPal, d day, dcx, cy, dk float64) {
	dc, hc := p.burgundy, p.rose

	switch d.date.Weekday() {
	case time.Saturday:
		dc, hc = hex(0x3F7BC8), hex(0x8DB9EC)
	case time.Sunday:
		dc, hc = hex(0xD8446C), hex(0xF29BB2)
	case time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday:
	}

	c.textMid(txt{Size: 18 * dk, Face: faceXBold, Track: 1.6}, dowEN[weekday(d.date)], dcx, cy-29*dk, alignC, dc)
	c.Fill(heart(dcx, cy-8*dk, 6*dk), hc)
	c.textMid(txt{Size: 36 * dk, Face: faceXBold, Track: 0.4}, fmt.Sprintf("%02d", d.date.Day()), dcx, cy+21*dk, alignC, dc)
}

func postmark(c *canvas, cx, cy, r float64, col color.RGBA) {
	c.Stroke(circle(cx, cy, r), 2.2, col)
	c.StrokeButt(dash(circle(cx, cy, r*0.78), 4, 4), 1.5, col)
	c.Fill(heart(cx, cy+1, r*0.30), col)
}
