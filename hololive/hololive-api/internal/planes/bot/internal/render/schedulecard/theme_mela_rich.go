package schedulecard

import (
	"image/color"
	"math"
	"strconv"
	"time"
)

type melaRichStyle struct {
	capShadow, capOutline, capFill, capCancelFill, capInset color.RGBA
	ink, sub, muted                                         color.RGBA
	tagFill, tagInk, changeFill, changeInk                  color.RGBA
	groupBG, groupStroke, bracketCol                        color.RGBA
	pillarOut, pillarIn                                     color.RGBA
	offBG, offWave, offPillBG, offPillInk                   color.RGBA
	diamondA, diamondB, diamondOut                          color.RGBA
	footInk                                                 color.RGBA
	talismanPlate                                           bool
}

func drawMelaShimenawa(c *canvas, x0, x1, y, sag float64, ink, ropeL, ropeD color.RGBA) {
	const steps = 48

	span := x1 - x0

	var arch shape

	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		px := x0 + span*t
		py := y + sag*math.Sin(math.Pi*t)

		if i == 0 {
			arch.MoveTo(px, py)
		} else {
			arch.LineTo(px, py)
		}
	}

	c.Stroke(arch, 19, ink)
	c.Stroke(arch, 13, ropeL)
	c.Stroke(xf(arch, move(0, 2)), 5.5, ropeD)

	const marks = 18

	for i := 1; i < marks; i++ {
		t := float64(i) / float64(marks)
		px := x0 + span*t
		py := y + sag*math.Sin(math.Pi*t)
		c.Stroke(line(px-4.5, py-6, px+4.5, py+6), 2.0, ink)
	}

	for _, f := range []float64{0.20, 0.50, 0.80} {
		px := x0 + span*f
		py := y + sag*math.Sin(math.Pi*f) + 6
		shide := poly(px-4.5, py, px+4.5, py, px+8, py+10, px+1, py+11, px+5, py+21, px-3.5, py+23, px-8, py+13, px-1, py+12)
		c.Fill(outline(shide, 3.2, false), ink)
		c.Fill(shide, hex(0xFFFFFF))
	}
}

func drawMelaRichTalisman(c *canvas, cx, cy, w, h, angle float64, fill, ink, accent color.RGBA) {
	m := place(cx, cy, 1, angle)
	body := xf(rrect(-w/2, -h/2, w, h, 4), m)
	c.Fill(outline(body, 4.0, false), ink)
	c.Fill(body, fill)
	c.Fill(xf(circle(0, -h*0.32, w*0.11), m), ink)
	c.Fill(xf(rrect(-w*0.26, -h*0.14, w*0.52, h*0.10, 2), m), accent)
	c.Fill(xf(rrect(-w*0.20, h*0.04, w*0.40, h*0.10, 2), m), ink)
	c.Fill(xf(rrect(-w*0.26, h*0.22, w*0.52, h*0.10, 2), m), accent)
}

func drawMelaMatoiSilhouette(c *canvas, cx, topY float64, col color.RGBA) {
	c.Fill(circle(cx, topY+44, 42), col)
	c.Fill(rrect(cx-24, topY+80, 48, 22, 6), col)

	for i := range 5 {
		sx := cx - 34 + float64(i)*17
		sh := 190.0 + float64((i%2)*28)
		c.Fill(rrect(sx-5, topY+104, 10, sh, 5), col)
	}
}

