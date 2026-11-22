package schedulecard

import (
	"fmt"
	"image/color"
	"math"
	"time"
)

type sopiaRichStyle struct {
	cardFill, cardQuiet, cardBorder, haloFill, haloStroke color.RGBA
	nodeFill, nodeRing, accentBar                         color.RGBA
	ink, sub, muted                                       color.RGBA
	timeBG, timeFG, unsureCol, changeBG, changeFG         color.RGBA
	changeRing, sepCol, sepDot, footInk                   color.RGBA
	cardRadius                                            float64
	dark, hexBadge, hudRail                               bool
}

func drawSopiaSignalBars(c *canvas, cx, py, pw float64, barCol, outlineCol color.RGBA) {
	for _, side := range []float64{-1, 1} {
		for k := range 3 {
			bx := cx + side*(pw/2+16+float64(k)*11.5)
			bh := 26.0 - float64(k)*4.5
			bar := rrect(bx-3.6, py-bh/2, 7.2, bh, 3.6)

			if outlineCol.A > 0 {
				c.Fill(outline(bar, 3.2, false), outlineCol)
			}

			c.Fill(bar, barCol)
		}
	}
}

func drawSopiaAntennaPin(c *canvas, cx, cy, r float64) {
	c.Fill(circle(cx, cy, r+4), hex(0xFFFFFF))
	c.FillLinear(circle(cx, cy, r+1.5), cx-r, cy-r, cx+r, cy+r, stop{0, hex(0x4CE6D8)}, stop{0.55, hex(0xFFEE58)}, stop{1, hex(0xFF7AC6)})
	c.Fill(circle(cx, cy, r-4.5), hex(0x22366E))
	c.Stroke(line(cx, cy+r*0.52, cx, cy-r*0.10), 2.4, hex(0x5CEAD8))
	c.Stroke(line(cx, cy-r*0.10, cx-r*0.40, cy-r*0.44), 2.2, hex(0xFFEE58))
	c.Stroke(line(cx, cy-r*0.10, cx+r*0.40, cy-r*0.44), 2.2, hex(0xFFEE58))
	c.Fill(circle(cx, cy-r*0.10, 3.2), hex(0xFFFFFF))
}

func drawSopiaHexPatch(c *canvas, cx, cy, r float64, d day) {
	sat := d.date.Weekday() == time.Saturday
	sun := d.date.Weekday() == time.Sunday

	var hexS, hexIn shape

	for i := range 6 {
		a := deg(30 + float64(i)*60)
		px, py := cx+r*math.Cos(a), cy+r*math.Sin(a)
		ix, iy := cx+(r-6.5)*math.Cos(a), cy+(r-6.5)*math.Sin(a)

		if i == 0 {
			hexS.MoveTo(px, py)
			hexIn.MoveTo(ix, iy)
		} else {
			hexS.LineTo(px, py)
			hexIn.LineTo(ix, iy)
		}
	}

	hexS.Close()
	hexIn.Close()

	c.Shadow(hexS, 0, 3, 6, rgba(0x121848, 0.34))

	outer, fill, dowCol, numCol := hex(0x2843C8), hex(0xFFE840), hex(0x162270), hex(0x162270)

	if sat {
		outer, fill, dowCol, numCol = hex(0x162270), hex(0x4CE6D8), hex(0x162270), hex(0x162270)
	} else if sun {
		outer, fill, dowCol, numCol = hex(0xC2285B), hex(0xFF7AC6), hex(0xFFFFFF), hex(0xFFFFFF)
	}

	c.Fill(outline(hexS, 4.5, false), hex(0xFFFFFF))
	c.Fill(hexS, outer)
	c.Fill(hexIn, fill)
	c.textMid(txt{Size: 15, Face: faceXBold, Track: 1.2}, dowEN[weekday(d.date)], cx, cy-15, alignC, dowCol)
	c.textMid(txt{Size: 29, Face: faceImpact, Track: 0.4}, fmt.Sprintf("%02d", d.date.Day()), cx, cy+11, alignC, numCol)
}

