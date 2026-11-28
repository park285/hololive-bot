package schedulecard

import (
	"image/color"
	"time"
	"unicode/utf8"
)

func (c *canvas) tall() bool { return c.h > c.w }

func (c *canvas) kMax() float64 {
	if c.tall() {
		return 1.24
	}

	return 1.12
}

type box struct{ X, Y, W, H float64 }

func (b box) cx() float64     { return b.X + b.W/2 }
func (b box) cy() float64     { return b.Y + b.H/2 }
func (b box) right() float64  { return b.X + b.W }
func (b box) bottom() float64 { return b.Y + b.H }
func (b box) rect() shape     { return rect(b.X, b.Y, b.W, b.H) }

func (b box) rrect(r float64) shape { return rrect(b.X, b.Y, b.W, b.H, r) }

func (b box) inset(d float64) box { return box{b.X + d, b.Y + d, b.W - 2*d, b.H - 2*d} }

var (
	dowEN = [...]string{"SUN", "MON", "TUE", "WED", "THU", "FRI", "SAT"}
	dowKO = [...]string{"일", "월", "화", "수", "목", "금", "토"}
)

func weekday(t time.Time) int { return int(t.Weekday()) }

type timeKind uint8

const (
	timePlain timeKind = iota
	timeUnsure
	timeCancelled
	timeNone
	timeChanged
)

func timeLabel(s slot) (string, timeKind) {
	switch {
	case s.more > 0, s.status == StatusOff:
		return "", timeNone
	case s.status == StatusCancelled && s.start == "":
		return "", timeNone
	case s.status == StatusCancelled:
		return s.start, timeCancelled
	}

	now := s.start

	switch {
	case now == "":
		now = "미정"
	case s.status == StatusTentative:
		now += " ?"
	}

	switch {
	case s.from != "":
		return s.from + " → " + now, timeChanged
	case s.start == "", s.status == StatusTentative:
		return now, timeUnsure
	}

	return now, timePlain
}

type titleBlock struct {
	main   []string
	inline string
	sub    []string
	tags   []string
	mainT  txt
	subT   txt
	tagT   txt
	mainLH float64
	subLH  float64
	tagLH  float64
	tagX   float64
	tagRow bool
	struck bool
	muted  bool
	marked bool
	cut    bool
}

type titleLimits struct {
	maxW            float64
	maxMain, maxSub int
	withTags        bool
}

func newTitleBlock(s slot, mainT, subT txt) titleBlock {
	return titleBlock{
		mainT: mainT, subT: subT, tagT: subT.scaled(0.8),
		mainLH: mainT.Size * 1.24, subLH: subT.Size * 1.32,
		struck: s.status == StatusCancelled,
		muted:  s.status == StatusCancelled || s.more > 0,
		marked: s.highlight,
	}
}

func layoutTitle(f *fontSet, s slot, mainT, subT txt, lim titleLimits) titleBlock {
	b := newTitleBlock(s, mainT, subT)

	if s.sub != "" && f.width(mainT, s.main)+mainT.Size*0.35+f.width(subT, s.sub) <= lim.maxW {
		b.main, b.inline = []string{s.main}, s.sub
	} else {
		b.main, b.cut = f.wrap(mainT, s.main, lim.maxW, lim.maxMain)

		if s.sub != "" {
			var cut bool

			b.sub, cut = f.wrap(subT, s.sub, lim.maxW, lim.maxSub)
			b.cut = b.cut || cut
		}
	}

	if lim.withTags {
		b.placeChips(f, s.tags, lim.maxW)
	}

	return b
}

func (b *titleBlock) lastLineWidth(f *fontSet) float64 {
	switch {
	case len(b.sub) > 0:
		return f.width(b.subT, b.sub[len(b.sub)-1])
	case len(b.main) == 0:
		return 0
	case b.inline != "":
		return f.width(b.mainT, b.main[len(b.main)-1]) + b.mainT.Size*0.35 + f.width(b.subT, b.inline)
	}

	return f.width(b.mainT, b.main[len(b.main)-1])
}