func drawMelaRichTable(c *canvas, t box, all []day, st melaRichStyle, updated string, h float64) {
	const dayGap = 11.0

	usable := box{t.X, t.Y, t.W, t.H - dayGap*float64(max(0, len(all)-1))}
	rows, days := fitFolded(all, func(d []day) (melaRows, bool) { return melaLines(c.f, c.kMax(), d, usable) })
	pitch, capH := rows.pitch, min(102, rows.capH)
	pcx := t.X + melaPlateW/2
	x0, x1 := t.X+melaPlateW+38, t.right()

	cys := make([]float64, len(days))
	tops := make([]float64, len(days))
	rowY := t.Y

	for i := range days {
		tops[i] = rowY
		cys[i] = rowY + pitch*rows.dayUnits[i]/2

		rowY += pitch*rows.dayUnits[i] + dayGap
	}

	step := closestStep(cys, t.H)

	c.Fill(rect(pcx-10, cys[0], 20, cys[len(days)-1]-cys[0]), st.pillarOut)
	c.Fill(rect(pcx-5.5, cys[0], 11, cys[len(days)-1]-cys[0]), st.pillarIn)

	for i, d := range days {
		cy := cys[i]
		blockTop := tops[i]
		blockH := pitch * rows.dayUnits[i]

		if len(d.slots) >= 2 && !d.off() {
			melaRichGroup(c, st, box{x0, blockTop, x1 - x0, blockH}, pcx, cy, capH)
		}

		switch {
		case d.off():
			melaRichOffRow(c, st, d, box{x0, cy - capH/2, x1 - x0, capH})
		case len(d.slots) == 0:
			c.Fill(rrect(x0, cy-capH/2, x1-x0, capH, capH/2), st.groupBG)
			c.StrokeButt(dash(rrect(x0, cy-capH/2, x1-x0, capH, capH/2), 9, 7), 2.4, st.groupStroke)
		default:
			before := 0.0

			for j, s := range d.slots {
				u := rows.slotUnits[i][j]
				sh := capH + pitch*(u-1)
				b := box{x0, blockTop + pitch*(before+u/2) - sh/2, x1 - x0, sh}
				melaRichSlot(c, st, s, j == 0, b, capH)

				before += u
			}
		}

		if st.talismanPlate {
			drawMelaTalismanDate(c, pcx, cy, min(1, (step-14)/92), d)
		} else {
			drawMelaDiamondDate(c, pcx, cy, min(capH*0.52, 46), (step-7)/2, d, st.diamondA, st.diamondB, st.diamondOut)
		}
	}

	c.textMid(txt{Size: 19, Face: faceBold, Track: 0.2}, updated, t.right()-4, h-24, alignR, st.footInk)
}

func melaRichGroup(c *canvas, st melaRichStyle, block box, pcx, cy, capH float64) {
	x0, x1 := block.X, block.right()
	blockTop, blockH := block.Y, block.H

	gb := box{x0 - 12, blockTop + 1, x1 - x0 + 16, blockH - 2}
	c.Fill(gb.rrect(24), st.groupBG)
	c.Stroke(gb.rrect(24), 1.8, st.groupStroke)

	bx := x0 - 20
	c.Stroke(line(pcx+42, cy, bx, cy), 4.0, st.bracketCol)
	c.Stroke(line(bx+8, blockTop+capH*0.42, bx, blockTop+capH*0.42, bx, blockTop+blockH-capH*0.42, bx+8, blockTop+blockH-capH*0.42), 3.4, st.bracketCol)
}

func melaRichOffRow(c *canvas, st melaRichStyle, d day, b box) {
	r := b.H / 2
	c.Fill(rrect(b.X+5, b.Y+6, b.W, b.H, r), st.capShadow)
	c.Fill(rrect(b.X, b.Y, b.W, b.H, r), st.capOutline)

	in := rrect(b.X+3.5, b.Y+3.5, b.W-7, b.H-7, r-3.5)
	c.Clip(in, 1, func(l *canvas) {
		l.Fill(in, st.offBG)
		seigaiha(l, b, 27, st.offBG, st.offWave, 1.7)
	})
	drawFlame(c, b.X+92, b.cy()-2, b.H*0.64, deg(-8), hex(0xFFFFFF), 2.6)
	drawMelaRichTalisman(c, b.right()-92, b.cy(), b.H*0.36, b.H*0.60, deg(10), hex(0xFFF2A6), hex(0xFFFFFF), hex(0xDE3D82))

	st1 := puffyStar(b.X+166, b.cy()-b.H*0.20, 10, 5, deg(12))
	c.Fill(outline(st1, 3.2, false), hex(0xFFFFFF))
	c.Fill(st1, hex(0xA9DCFF))

	st2 := puffyStar(b.right()-158, b.cy()+b.H*0.16, 12, 6, deg(-10))
	c.Fill(outline(st2, 3.2, false), hex(0xFFFFFF))
	c.Fill(st2, hex(0xFFF2A6))

	tT := txt{Size: min(30, b.H*0.42), Face: faceXBold, Track: 3}
	label := layoutOff(c.f, d.slots[0], tT, txt{Size: tT.Size * 0.72, Face: faceBold}, melaOffTextW(b))
	lw := label.width(c.f)
	tw, th := lw+56, tT.Size*1.6
	tm := place(b.cx(), b.cy(), 1, deg(-4))
	c.Fill(xf(rrect(-tw/2, -th/2, tw, th, 8), tm), hex(0xFFFFFF))
	c.Fill(xf(rrect(-tw/2+3.5, -th/2+3.5, tw-7, th-7, 5), tm), st.offPillBG)

	mainS, subS := label.shape(c, -lw/2, 0)
	c.Fill(xf(mainS, tm), st.offPillInk)
	c.Fill(xf(subS, tm), st.offPillInk)
}