func drawSopiaRichBadge(c *canvas, bcx, cy float64, d day, st sopiaRichStyle) {
	if st.hexBadge {
		drawSopiaHexPatch(c, bcx, cy, sopiaBadgeR+3, d)

		return
	}

	sat := d.date.Weekday() == time.Saturday
	sun := d.date.Weekday() == time.Sunday

	ringM := place(bcx, cy+10, 1, deg(-13))
	ring := func(a0, a1 float64) {
		var s shape

		s.Arc(0, 0, 64, 24, a0, a1)

		s = xf(s, ringM)
		c.StrokeButt(s, 10.5, hex(0xFFFFFF))
		c.StrokeButt(s, 6.2, hex(0xFFEE58))
	}

	if sat {
		ring(math.Pi, 2*math.Pi)
	}

	sc := scallop(bcx, cy, sopiaBadgeR, 3.2, 12)
	c.Shadow(sc, 0, 3, 5, rgba(0x1E2050, 0.34))

	outer := hex(0x8A92F0)

	if sun {
		outer = hex(0xEE6D9D)
	} else if st.dark {
		outer = hex(0x5CEAD8)
	}

	c.Fill(scallop(bcx, cy, sopiaBadgeR+3.6, 3.2, 12), outer)
	c.Fill(sc, hex(0xFFFFFF))

	inner := circle(bcx, cy, sopiaBadgeR-7.5)
	c.Clip(inner, 1, func(l *canvas) {
		fill := hex(0xFFEE58)

		if sun {
			fill = hex(0xFFE0EE)
		}

		l.Fill(inner, fill)
		dots(l, box{bcx - 48, cy - 48, 96, 96}, 9, 2.3, rgba(0xFFFFFF, 0.52))
	})
	c.StrokeButt(dash(circle(bcx, cy, sopiaBadgeR-12.5), 3, 3.4), 1.3, rgba(0xFFFFFF, 0.95))

	dowCol, numCol := hex(0x1C659A), hex(0x30346E)

	if sat {
		dowCol, numCol = hex(0x2742C8), hex(0x2742C8)
	} else if sun {
		dowCol, numCol = hex(0xC2285B), hex(0xC2285B)
	}

	c.textMid(txt{Size: 15, Face: faceXBold, Track: 1.3}, dowEN[weekday(d.date)], bcx, cy-15, alignC, dowCol)
	c.textMid(txt{Size: 30, Face: faceRound, Track: 0.4}, fmt.Sprintf("%02d", d.date.Day()), bcx, cy+10, alignC, numCol)

	if sat {
		ring(0, math.Pi)
	}
}

type richRowCell struct {
	d       day
	slots   []slotBox
	stack   []bool
	capW    []float64
	titleXs []float64
	h       float64
	content float64
}

type smartRowSpec struct {
	st                                        rowStyle
	innerW, avail, gap, minRow, pad, capHBase float64
}

type smartRowLayout struct {
	cells []richRowCell
	lines int
	ok    bool
	bpw   float64
}

func smartBasePW(f *fontSet, days []day, st rowStyle, k float64) float64 {
	w := f.width(st.time.scaled(k), "00:00") + 28*k

	for _, d := range days {
		if d.off() {
			continue
		}

		for _, s := range d.slots {
			label, kind := timeLabel(s)
			switch kind {
			case timePlain, timeCancelled:
				w = max(w, f.width(st.time.scaled(k), label)+28*k)
			case timeUnsure:
				w = max(w, f.width(st.pill.scaled(k), label)+2*st.capPad*k)
			case timeNone:
				if s.status == StatusOff {
					w = max(w, f.width(st.pill.scaled(k), "OFF")+2*st.capPad*k)
				}
			case timeChanged:
			}
		}
	}

	return w
}

func smartSlotOffset(f *fontSet, spec smartRowSpec, s slot, k, bpw float64, mainT, subT txt) (cw, txOff float64, stacked bool) {
	st := spec.st
	label, kind := timeLabel(s)

	cw = bpw
	txOff = bpw + st.gutter*k

	if kind != timeChanged {
		return cw, txOff, false
	}

	cw = f.width(st.pill.scaled(k), label) + 2*st.capPad*k

	inlineW := spec.innerW - (cw + st.gutter*k)
	tbTry := layoutTitle(f, s, mainT, subT, titleLimits{maxW: inlineW, maxMain: 1, maxSub: 1, withTags: true})

	if !tbTry.cut && len(tbTry.main) == 1 && len(tbTry.sub) == 0 && !tbTry.tagRow {
		return cw, cw + st.gutter*k, false
	}

	return cw, 0, true
}

