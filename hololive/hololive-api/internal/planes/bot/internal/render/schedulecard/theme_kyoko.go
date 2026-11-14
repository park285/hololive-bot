package schedulecard

import (
	"image"
	"image/color"
	"math"
	"strconv"
	"time"
)

type kyokoPal struct {
	ink, paper, cell, orange, cobalt, sub, rule, white, yellow color.RGBA
}

var kyokoColors = kyokoPal{
	ink: hex(0x17181C), paper: hex(0xF3F1EC), cell: hex(0xFCFBF8), orange: hex(0xF26B1D), cobalt: hex(0x2142E3),
	sub: hex(0x55565C), rule: hex(0xD8D4CC), white: hex(0xFFFFFF), yellow: hex(0xFFC41F),
}

func renderKyoko(c *canvas, v view) {
	p := kyokoColors
	w, h := c.w, c.h
	b := kyokoBoard(c)

	c.Fill(rect(0, 0, w, h), hex(0x24252C))
	c.Fill(xf(rrect(b.X-46, b.Y-60, b.W+130, b.H+120, 6), rotAt(deg(-2.2), b.cx(), b.cy())), p.cobalt)
	c.Fill(xf(rrect(b.X+30, b.Y-24, b.W+36, b.H+48, 6), rotAt(deg(1.6), b.cx(), b.cy())), p.orange)

	if !c.tall() {
		c.Fill(poly(0, h, 0, h-114, 250, h), p.orange)
		c.Fill(poly(0, h, 0, h-64, 140, h), p.cobalt)
		c.Clip(poly(0, 0, 210, 0, 0, 96), 1, func(l *canvas) {
			l.Fill(rect(0, 0, 220, 100), p.yellow)
			stripes(l, box{0, 0, 220, 100}, 16, 16, deg(40), p.ink)
		})
		kyokoHolder(c, p, box{28, 36, 336, h - 72}, v.photo)
	}

	board := b.rrect(8)
	c.Shadow(board, 0, 7, 14, rgba(0x000000, 0.38))
	c.Fill(board, p.paper)
	c.Stroke(b.inset(1.5).rrect(7), 3, p.ink)
	c.Clip(rect(b.cx()-200, b.Y+3, 400, 15), 1, func(l *canvas) {
		l.Fill(rect(b.cx()-200, b.Y+3, 400, 15), p.white)
		stripes(l, box{b.cx() - 200, b.Y + 3, 400, 15}, 11, 11, deg(38), p.orange)
	})

	kyokoHeader(c, p, b, v)
	kyokoCells(c, p, kyokoGrid(c, b), v.days)

	fy := b.bottom() - 23

	for i := range 16 {
		x := b.X + 28 + float64(i)*12
		c.Stroke(line(x, fy-5.5, x+5.5, fy, x, fy+5.5), 2.2, p.ink)
	}

	c.textMid(txt{Size: 19, Face: faceBold, Track: 0.2}, v.updated, b.right()-26, fy, alignR, p.sub)
}

func kyokoBoard(c *canvas) box {
	if c.tall() {
		return box{28, 28, c.w - 56, c.h - 56}
	}

	return box{388, 28, c.w - 388 - 28, c.h - 56}
}

func kyokoHeadH(c *canvas) float64 {
	if c.tall() {
		return 250
	}

	return 164
}

func kyokoGrid(c *canvas, b box) box {
	const foot = 44.0

	headH := kyokoHeadH(c)

	return box{b.X + 22, b.Y + headH, b.W - 44, b.H - headH - foot}
}

func kyokoHeader(c *canvas, p kyokoPal, b box, v view) {
	headH, nx, nameMax := kyokoHeadH(c), b.X+30, 78.0

	if c.tall() {
		nx, nameMax = b.X+262, 104
		kyokoIDCard(c, p, box{b.X + 28, b.Y + 34, 204, 204}, v.photo, false)
	}

	nameW := b.right() - 150 - nx
	nameT := c.f.fitSize(txt{Size: nameMax, Face: faceImpact}, v.name, nameW, 40)
	ny := b.Y + headH*0.40
	c.textMid(nameT, c.f.clamp(nameT, v.name, nameW), nx, ny, alignL, p.ink)

	for i, col := range []color.RGBA{p.cobalt, p.orange} {
		x := b.right() - 126 + float64(i)*44
		c.Fill(poly(x, ny+34, x+27, ny+34, x+59, ny-34, x+32, ny-34), col)
	}

	py := b.Y + headH - 36
	pw := pill(c, nx, py, 42, 20, txt{Size: 24, Face: faceXBold, Track: 0.8}, v.period, p.ink, p.white)
	c.Fill(circle(nx+pw+22, py, 8.5), p.ink)

	lx := nx + pw + 44
	c.Fill(rrect(lx, py-2.5, b.right()-30-lx-62, 5, 2.5), p.ink)

	for i := range 3 {
		x := b.right() - 80 + float64(i)*17
		c.Stroke(line(x, py-9, x+9, py, x, py+9), 3.2, p.ink)
	}
}

