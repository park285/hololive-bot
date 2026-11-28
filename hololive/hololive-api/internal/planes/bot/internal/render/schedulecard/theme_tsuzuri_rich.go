package schedulecard

import (
	"fmt"
	"image/color"
	"time"
)

type tsuzuriRichStyle struct {
	rowFill, rowQuiet, rowBorder, sepCol              color.RGBA
	dateBG, dateSatBG, dateSunBG                      color.RGBA
	dateInk, dateSatInk, dateSunInk, heartCol         color.RGBA
	ink, sub, muted                                   color.RGBA
	timeBG, timeFG, unsureCol, changeBG, changeBorder color.RGBA
	changeFG, offBG, offBorder, offFG, footInk        color.RGBA
	footBoxBG, footBoxBorder                          color.RGBA
	mainFace, subFace                                 face
	mainSize, subSize                                 float64
	dark, stampDate, goldRivets                       bool
}

func drawWavyCancelLines(c *canvas, x0, y0 float64, lines int, col color.RGBA) {
	for k := range lines {
		wy := y0 + float64(k)*14

		var wave shape

		for s := range 17 {
			wx := x0 + float64(s)*5.6
			dy := 3.4

			if s%2 == 1 {
				dy = -3.4
			}

			if s == 0 {
				wave.MoveTo(wx, wy+dy)
			} else {
				wave.LineTo(wx, wy+dy)
			}
		}

		c.Stroke(wave, 1.8, col)
	}
}

func drawTsuzuriFooterBar(c *canvas, t box, updated string, st tsuzuriRichStyle) {
	fy := t.bottom() - 18
	fb := box{t.X + 4, fy - 17, t.W - 8, 34}
	c.Fill(fb.rrect(17), st.footBoxBG)
	c.StrokeButt(dash(fb.rrect(17), 5, 5), 1.6, st.footBoxBorder)

	envCol := st.changeBorder
	drawEnvelope(c, fb.X+26, fy, 26, 0, envelopeStyle{hex(0xFFFFFF), envCol, envCol, 1.5})
	drawEnvelope(c, fb.right()-26, fy, 26, 0, envelopeStyle{hex(0xFFFFFF), envCol, envCol, 1.5})
	c.textMid(txt{Size: 16, Face: faceXBold, Track: 1.6}, "SUZUNA TSUZURI  ·  MAIL POST", fb.X+52, fy+0.5, alignL, st.footInk)
	c.textMid(txt{Size: 18, Face: faceBold, Track: 0.2}, updated, fb.right()-52, fy+0.5, alignR, st.footInk)
}

const tsuzuriRichDateW = 106.0

func tsuzuriRowStyle(st tsuzuriRichStyle) rowStyle {
	return rowStyle{
		main: txt{Size: st.mainSize, Face: st.mainFace}, sub: txt{Size: st.subSize, Face: st.subFace},
		time: txt{Size: 27, Face: faceXBold, Track: 0.2}, pill: txt{Size: 23.5, Face: faceXBold, Track: 0.2},
		capPad: 14, gutter: 22, minSlot: 46, slotGap: 10,
	}
}

func tsuzuriRichSpec(st tsuzuriRichStyle, t box) smartRowSpec {
	timeX := t.X + tsuzuriRichDateW + 22

	return smartRowSpec{st: tsuzuriRowStyle(st), innerW: t.right() - 22 - timeX, avail: t.H - 48, gap: 10, minRow: 80, pad: 11, capHBase: 40}
}