func smartMeasureCell(f *fontSet, spec smartRowSpec, d day, k, bpw float64) (richRowCell, int, bool) {
	st := spec.st
	mainT, subT := st.main.scaled(k), st.sub.scaled(k)
	cell := richRowCell{d: d}
	content := st.slotGap * k * float64(max(0, len(d.slots)-1))
	ok, lines := true, 0

	for _, s := range d.slots {
		cw, txOff, stacked := smartSlotOffset(f, spec, s, k, bpw, mainT, subT)
		tw := spec.innerW - txOff
		lim := titleLimits{maxW: tw, maxMain: 2, maxSub: 1, withTags: true}

		if d.off() {
			lim.maxMain, lim.maxSub, lim.withTags = 1, 0, false
		} else if len(d.slots) == 1 {
			lim.maxSub = 2
		}

		tb := layoutTitle(f, s, mainT, subT, lim)
		sh := max(tb.height(), st.minSlot*k)

		if stacked {
			sh = spec.capHBase*k + 7*k + tb.height()
		}

		cell.slots = append(cell.slots, slotBox{s, tb, sh})
		cell.stack = append(cell.stack, stacked)
		cell.capW = append(cell.capW, cw)
		cell.titleXs = append(cell.titleXs, txOff)

		content += sh
		lines += len(tb.main)

		ok = ok && (!tb.cut || k <= softMin)
	}

	cell.content = content
	cell.h = max(spec.minRow, content+2*spec.pad)

	return cell, lines, ok
}

func smartBuild(f *fontSet, days []day, spec smartRowSpec, k float64) smartRowLayout {
	bpw := smartBasePW(f, days, spec.st, k)
	out := make([]richRowCell, 0, len(days))
	total := spec.gap * float64(len(days)-1)
	ok, lines := true, 0

	for _, d := range days {
		cell, cellLines, cellOK := smartMeasureCell(f, spec, d, k, bpw)

		total += cell.h
		lines += cellLines

		ok = ok && cellOK

		out = append(out, cell)
	}

	return smartRowLayout{cells: out, lines: lines, ok: ok && total <= spec.avail, bpw: bpw}
}

type smartFit struct {
	cells  []richRowCell
	k, bpw float64
}

func smartFitScale(f *fontSet, days []day, spec smartRowSpec, kMax float64) (smartFit, bool) {
	baseLines := -1

	for k := kMax; ; k -= 0.03 {
		l := smartBuild(f, days, spec, k)
		ok := l.ok

		if k > 1 {
			if baseLines < 0 {
				baseLines = smartBuild(f, days, spec, 1).lines
			}

			ok = ok && l.lines <= baseLines
		}

		if ok || k <= kMin {
			return smartFit{cells: l.cells, k: k, bpw: l.bpw}, l.ok
		}
	}
}

func fitSmartCardRows(f *fontSet, days []day, spec smartRowSpec, kMax float64) ([]richRowCell, float64, float64) {
	fit, _ := fitFolded(days, func(d []day) (smartFit, bool) { return smartFitScale(f, d, spec, kMax) })
	best := fit.cells
	total := spec.gap * float64(len(best)-1)

	for i := range best {
		total += best[i].h
	}

	slack := max(0, spec.avail-total)
	extra := slack / float64(len(best))

	for i := range best {
		best[i].h += extra
	}

	return best, fit.k, fit.bpw
}

var sopiaRowStyle = rowStyle{
	main: txt{Size: 38, Face: faceRound}, sub: txt{Size: 24, Face: faceRound},
	time: txt{Size: 27, Face: faceRound, Track: 0.4}, pill: txt{Size: 24, Face: faceRound, Track: 0.4},
	capPad: 15, gutter: 24, minSlot: 46, slotGap: 11,
}

func sopiaRichSpec(t box) smartRowSpec {
	timeX := t.X + 2*sopiaBadgeR + 28

	return smartRowSpec{st: sopiaRowStyle, innerW: t.right() - 26 - timeX, avail: t.H - 46, gap: 12, minRow: 2*sopiaBadgeR - 2, pad: 12, capHBase: 42}
}

