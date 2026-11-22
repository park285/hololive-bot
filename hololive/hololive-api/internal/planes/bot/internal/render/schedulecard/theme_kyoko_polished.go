package schedulecard

import (
	"image/color"
	"slices"
	"strconv"
	"time"
)

type kyokoClassicTheme struct {
	bg, paper, cell, offCell, ink, sub, muted, rule, white color.RGBA
	orange, orangeDeep, cobalt, yellow, lime               color.RGBA
	dark                                                   bool
}

var kyokoClassicLight = kyokoClassicTheme{
	bg: hex(0x24252C), paper: hex(0xF3F1EC), cell: hex(0xFCFBF8), offCell: hex(0xECEAE3),
	ink: hex(0x17181C), sub: hex(0x55565C), muted: hex(0x76777F), rule: hex(0xD8D4CC), white: hex(0xFFFFFF),
	orange: hex(0xF26B1D), orangeDeep: hex(0xC94908), cobalt: hex(0x2142E3), yellow: hex(0xFFC41F), lime: hex(0xB4F02E),
}

var kyokoClassicDark = kyokoClassicTheme{
	bg: hex(0x0E1017), paper: hex(0x181B26), cell: hex(0x202433), offCell: hex(0x1A1D29),
	ink: hex(0xF4F3EE), sub: hex(0xAEB3C6), muted: hex(0x7F859B), rule: hex(0x31374D), white: hex(0xFFFFFF),
	orange: hex(0xF26B1D), orangeDeep: hex(0xD85610), cobalt: hex(0x2E55FF), yellow: hex(0xFFC41F), lime: hex(0xB9F536),
	dark: true,
}

func renderKyokoPolishedLight(c *canvas, v view) {
	renderKyokoPolished(c, v, kyokoClassicLight)
}

func renderKyokoPolishedDark(c *canvas, v view) {
	renderKyokoPolished(c, v, kyokoClassicDark)
}

func renderKyokoPolished(c *canvas, v view, p kyokoClassicTheme) {
	w, h := c.w, c.h
	b := box{28, 28, w - 56, h - 56}

	c.Fill(rect(0, 0, w, h), p.bg)
	c.Fill(xf(rrect(b.X-46, b.Y-60, b.W+130, b.H+120, 6), rotAt(deg(-2.2), b.cx(), b.cy())), p.cobalt)
	c.Fill(xf(rrect(b.X+30, b.Y-24, b.W+36, b.H+48, 6), rotAt(deg(1.6), b.cx(), b.cy())), p.orange)

	c.Clip(poly(0, 0, 170, 0, 0, 84), 1, func(l *canvas) {
		l.Fill(rect(0, 0, 176, 90), p.yellow)
		stripes(l, box{0, 0, 176, 90}, 13, 13, deg(40), hex(0x17181C))
	})
	c.Clip(poly(w, h, w-185, h, w, h-84), 1, func(l *canvas) {
		l.Fill(rect(w-190, h-90, 195, 95), p.yellow)
		stripes(l, box{w - 190, h - 90, 195, 95}, 13, 13, deg(40), hex(0x17181C))
	})

	board := b.rrect(8)
	c.Shadow(board, 0, 7, 14, rgba(0x000000, 0.38))
	c.Fill(board, p.paper)

	borderCol := p.ink
	if p.dark {
		borderCol = hex(0x3B425C)
	}

	c.Stroke(b.inset(1.5).rrect(7), 3, borderCol)

	c.Clip(rect(b.cx()-210, b.Y+3, 420, 15), 1, func(l *canvas) {
		l.Fill(rect(b.cx()-210, b.Y+3, 420, 15), p.white)
		stripes(l, box{b.cx() - 210, b.Y + 3, 420, 15}, 11, 11, deg(38), p.orange)
	})

	const headH = 266.0

	kyokoPolishedHeader(c, p, b, headH, v)
	kyokoPolishedCells(c, p, box{b.X + 22, b.Y + headH, b.W - 44, b.H - headH - 44}, v.days)

	fy := b.bottom() - 23

	for i := range 14 {
		x := b.X + 28 + float64(i)*12
		col := p.ink

		if i >= 11 {
			col = p.orange
		} else if i >= 8 {
			col = p.cobalt
		}

		c.Stroke(line(x, fy-5.5, x+5.5, fy, x, fy+5.5), 2.3, col)
	}

	gx := b.X + 216
	c.Stroke(circle(gx, fy, 8), 1.6, p.ink)
	c.Stroke(ellipse(gx, fy, 4, 8), 1.3, p.ink)
	c.Stroke(line(gx-8, fy, gx+8, fy), 1.3, p.ink)

	c.textMid(txt{Size: 19, Face: faceBold, Track: 0.2}, v.updated, b.right()-26, fy, alignR, p.sub)
}