func (b *titleBlock) placeChips(f *fontSet, tags []string, maxW float64) {
	if len(tags) == 0 {
		return
	}

	lead := b.lastLineWidth(f) + b.mainT.Size*0.35
	if inline := fitChips(f, b.tagT, tags, maxW-lead); len(inline) == len(tags) {
		b.tags, b.tagX = inline, lead

		return
	}

	b.tags, b.tagX, b.tagRow = fitChips(f, b.tagT, tags, maxW), 0, true
	if len(b.tags) > 0 {
		b.tagLH = b.tagT.Size*1.5 + b.subT.Size*0.3
	}
}

func chipPad(t txt) float64 { return t.Size * 0.5 }
func chipGap(t txt) float64 { return t.Size * 0.4 }

func fitChips(f *fontSet, t txt, tags []string, maxW float64) []string {
	var out []string

	x := 0.0

	for _, tag := range tags {
		w := f.width(t, tag) + 2*chipPad(t)
		if x+w > maxW {
			break
		}

		out = append(out, tag)

		x += w + chipGap(t)
	}

	return out
}

func (b *titleBlock) height() float64 {
	return float64(len(b.main))*b.mainLH + float64(len(b.sub))*b.subLH + b.tagLH
}

func (b *titleBlock) draw(c *canvas, x, top float64, mainCol, subCol color.RGBA) {
	y, lastCY := top, top
	mainMid, subMid := c.f.mid(b.mainT), c.f.mid(b.subT)

	if b.muted {
		mainCol = subCol
	}

	for i, l := range b.main {
		lastCY = y + b.mainLH/2

		base := lastCY + mainMid
		lw := c.f.width(b.mainT, l)
		inline := i == len(b.main)-1 && b.inline != ""

		if b.marked {
			markW := lw

			if inline {
				markW += b.mainT.Size*0.35 + c.f.width(b.subT, b.inline)
			}

			c.Fill(markBand(x, base, markW, b.mainT), markInk)
		}

		c.text(b.mainT, l, x, base, alignL, mainCol)

		if inline {
			c.text(b.subT, b.inline, x+lw+b.mainT.Size*0.35, base-mainMid+subMid, alignL, subCol)
		}

		if b.struck {
			strike(c, x, lastCY, lw, b.mainT, mainCol)
		}

		y += b.mainLH
	}

	for _, l := range b.sub {
		lastCY = y + b.subLH/2
		c.text(b.subT, l, x, lastCY+subMid-1, alignL, subCol)

		y += b.subLH
	}

	switch {
	case len(b.tags) == 0:
	case b.tagRow:
		drawChips(c, b.tagT, b.tags, x, y+b.tagLH-b.tagT.Size*0.75, subCol)
	default:
		drawChips(c, b.tagT, b.tags, x+b.tagX, lastCY, subCol)
	}
}

func drawChips(c *canvas, t txt, tags []string, x, cy float64, col color.RGBA) {
	h := t.Size * 1.5

	for _, tag := range tags {
		w := c.f.width(t, tag) + 2*chipPad(t)
		c.Stroke(rrect(x, cy-h/2, w, h, h/2), 1.4, col)
		c.textMid(t, tag, x+w/2, cy, alignC, col)

		x += w + chipGap(t)
	}
}

var markInk = rgba(0xFFD43B, 0.62)

func markBand(x, base, w float64, t txt) shape {
	h := t.Size * 0.42

	return rrect(x-t.Size*0.12, base-h*0.72, w+t.Size*0.24, h, h*0.3)
}

func strike(c *canvas, x, cy, w float64, t txt, col color.RGBA) {
	th := max(1.6, t.Size*0.07)
	c.Fill(rrect(x-2, cy-th/2, w+4, th, th/2), col)
}

func textStruck(c *canvas, t txt, s string, x, cy float64, col color.RGBA) {
	c.textMid(t, s, x, cy, alignL, col)
	strike(c, x, cy, c.f.width(t, s), t, col)
}

