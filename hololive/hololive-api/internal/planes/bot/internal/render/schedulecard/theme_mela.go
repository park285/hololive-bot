package schedulecard

import (
	"image"
	"image/color"
	"math"
	"slices"
	"strconv"
)

type melaPal struct {
	navy, navyD, teal, cyanD, cream, pink, sub, white color.RGBA
}

var melaColors = melaPal{
	navy: hex(0x1B3D92), navyD: hex(0x15307A), teal: hex(0x27B5A4), cyanD: hex(0x2A9FD8),
	cream: hex(0xFFF2A6), pink: hex(0xFFA3D5), sub: hex(0x3F5FB0), white: hex(0xFFFFFF),
}

func renderMela(c *canvas, v view) {
	p := melaColors
	w, h := c.w, c.h
	c.FillLinear(rect(0, 0, w, h), 0, 0, 0, h, stop{0, hex(0xA9EEF8)}, stop{1, hex(0x6ED3EE)})
	checker(c, box{0, 0, w, h}, 50, rgba(0xFFFFFF, 0.15))
	c.Clip(rect(0, 0, w, 30), 1, func(l *canvas) {
		l.Fill(rect(0, 0, w, 30), hex(0x86E2DA))
		checker(l, box{0, 0, w, 30}, 15, rgba(0xFFFFFF, 0.55))
	})
	c.Fill(rect(0, 30, w, 3), rgba(0x1B3D92, 0.20))

	vb, tb := melaBoxes(c)

	if c.tall() {
		melaBand(c, p, vb, v)
	} else {
		melaColumn(c, p, vb, v)
	}

	melaTable(c, p, tb, v.days)
	c.textMid(txt{Size: 19, Face: faceBold, Track: 0.2}, v.updated, tb.right()-4, h-27, alignR, p.navyD)
}

func melaBoxes(c *canvas) (vb, tb box) {
	w, h := c.w, c.h

	if c.tall() {
		vb = box{28, 46, w - 56, 290}

		return vb, box{28, vb.bottom() + 20, w - 56, h - 56 - vb.bottom() - 20}
	}

	vb = box{w - 28 - 372, 48, 372, h - 48 - 28}

	return vb, box{28, 52, vb.X - 36 - 28, h - 52 - 56}
}

func melaColumn(c *canvas, p melaPal, vb box, v view) {
	r := min(vb.W/2-34, 150)
	pcy := vb.Y + 24 + r
	melaWindow(c, p, v.photo, vb.cx(), pcy, r)

	ny := pcy + r + 74
	melaName(c, p, v, vb.cx(), ny, vb.W-6, 72)

	top := ny + 126
	melaWaves(c, p, box{vb.X + 6, top, vb.W - 12, vb.bottom() - top})
}

func melaBand(c *canvas, p melaPal, vb box, v view) {
	r := vb.H/2 - 26
	pcx := vb.right() - r - 34
	melaWindow(c, p, v.photo, pcx, vb.cy(), r)

	nw := pcx - r - 60 - vb.X
	melaName(c, p, v, vb.X+nw/2+8, vb.cy()-34, nw, 104)

	for _, s := range [][3]float64{{vb.X + 30, vb.Y + 34, 11}, {pcx - r - 34, vb.bottom() - 30, 10}} {
		sp := sparkle(s[0], s[1], s[2])
		c.Fill(outline(sp, 4, false), p.white)
		c.Fill(sp, p.pink)
	}
}

func melaWindow(c *canvas, p melaPal, photo image.Image, cx, cy, r float64) {
	c.Fill(circle(cx+7, cy+8, r+12), p.cyanD)
	c.Fill(circle(cx, cy, r+12), p.navy)
	c.Fill(circle(cx, cy, r+6), p.white)
	c.Medallion(photo, cx, cy, r, 0, 0xB4F0F8, 0.62)

	drawFlame(c, cx-r*0.88, cy-r*0.66, r*0.64, deg(-14), p.navy, 4)
	drawFlame(c, cx+r*0.92, cy+r*0.62, r*0.46, deg(12), p.navy, 3.5)

	sp := sparkle(cx+r*0.94, cy-r*0.80, r*0.09)
	c.Fill(outline(sp, 4, false), p.white)
	c.Fill(sp, p.pink)
}