type kyokoCell struct {
	d     day
	slots []slotBox
	stack []bool
	h     float64
}

const (
	kyokoHeadW    = 98.0
	kyokoCellGap  = 12.0
	kyokoCapH     = 38.0
	kyokoStampMin = 118.0
)

var kyokoRow = rowStyle{
	main: txt{Size: 32, Face: faceXBold}, sub: txt{Size: 22, Face: faceBold},
	time: txt{Size: 23, Face: faceXBold, Track: 0.4}, pill: txt{Size: 23, Face: faceXBold, Track: 0.4},
	capPad: 14, gutter: 13, minSlot: 40, slotGap: 10,
}

func kyokoPillW(f *fontSet, k float64, label string) float64 {
	capT := kyokoRow.pill.scaled(k)

	return max(f.width(capT, "00:00"), f.width(capT, label)) + 2*kyokoRow.capPad*k
}

func kyokoTitleW(f *fontSet, cellW, k float64, s slot, stacked bool) float64 {
	w := cellW - kyokoHeadW - 24 - 16

	if stacked {
		return w
	}

	label, _ := timeLabel(s)

	return w - kyokoPillW(f, k, label) - kyokoRow.gutter*k
}

func kyokoCellFor(f *fontSet, d day, cellW, k float64) (cell kyokoCell, lines int, ok bool) {
	st := kyokoRow

	cell = kyokoCell{d: d}
	ok = true

	content := st.slotGap * k * float64(max(0, len(d.slots)-1))
	mainT, subT := st.main.scaled(k), st.sub.scaled(k)

	for _, s := range d.slots {
		label, _ := timeLabel(s)
		pw := kyokoPillW(f, k, label)
		lim := titleLimits{maxW: kyokoTitleW(f, cellW, k, s, false), maxMain: 2, maxSub: 1, withTags: true}

		if d.off() {
			lim.maxMain, lim.maxSub, lim.withTags = 1, 0, false
		}

		tb := layoutTitle(f, s, mainT, subT, lim)
		sb := slotBox{s, tb, max(tb.height(), st.minSlot*k)}
		stack := false

		// 넓은 캡슐 옆에서 제목이 접히거나 잘리면 캡슐을 제목 위로 올려 폭을 돌려준다.
		if !d.off() && pw > kyokoPillW(f, k, "") && (tb.cut || len(tb.main) > 1) {
			lim.maxW = kyokoTitleW(f, cellW, k, s, true)
			if up := layoutTitle(f, s, mainT, subT, lim); (!up.cut && len(up.main) < len(tb.main)) || (tb.cut && !up.cut) {
				stack = true
				sb = slotBox{s, up, kyokoCapH*k + 5*k + up.height()}
			}
		}

		cell.slots = append(cell.slots, sb)
		cell.stack = append(cell.stack, stack)

		content += sb.h
		lines += len(sb.tb.main)

		ok = ok && (!sb.tb.cut || k <= softMin)
	}

	cell.h = max(92, content+26)

	return cell, lines, ok
}

type kyokoLayout struct {
	cells    [][]kyokoCell
	ok, fits bool
	lines    int
}

func kyokoBuild(f *fontSet, cols [][]day, cellW, avail, k float64) kyokoLayout {
	out := make([][]kyokoCell, len(cols))
	ok, fits, lines := true, true, 0

	for ci, days := range cols {
		total := kyokoCellGap * float64(len(days)-1)

		for _, d := range days {
			cell, n, cellOK := kyokoCellFor(f, d, cellW, k)

			total += cell.h
			lines += n

			ok = ok && cellOK

			out[ci] = append(out[ci], cell)
		}

		// 날짜가 적은 단에는 도장 칸 자리를 남긴다.
		if len(days) < len(cols[0]) {
			total += kyokoCellGap + kyokoStampMin
		}

		fits = fits && total <= avail
	}

	return kyokoLayout{cells: out, ok: ok && fits, fits: fits, lines: lines}
}

type kyokoPlan struct {
	k     float64
	cells [][]kyokoCell
	cellW float64
}