func drawTsuzuriRichTable(c *canvas, t box, v view, st tsuzuriRichStyle) {
	dateW := tsuzuriRichDateW
	timeX := t.X + dateW + 22
	rowSt := tsuzuriRowStyle(st)
	spec := tsuzuriRichSpec(st, t)
	cells, k, basePW := fitSmartCardRows(c.f, v.days, spec, c.kMax())
	rowY := t.Y

	for i := range cells {
		cell := &cells[i]
		d := cell.d
		rh, y := cell.h, rowY

		rowY += rh + spec.gap

		cy := y + rh/2

		tsuzuriRichRowFrame(c, st, box{t.X, y, t.W, rh}, d)
		tsuzuriRichDate(c, st, d, t.X, y, rh, cy)

		c.StrokeButt(dash(line(t.X+dateW+8, y+14, t.X+dateW+8, y+rh-14), 3, 5), 1.5, st.sepCol)

		switch {
		case len(d.slots) == 0:
			c.Fill(rrect(timeX, cy-2, 28, 4, 2), st.sepCol)
		case d.off():
			mc := crescent(timeX+15, cy, 13, deg(-16))
			c.Fill(mc, hex(0xE8BA54))
			c.Stroke(mc, 1.3, hex(0xB88222))
			layoutOff(c.f, d.slots[0], rowSt.main.scaled(k), rowSt.sub.scaled(k), t.right()-20-timeX-40).draw(c, timeX+40, cy, st.sub, st.sub)
		default:
			tsuzuriRichSlots(c, st, cell, k, basePW, timeX, t.right(), y+(rh-cell.content)/2)
		}
	}

	drawTsuzuriFooterBar(c, t, v.updated, st)
}

func tsuzuriRichRowFrame(c *canvas, st tsuzuriRichStyle, cardBox box, d day) {
	cardSh := cardBox.rrect(20)
	fill := st.rowFill

	if d.off() || len(d.slots) == 0 {
		fill = st.rowQuiet
	}

	if st.dark {
		c.Shadow(cardSh, 0, 4, 10, rgba(0x000000, 0.34))
	} else {
		c.Shadow(cardSh, 0, 2.5, 6, rgba(0x8A2D3B, 0.10))
	}

	c.Fill(cardSh, fill)
	c.Stroke(cardSh, 2.0, st.rowBorder)

	if st.goldRivets {
		for _, pt := range [][2]float64{{cardBox.right() - 12, cardBox.Y + 12}, {cardBox.right() - 12, cardBox.bottom() - 12}} {
			c.Fill(circle(pt[0], pt[1], 3.0), hex(0xF0CA73))
		}
	}
}

func tsuzuriRichDate(c *canvas, st tsuzuriRichStyle, d day, x, y, rh, cy float64) {
	dateW := tsuzuriRichDateW
	dcx := x + dateW/2 + 6
	dc, hc, dbg := st.dateInk, st.heartCol, st.dateBG

	switch d.date.Weekday() {
	case time.Saturday:
		dc, hc, dbg = st.dateSatInk, hex(0x8DB9EC), st.dateSatBG
	case time.Sunday:
		dc, hc, dbg = st.dateSunInk, hex(0xF29BB2), st.dateSunBG
	case time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday:
	}

	if st.stampDate {
		r := min(38.0, rh/2-7)
		c.Fill(circle(dcx, cy, r), dbg)
		c.Stroke(circle(dcx, cy, r), 2.2, dc)
		c.StrokeButt(dash(circle(dcx, cy, r-5), 3, 3.5), 1.2, rgba(0xE2AE46, 0.72))
		c.textMid(txt{Size: r * 0.80, Face: faceXBold}, fmt.Sprintf("%02d", d.date.Day()), dcx, cy-r*0.24, alignC, dc)
		c.textMid(txt{Size: r * 0.56, Face: faceBold}, dowKO[weekday(d.date)], dcx, cy+r*0.42, alignC, dc)

		return
	}

	dk := min(1, rh/94)
	c.Fill(rrect(x+8, y+8, dateW-8, rh-16, 14), dbg)
	c.textMid(txt{Size: 17 * dk, Face: faceXBold, Track: 1.5}, dowEN[weekday(d.date)], dcx-2, cy-26*dk, alignC, dc)
	c.Fill(heart(dcx-2, cy-7*dk, 5.5*dk), hc)
	c.textMid(txt{Size: 33 * dk, Face: faceXBold, Track: 0.4}, fmt.Sprintf("%02d", d.date.Day()), dcx-2, cy+20*dk, alignC, dc)
}