func kyokoPolishedHeader(c *canvas, p kyokoClassicTheme, b box, headH float64, v view) {
	kyokoPolishedAvatar(c, p, b, v)

	nx := b.X + 280
	ny := kyokoPolishedTitle(c, p, b, headH, v, nx)

	kyokoPolishedBarcode(c, p, b.right()-146, ny+44)
	kyokoPolishedPeriod(c, p, b, nx, b.Y+headH-36, v.period)
}

func kyokoPolishedAvatar(c *canvas, p kyokoClassicTheme, b box, v view) {
	hb := box{b.X + 24, b.Y + 24, 218, 216}
	holder := hb.rrect(22)
	c.Shadow(holder, 0, 5, 11, rgba(0x000000, 0.36))
	c.FillLinear(holder, 0, hb.Y, 0, hb.bottom(), stop{0, hex(0x272933)}, stop{1, hex(0x14151B)})
	c.StrokeButt(dash(hb.inset(8).rrect(15), 8, 6), 1.8, rgba(0xFFFFFF, 0.65))
	c.Fill(rrect(hb.cx()-24, hb.Y+12, 48, 8, 4), hex(0x08090B))

	card := box{hb.X + 17, hb.Y + 28, hb.W - 34, hb.H - 45}
	kyokoIDCard(c, kyokoColors, card, v.photo, false)
	kyokoBadgeMini(c, hb.right()-10, hb.bottom()-16, 0.54)

	pm := place(hb.X+16, hb.bottom()-14, 0.40, deg(-14))
	blob := xf(rrect(-48, -42, 96, 84, 32), pm)
	c.Shadow(blob, 0, 2.5, 4, rgba(0x000000, 0.35))
	c.Fill(outline(blob, 8, false), p.white)
	c.Fill(blob, p.yellow)
	c.Fill(xf(pawUnit(), place(0, 5, 0.60, 0).Then(pm)), hex(0x17181C))
}

func kyokoPolishedTitle(c *canvas, p kyokoClassicTheme, b box, headH float64, v view, nx float64) float64 {
	nameW := b.right() - 178 - nx
	nameT := c.f.fitSize(txt{Size: 98, Face: faceImpact}, v.name, nameW, 40)
	ny := b.Y + headH*0.38
	c.textMid(nameT, c.f.clamp(nameT, v.name, nameW), nx, ny, alignL, p.ink)

	for i, col := range []color.RGBA{p.cobalt, p.orange, p.lime} {
		x := b.right() - 160 + float64(i)*36
		sw := 24.0

		if i == 2 {
			sw = 13
		}

		sh := poly(x, ny+32, x+sw, ny+32, x+sw+28, ny-32, x+28, ny-32)
		c.Fill(sh, col)

		if i == 2 {
			c.Stroke(sh, 1.6, hex(0x17181C))
		}
	}

	return ny
}

func kyokoPolishedBarcode(c *canvas, p kyokoClassicTheme, barX, barY float64) {
	for i, bw := range []float64{3, 1.5, 4.5, 2, 1.5, 5, 2, 3, 1.5, 4, 2, 1.5, 5, 2.5, 3, 1.5, 4, 2, 3} {
		if i%2 == 0 {
			c.Fill(rect(barX, barY, bw, 16), p.ink)
		}

		barX += bw + 1.8
	}
}

func kyokoPolishedPeriod(c *canvas, p kyokoClassicTheme, b box, nx, py float64, period string) {
	pillBG, pillFG := hex(0x17181C), p.white
	if p.dark {
		pillBG, pillFG = p.lime, hex(0x17181C)
	}

	pw := pill(c, nx, py, 42, 20, txt{Size: 24, Face: faceXBold, Track: 0.8}, period, pillBG, pillFG)
	c.Fill(circle(nx+pw+22, py, 8.5), p.orange)
	c.Stroke(circle(nx+pw+22, py, 8.5), 2, p.ink)

	lx := nx + pw + 44
	c.Fill(rrect(lx, py-2.5, b.right()-30-lx-66, 5, 2.5), p.ink)

	for i, col := range []color.RGBA{p.cobalt, p.orange, p.ink} {
		x := b.right() - 82 + float64(i)*17
		c.Stroke(line(x, py-9, x+9, py, x, py+9), 3.4, col)
	}
}