func melaName(c *canvas, p melaPal, v view, cx, cy, maxW, maxSize float64) {
	const tilt = -3.5

	nameT := c.f.fitSize(txt{Size: maxSize, Face: faceFat, Track: 1}, v.name, maxW-26, 36)
	ns := xf(c.textMidShape(nameT, c.f.clamp(nameT, v.name, maxW-26)), place(cx, cy, 1, deg(tilt)))
	no := outline(ns, max(13, nameT.Size*0.25), false)
	c.Fill(xf(no, move(5, 6)), p.cyanD)
	c.Fill(no, p.navy)
	c.Fill(ns, p.white)

	perT := txt{Size: 26, Face: faceXBold, Track: 0.8}
	pw := c.f.width(perT, v.period) + 50
	pm := place(cx+8, cy+nameT.Size*0.5+46, 1, deg(tilt))
	c.Fill(xf(rrect(-pw/2+4, -23+5, pw, 46, 8), pm), p.navyD)
	c.Fill(xf(rrect(-pw/2, -23, pw, 46, 8), pm), p.navy)
	c.Fill(xf(rrect(-pw/2+3.5, -19.5, pw-7, 39, 5), pm), p.cream)
	c.Fill(xf(c.textMidShape(perT, v.period), pm), p.navy)
}

func melaWaves(c *canvas, p melaPal, b box) {
	if b.H < 120 {
		return
	}

	wh := min(b.H*0.5, 170)
	wb := box{b.X, b.bottom() - wh, b.W, wh}
	// 가운데 불꽃 끝이 위쪽 기한 딱지를 덮지 않게 높이를 묶는다.
	fh := min((b.H-wh-6)/0.88, 250)

	drawFlame(c, b.cx()-b.W*0.30, wb.Y-fh*0.12, fh*0.62, deg(-10), p.navy, 4)
	drawFlame(c, b.cx()+b.W*0.31, wb.Y-fh*0.16, fh*0.70, deg(9), p.navy, 4)
	drawFlame(c, b.cx(), wb.Y-fh*0.30, fh, 0, p.navy, 5)

	c.Fill(box{wb.X + 5, wb.Y + 6, wb.W, wb.H}.rrect(26), p.cyanD)
	c.Fill(wb.rrect(26), p.navyD)

	in := wb.inset(4).rrect(22)
	c.Clip(in, 1, func(l *canvas) {
		l.Fill(in, p.navy)
		seigaiha(l, wb, 30, p.navy, hex(0x3F66C4), 1.8)
	})

	for _, s := range [][2]float64{{0.16, 0.36}, {0.84, 0.30}, {0.60, 0.74}} {
		c.Fill(puffyStar(wb.X+wb.W*s[0], wb.Y+wb.H*s[1], 11, 5.5, deg(10)), hex(0x8FC2FA))
	}
}

const melaPlateW = 124.0

// melaMinCapH는 가장 작은 한 줄 제목(0.8배) 글자가 캡슐 흰 바탕 안에 드는 높이다.
const melaMinCapH = 40.0

const melaMinK = 0.8

const melaFitRounds = 6

// melaRows의 높이 단위는 줄 간격 pitch다. 제목이 기본 캡슐(capH)에 들지 않는 칸만 1보다 큰 단위를 받아 캡슐이 세로로 늘고,
// 그만큼 나머지 칸의 줄 간격이 준다. 휴방·빈 날은 slotUnits가 nil이다.
type melaRows struct {
	days      []day
	dayUnits  []float64
	slotUnits [][]float64
	pitch     float64
	capH      float64
}

func melaLines(f *fontSet, kMax float64, days []day, t box) (melaRows, bool) {
	r := melaRows{days: days, dayUnits: make([]float64, len(days)), slotUnits: make([][]float64, len(days))}

	for i, d := range days {
		r.dayUnits[i] = float64(max(1, len(d.slots)))

		if !d.off() {
			r.slotUnits[i] = slices.Repeat([]float64{1}, len(d.slots))
		}
	}

	r.measure(t)

	if r.capH < melaMinCapH {
		return r, false
	}

	for range melaFitRounds {
		next := r.clone()
		if !next.grow(f, kMax, t) {
			break
		}

		next.measure(t)

		if next.capH < melaMinCapH {
			break
		}

		r = next
	}

	return r, true
}