type rowStyle struct {
	main, sub  txt
	time, pill txt
	capPad     float64
	gutter     float64
	minSlot    float64
	slotGap    float64
}

type slotBox struct {
	s  slot
	tb titleBlock
	h  float64
}

type tableFit struct {
	k       float64
	heights []float64
	slots   [][]slotBox
	totals  []float64
	timeW   float64
	titleW  float64
	lines   int
}

type tableSpec struct {
	innerW, avail, gap, minRow, pad, kMax float64
}

const (
	softMin = 0.86
	kMin    = 0.62
)

func timeColW(f *fontSet, days []day, st rowStyle, k float64) float64 {
	w := 0.0

	for _, d := range days {
		for _, s := range d.slots {
			t, kind := timeLabel(s)

			switch kind {
			case timeUnsure, timeChanged:
				w = max(w, f.width(st.pill.scaled(k), t)+2*st.capPad*k)
			case timePlain, timeCancelled:
				w = max(w, f.width(st.time.scaled(k), t))
			case timeNone:
			}
		}
	}

	return w
}

func buildTable(f *fontSet, days []day, st rowStyle, sp tableSpec, k float64, tight bool) (tableFit, float64, bool) {
	t := tableFit{k: k, timeW: timeColW(f, days, st, k)}

	t.titleW = sp.innerW - t.timeW - st.gutter*k

	total := sp.gap * float64(len(days)-1)
	cut := false

	for _, d := range days {
		lim := titleLimits{maxW: t.titleW, maxMain: 2, maxSub: 1, withTags: true}

		switch {
		case d.off():
			lim.maxMain, lim.maxSub, lim.withTags = 1, 0, false
		case len(d.slots) == 1:
			lim.maxSub = 2
		case tight:
			lim.maxMain, lim.withTags = 1, false
		}

		boxes := make([]slotBox, 0, len(d.slots))
		h := st.slotGap * k * float64(max(0, len(d.slots)-1))

		for _, s := range d.slots {
			tb := layoutTitle(f, s, st.main.scaled(k), st.sub.scaled(k), lim)
			sh := max(tb.height(), st.minSlot*k)

			boxes = append(boxes, slotBox{s, tb, sh})

			h += sh

			cut = cut || tb.cut

			t.lines += len(tb.main)
		}

		t.slots = append(t.slots, boxes)
		t.totals = append(t.totals, h)
		t.heights = append(t.heights, max(sp.minRow, h+2*sp.pad))

		total += t.heights[len(t.heights)-1]
	}

	return t, total, cut
}

func fitWeek(f *fontSet, days []day, st rowStyle, sp tableSpec) (tableFit, []day) {
	return fitFolded(days, func(d []day) (tableFit, bool) { return fitTable(f, d, st, sp) })
}

func fitTable(f *fontSet, days []day, st rowStyle, sp tableSpec) (tableFit, bool) {
	spread := func(t tableFit, total float64) (tableFit, bool) {
		for i := range t.heights {
			t.heights[i] += max(0, sp.avail-total) / float64(len(t.heights))
		}

		return t, total <= sp.avail
	}

	base := -1

	for k := sp.kMax; ; k -= 0.03 {
		t, total, cut := buildTable(f, days, st, sp, k, false)

		if k > 1 {
			if base < 0 {
				b, _, _ := buildTable(f, days, st, sp, 1, false)

				base = b.lines
			}

			if total <= sp.avail && !cut && t.lines <= base {
				return spread(t, total)
			}

			continue
		}

		if total <= sp.avail && (!cut || k <= softMin) {
			return spread(t, total)
		}

		if k > softMin {
			continue
		}

		if t, total, _ := buildTable(f, days, st, sp, k, true); total <= sp.avail || k <= kMin {
			return spread(t, total)
		}
	}
}

type offLine struct {
	main, sub   string
	mainT, subT txt
	marked      bool
}