func kyokoFit(f *fontSet, cols [][]day, cellW, avail, kTop float64) (kyokoPlan, bool) {
	base := -1

	for k := kTop; ; k -= 0.03 {
		l := kyokoBuild(f, cols, cellW, avail, k)

		if k > 1 {
			if base < 0 {
				base = kyokoBuild(f, cols, cellW, avail, 1).lines
			}

			l.ok = l.ok && l.lines <= base
		}

		if l.ok || k <= kMin {
			return kyokoPlan{k: k, cells: l.cells, cellW: cellW}, l.fits
		}
	}
}

const kyokoColGap = 14.0

func kyokoPlanFor(f *fontSet, days []day, g box, tall bool, kTop float64) kyokoPlan {
	plan, _ := fitFolded(days, func(d []day) (kyokoPlan, bool) {
		cols := [][]day{d}

		if !tall {
			half := (len(d) + 1) / 2

			cols = [][]day{d[:half], d[half:]}
		}

		cw := (g.W - kyokoColGap*float64(len(cols)-1)) / float64(len(cols))

		return kyokoFit(f, cols, cw, g.H, kTop)
	})

	return plan
}

func kyokoCells(c *canvas, p kyokoPal, g box, days []day) {
	plan := kyokoPlanFor(c.f, days, g, c.tall(), c.kMax())
	k, cells, cw, colGap := plan.k, plan.cells, plan.cellW, kyokoColGap

	for ci, col := range cells {
		kyokoColumn(c, p, box{g.X + float64(ci)*(cw+colGap), g.Y, cw, g.H}, col, len(col) < len(cells[0]), k)
	}
}

func kyokoColumn(c *canvas, p kyokoPal, g box, col []kyokoCell, short bool, k float64) {
	total := kyokoCellGap * float64(len(col)-1)
	weight := 0.0

	for _, cell := range col {
		total += cell.h
		weight += float64(max(1, len(cell.slots)))
	}

	// 남는 높이: 날짜가 적은 단은 도장 칸으로, 아니면 일정 건수에 비례해 칸에 나눈다.
	slack := max(0, g.H-total)
	stamp := 0.0

	if short && slack >= kyokoStampMin+kyokoCellGap {
		stamp, slack = min(slack-kyokoCellGap, 190), max(0, slack-kyokoCellGap-190)
	}

	y := g.Y

	for i := range col {
		h := col[i].h + slack*float64(max(1, len(col[i].slots)))/weight
		kyokoDrawCell(c, p, box{g.X, y, g.W, h}, &col[i], k)

		y += h + kyokoCellGap
	}

	if stamp > 0 {
		sb := box{g.X, y, g.W, stamp}
		c.Fill(sb.rect(), p.cell)
		c.Stroke(sb.rect(), 2.4, p.ink)
		c.Clip(sb.inset(2).rect(), 1, func(l *canvas) {
			kyokoStamp(l, sb.cx(), sb.cy(), min(1, (stamp-16)/190))
		})
	}
}

func kyokoDrawHead(c *canvas, p kyokoPal, b box, d day) {
	hb := box{b.X + 5, b.Y + 5, kyokoHeadW, b.H - 10}
	c.Fill(hb.rect(), p.ink)

	dk := min(1, hb.H/100)
	c.textMid(txt{Size: 58 * dk, Face: faceImpact}, strconv.Itoa(d.date.Day()), hb.cx(), hb.cy()-13*dk, alignC, p.white)

	tab := p.ink

	switch d.date.Weekday() {
	case time.Saturday:
		tab = p.cobalt
	case time.Sunday:
		tab = p.orange
	case time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday:
	}

	ty := hb.cy() + 30*dk
	tw, th := 58*dk, 22*dk

	if tab != p.ink {
		c.Fill(poly(hb.cx()-tw/2+6*dk, ty-th/2, hb.cx()+tw/2+6*dk, ty-th/2, hb.cx()+tw/2-6*dk, ty+th/2, hb.cx()-tw/2-6*dk, ty+th/2), tab)
	}

	c.textMid(txt{Size: 16 * dk, Face: faceXBold, Track: 1.4}, dowEN[weekday(d.date)], hb.cx(), ty, alignC, p.white)
}