func drawSopiaRichTable(c *canvas, t box, v view, st sopiaRichStyle) {
	bcx := t.X + sopiaBadgeR + 4
	sx := bcx + 14
	sw := t.right() - sx
	timeX := t.X + 2*sopiaBadgeR + 28
	spec := sopiaRichSpec(t)
	cells, k, basePW := fitSmartCardRows(c.f, v.days, spec, c.kMax())
	rowY := t.Y

	if len(cells) >= 2 {
		yFirst := t.Y + cells[0].h/2
		yLast := t.Y + (t.H - 46) - cells[len(cells)-1].h/2
		c.StrokeButt(dash(line(bcx, yFirst, bcx, yLast), 6, 7), 2.4, st.sepCol)
	}

	for i := range cells {
		cell := &cells[i]
		d := cell.d
		rh, y := cell.h, rowY

		rowY += rh + spec.gap

		cy := y + rh/2
		r := box{sx, y, sw, rh}

		sopiaRichCardFrame(c, st, r, d)
		drawSopiaRichBadge(c, bcx, cy, d, st)

		switch {
		case len(d.slots) == 0:
			c.Fill(rrect(timeX, cy-2, 30, 4, 2), st.cardBorder)
		case d.off():
			sopiaRichOffRow(c, st, d, k, r, timeX, cy)
		default:
			sopiaRichSlots(c, st, cell, k, basePW, timeX, r, y+(rh-cell.content)/2)
		}
	}

	sopiaRichFooter(c, st, t, v.updated)
}

func sopiaRichCardFrame(c *canvas, st sopiaRichStyle, r box, d day) {
	cr := st.cardRadius
	halo := rrect(r.X-6, r.Y-6, r.W+12, r.H+12, cr+6)
	c.Fill(halo, st.haloFill)
	c.Stroke(halo, 1.6, st.haloStroke)

	fill := st.cardFill

	if d.off() || len(d.slots) == 0 {
		fill = st.cardQuiet
	}

	slotShape := r.rrect(cr)
	c.Shadow(slotShape, 0, 3.5, 7, rgba(0x161845, 0.16))
	c.Fill(slotShape, fill)
	c.Stroke(slotShape, 2.6, st.cardBorder)

	if st.hudRail && !d.off() {
		barCol := st.accentBar

		if d.date.Weekday() == time.Sunday {
			barCol = hex(0xFF7AC6)
		} else if d.date.Weekday() == time.Saturday {
			barCol = hex(0xFFEE58)
		}

		c.Fill(rrect(r.right()-14, r.Y+14, 5, r.H-28, 2.5), barCol)
	}

	for _, nd := range [][3]float64{{r.X + r.W*0.36, r.Y - 6, 1}, {r.X + r.W*0.88, r.bottom() + 6, 0}} {
		c.Fill(circle(nd[0], nd[1], 6.4), st.nodeRing)

		col := st.nodeFill

		if nd[2] == 0 {
			col = hex(0x5CEAD8)
		}

		c.Fill(circle(nd[0], nd[1], 4.0), col)
	}
}

func sopiaRichOffRow(c *canvas, st sopiaRichStyle, d day, k float64, r box, timeX, cy float64) {
	offT := txt{Size: 35 * k, Face: faceRound, Track: 3.5}
	offL := layoutOff(c.f, d.slots[0], offT, sopiaRowStyle.sub.scaled(k), r.W-180)
	ow := offL.width(c.f)
	centerX := (timeX + r.right() - 24) / 2
	startX := centerX - (ow+44)/2
	c.StrokeButt(dash(line(timeX+8, cy, startX-24, cy), 5, 6), 1.6, st.sepCol)
	c.StrokeButt(dash(line(startX+ow+68, cy, r.right()-32, cy), 5, 6), 1.6, st.sepCol)

	mc := crescent(startX+14, cy, 14.5, deg(-18))
	c.Fill(outline(mc, 3.4, false), hex(0xFFFFFF))
	c.Fill(mc, hex(0xFFEE58))
	c.Stroke(mc, 1.3, hex(0x5F67D6))
	c.Fill(sparkle(startX+33, cy-11, 6.5), hex(0x5CEAD8))
	offL.draw(c, startX+44, cy, st.sub, st.sub)
}

func sopiaRichSlots(c *canvas, st sopiaRichStyle, cell *richRowCell, k, basePW, timeX float64, r box, sy float64) {
	rowSt := sopiaRowStyle
	capH := 42.0 * k

	for j := range cell.slots {
		sb := &cell.slots[j]
		stacked := cell.stack[j]

		if j > 0 {
			gy := sy - rowSt.slotGap*k/2
			c.StrokeButt(dash(line(timeX, gy, r.right()-26, gy), 5, 5), 1.5, st.sepCol)
			c.Fill(circle((timeX+r.right()-26)/2, gy, 3.6), st.cardBorder)
			c.Fill(circle((timeX+r.right()-26)/2, gy, 2.0), st.sepDot)
		}

		top := sy + (sb.h-sb.tb.height())/2
		pcy := sy + sb.h/2

		if stacked {
			pcy = sy + capH/2
			top = sy + capH + 7*k
		}

		label, kind := timeLabel(sb.s)
		cw := cell.capW[j]

		if !stacked && kind != timeChanged {
			cw = basePW
		}

		sopiaRichTime(c, st, kind, label, timeX, pcy, cw, capH, k)
		sb.tb.draw(c, timeX+cell.titleXs[j], top, st.ink, st.sub)

		sy += sb.h + rowSt.slotGap*k
	}
}