func tsuzuriRichSlots(c *canvas, st tsuzuriRichStyle, cell *richRowCell, k, basePW, timeX, right, sy float64) {
	rowSt := tsuzuriRowStyle(st)
	capH := 40.0 * k

	for j := range cell.slots {
		sb := &cell.slots[j]
		stacked := cell.stack[j]

		if j > 0 {
			gy := sy - rowSt.slotGap*k/2
			c.StrokeButt(dash(line(timeX, gy, right-18, gy), 3, 5), 1.4, st.sepCol)
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

		tsuzuriRichTime(c, st, sb.s, kind, label, timeX, pcy, cw, capH, k)
		tsuzuriRichTitle(c, st, sb, timeX+cell.titleXs[j], top, pcy, k)

		sy += sb.h + rowSt.slotGap*k
	}
}

func tsuzuriRichTime(c *canvas, st tsuzuriRichStyle, s slot, kind timeKind, label string, timeX, pcy, cw, capH, k float64) {
	rowSt := tsuzuriRowStyle(st)

	switch kind {
	case timeUnsure:
		capT := rowSt.pill.scaled(k)

		c.StrokeButt(dash(rrect(timeX, pcy-capH/2, cw, capH, capH/2), 6, 4.5), 2.2, st.unsureCol)
		c.textMid(capT, label, timeX+cw/2, pcy, alignC, st.unsureCol)
	case timeChanged:
		capT := rowSt.pill.scaled(k)
		sh := rrect(timeX, pcy-capH/2, cw, capH, capH/2)
		c.Fill(sh, st.changeBG)
		c.Stroke(sh, 2.2, st.changeBorder)
		c.textMid(capT, label, timeX+cw/2, pcy, alignC, st.changeFG)
	case timeCancelled:
		capT := rowSt.time.scaled(k)

		c.Fill(rrect(timeX, pcy-capH/2, cw, capH, capH/2), st.offBG)

		lw := c.f.width(capT, label)
		textStruck(c, capT, label, timeX+cw/2-lw/2, pcy, st.muted)
	case timePlain:
		capT := rowSt.time.scaled(k)

		c.Fill(rrect(timeX, pcy-capH/2, cw, capH, capH/2), st.timeBG)
		c.textMid(capT, label, timeX+cw/2, pcy, alignC, st.timeFG)
	case timeNone:
		if s.status == StatusOff {
			capT := rowSt.pill.scaled(k)
			sh := rrect(timeX, pcy-capH/2, cw, capH, capH/2)
			c.Fill(sh, st.offBG)
			c.StrokeButt(dash(sh, 5, 4), 1.6, st.offBorder)
			c.textMid(capT, "OFF", timeX+cw/2, pcy, alignC, st.offFG)
		}
	}
}

func tsuzuriRichTitle(c *canvas, st tsuzuriRichStyle, sb *slotBox, titleX, top, pcy, k float64) {
	if sb.s.status == StatusOff && sb.s.main == "휴방" && sb.s.sub == "" && len(sb.tb.tags) == 0 {
		mc := crescent(titleX+13, pcy, 11.5, deg(-16))
		c.Fill(mc, hex(0xE8BA54))
		c.Stroke(mc, 1.2, hex(0xB88222))

		mainT := tsuzuriRowStyle(st).main.scaled(k)

		if sb.s.highlight {
			c.Fill(markBand(titleX+34, pcy+c.f.mid(mainT), c.f.width(mainT, "휴방"), mainT), markInk)
		}

		c.textMid(mainT, "휴방", titleX+34, pcy, alignL, st.sub)

		return
	}

	mainCol := st.ink

	if sb.s.status == StatusCancelled {
		mainCol = st.muted
	}

	sb.tb.draw(c, titleX, top, mainCol, st.sub)
}

func renderRichTsuzuriA(c *canvas, v view) {
	const px = tsuzuriPageX

	p := tsuzuriColors
	w, h := c.w, c.h

	tsuzuriADiary(c, p)

	vb := box{px + 62, 40, w - px - 62 - 44, 286}
	tb := box{vb.X, vb.bottom() + 14, vb.W, h - 38 - vb.bottom() - 14}
	c.StrokeButt(dash(line(vb.X, vb.bottom()+2, vb.right(), vb.bottom()+2), 7, 7), 2, p.line)

	tsuzuriAHeader(c, p, v, vb)

	drawTsuzuriRichTable(c, tb, v, tsuzuriRichStyle{
		rowFill: hex(0xFFFFFF), rowQuiet: hex(0xFDF1F3), rowBorder: hex(0xF1C2C8), sepCol: hex(0xF1C2C8),
		dateBG: hex(0xFFF3F5), dateSatBG: hex(0xEDF5FF), dateSunBG: hex(0xFFEBF0),
		dateInk: p.burgundy, dateSatInk: hex(0x3674C4), dateSunInk: hex(0xD63B63), heartCol: p.rose,
		ink: p.ink, sub: p.sub, muted: hex(0x9E837E),
		timeBG: hex(0xFBE9EC), timeFG: p.burgundy, unsureCol: p.burgundy,
		changeBG: hex(0xFBE0E5), changeBorder: p.ribbon, changeFG: p.burgundy,
		offBG: hex(0xFDF1F3), offBorder: p.rose, offFG: p.sub, footInk: p.sub,
		footBoxBG: hex(0xFFFFFF), footBoxBorder: p.rose,
		mainFace: faceHand, subFace: faceBold, mainSize: 42, subSize: 20.5,
	})
}

func tsuzuriADiary(c *canvas, p tsuzuriPal) {
	const px = tsuzuriPageX

	w, h := c.w, c.h
	c.Fill(rect(0, 0, w, h), p.bg)
	dots(c, box{0, 0, w, h}, 38, 3.4, rgba(0xFFFFFF, 0.45))

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
		l.Fill(xf(goatSilhouette(), place(w*0.60, h*0.60, 7.2, 0)), rgba(0xD64A5B, 0.045))
	})
	c.Stroke(rrect(px+10, 30, w-px-40, h-60, 16), 1.6, p.line)
	tsuzuriRings(c, px, 20, h-20)

	rb := bookmarkRibbon(w-98, 20, 56, 86, 16)
	c.Shadow(rb, 0, 3, 5, rgba(0x8A2D3B, 0.22))
	c.Fill(rb, p.ribbon)
	c.StrokeButt(dash(line(w-92, 24, w-92, 98), 3.5, 3.5), 1.3, rgba(0xFFFFFF, 0.55))
	c.StrokeButt(dash(line(w-48, 24, w-48, 98), 3.5, 3.5), 1.3, rgba(0xFFFFFF, 0.55))
	drawEnvelope(c, w-70, 56, 32, 0, envelopeStyle{p.white, p.ribbon, p.ribbon, 1.5})
}