func kyokoDrawCell(c *canvas, p kyokoPal, b box, cell *kyokoCell, k float64) {
	st := kyokoRow
	d := cell.d

	c.Fill(b.rect(), p.cell)
	c.Stroke(b.rect(), 2.4, p.ink)
	kyokoDrawHead(c, p, b, d)

	x0 := b.X + 5 + kyokoHeadW + 16

	switch {
	case len(d.slots) == 0:
		c.Fill(rrect(x0, b.cy()-2, 30, 4, 2), p.rule)

		return
	case d.off():
		c.Fill(xf(pawUnit(), place(x0+22, b.cy(), 0.40, deg(-12))), p.rule)

		offT := txt{Size: 32 * k, Face: faceXBold, Track: 2}
		layoutOff(c.f, d.slots[0], offT, kyokoRow.sub.scaled(k), b.right()-14-x0-54).draw(c, x0+54, b.cy(), p.sub, p.sub)

		return
	}

	content := st.slotGap * k * float64(len(cell.slots)-1)
	for j := range cell.slots {
		content += cell.slots[j].h
	}

	sy := b.cy() - content/2

	for j := range cell.slots {
		sb := &cell.slots[j]
		if j > 0 {
			gy := sy - st.slotGap*k/2
			c.Fill(rect(x0, gy-0.7, b.right()-14-x0, 1.4), p.rule)
		}

		kyokoDrawSlot(c, p, x0, sy, sb, cell.stack[j], k)

		sy += sb.h + st.slotGap*k
	}
}

func kyokoDrawSlot(c *canvas, p kyokoPal, x0, sy float64, sb *slotBox, stacked bool, k float64) {
	capH := kyokoCapH * k

	// 제목이 여러 줄이면 캡슐을 첫 줄에 맞춘다.
	top := sy + (sb.h-sb.tb.height())/2
	pcy := top + sb.tb.mainLH/2

	if stacked {
		pcy = sy + capH/2
		top = sy + capH + 5*k
	}

	label, kind := timeLabel(sb.s)
	w := kyokoPillW(c.f, k, label)
	body := rrect(x0, pcy-capH/2, w, capH, capH/2)
	capT := kyokoRow.pill.scaled(k)

	switch kind {
	case timeUnsure:
		c.Fill(body, p.white)
		c.StrokeButt(dash(body, 5, 4), 2.2, p.ink)
		c.textMid(capT, label, x0+w/2, pcy, alignC, p.ink)
	case timeChanged:
		c.Fill(body, p.orange)
		c.textMid(capT, label, x0+w/2, pcy, alignC, p.ink)
	case timeCancelled:
		c.Fill(body, p.rule)

		lw := c.f.width(capT, label)
		textStruck(c, capT, label, x0+w/2-lw/2, pcy, p.sub)
	case timePlain:
		c.Fill(body, p.ink)
		c.textMid(capT, label, x0+w/2, pcy, alignC, p.white)
	case timeNone:
	}

	titleX := x0 + w + kyokoRow.gutter*k

	if stacked {
		titleX = x0
	}

	sb.tb.draw(c, titleX, top, p.ink, p.sub)
}

func kyokoIDCard(c *canvas, p kyokoPal, b box, photo image.Image, strip bool) {
	card := b.rrect(18)
	c.Shadow(card, 0, 4, 8, rgba(0x000000, 0.30))
	c.Fill(card, hex(0xEDEDEF))

	ph := b.H - 32

	if strip {
		ph -= 60
	}

	pb := box{b.X + 16, b.Y + 16, b.W - 32, ph}
	c.Fill(box{pb.X - 5, pb.Y - 5, pb.W + 10, pb.H + 10}.rrect(17), p.orange)

	if photo == nil {
		c.Fill(pb.rrect(12), hex(0x3A3B44))
	} else {
		c.Photo(photo, pb.rrect(12), pb, 0)
	}

	// 사진 모서리에 남는 무대 배경을 검정으로 눌러 3색 규칙에 맞춘다.
	c.Vignette(pb.rrect(12), pb.cx(), pb.cy(), min(pb.W, pb.H)*0.50, math.Hypot(pb.W, pb.H)*0.52, 0x17181C, 0.72)

	if strip {
		kyokoBarcode(c, p, pb)
	}
}

func kyokoBarcode(c *canvas, p kyokoPal, pb box) {
	y0 := pb.bottom() + 18
	c.Clip(rrect(pb.X, y0, 96, 42, 5), 1, func(l *canvas) {
		stripes(l, box{pb.X, y0, 96, 42}, 12, 12, deg(38), p.ink)
	})

	bars := []float64{3, 1.5, 5, 2, 1.5, 4, 2.5, 1.5, 6, 2, 3.5, 1.5, 2.5, 5, 1.5, 3, 2, 4.5, 1.5, 2.5, 6, 2, 1.5, 3.5, 2.5, 4, 1.5, 5, 2, 1.5, 3, 2.5, 6, 1.5, 2, 4, 3}
	bx := pb.X + 112

	for i, w := range bars {
		if i%2 == 0 && bx+w <= pb.right() {
			c.Fill(rect(bx, y0, w, 42), p.ink)
		}

		bx += w + 1.7
	}
}