func (r *melaRows) clone() melaRows {
	c := *r

	c.dayUnits = slices.Clone(r.dayUnits)
	c.slotUnits = slices.Clone(r.slotUnits)

	for i := range c.slotUnits {
		c.slotUnits[i] = slices.Clone(c.slotUnits[i])
	}

	return c
}

func (r *melaRows) measure(t box) {
	total := 0.0

	for _, u := range r.dayUnits {
		total += u
	}

	r.pitch = t.H / total
	r.capH = min(104, r.pitch-16)
}

func (r *melaRows) grow(f *fontSet, kMax float64, t box) bool {
	grew := false

	for i, units := range r.slotUnits {
		for j, s := range r.days[i].slots[:len(units)] {
			if u := melaSlotUnits(f, kMax, s, t, r.pitch, r.capH); u > units[j] {
				r.dayUnits[i] += u - units[j]

				units[j] = u
				grew = true
			}
		}
	}

	return grew
}

// melaSlotUnits는 기본 캡슐에 들지 않는 제목을 기본 글자 크기(1배) 이상으로 다 보이는 가장 낮은 캡슐 높이를 찾아 단위로 바꾼다.
// 기본 캡슐의 세 배로도 들지 않으면 늘리지 않고 melaTitle이 줄여 그린다.
func melaSlotUnits(f *fontSet, kMax float64, s slot, t box, pitch, capH float64) float64 {
	kTop := min(kMax, max(1, capH/84))
	if melaFits(f, s, t, capH, capH, kTop, melaMinK) {
		return 1
	}

	for h := capH + 4; h <= 3*capH; h += 4 {
		if melaFits(f, s, t, h, capH, kTop, min(1, kTop)) {
			return 1 + math.Ceil((h-capH)/pitch*8)/8
		}
	}

	return 1
}

func melaFits(f *fontSet, s slot, t box, h, capH, kTop, kMin float64) bool {
	b := box{t.X + melaPlateW + 34, 0, melaCapsuleW(t), h}
	w := b.right() - 66 - melaTitleX(f, b, capH, s)

	if _, ok := melaFitTitle(f, s, w, h-16, kTop, kMin, true); ok {
		return true
	}

	if len(s.tags) == 0 {
		return false
	}

	_, ok := melaFitTitle(f, s, w, h-16, kTop, kMin, false)

	return ok
}

func melaCapsuleW(t box) float64 { return t.right() - (t.X + melaPlateW + 34) }

func melaTable(c *canvas, p melaPal, t box, all []day) {
	const plateW = melaPlateW

	rows, days := fitFolded(all, func(d []day) (melaRows, bool) { return melaLines(c.f, c.kMax(), d, t) })
	pitch, capH := rows.pitch, rows.capH
	pcx := t.X + plateW/2
	x0, x1 := t.X+plateW+34, t.right()
	cys := make([]float64, len(days))
	rowY := t.Y

	for i := range days {
		cys[i] = rowY + pitch*rows.dayUnits[i]/2

		rowY += pitch * rows.dayUnits[i]
	}

	c.Fill(rect(pcx-11, cys[0], 22, cys[len(days)-1]-cys[0]), p.navy)
	c.Fill(rect(pcx-6.5, cys[0], 13, cys[len(days)-1]-cys[0]), p.teal)

	for i, d := range days {
		cy := cys[i]
		blockTop := cy - pitch*rows.dayUnits[i]/2

		switch {
		case d.off():
			melaOffBar(c, p, box{x0, cy - capH/2, x1 - x0, capH}, d.slots[0])
		case len(d.slots) == 0:
			c.Fill(rrect(x0, cy-capH/2, x1-x0, capH, capH/2), rgba(0xFFFFFF, 0.35))
			c.StrokeButt(dash(rrect(x0, cy-capH/2, x1-x0, capH, capH/2), 9, 7), 2.4, rgba(0x1B3D92, 0.45))
		default:
			before := 0.0

			for j, s := range d.slots {
				u := rows.slotUnits[i][j]
				h := capH + pitch*(u-1)

				melaCapsule(c, p, box{x0, blockTop + pitch*(before+u/2) - h/2, x1 - x0, h}, capH, s)

				before += u
			}
		}

		melaPlate(c, p, pcx, cy, plateW, min(capH, 84), d)
	}
}