type kpCell struct {
	d     day
	slots []slotBox
	langs []string
	stack []bool
	h     float64
}

type kpLayout struct {
	cells  []kpCell
	lines  int
	ok     bool
	basePW float64
}

type kpSlotFrame struct {
	x0, k, basePW, capH, langW float64
	hasLang                    bool
}

func kpSplitLang(s slot) (string, slot) {
	cleaned := s

	var (
		lang string
		rest []string
	)

	for _, t := range s.tags {
		if lang == "" && (t == "EN" || t == "JP") {
			lang = t
			continue
		}

		rest = append(rest, t)
	}

	cleaned.tags = rest

	return lang, cleaned
}

func kpLangW(f *fontSet, k float64) float64 {
	return f.width(kyokoRow.pill.scaled(k*0.92), "EN") + 18*k
}

func kpBasePW(f *fontSet, days []day, k float64) float64 {
	w := kyokoPillW(f, k, "00:00")

	for _, d := range days {
		if d.off() {
			continue
		}

		for _, s := range d.slots {
			label, kind := timeLabel(s)
			if kind == timeChanged {
				continue
			}

			w = max(w, kyokoPillW(f, k, label))
		}
	}

	return w
}

func kpHasLang(days []day) bool {
	for i := range days {
		if slices.ContainsFunc(days[i].slots, func(s slot) bool { return slices.Contains(s.tags, "EN") || slices.Contains(s.tags, "JP") }) {
			return true
		}
	}

	return false
}

func kpMeasureCell(f *fontSet, g box, d day, k, basePW float64, hasLang bool) (kpCell, int, bool) {
	mainT, subT := kyokoRow.main.scaled(k), kyokoRow.sub.scaled(k)
	langW := kpLangW(f, k)
	cell := kpCell{d: d}
	content := kyokoRow.slotGap * k * float64(max(0, len(d.slots)-1))
	ok, lines := true, 0

	for _, raw := range d.slots {
		lang, s := kpSplitLang(raw)
		label, kind := timeLabel(s)
		pw := kyokoPillW(f, k, label)
		stack := !d.off() && kind == timeChanged && pw > basePW*1.15
		tw := g.W - kyokoHeadW - 40

		if !stack {
			tw -= basePW + kyokoRow.gutter*k

			if hasLang {
				tw -= langW + 9*k
			}
		}

		lim := titleLimits{maxW: tw, maxMain: 2, maxSub: 1, withTags: true}

		if d.off() {
			lim.maxMain, lim.maxSub, lim.withTags = 1, 0, false
		}

		tb := layoutTitle(f, s, mainT, subT, lim)
		sh := max(tb.height(), kyokoRow.minSlot*k)

		if stack {
			sh = kyokoCapH*k + 6*k + tb.height()
		}

		cell.slots = append(cell.slots, slotBox{s, tb, sh})
		cell.langs = append(cell.langs, lang)
		cell.stack = append(cell.stack, stack)

		content += sh
		lines += len(tb.main)

		ok = ok && (!tb.cut || k <= softMin)
	}

	cell.h = max(90, content+24)

	return cell, lines, ok
}

func kpBuild(f *fontSet, g box, days []day, k float64, hasLang bool) kpLayout {
	basePW := kpBasePW(f, days, k)
	out := make([]kpCell, 0, len(days))
	total := kyokoCellGap * float64(len(days)-1)
	ok, lines := true, 0

	for _, d := range days {
		cell, cellLines, cellOK := kpMeasureCell(f, g, d, k, basePW, hasLang)

		total += cell.h
		lines += cellLines

		ok = ok && cellOK

		out = append(out, cell)
	}

	return kpLayout{cells: out, lines: lines, ok: ok && total <= g.H, basePW: basePW}
}

func kpFit(f *fontSet, g box, days []day, kMax float64, hasLang bool) (kpLayout, float64) {
	baseLines := -1

	for k := kMax; ; k -= 0.03 {
		l := kpBuild(f, g, days, k, hasLang)
		ok := l.ok

		if k > 1 {
			if baseLines < 0 {
				baseLines = kpBuild(f, g, days, 1, hasLang).lines
			}

			ok = ok && l.lines <= baseLines
		}

		if ok || k <= kMin {
			return l, k
		}
	}
}