func sopiaRichTime(c *canvas, st sopiaRichStyle, kind timeKind, label string, timeX, pcy, cw, capH, k float64) {
	rowSt := sopiaRowStyle

	switch kind {
	case timeUnsure:
		capT := rowSt.pill.scaled(k)
		sh := rrect(timeX, pcy-capH/2, cw, capH, capH/2)
		c.StrokeButt(dash(sh, 6, 4.5), 2.3, st.unsureCol)
		c.textMid(capT, label, timeX+cw/2, pcy, alignC, st.unsureCol)
	case timeChanged:
		capT := rowSt.pill.scaled(k)

		c.Fill(rrect(timeX-2.5, pcy-capH/2-2.5, cw+5, capH+5, capH/2+2.5), st.changeRing)
		pill(c, timeX, pcy, capH, rowSt.capPad*k, capT, label, st.changeBG, st.changeFG)
	case timeCancelled:
		capT := rowSt.time.scaled(k)

		c.Fill(rrect(timeX, pcy-capH/2, cw, capH, capH/2), rgba(0x8A92F0, 0.22))

		lw := c.f.width(capT, label)
		textStruck(c, capT, label, timeX+cw/2-lw/2, pcy, st.muted)
	case timePlain:
		capT := rowSt.time.scaled(k)

		c.Fill(rrect(timeX, pcy-capH/2, cw, capH, capH/2), st.timeBG)
		c.textMid(capT, label, timeX+cw/2, pcy, alignC, st.timeFG)
	case timeNone:
	}
}

func sopiaRichFooter(c *canvas, st sopiaRichStyle, t box, updated string) {
	fy := t.bottom() - 14
	drawSaturn(c, t.X+36, fy-1, 14, deg(-16), saturnStyle{body: hex(0x5F67D6), band: rgba(0xFFFFFF, 0.72), ring: hex(0xFFEE58), ink: hex(0xFFFFFF)})

	sp := puffyStar(t.X+76, fy, 9.5, 4.8, deg(10))
	c.Fill(outline(sp, 4.5, false), hex(0xFFFFFF))
	c.Fill(sp, hex(0xFFEE58))

	wmCol := rgba(0x5F67D6, 0.72)

	if st.dark {
		wmCol = rgba(0x5CEAD8, 0.65)
	}

	c.textMid(txt{Size: 16, Face: faceXBold, Track: 2.2}, "SORASHINA SOPIA", t.X+104, fy+1, alignL, wmCol)
	c.textMid(txt{Size: 19, Face: faceBold, Track: 0.2}, updated, t.right()-4, fy+1, alignR, st.footInk)
}

func renderRichSopiaA(c *canvas, v view) {
	p := sopiaColors
	w, h := c.w, c.h

	sopiaABackdrop(c, p)

	vb := box{28, 28, w - 56, 294}
	tb := box{28, vb.bottom() + 20, w - 56, h - 26 - vb.bottom() - 20}

	sopiaAHeaderFrame(c, p, vb)
	sopiaAHeaderContent(c, p, v, vb)

	drawSopiaRichTable(c, tb, v, sopiaRichStyle{
		cardFill: p.white, cardQuiet: hex(0xE8EAFF), cardBorder: p.peri,
		haloFill: rgba(0xFFFFFF, 0.54), haloStroke: rgba(0x8A92F0, 0.62),
		nodeFill: p.yel, nodeRing: p.peri,
		ink: p.navy, sub: p.sub, muted: hex(0x8388C6),
		timeBG: hex(0xEEF0FF), timeFG: p.navy, unsureCol: p.periD,
		changeBG: p.yel, changeFG: p.navy, changeRing: p.periD,
		sepCol: rgba(0x8A92F0, 0.52), sepDot: p.yel, footInk: p.navy,
		cardRadius: 24,
	})
}