func tsuzuriAHeader(c *canvas, p tsuzuriPal, v view, vb box) {
	const sw = 220.0

	tsuzuriStamp(c, p, v.photo, vb.X+sw/2+12, vb.cy(), sw)
	drawBell(c, vb.X+sw+2, vb.Y+54, 0.56, deg(16), hex(0xE8BA54), hex(0xB88222), p.burgundy)

	nx := vb.X + sw + 44
	nw := vb.right() - nx - 122
	tsuzuriName(c, p, v, nx+nw/2, vb.Y+108, nw, 94)

	perT := txt{Size: 24, Face: faceXBold, Track: 0.8}
	pw := c.f.width(perT, v.period) + 52
	nameT := c.f.fitSize(txt{Size: 94, Face: faceHand}, v.name, nw, 34)
	ry := vb.Y + 108 + nameT.Size*0.56 + 44
	drawLeafSprig(c, nx+nw/2-pw/2-34, ry+4, true, p.rose)
	drawLeafSprig(c, nx+nw/2+pw/2+34, ry+4, false, p.rose)

	goatPostmark(c, vb.right()-156, vb.Y+58, 30, rgba(0x8A2D3B, 0.25))
	drawWavyCancelLines(c, vb.right()-260, vb.Y+44, 3, rgba(0x8A2D3B, 0.16))
	drawEnvelope(c, vb.right()-54, vb.bottom()-54, 70, deg(10), envelopeStyle{p.white, hex(0xE08C99), p.red, 2.2})
	c.Fill(heart(vb.right()-104, vb.bottom()-100, 7.5), p.rose)
}