func melaRichSlot(c *canvas, st melaRichStyle, s slot, first bool, b box, capH float64) {
	r := min(b.H/2, capH/2)
	c.Fill(rrect(b.X+5, b.Y+6, b.W, b.H, r), st.capShadow)
	c.Fill(rrect(b.X, b.Y, b.W, b.H, r), st.capOutline)

	bg := st.capFill

	if s.status == StatusCancelled {
		bg = st.capCancelFill
	}

	c.Fill(rrect(b.X+3.5, b.Y+3.5, b.W-7, b.H-7, max(4, r-3.5)), bg)
	c.Stroke(rrect(b.X+9.5, b.Y+9.5, b.W-19, b.H-19, max(4, r-9.5)), 1.8, st.capInset)

	if first {
		c.Fill(puffyStar(b.X+20, b.Y+4, 9.5, 4.8, deg(-12)), st.capOutline)
		c.Fill(puffyStar(b.X+20, b.Y+4, 6.5, 3.2, deg(-12)), hex(0xA9DCFF))
		drawFlame(c, b.right()-24, b.Y+4, min(42, capH*0.52), deg(7), st.capOutline, 2.8)
	} else {
		sp := sparkle(b.right()-26, b.Y+8, min(11, capH*0.14))
		c.Fill(outline(sp, 3.4, false), st.capOutline)
		c.Fill(sp, hex(0xFFA3D5))
	}

	rightReserve := 40.0

	if label, kind := timeLabel(s); kind != timeNone {
		rightReserve = melaRichTag(c, st, kind, label, b, capH)
	}

	tx := b.X + 34
	tb := melaTitle(c.f, s, b.right()-rightReserve-tx, b.H-18, min(c.kMax(), max(1, capH/84)))
	inkC := st.ink

	if s.status == StatusCancelled {
		inkC = st.muted
	}

	tb.draw(c, tx, b.cy()-tb.height()/2-1, inkC, st.sub)
}

func melaRichTag(c *canvas, st melaRichStyle, kind timeKind, label string, b box, capH float64) float64 {
	tagT, tagW := melaTagSize(c.f, capH, label)
	th := tagT.Size * 1.54
	tagCX := b.right() - tagW/2 - 14
	tagCY := b.bottom() - th/2 + 1
	tm := place(tagCX, tagCY, 1, deg(-3.0))
	fill, ink := st.tagFill, st.tagInk

	switch kind {
	case timeUnsure, timeCancelled:
		fill = hex(0xFFFFFF)

		if kind == timeCancelled {
			ink = st.muted
		}
	case timeChanged:
		fill, ink = st.changeFill, st.changeInk
	case timePlain, timeNone:
	}

	c.Fill(xf(rrect(-tagW/2+3, -th/2+4, tagW, th, 8), tm), st.capOutline)
	c.Fill(xf(rrect(-tagW/2, -th/2, tagW, th, 8), tm), st.capOutline)
	c.Fill(xf(rrect(-tagW/2+3.5, -th/2+3.5, tagW-7, th-7, 5), tm), fill)

	if kind == timeUnsure {
		in := tagT.Size * 0.23
		c.StrokeButt(dash(xf(rrect(-tagW/2+in, -th/2+in, tagW-2*in, th-2*in, 3), tm), 5, 4), 1.6, st.capOutline)
	}

	c.Fill(xf(c.textMidShape(tagT, label), tm), ink)

	if kind == timeCancelled {
		lw := c.f.width(tagT, label)
		c.Fill(xf(rrect(-lw/2-2, -1, lw+4, 2.4, 1.2), tm), ink)
	}

	return tagW + 30
}