func kyokoPolishedCells(c *canvas, p kyokoClassicTheme, g box, days []day) {
	hasLang := kpHasLang(days)
	l, k := kpFit(c.f, g, days, c.kMax(), hasLang)
	total := kyokoCellGap * float64(len(l.cells)-1)
	weight := 0.0

	for i := range l.cells {
		total += l.cells[i].h
		weight += float64(max(1, len(l.cells[i].slots)))
	}

	slack := max(0, g.H-total)
	y := g.Y

	for i := range l.cells {
		h := l.cells[i].h + slack*float64(max(1, len(l.cells[i].slots)))/weight
		kyokoPolishedDrawCell(c, p, box{g.X, y, g.W, h}, &l.cells[i], k, l.basePW, hasLang)

		y += h + kyokoCellGap
	}
}

func kyokoPolishedCellFrame(c *canvas, p kyokoClassicTheme, b box, d day) {
	bg := p.cell

	if d.off() {
		bg = p.offCell
	}

	c.Fill(b.rect(), bg)

	borderCol := p.ink
	if p.dark {
		borderCol = hex(0x3B425C)
	}

	c.Stroke(b.rect(), 2.4, borderCol)

	hb := box{b.X + 5, b.Y + 5, kyokoHeadW, b.H - 10}
	headBG := hex(0x17181C)

	if p.dark {
		headBG = hex(0x11131C)
	}

	c.Fill(hb.rect(), headBG)

	dk := min(1, hb.H/100)
	c.textMid(txt{Size: 56 * dk, Face: faceImpact}, strconv.Itoa(d.date.Day()), hb.cx(), hb.cy()-14*dk, alignC, p.white)

	tab, fg := hex(0x2E303A), p.white

	switch d.date.Weekday() {
	case time.Saturday:
		tab, fg = p.cobalt, p.white
	case time.Sunday:
		tab, fg = p.orange, hex(0x17181C)
	case time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday:
	}

	ty := hb.cy() + 29*dk
	tw, th := 62*dk, 22*dk
	c.Fill(poly(hb.cx()-tw/2+6*dk, ty-th/2, hb.cx()+tw/2+6*dk, ty-th/2, hb.cx()+tw/2-6*dk, ty+th/2, hb.cx()-tw/2-6*dk, ty+th/2), tab)

	if tab == hex(0x2E303A) {
		c.Fill(rect(hb.cx()-tw/2-4*dk, ty+th/2-2.2*dk, tw, 2.2*dk), p.orange)
	}

	c.textMid(txt{Size: 15.5 * dk, Face: faceXBold, Track: 1.4}, dowEN[weekday(d.date)], hb.cx(), ty, alignC, fg)
}

func kyokoPolishedDrawCell(c *canvas, p kyokoClassicTheme, b box, cell *kpCell, k, basePW float64, hasLang bool) {
	d := cell.d
	kyokoPolishedCellFrame(c, p, b, d)

	x0 := b.X + 5 + kyokoHeadW + 16

	if len(d.slots) == 0 {
		c.Fill(rrect(x0, b.cy()-2, 30, 4, 2), p.rule)

		return
	}

	if d.off() {
		c.Fill(xf(pawUnit(), place(x0+22, b.cy(), 0.40, deg(-12))), p.yellow)

		offT := txt{Size: 32 * k, Face: faceXBold, Track: 2}
		layoutOff(c.f, d.slots[0], offT, kyokoRow.sub.scaled(k), b.right()-14-x0-54).draw(c, x0+54, b.cy(), p.sub, p.sub)

		return
	}

	content := kyokoRow.slotGap * k * float64(len(cell.slots)-1)
	for j := range cell.slots {
		content += cell.slots[j].h
	}

	sy := b.cy() - content/2
	frame := kpSlotFrame{x0: x0, k: k, basePW: basePW, capH: kyokoCapH * k, langW: kpLangW(c.f, k), hasLang: hasLang}

	for j := range cell.slots {
		if j > 0 {
			gy := sy - kyokoRow.slotGap*k/2
			c.Fill(rect(x0, gy-0.7, b.right()-14-x0, 1.4), p.rule)
		}

		kyokoPolishedSlot(c, p, frame, &cell.slots[j], cell.langs[j], cell.stack[j], sy)

		sy += cell.slots[j].h + kyokoRow.slotGap*k
	}
}