type tsuzuriAirmailPal struct {
	postRed, postNavy, gold color.RGBA
}

var tsuzuriAirmail = tsuzuriAirmailPal{postRed: hex(0xC9384D), postNavy: hex(0x284A8A), gold: hex(0xE2AE46)}

func renderRichTsuzuriB(c *canvas, v view) {
	p := tsuzuriColors
	ap := tsuzuriAirmail

	board := box{22, 22, c.w - 44, c.h - 44}
	tsuzuriBEnvelope(c, ap, board)

	vb := box{board.X + 24, board.Y + 20, board.W - 48, 266}

	tb := box{board.X + 24, vb.bottom() + 30, board.W - 48, board.bottom() - 18 - vb.bottom() - 30}

	sealY := board.Y + 280
	c.Shadow(scallop(board.cx(), sealY, 21, 2.2, 14), 0, 3, 5, rgba(0x8A2D3B, 0.30))
	c.Fill(scallop(board.cx(), sealY, 21, 2.2, 14), ap.postRed)
	c.Fill(heart(board.cx(), sealY, 8.2), hex(0xFFFFFF))

	tsuzuriBHeader(c, p, ap, v, vb)

	drawTsuzuriRichTable(c, tb, v, tsuzuriRichStyle{
		rowFill: hex(0xFFFFFF), rowQuiet: hex(0xF6EFF2), rowBorder: hex(0xD8DCEB), sepCol: hex(0xE0C5CB),
		dateBG: hex(0xFFF6F7), dateSatBG: hex(0xEAF2FF), dateSunBG: hex(0xFFE9EE),
		dateInk: ap.postNavy, dateSatInk: hex(0x2B5FA3), dateSunInk: ap.postRed, heartCol: ap.postRed,
		ink: hex(0x2A2328), sub: hex(0x6D5A62), muted: hex(0x96858C),
		timeBG: hex(0xEEF2FA), timeFG: ap.postNavy, unsureCol: ap.postRed,
		changeBG: hex(0xFDE2E6), changeBorder: ap.postRed, changeFG: p.burgundy,
		offBG: hex(0xF9EDF0), offBorder: p.rose, offFG: hex(0x6D5A62), footInk: ap.postNavy,
		footBoxBG: hex(0xFFF8F9), footBoxBorder: ap.postNavy,
		mainFace: faceXBold, subFace: faceBold, mainSize: 34, subSize: 22,
		stampDate: true,
	})
}

func tsuzuriBEnvelope(c *canvas, ap tsuzuriAirmailPal, board box) {
	w, h := c.w, c.h

	c.Fill(rect(0, 0, w, h), hex(0xFFFBF6))
	c.Clip(rect(0, 0, w, h), 0.94, func(l *canvas) {
		for d := -h; d < w+h; d += 56 {
			l.Fill(poly(d, 0, d+20, 0, d-h+20, h, d-h, h), ap.postRed)
			l.Fill(poly(d+28, 0, d+48, 0, d-h+48, h, d-h+28, h), ap.postNavy)
		}
	})

	c.Shadow(board.rrect(22), 0, 5, 14, rgba(0x3B1D24, 0.25))
	c.Fill(board.rrect(22), hex(0xFFFBF7))
	c.Stroke(board.rrect(22), 2.5, ap.postNavy)
	c.Clip(board.rrect(22), 1, func(l *canvas) {
		var g shape

		for y := 48.0; y < h-24; y += 32 {
			g.Add(rect(board.X, y, board.W, 1))
		}

		l.Fill(g, rgba(0xD89EA8, 0.16))
		l.Fill(poly(board.X, board.Y, board.right(), board.Y, board.cx(), board.Y+280), rgba(0xF8E3E7, 0.45))
		l.Stroke(line(board.X, board.Y, board.cx(), board.Y+280, board.right(), board.Y), 2.0, rgba(0xC9384D, 0.30))
	})
}