func closestStep(cys []float64, limit float64) float64 {
	for i := 1; i < len(cys); i++ {
		limit = min(limit, cys[i]-cys[i-1])
	}

	return limit
}

func drawMelaDiamondDate(c *canvas, cx, cy, r, maxR float64, d day, colA, colB, outCol color.RGBA) {
	r = min(max(41, min(48, r+2)), maxR)
	c.Fill(xf(roundedDiamond(cx, cy, r+3.5, 11), move(4, 5)), rgba(0x0B1E54, 0.35))
	c.Fill(roundedDiamond(cx, cy, r+3.5, 11), outCol)
	c.FillLinear(roundedDiamond(cx, cy, r-2, 8), cx-r, cy-r, cx+r, cy+r, stop{0, colA}, stop{1, colB})
	c.Stroke(roundedDiamond(cx, cy, r-8.5, 5), 1.8, rgba(0xFFFFFF, 0.85))

	numS := xf(c.textMidShape(txt{Size: r * 0.74, Face: faceFat}, strconv.Itoa(d.date.Day())), move(cx-2, cy-2))
	c.Fill(outline(numS, 5.8, false), hex(0x1B3D92))
	c.Fill(numS, hex(0xFFFFFF))

	ts := r * 0.72
	tm := place(cx+r*0.76, cy+r*0.64, 1, deg(9))
	tagBG, tagFG := hex(0xFFF2A6), hex(0x1B3D92)

	switch d.date.Weekday() {
	case time.Saturday:
		tagBG, tagFG = hex(0x2F6FE0), hex(0xFFFFFF)
	case time.Sunday:
		tagBG, tagFG = hex(0xDE3D82), hex(0xFFFFFF)
	case time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday:
	}

	c.Fill(xf(rrect(-ts/2-3.5, -ts/2-3.5, ts+7, ts+7, 7), tm), outCol)
	c.Fill(xf(rrect(-ts/2, -ts/2, ts, ts, 4.5), tm), tagBG)
	c.Fill(xf(c.textMidShape(txt{Size: ts * 0.66, Face: faceXBold}, dowKO[weekday(d.date)]), tm), tagFG)
}

func drawMelaTalismanDate(c *canvas, cx, cy, s float64, d day) {
	navy := hex(0x1C144A)
	bg, bd, numCol, dowCol := hex(0x2D1F6E), hex(0x66EEFF), hex(0xFFFFFF), hex(0xFFF08A)

	switch d.date.Weekday() {
	case time.Saturday:
		bg, bd, numCol, dowCol = hex(0x164E8A), hex(0x66EEFF), hex(0x66EEFF), hex(0xFFFFFF)
	case time.Sunday:
		bg, bd, numCol, dowCol = hex(0x8E1E5C), hex(0xFF7AC6), hex(0xFFF08A), hex(0xFFD6EE)
	case time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday:
	}

	sh := rrect(cx-44*s, cy-46*s, 88*s, 92*s, 16*s)
	c.Fill(xf(sh, move(4*s, 5*s)), rgba(0x000000, 0.35))
	c.Fill(sh, bg)
	c.Stroke(sh, 3.0*s, bd)
	c.Fill(circle(cx, cy-36*s, 3.2*s), bd)

	numS := xf(c.textMidShape(txt{Size: 38 * s, Face: faceFat}, strconv.Itoa(d.date.Day())), move(cx, cy-8*s))
	c.Fill(outline(numS, 4.2*s, false), navy)
	c.Fill(numS, numCol)
	c.textMid(txt{Size: 25 * s, Face: faceXBold}, dowKO[weekday(d.date)], cx, cy+26*s, alignC, dowCol)
}