func sopiaABackdrop(c *canvas, p sopiaPal) {
	w, h := c.w, c.h
	c.Fill(rect(0, 0, w, h), p.lav)

	var st shape

	for x := 0.0; x < w; x += 40 {
		st.Add(rect(x, 0, 20, h))
	}

	c.Fill(st, hex(0xD8DBFD))
	c.Glow(w*0.35, h*0.55, 340, 0xFFF7A6, 0.52)
	c.Glow(w*0.75, h+20, 340, 0xFFF7A6, 0.44)
	c.Glow(w, 0, 280, 0xFFFFFF, 0.55)

	c.StrokeButt(dash(rrect(12, 12, w-24, h-24, 38), 10, 10), 2.0, rgba(0x8A92F0, 0.42))
}

func sopiaAHeaderFrame(c *canvas, p sopiaPal, vb box) {
	c.Shadow(vb.rrect(46), 0, 8, 16, rgba(0x5C63C8, 0.42))
	c.Fill(vb.rrect(46), p.peri)
	c.Fill(vb.inset(4).rrect(42), p.white)
	sopiaSky(c, vb.inset(9))

	c.Clip(vb.inset(9).rrect(37), 1, func(l *canvas) {
		var arc shape

		arc.Arc(vb.cx()+30, vb.bottom()+30, vb.W*0.48, vb.H*0.82, math.Pi*1.12, math.Pi*1.90)
		l.StrokeButt(dash(arc, 8, 8), 2.0, rgba(0x4CE6D8, 0.46))
	})
}

func sopiaAHeaderContent(c *canvas, p sopiaPal, v view, vb box) {
	r := vb.H/2 - 34
	pcx := vb.X + 42 + r
	sopiaPorthole(c, p, v.photo, pcx, vb.cy(), r)
	drawSopiaAntennaPin(c, pcx+r*0.76, vb.cy()-r*0.74, 22)

	nx := pcx + r + 42
	nw := vb.right() - 150 - nx

	wsT := txt{Size: 15, Face: faceXBold, Track: 2.0}
	wsW := c.f.width(wsT, "WEEKLY SCHEDULE") + 32
	pill(c, nx+nw/2-wsW/2, vb.Y+42, 28, 16, wsT, "WEEKLY SCHEDULE", rgba(0x5CEAD8, 0.24), hex(0x96FAF0))
	sopiaName(c, p, v, nx+nw/2, vb.cy()-14, nw, 86)

	perT := txt{Size: 25, Face: faceRound, Track: 1.4}
	pw := c.f.width(perT, v.period) + 84
	nameT := c.f.fitSize(txt{Size: 86, Face: faceRound, Track: 1}, v.name, nw-18, 34)
	drawSopiaSignalBars(c, nx+nw/2, vb.cy()-14+nameT.Size*0.5+42, pw, p.yel, p.white)

	drawRocket(c, vb.right()-86, vb.cy()+8, 1.22, deg(30), p.periD)
	p.sticker(c, puffyStar(vb.right()-148, vb.Y+50, 15, 7.5, deg(-10)), p.yel)
	c.Fill(sparkle(vb.right()-38, vb.bottom()-44, 10), hex(0x5CEAD8))
}

func renderRichSopiaB(c *canvas, v view) {
	w, h := c.w, c.h

	sopiaBBackdrop(c)

	vb := box{30, 30, w - 60, 290}
	tb := box{30, vb.bottom() + 20, w - 60, h - 28 - vb.bottom() - 20}

	sopiaBHeaderFrame(c, vb)

	r := vb.H/2 - 34
	pcx := vb.right() - 44 - r

	sopiaBAvatar(c, v, vb, pcx, r)
	sopiaBTitle(c, v, vb, pcx, r)

	drawSopiaRichTable(c, tb, v, sopiaRichStyle{
		cardFill: hex(0x22255C), cardQuiet: hex(0x1A1D4A), cardBorder: hex(0x6D78E0),
		haloFill: rgba(0x5CEAD8, 0.10), haloStroke: rgba(0x5CEAD8, 0.36),
		nodeFill: hex(0xFFEE58), nodeRing: hex(0x5CEAD8), accentBar: hex(0x5CEAD8),
		ink: hex(0xFAF8FF), sub: hex(0xC6CAFA), muted: hex(0x888CC8),
		timeBG: hex(0x363B85), timeFG: hex(0xFFEE58), unsureCol: hex(0x5CEAD8),
		changeBG: hex(0xFFEE58), changeFG: hex(0x1E2054), changeRing: hex(0x5CEAD8),
		sepCol: rgba(0x7B84E8, 0.42), sepDot: hex(0x5CEAD8), footInk: hex(0xDBDDFD),
		cardRadius: 20, dark: true, hudRail: true,
	})
}