func tsuzuriBHeader(c *canvas, p tsuzuriPal, ap tsuzuriAirmailPal, v view, vb box) {
	const sw = 212.0

	tsuzuriStamp(c, p, v.photo, vb.right()-sw/2-14, vb.cy(), sw)
	drawBell(c, vb.right()-12, vb.Y+48, 0.56, deg(18), ap.gold, hex(0xB88222), p.burgundy)
	goatPostmark(c, vb.right()-sw-84, vb.Y+64, 30, rgba(0x284A8A, 0.30))
	drawWavyCancelLines(c, vb.right()-sw-210, vb.Y+48, 3, rgba(0x284A8A, 0.24))

	nx := vb.X + 16
	nw := vb.right() - sw - 120 - nx
	c.Stroke(rrect(nx, vb.Y+14, 146, 30, 6), 2.0, ap.postNavy)
	c.textMid(txt{Size: 16, Face: faceXBold, Track: 2.2}, "AIR MAIL", nx+73, vb.Y+29, alignC, ap.postNavy)

	nameT := c.f.fitSize(txt{Size: 84, Face: faceHand}, v.name, nw, 40)
	c.textMid(nameT, c.f.clamp(nameT, v.name, nw), nx, vb.Y+104, alignL, p.burgundy)

	perT := txt{Size: 24, Face: faceXBold, Track: 0.8}
	pw := c.f.width(perT, v.period) + 46
	c.Fill(rrect(nx, vb.Y+166, pw, 42, 21), ap.postNavy)
	c.textMid(perT, v.period, nx+pw/2, vb.Y+187, alignC, hex(0xFFFFFF))
	drawEnvelope(c, nx+pw+46, vb.Y+187, 46, deg(-8), envelopeStyle{hex(0xFFFFFF), ap.postRed, ap.postRed, 2.0})
}

type tsuzuriUniformPal struct {
	postRed, gold, ivory color.RGBA
}

var tsuzuriUniform = tsuzuriUniformPal{postRed: hex(0xB82B41), gold: hex(0xF0CA73), ivory: hex(0xFFF8F0)}

func renderRichTsuzuriC(c *canvas, v view) {
	up := tsuzuriUniform
	w, h := c.w, c.h

	board := box{24, 24, w - 48, h - 48}
	tsuzuriCBoard(c, up, board)

	c.Fill(xf(goatSilhouette(), place(w*0.62, h*0.60, 7.2, 0)), rgba(0xF0CA73, 0.055))

	vb := box{board.X + 24, board.Y + 24, board.W - 48, 274}
	tb := box{board.X + 24, vb.bottom() + 18, board.W - 48, board.bottom() - 18 - vb.bottom() - 18}

	tsuzuriCHeader(c, up, v, vb)

	c.Fill(rect(vb.X, vb.bottom()+4, vb.W, 2), rgba(0xF0CA73, 0.48))

	drawTsuzuriRichTable(c, tb, v, tsuzuriRichStyle{
		rowFill: hex(0x28223C), rowQuiet: hex(0x211B31), rowBorder: rgba(0xF0CA73, 0.48), sepCol: rgba(0xF0CA73, 0.30),
		dateBG: hex(0x3A2034), dateSatBG: hex(0x1E3A6E), dateSunBG: hex(0x7A1E30),
		dateInk: up.gold, dateSatInk: hex(0xA6CEFF), dateSunInk: hex(0xFFD6DE), heartCol: up.postRed,
		ink: up.ivory, sub: hex(0xD8C1C6), muted: hex(0x9C868D),
		timeBG: hex(0x3C2D4F), timeFG: up.gold, unsureCol: up.gold,
		changeBG: up.postRed, changeBorder: up.gold, changeFG: up.ivory,
		offBG: hex(0x36243B), offBorder: up.gold, offFG: hex(0xD8C1C6), footInk: hex(0xE8D4AC),
		footBoxBG: hex(0x1D172B), footBoxBorder: rgba(0xF0CA73, 0.55),
		mainFace: faceXBold, subFace: faceBold, mainSize: 34, subSize: 22,
		dark: true, stampDate: true, goldRivets: true,
	})
}