func melaPlate(c *canvas, p melaPal, cx, cy, w, h float64, d day) {
	hex6 := func(in float64) shape {
		hw, hh, sl := w/2-in, h/2-in, h*0.36

		return poly(cx-hw, cy, cx-hw+sl, cy-hh, cx+hw-sl, cy-hh, cx+hw, cy, cx+hw-sl, cy+hh, cx-hw+sl, cy+hh)
	}

	c.Fill(xf(hex6(-3), move(4, 5)), p.cyanD)
	c.Fill(hex6(-3), p.navy)
	c.Fill(hex6(3), p.teal)
	c.Stroke(hex6(10), 1.8, rgba(0xFFFFFF, 0.85))

	numT := txt{Size: h * 0.56, Face: faceFat}
	num := xf(c.textMidShape(numT, strconv.Itoa(d.date.Day())), move(cx-w*0.13, cy-1))
	c.Fill(outline(num, 7, false), p.navy)
	c.Fill(num, p.white)

	ts := h * 0.40
	tm := place(cx+w*0.27, cy+h*0.14, 1, deg(8))
	c.Fill(xf(rrect(-ts/2-3.5, -ts/2-3.5, ts+7, ts+7, 7), tm), p.navy)
	c.Fill(xf(rrect(-ts/2, -ts/2, ts, ts, 4.5), tm), p.cream)
	c.Fill(xf(c.textMidShape(txt{Size: ts * 0.68, Face: faceXBold}, dowKO[weekday(d.date)]), tm), p.navy)
}

func melaStyle(k float64) (mainT, subT txt) {
	return txt{Size: 37 * k, Face: faceSign}, txt{Size: 26 * k, Face: faceBold}
}

func melaFitTitle(f *fontSet, s slot, w, maxH, kTop, kMin float64, withTags bool) (titleBlock, bool) {
	lim := titleLimits{maxW: w, maxMain: 2, maxSub: 2, withTags: withTags}
	baseMain, baseSub := melaStyle(1)
	base := layoutTitle(f, s, baseMain, baseSub, lim)

	for k := kTop; k >= kMin-0.001; k -= 0.04 {
		mainT, subT := melaStyle(k)
		tb := layoutTitle(f, s, mainT, subT, lim)
		grew := k > 1 && len(tb.main) > len(base.main)

		if tb.height() <= maxH && !tb.cut && !grew {
			return tb, true
		}
	}

	return titleBlock{}, false
}

func melaTitle(f *fontSet, s slot, w, maxH, kTop float64) titleBlock {
	if tb, ok := melaFitTitle(f, s, w, maxH, kTop, melaMinK, true); ok {
		return tb
	}

	if len(s.tags) > 0 {
		if tb, ok := melaFitTitle(f, s, w, maxH, kTop, melaMinK, false); ok {
			return tb
		}
	}

	mainT, subT := melaStyle(melaMinK)
	tb := newTitleBlock(s, mainT, subT)

	tb.main = []string{f.clamp(mainT, s.main, w)}

	if rest := w - f.width(mainT, s.main) - mainT.Size*0.35; s.sub != "" && rest > 0 {
		if sub := f.clamp(subT, s.sub, rest); len([]rune(sub)) > 4 {
			tb.inline = sub
		}
	}

	tb.placeChips(f, s.tags, w)

	if tb.tagRow {
		tb.tags, tb.tagLH = nil, 0
	}

	return tb
}

func melaCapsule(c *canvas, p melaPal, b box, capH float64, s slot) {
	r := b.H / 2
	c.Fill(rrect(b.X+5, b.Y+6, b.W, b.H, r), p.cyanD)
	c.Fill(rrect(b.X, b.Y, b.W, b.H, r), p.navy)
	c.Fill(rrect(b.X+3.5, b.Y+3.5, b.W-7, b.H-7, r-3.5), p.white)
	c.Stroke(rrect(b.X+9.5, b.Y+9.5, b.W-19, b.H-19, r-9.5), 1.8, hex(0xBFE9F8))
	drawFlame(c, b.right()-38, b.Y+3, min(46, capH*0.58), deg(7), p.navy, 3)

	if label, kind := timeLabel(s); kind != timeNone {
		melaTag(c, p, b, capH, label, kind)
	}

	tx := melaTitleX(c.f, b, capH, s)
	tb := melaTitle(c.f, s, b.right()-66-tx, b.H-16, min(c.kMax(), max(1, capH/84)))
	tb.draw(c, tx, b.cy()-tb.height()/2-1, p.navy, p.sub)
}