func sopiaBBackdrop(c *canvas) {
	w, h := c.w, c.h
	c.FillLinear(rect(0, 0, w, h), 0, 0, w, h, stop{0, hex(0x14163D)}, stop{0.55, hex(0x232262)}, stop{1, hex(0x121333)})
	c.Glow(w*0.82, 160, 340, 0x4CE6D8, 0.30)
	c.Glow(140, h*0.56, 360, 0x8F65FF, 0.26)
	c.Glow(w*0.65, h-60, 320, 0xFFEE58, 0.14)
	orbitStars(c)

	c.Stroke(rrect(14, 14, w-28, h-28, 36), 2.2, rgba(0x5CEAD8, 0.42))
	c.StrokeButt(dash(rrect(22, 22, w-44, h-44, 30), 14, 10), 1.4, rgba(0xA9AFF8, 0.32))
}

func sopiaBHeaderFrame(c *canvas, vb box) {
	c.Shadow(vb.rrect(40), 0, 8, 18, rgba(0x07081C, 0.55))
	c.Fill(vb.rrect(40), hex(0x5CEAD8))
	c.Fill(vb.inset(3).rrect(37), hex(0x161945))
	sopiaSky(c, vb.inset(7))

	c.Clip(vb.inset(7).rrect(32), 0.12, func(l *canvas) {
		var sl shape

		for y := vb.Y + 12; y < vb.bottom()-12; y += 10 {
			sl.Add(rect(vb.X, y, vb.W, 2))
		}

		l.Fill(sl, hex(0x5CEAD8))
	})
}

func sopiaBAvatar(c *canvas, v view, vb box, pcx, r float64) {
	c.FillLinear(circle(pcx, vb.cy(), r+13), pcx-r, vb.cy()-r, pcx+r, vb.cy()+r, stop{0, hex(0x5CEAD8)}, stop{0.55, hex(0xFFEE58)}, stop{1, hex(0xFF7AC6)})
	c.Fill(circle(pcx, vb.cy(), r+5), hex(0x161945))
	c.Medallion(v.photo, pcx, vb.cy(), r, 0, 0x5CEAD8, 0.45)
	drawSopiaAntennaPin(c, pcx-r*0.76, vb.cy()-r*0.74, 22)
	drawSaturn(c, pcx+r*0.72, vb.cy()+r*0.70, r*0.26, deg(-16), sopiaColors.saturn())
}

func sopiaBTitle(c *canvas, v view, vb box, pcx, r float64) {
	nx := vb.X + 44
	nw := pcx - r - 56 - nx
	pill(c, nx, vb.Y+52, 30, 16, txt{Size: 16, Face: faceXBold, Track: 2.0}, "ORBITAL LOG // WEEKLY", rgba(0x5CEAD8, 0.22), hex(0x5CEAD8))

	nameT := c.f.fitSize(txt{Size: 88, Face: faceRound, Track: 1}, v.name, nw, 34)
	ns := c.textShape(nameT, c.f.clamp(nameT, v.name, nw), nx, vb.cy()+6+c.f.mid(nameT), alignL)
	c.Fill(outline(ns, 13, false), hex(0x121333))
	c.Fill(outline(ns, 7, false), hex(0x5CEAD8))
	c.Fill(ns, hex(0xFFEE58))

	perT := txt{Size: 24, Face: faceRound, Track: 1.4}
	pw := pill(c, nx, vb.bottom()-54, 44, 24, perT, v.period, hex(0x282E7A), hex(0xFFFFFF))
	c.Stroke(rrect(nx, vb.bottom()-76, pw, 44, 22), 2.0, hex(0x5CEAD8))

	for k := range 5 {
		bx := nx + pw + 22 + float64(k)*12
		bh := []float64{12, 22, 32, 20, 14}[k]
		col := hex(0x5CEAD8)

		if k == 2 {
			col = hex(0xFFEE58)
		}

		c.Fill(rrect(bx, vb.bottom()-54-bh/2, 6, bh, 3), col)
	}

	drawRocket(c, pcx-r-68, vb.Y+64, 0.88, deg(32), hex(0x161945))
}

type sopiaPopPal struct {
	cobalt, navy, yel, cyan, pink color.RGBA
}

var sopiaPop = sopiaPopPal{
	cobalt: hex(0x2843C8), navy: hex(0x162270), yel: hex(0xFFE840), cyan: hex(0x4CE6D8), pink: hex(0xFF7AC6),
}