func kyokoPolishedSlot(c *canvas, p kyokoClassicTheme, fr kpSlotFrame, sb *slotBox, lang string, stacked bool, sy float64) {
	k, x0 := fr.k, fr.x0
	top := sy + (sb.h-sb.tb.height())/2
	pcy := top + sb.tb.mainLH/2

	if stacked {
		pcy = sy + fr.capH/2
		top = sy + fr.capH + 6*k
	}

	label, kind := timeLabel(sb.s)
	w := kyokoPillW(c.f, k, label)

	if !stacked {
		w = max(w, fr.basePW)
	}

	kyokoPolishedPill(c, p, kind, label, rrect(x0, pcy-fr.capH/2, w, fr.capH, fr.capH/2), x0+w/2, pcy, kyokoRow.pill.scaled(k))

	canceled := sb.s.status == StatusCancelled
	titleX := x0

	if stacked {
		kyokoPolishedLang(c, p, fr, lang, canceled, x0+w+9*k, pcy)
	} else {
		titleX = x0 + fr.basePW + kyokoRow.gutter*k

		if fr.hasLang {
			kyokoPolishedLang(c, p, fr, lang, canceled, x0+fr.basePW+9*k, pcy)

			titleX = x0 + fr.basePW + 9*k + fr.langW + kyokoRow.gutter*k
		}
	}

	mainCol := p.ink

	if canceled {
		mainCol = p.muted
	}

	sb.tb.draw(c, titleX, top, mainCol, p.sub)
}

func kyokoPolishedPill(c *canvas, p kyokoClassicTheme, kind timeKind, label string, body shape, cx, pcy float64, capT txt) {
	switch kind {
	case timeUnsure:
		bgC := p.white
		if p.dark {
			bgC = hex(0x181B26)
		}

		c.Fill(body, bgC)
		c.StrokeButt(dash(body, 5, 4), 2.2, p.ink)
		c.textMid(capT, label, cx, pcy, alignC, p.ink)
	case timeChanged:
		c.Fill(body, p.orange)
		c.textMid(capT, label, cx, pcy, alignC, hex(0x17181C))
	case timeCancelled:
		c.Fill(body, p.rule)

		lw := c.f.width(capT, label)
		textStruck(c, capT, label, cx-lw/2, pcy, p.muted)
	case timePlain:
		plainBG, plainFG := hex(0x17181C), p.white
		if p.dark {
			plainBG, plainFG = hex(0x2F354C), p.white
		}

		c.Fill(body, plainBG)
		c.textMid(capT, label, cx, pcy, alignC, plainFG)
	case timeNone:
	}
}

func kyokoPolishedLang(c *canvas, p kyokoClassicTheme, fr kpSlotFrame, lang string, canceled bool, lx, pcy float64) {
	if lang == "" {
		return
	}

	fill, lfg := p.cobalt, p.white

	if lang == "JP" {
		fill = p.orangeDeep
	}

	if canceled {
		fill, lfg = p.rule, p.muted
	}

	c.Fill(rrect(lx, pcy-fr.capH/2, fr.langW, fr.capH, 8*fr.k), fill)
	c.textMid(kyokoRow.pill.scaled(fr.k*0.92), lang, lx+fr.langW/2, pcy, alignC, lfg)
}

func kyokoBadgeMini(c *canvas, bcx, bcy, s float64) {
	outer := xf(shieldUnit(), place(bcx, bcy, s*1.08, 0))
	c.Shadow(outer, 0, 4*s, 8*s, rgba(0x000000, 0.5))
	c.Fill(outer, hex(0x8E6212))

	body := xf(shieldUnit(), place(bcx, bcy, s, 0))
	c.FillLinear(body, bcx-54*s, bcy-66*s, bcx+54*s, bcy+66*s, stop{0, hex(0xFFE894)}, stop{0.5, hex(0xF0BE3C)}, stop{1, hex(0xC98F1E)})

	ecy := bcy - 4*s
	c.Fill(circle(bcx, ecy, 33*s), hex(0xFFFFFF))
	c.Fill(circle(bcx, ecy, 29*s), hex(0x2142E3))
	c.Fill(xf(pawUnit(), place(bcx, ecy+2.5*s, 0.36*s, 0)), hex(0xFFFFFF))
}