func melaTagSize(f *fontSet, capH float64, label string) (tagT txt, tagW float64) {
	tagT = txt{Size: min(30, capH*0.38), Face: faceXBold, Track: 0.4}

	return tagT, f.width(tagT, label) + tagT.Size*1.15
}

func melaTitleX(f *fontSet, b box, capH float64, s slot) float64 {
	label, kind := timeLabel(s)
	if kind == timeNone {
		return b.X + b.H/2
	}

	_, tagW := melaTagSize(f, capH, label)

	return b.X - 10 + tagW + 18
}

func melaTag(c *canvas, p melaPal, b box, capH float64, label string, kind timeKind) {
	tagT, tagW := melaTagSize(c.f, capH, label)
	th := tagT.Size * 1.56
	tm := place(b.X-10+tagW/2, b.cy()-1, 1, deg(-3))
	fill, ink := p.cream, p.navy

	switch kind {
	case timeUnsure, timeCancelled:
		fill = p.white
	case timeChanged:
		fill = p.pink
	case timePlain, timeNone:
	}

	if kind == timeCancelled {
		ink = p.sub
	}

	c.Fill(xf(rrect(-tagW/2+3, -th/2+4, tagW, th, 8), tm), p.navyD)
	c.Fill(xf(rrect(-tagW/2, -th/2, tagW, th, 8), tm), p.navy)
	c.Fill(xf(rrect(-tagW/2+3.5, -th/2+3.5, tagW-7, th-7, 5), tm), fill)

	if kind == timeUnsure {
		in := tagT.Size * 0.23
		c.StrokeButt(dash(xf(rrect(-tagW/2+in, -th/2+in, tagW-2*in, th-2*in, 3), tm), 5, 4), 1.6, p.navy)
	}

	c.Fill(xf(c.textMidShape(tagT, label), tm), ink)

	if kind == timeCancelled {
		lw := c.f.width(tagT, label)
		c.Fill(xf(rrect(-lw/2-2, -1, lw+4, 2.2, 1.1), tm), ink)
	}
}

func melaOffBar(c *canvas, p melaPal, b box, s slot) {
	r := b.H / 2
	c.Fill(rrect(b.X+5, b.Y+6, b.W, b.H, r), p.cyanD)
	c.Fill(rrect(b.X, b.Y, b.W, b.H, r), p.navyD)

	in := rrect(b.X+3.5, b.Y+3.5, b.W-7, b.H-7, r-3.5)
	c.Clip(in, 1, func(l *canvas) {
		l.Fill(in, p.navy)
		seigaiha(l, b, 27, p.navy, hex(0x345BBA), 1.7)
	})

	for _, f := range [][2]float64{{b.X + 104, -8}, {b.right() - 104, 8}} {
		drawFlame(c, f[0], b.cy()-2, b.H*0.62, deg(f[1]), p.white, 2.6)
	}

	tT := txt{Size: min(30, b.H*0.42), Face: faceXBold, Track: 3}
	label := layoutOff(c.f, s, tT, txt{Size: tT.Size * 0.72, Face: faceBold}, melaOffTextW(b))
	lw := label.width(c.f)
	tw := lw + 56
	th := tT.Size * 1.6
	tm := place(b.cx(), b.cy(), 1, deg(-4))
	c.Fill(xf(rrect(-tw/2, -th/2, tw, th, 8), tm), p.white)
	c.Fill(xf(rrect(-tw/2+3.5, -th/2+3.5, tw-7, th-7, 5), tm), hex(0xA9DCFF))

	mainS, subS := label.shape(c, -lw/2, 0)
	c.Fill(xf(mainS, tm), p.navy)
	c.Fill(xf(subS, tm), p.sub)
}

func melaOffTextW(b box) float64 { return b.W - 264 - b.H*0.62 }