func renderRichSopiaC(c *canvas, v view) {
	pp := sopiaPop
	w, h := c.w, c.h

	board := box{26, 26, w - 52, h - 52}
	sopiaCBackdrop(c, pp, board)

	vb := box{board.X + 20, board.Y + 24, board.W - 40, 268}
	tb := box{board.X + 20, vb.bottom() + 18, board.W - 40, board.bottom() - 18 - vb.bottom() - 18}

	sopiaCHeader(c, pp, v, vb)

	drawSopiaRichTable(c, tb, v, sopiaRichStyle{
		cardFill: hex(0xFFFFFF), cardQuiet: hex(0xE9ECFF), cardBorder: pp.navy,
		haloFill: rgba(0x4CE6D8, 0.24), haloStroke: rgba(0x2843C8, 0.50),
		nodeFill: pp.yel, nodeRing: pp.navy,
		ink: pp.navy, sub: hex(0x4A559E), muted: hex(0x7C84BA),
		timeBG: pp.cobalt, timeFG: hex(0xFFFFFF), unsureCol: pp.cobalt,
		changeBG: pp.yel, changeFG: pp.navy, changeRing: pp.navy,
		sepCol: rgba(0x2843C8, 0.36), sepDot: pp.pink, footInk: pp.navy,
		cardRadius: 16, hexBadge: true,
	})
}

func sopiaCBackdrop(c *canvas, pp sopiaPopPal, board box) {
	w, h := c.w, c.h
	c.FillLinear(rect(0, 0, w, h), 0, 0, w, h, stop{0, hex(0x2B44CC)}, stop{1, hex(0x1A257E)})
	dots(c, box{0, 0, w, h}, 24, 3.2, rgba(0xFFFFFF, 0.12))
	c.Fill(poly(0, 0, 290, 0, 0, 230), pp.yel)
	c.Fill(poly(w, h, w-200, h, w, h-160), pp.cyan)
	c.Fill(poly(w, 0, w-190, 0, w, 170), pp.pink)

	c.Shadow(board.rrect(28), 0, 8, 18, rgba(0x0B1040, 0.48))
	c.Fill(board.rrect(28), hex(0xF7F8FF))
	c.Stroke(board.rrect(28), 4.2, pp.navy)

	c.Clip(rect(board.cx()-180, board.Y+4, 360, 12), 1, func(l *canvas) {
		l.Fill(rect(board.cx()-180, board.Y+4, 360, 12), pp.yel)
		stripes(l, box{board.cx() - 180, board.Y + 4, 360, 12}, 10, 10, deg(38), pp.cobalt)
	})
}

func sopiaCHeader(c *canvas, pp sopiaPopPal, v view, vb box) {
	p := sopiaColors

	c.Fill(vb.rrect(24), pp.cobalt)
	c.Clip(vb.rrect(24), 0.18, func(l *canvas) {
		dots(l, vb, 18, 3.0, hex(0xFFFFFF))
	})
	c.Stroke(vb.rrect(24), 3.4, pp.navy)

	r := vb.H/2 - 28
	pcx := vb.X + 36 + r
	c.FillLinear(circle(pcx, vb.cy(), r+13), pcx-r, vb.cy()-r, pcx+r, vb.cy()+r, stop{0, pp.cyan}, stop{0.5, pp.yel}, stop{1, pp.pink})
	c.Fill(circle(pcx, vb.cy(), r+6), hex(0xFFFFFF))
	c.Medallion(v.photo, pcx, vb.cy(), r, 0, 0xC8D2FF, 0.50)
	drawSopiaAntennaPin(c, pcx+r*0.76, vb.cy()-r*0.74, 20)
	drawSaturn(c, pcx-r*0.78, vb.cy()+r*0.70, r*0.26, deg(-18), p.saturn())

	nx := pcx + r + 38
	nw := vb.right() - 138 - nx
	sopiaName(c, p, v, nx+nw/2, vb.cy()-26, nw, 86)

	perT := txt{Size: 25, Face: faceRound, Track: 1.4}
	pw := c.f.width(perT, v.period) + 84
	nameT := c.f.fitSize(txt{Size: 86, Face: faceRound, Track: 1}, v.name, nw-18, 34)
	drawSopiaSignalBars(c, nx+nw/2, vb.cy()-26+nameT.Size*0.5+42, pw, pp.cyan, pp.navy)
	drawRocket(c, vb.right()-78, vb.cy()+6, 1.15, deg(28), pp.navy)
	p.sticker(c, puffyStar(vb.right()-132, vb.Y+42, 14, 7, deg(-12)), pp.yel)
}