func drawMelaDarkTitle(c *canvas, v view, cx, y, maxW, maxSize float64, rimCol, shadowCol, pillBG, pillOut, pillFG color.RGBA) {
	nameT := c.f.fitSize(txt{Size: maxSize, Face: faceFat, Track: 2}, v.name, maxW-20, 34)
	ns := xf(c.textMidShape(nameT, c.f.clamp(nameT, v.name, maxW-20)), move(cx, y))
	c.Fill(outline(ns, 17, false), rimCol)
	c.Fill(outline(xf(ns, move(3, 5)), 11, false), shadowCol)
	c.Fill(outline(ns, 11, false), shadowCol)
	c.Fill(ns, hex(0xFFFFFF))

	perT := txt{Size: 24, Face: faceXBold, Track: 1}
	pw := c.f.width(perT, v.period) + 54
	ph := 42.0
	py := y + nameT.Size*0.56 + 38
	tm := place(cx, py, 1, deg(-2.5))
	c.Fill(xf(rrect(-pw/2+3, -ph/2+4, pw, ph, 10), tm), shadowCol)
	c.Fill(xf(rrect(-pw/2-3.5, -ph/2-3.5, pw+7, ph+7, 12), tm), pillOut)
	c.Fill(xf(rrect(-pw/2, -ph/2, pw, ph, 9), tm), pillBG)
	c.Fill(xf(c.textMidShape(perT, v.period), tm), pillFG)
}

func renderRichMelaA(c *canvas, v view) {
	p := melaColors
	w, h := c.w, c.h
	c.FillLinear(rect(0, 0, w, h), 0, 0, 0, h, stop{0, hex(0xA9EEF8)}, stop{1, hex(0x6ED3EE)})
	checker(c, box{0, 0, w, h}, 50, rgba(0xFFFFFF, 0.15))
	drawMelaMatoiSilhouette(c, 92, 180, rgba(0xFFFFFF, 0.22))
	c.Clip(rect(0, h-210, w, 210), 0.22, func(l *canvas) {
		seigaiha(l, box{0, h - 210, w, 210}, 34, hex(0x2A9FD8), hex(0xFFFFFF), 2.0)
	})
	c.Clip(rect(0, 0, w, 30), 1, func(l *canvas) {
		l.Fill(rect(0, 0, w, 30), hex(0x86E2DA))
		checker(l, box{0, 0, w, 30}, 15, rgba(0xFFFFFF, 0.55))
	})
	c.Fill(rect(0, 30, w, 3), rgba(0x1B3D92, 0.24))

	vb, tb := melaBoxes(c)
	drawMelaShimenawa(c, 0, vb.right()-295, 31, 18, p.navy, hex(0xF6FDFF), hex(0xB8E5F8))

	cloud1 := wafuCloudLine(vb.X+130, vb.Y+206, 220, 62)
	c.Fill(cloud1, rgba(0xFFFFFF, 0.36))
	c.Stroke(cloud1, 2.0, rgba(0x1B3D92, 0.18))

	cloud2 := wafuCloudLine(vb.X+520, vb.Y+92, 160, 48)
	c.Fill(cloud2, rgba(0xFFFFFF, 0.30))
	c.Stroke(cloud2, 1.8, rgba(0x1B3D92, 0.16))

	r := vb.H/2 - 26
	pcx := vb.right() - r - 30
	melaWindow(c, p, v.photo, pcx, vb.cy()+6, r)
	drawMelaRichTalisman(c, vb.X+52, vb.Y+108, 30, 50, deg(-12), hex(0xFFF2A6), p.navy, hex(0xDE3D82))

	nw := pcx - r - 60 - vb.X
	melaName(c, p, v, vb.X+nw/2+24, vb.cy()-18, nw-24, 100)

	for _, s := range [][3]float64{{pcx - r - 38, vb.bottom() - 28, 11}, {vb.X + nw*0.88, vb.Y + 58, 9}} {
		sp := sparkle(s[0], s[1], s[2])
		c.Fill(outline(sp, 4, false), p.white)
		c.Fill(sp, p.pink)
	}

	drawMelaRichTable(c, tb, v.days, melaRichStyle{
		capShadow: p.cyanD, capOutline: p.navy, capFill: p.white, capCancelFill: hex(0xEDF5FB), capInset: hex(0xBFE9F8),
		ink: p.navy, sub: p.sub, muted: hex(0x6479B8),
		tagFill: p.cream, tagInk: p.navy, changeFill: p.pink, changeInk: p.navy,
		groupBG: rgba(0xFFFFFF, 0.28), groupStroke: rgba(0x1B3D92, 0.28), bracketCol: p.navy,
		pillarOut: p.navy, pillarIn: p.teal,
		offBG: p.navy, offWave: hex(0x345BBA), offPillBG: hex(0xA9DCFF), offPillInk: p.navy,
		diamondA: hex(0x52D8C8), diamondB: hex(0x27B5A4), diamondOut: p.navy,
		footInk: p.navyD,
	}, v.updated, h)
}