func layoutOff(f *fontSet, s slot, mainT, subT txt, maxW float64) offLine {
	l := offLine{main: f.clamp(mainT, s.main, maxW), mainT: mainT, subT: subT, marked: s.highlight}

	rest := maxW - f.width(mainT, l.main) - l.gap()
	if s.sub == "" || rest <= 0 {
		return l
	}

	if sub := f.clamp(subT, s.sub, rest); sub == s.sub || utf8.RuneCountInString(sub) > 4 {
		l.sub = sub
	}

	return l
}

func (l offLine) gap() float64 { return l.mainT.Size * 0.35 }

func (l offLine) width(f *fontSet) float64 {
	w := f.width(l.mainT, l.main)
	if l.sub != "" {
		w += l.gap() + f.width(l.subT, l.sub)
	}

	return w
}

func (l offLine) shape(c *canvas, x, cy float64) (mainS, subS shape) {
	mainS = c.textShape(l.mainT, l.main, x, cy+c.f.mid(l.mainT), alignL)
	if l.sub != "" {
		subS = c.textShape(l.subT, l.sub, x+c.f.width(l.mainT, l.main)+l.gap(), cy+c.f.mid(l.subT), alignL)
	}

	return mainS, subS
}

func (l offLine) markShape(c *canvas, x, cy float64) shape {
	if !l.marked {
		return shape{}
	}

	return markBand(x, cy+c.f.mid(l.mainT), l.width(c.f), l.mainT)
}

func (l offLine) draw(c *canvas, x, cy float64, mainCol, subCol color.RGBA) {
	c.Fill(l.markShape(c, x, cy), markInk)

	mainS, subS := l.shape(c, x, cy)
	c.Fill(mainS, mainCol)
	c.Fill(subS, subCol)
}

func pill(c *canvas, x, cy, h, padX float64, t txt, s string, bg, fg color.RGBA) float64 {
	w := c.f.width(t, s) + padX*2
	c.Fill(rrect(x, cy-h/2, w, h, h/2), bg)
	c.textMid(t, s, x+w/2, cy, alignC, fg)

	return w
}

type slotPaint struct {
	timeInk, unsureInk, ink, sub color.RGBA
	changedFill, changedInk      color.RGBA
	changedRing                  color.RGBA
	capH                         float64
	dashOn, dashOff, dashW       float64
	separator                    func(gy float64)
}

func drawTableSlots(c *canvas, st rowStyle, fit *tableFit, i int, timeX, sy float64, sp slotPaint) {
	k := fit.k
	titleX := timeX + fit.timeW + st.gutter*k
	capH := sp.capH * k

	for j := range fit.slots[i] {
		sb := &fit.slots[i][j]
		scy := sy + sb.h/2

		if j > 0 && sp.separator != nil {
			sp.separator(sy - st.slotGap*k/2)
		}

		label, kind := timeLabel(sb.s)

		switch kind {
		case timeUnsure:
			capT := st.pill.scaled(k)
			w := c.f.width(capT, label) + 2*st.capPad*k
			c.StrokeButt(dash(rrect(timeX, scy-capH/2, w, capH, capH/2), sp.dashOn, sp.dashOff), sp.dashW, sp.unsureInk)
			c.textMid(capT, label, timeX+w/2, scy, alignC, sp.unsureInk)
		case timeChanged:
			capT := st.pill.scaled(k)

			if sp.changedRing.A > 0 {
				w := c.f.width(capT, label) + 2*st.capPad*k
				c.Fill(rrect(timeX-2.5, scy-capH/2-2.5, w+5, capH+5, capH/2+2.5), sp.changedRing)
			}

			pill(c, timeX, scy, capH, st.capPad*k, capT, label, sp.changedFill, sp.changedInk)
		case timeCancelled:
			textStruck(c, st.time.scaled(k), label, timeX, scy, sp.sub)
		case timePlain:
			c.textMid(st.time.scaled(k), label, timeX, scy, alignL, sp.timeInk)
		case timeNone:
		}

		sb.tb.draw(c, titleX, scy-sb.tb.height()/2, sp.ink, sp.sub)

		sy += sb.h + st.slotGap*k
	}
}