func kyokoHolder(c *canvas, p kyokoPal, b box, photo image.Image) {
	holder := b.rrect(28)
	c.Shadow(holder, 0, 8, 16, rgba(0x000000, 0.55))
	c.FillLinear(holder, 0, b.Y, 0, b.bottom(), stop{0, hex(0x25262E)}, stop{1, hex(0x121318)})
	c.StrokeButt(dash(b.inset(12).rrect(18), 11, 8), 2.2, rgba(0xFFFFFF, 0.78))
	c.Fill(rrect(b.cx()-36, b.Y+26, 72, 14, 7), hex(0x08090B))

	card := box{b.X + 28, b.Y + 58, b.W - 56, (b.W-56-32)*1.10 + 92}
	kyokoIDCard(c, p, card, photo, true)

	rest := b.bottom() - card.bottom()
	kyokoBadge(c, p, b.X+b.W*0.40, card.bottom()+rest*0.37, min(1.9, (rest*0.52)/122))

	sm := place(b.X+b.W*0.72, card.bottom()+rest*0.80, min(1, rest/420), deg(13))
	blob := xf(rrect(-54, -48, 108, 96, 37), sm)
	c.Shadow(blob, 0, 4, 6, rgba(0x000000, 0.45))
	c.Fill(outline(blob, 10, false), p.white)
	c.Fill(blob, p.yellow)
	c.Fill(xf(pawUnit(), place(0, 6, 0.68, 0).Then(sm)), p.ink)
}

func kyokoBadge(c *canvas, p kyokoPal, bcx, bcy, s float64) {
	outer := xf(shieldUnit(), place(bcx, bcy, s*1.07, 0))
	c.Shadow(outer, 0, 8, 12, rgba(0x000000, 0.55))
	c.Fill(outer, hex(0x8E6212))

	body := xf(shieldUnit(), place(bcx, bcy, s, 0))
	c.FillLinear(body, bcx-54*s, bcy-66*s, bcx+54*s, bcy+66*s, stop{0, hex(0xFFE894)}, stop{0.45, hex(0xF0BE3C)}, stop{1, hex(0xC98F1E)})
	c.Clip(body, 0.22, func(l *canvas) {
		dots(l, box{bcx - 60*s, bcy - 72*s, 120 * s, 144 * s}, 9, 2.2, p.white)
	})
	c.Stroke(xf(shieldUnit(), place(bcx, bcy, s*0.87, 0)), 2.2, rgba(0xFFF4C4, 0.9))

	ecy := bcy - 4*s
	c.Fill(circle(bcx, ecy, 36*s), hex(0x8E6212))
	c.Fill(circle(bcx, ecy, 34*s), p.white)
	c.Fill(circle(bcx, ecy, 30*s), p.cobalt)
	c.StrokeButt(dash(circle(bcx, ecy, 26*s), 2.2, 5.3), 2.4, rgba(0xFFFFFF, 0.55))
	c.Fill(xf(pawUnit(), place(bcx, ecy+2.5*s, 0.37*s, 0)), p.white)
}

func kyokoStamp(c *canvas, cx, cy, s float64) {
	oc := rgba(0xF26B1D, 0.84)
	ox, oy := cx-58*s, cy
	c.Stroke(circle(ox, oy, 86*s), 5*s, oc)
	c.StrokeButt(dash(circle(ox, oy, 76*s), 2.6*s, 7.35*s), 7*s, oc)
	c.Stroke(circle(ox, oy, 66*s), 2*s, oc)
	c.Fill(xf(pawUnit(), place(ox, oy+6*s, 0.84*s, deg(-14))), oc)

	bc := rgba(0x2142E3, 0.80)
	sx, sy := cx+92*s, cy+34*s
	c.Stroke(circle(sx, sy, 42*s), 4*s, bc)
	c.Stroke(circle(sx, sy, 33*s), 1.6*s, bc)

	var star shape

	for i := range 10 {
		r := 21.0 * s

		if i%2 == 1 {
			r = 9 * s
		}

		a := deg(10) - math.Pi/2 + float64(i)*math.Pi/5

		if i == 0 {
			star.MoveTo(sx+r*math.Cos(a), sy+r*math.Sin(a))
		} else {
			star.LineTo(sx+r*math.Cos(a), sy+r*math.Sin(a))
		}
	}

	star.Close()
	c.Fill(star, bc)
}