func tsuzuriCBoard(c *canvas, up tsuzuriUniformPal, board box) {
	w, h := c.w, c.h

	c.FillLinear(rect(0, 0, w, h), 0, 0, w, h, stop{0, hex(0x9E2034)}, stop{1, hex(0x62111F)})
	dots(c, box{0, 0, w, h}, 28, 2.2, rgba(0xF0CA73, 0.16))

	c.Shadow(board.rrect(28), 0, 8, 18, rgba(0x000000, 0.5))
	c.FillLinear(board.rrect(28), 0, board.Y, 0, board.bottom(), stop{0, hex(0x1E2544)}, stop{1, hex(0x2A1725)})
	c.Stroke(board.rrect(28), 3.8, up.gold)
	c.Stroke(board.inset(7).rrect(22), 1.5, rgba(0xF0CA73, 0.58))

	for _, corner := range [][4]float64{
		{board.X + 18, board.Y + 18, 1, 1},
		{board.right() - 18, board.Y + 18, -1, 1},
		{board.X + 18, board.bottom() - 18, 1, -1},
		{board.right() - 18, board.bottom() - 18, -1, -1},
	} {
		cx, cy, dx, dy := corner[0], corner[1], corner[2], corner[3]
		c.Fill(circle(cx, cy, 4.0), up.gold)
		c.Stroke(line(cx, cy+dy*24, cx, cy, cx+dx*24, cy), 2.2, rgba(0xF0CA73, 0.72))
	}

	rb := bookmarkRibbon(board.right()-96, board.Y, 52, 82, 15)
	c.Shadow(rb, 0, 4, 6, rgba(0x000000, 0.35))
	c.Fill(rb, up.postRed)
	c.Stroke(rb, 2.0, up.gold)
	drawEnvelope(c, board.right()-70, board.Y+34, 28, 0, envelopeStyle{up.ivory, up.gold, up.postRed, 1.4})
}

func tsuzuriCHeader(c *canvas, up tsuzuriUniformPal, v view, vb box) {
	const sw = 212.0

	tsuzuriStamp(c, tsuzuriColors, v.photo, vb.X+sw/2+12, vb.cy(), sw)
	drawBell(c, vb.X+sw+2, vb.Y+52, 0.58, deg(16), up.gold, hex(0xB88222), hex(0x42141E))

	nx := vb.X + sw + 46
	nw := vb.right() - nx - 118
	nameT := c.f.fitSize(txt{Size: 90, Face: faceHand}, v.name, nw, 40)
	c.textMid(nameT, c.f.clamp(nameT, v.name, nw), nx+nw/2, vb.Y+100, alignC, up.ivory)

	perT := txt{Size: 24, Face: faceXBold, Track: 0.8}
	pw := c.f.width(perT, v.period) + 54
	ry := vb.Y + 188
	c.Fill(rrect(nx+nw/2-pw/2, ry-22, pw, 44, 6), up.postRed)
	c.Stroke(rrect(nx+nw/2-pw/2, ry-22, pw, 44, 6), 2.0, up.gold)
	c.textMid(perT, v.period, nx+nw/2, ry, alignC, up.ivory)
	drawLeafSprig(c, nx+nw/2-pw/2-28, ry, true, up.gold)
	drawLeafSprig(c, nx+nw/2+pw/2+28, ry, false, up.gold)

	goatPostmark(c, vb.right()-126, vb.Y+54, 28, rgba(0xF0CA73, 0.34))
	drawEnvelope(c, vb.right()-56, vb.bottom()-54, 66, deg(10), envelopeStyle{up.ivory, up.gold, up.postRed, 2.2})
}