func renderRichMelaB(c *canvas, v view) {
	w, h := c.w, c.h
	c.FillLinear(rect(0, 0, w, h), 0, 0, 0, h, stop{0, hex(0x132663)}, stop{1, hex(0x091334)})
	c.Clip(rect(0, 0, w, h), 0.14, func(l *canvas) {
		seigaiha(l, box{0, 0, w, h}, 40, hex(0x0D1E52), hex(0x5EE9F6), 1.6)
	})

	c.Fill(rect(0, 0, 18, h), hex(0x0D1B4A))
	c.Fill(rect(18, 0, 3, h), hex(0xF0CA73))
	c.Glow(190, 175, 320, 0x3FD6F5, 0.34)
	c.Glow(w-180, 160, 290, 0xFFF08A, 0.18)
	c.Glow(160, h*0.68, 340, 0x22B8A8, 0.18)

	c.Clip(rect(0, 0, w, 28), 0.9, func(l *canvas) {
		l.Fill(rect(0, 0, w, 28), hex(0x1B5488))
		checker(l, box{0, 0, w, 28}, 14, rgba(0xF0CA73, 0.45))
	})
	c.Fill(rect(0, 28, w, 3), hex(0xF0CA73))

	vb, tb := melaBoxes(c)

	drawMelaShimenawa(c, vb.X+285, w, 29, 18, hex(0x091334), hex(0xFFF3B2), hex(0xD6B456))
	c.Stroke(wafuCloudLine(vb.right()-210, vb.Y+206, 220, 56), 2.0, rgba(0xF0CA73, 0.32))

	r := vb.H/2 - 26
	pcx := vb.X + r + 32
	c.Fill(circle(pcx, vb.cy()+6, r+13), hex(0xF0CA73))
	c.Fill(circle(pcx, vb.cy()+6, r+6), hex(0xFFFFFF))
	c.Medallion(v.photo, pcx, vb.cy()+6, r, 0, 0x62E2F5, 0.48)
	drawFlame(c, pcx+r*0.86, vb.cy()+6-r*0.66, r*0.66, deg(14), hex(0x091334), 4)
	drawFlame(c, pcx-r*0.86, vb.cy()+6+r*0.62, r*0.48, deg(-12), hex(0x091334), 3.5)

	nx := pcx + r + 42
	nw := vb.right() - nx - 58
	drawMelaDarkTitle(c, v, nx+nw/2, vb.cy()-18, nw, 100, rgba(0x5EE9F6, 0.52), hex(0x091334), hex(0xFFF18E), hex(0xF0CA73), hex(0x0F2054))

	sx, sy := vb.right()-38, vb.cy()-16
	sm := place(sx, sy, 1, deg(-4.5))
	c.Fill(xf(rrect(-26, -26, 52, 52, 7), sm), hex(0xD63558))
	c.Stroke(xf(rrect(-21, -21, 42, 42, 4.5), sm), 2.0, hex(0xFFF3B2))
	c.Fill(xf(c.textMidShape(txt{Size: 27, Face: faceFat}, "火"), sm), hex(0xFFF3B2))

	for _, s := range [][3]float64{{vb.right() - 48, vb.bottom() - 32, 11}, {nx + 18, vb.Y + 84, 10}} {
		sp := sparkle(s[0], s[1], s[2])
		c.Fill(outline(sp, 3.6, false), hex(0xFFFFFF))
		c.Fill(sp, hex(0xFF72BA))
	}

	drawMelaRichTable(c, tb, v.days, melaRichStyle{
		capShadow: hex(0x050B24), capOutline: hex(0x5EE9F6), capFill: hex(0x162E70), capCancelFill: hex(0x112254), capInset: rgba(0xF0CA73, 0.34),
		ink: hex(0xF6FDFF), sub: hex(0xA9C8EC), muted: hex(0x7B96C4),
		tagFill: hex(0xFFF18E), tagInk: hex(0x0F2054), changeFill: hex(0xFF72BA), changeInk: hex(0x0F2054),
		groupBG: rgba(0x5EE9F6, 0.12), groupStroke: rgba(0xF0CA73, 0.42), bracketCol: hex(0xF0CA73),
		pillarOut: hex(0xF0CA73), pillarIn: hex(0x1D4FA8),
		offBG: hex(0x0F2054), offWave: hex(0x2D5CC4), offPillBG: hex(0x5EE9F6), offPillInk: hex(0x0F2054),
		diamondA: hex(0x2D76D6), diamondB: hex(0x19479C), diamondOut: hex(0xF0CA73),
		footInk: hex(0xA9C8EC),
	}, v.updated, h)
}

func renderRichMelaC(c *canvas, v view) {
	w, h := c.w, c.h
	c.FillLinear(rect(0, 0, w, h), 0, 0, w, h, stop{0, hex(0x3B237A)}, stop{0.48, hex(0x282975)}, stop{0.52, hex(0x1E7CA8)}, stop{1, hex(0x5CE0EB)})
	checker(c, box{0, 0, w, h}, 44, rgba(0xFFFFFF, 0.10))

	board := box{22, 22, w - 44, h - 44}
	c.Shadow(board.rrect(30), 0, 8, 16, rgba(0x0E0B28, 0.45))
	c.Fill(board.rrect(30), hex(0x18153E))
	c.Stroke(board.rrect(30), 4.0, hex(0x66EEFF))
	c.Stroke(board.inset(6).rrect(25), 1.6, rgba(0xFF72BA, 0.55))

	c.Fill(circle(board.cx(), board.Y+16, 6.5), hex(0xFF72BA))
	c.Stroke(line(board.cx()-110, board.Y, board.cx(), board.Y+16, board.cx()+110, board.Y), 3.4, hex(0xFF72BA))

	vb := box{board.X + 20, board.Y + 24, board.W - 40, 272}
	tb := box{board.X + 20, vb.bottom() + 18, board.W - 40, board.bottom() - 28 - vb.bottom() - 18}

	c.Stroke(wafuCloudLine(vb.X+220, vb.Y+60, 240, 56), 1.8, rgba(0xFF72BA, 0.36))
	c.Stroke(wafuCloudLine(vb.X+470, vb.bottom()-44, 190, 44), 1.6, rgba(0x66EEFF, 0.36))

	r := vb.H/2 - 26
	pcx := vb.right() - r - 28
	c.FillLinear(circle(pcx, vb.cy(), r+13), pcx-r, vb.cy(), pcx+r, vb.cy(), stop{0, hex(0xFF72BA)}, stop{0.5, hex(0x946BFF)}, stop{1, hex(0x66EEFF)})
	c.Fill(circle(pcx, vb.cy(), r+5), hex(0xFFFFFF))
	c.Medallion(v.photo, pcx, vb.cy(), r, 0, 0x8CEAF6, 0.45)
	drawFlame(c, pcx-r*0.86, vb.cy()-r*0.64, r*0.62, deg(-14), hex(0x18153E), 3.5)

	nw := pcx - r - 56 - vb.X
	drawMelaDarkTitle(c, v, vb.X+nw/2+12, vb.cy()-18, nw, 96, rgba(0xFF72BA, 0.56), hex(0x18153E), hex(0x66EEFF), hex(0xFF72BA), hex(0x18153E))

	drawMelaRichTable(c, tb, v.days, melaRichStyle{
		capShadow: hex(0x0B0924), capOutline: hex(0x9C7CFF), capFill: hex(0x252059), capCancelFill: hex(0x1D1947), capInset: rgba(0x66EEFF, 0.30),
		ink: hex(0xFAF8FF), sub: hex(0xC5C2F0), muted: hex(0x8D8AB8),
		tagFill: hex(0x66EEFF), tagInk: hex(0x18153E), changeFill: hex(0xFF72BA), changeInk: hex(0x18153E),
		groupBG: rgba(0xFF72BA, 0.14), groupStroke: rgba(0xFF72BA, 0.42), bracketCol: hex(0xFF72BA),
		pillarOut: hex(0xFF72BA), pillarIn: hex(0x66EEFF),
		offBG: hex(0x2A1A59), offWave: hex(0x523AA0), offPillBG: hex(0xFF72BA), offPillInk: hex(0x18153E),
		footInk:       hex(0xC5C2F0),
		talismanPlate: true,
	}, v.updated, h-16)
}
