package render

import (
	"fmt"
	"image/color"
	"math"
	"slices"
	"strconv"
)

type calColophonLine struct {
	day       int
	mainLabel string
	metaLabel string
	hue       float64
	chromatic bool
}

func buildPosterColophonLines(entries []calEntry, isBday bool) []calColophonLine {
	var days []int

	byDay := make(map[int][]calEntry)

	for _, e := range entries {
		if e.day < 1 || e.day > 31 {
			continue
		}

		if _, exists := byDay[e.day]; !exists {
			days = append(days, e.day)
		}

		byDay[e.day] = append(byDay[e.day], e)
	}

	slices.Sort(days)

	out := make([]calColophonLine, 0, len(entries))

	for _, d := range days {
		list := byDay[d]
		if len(list) == 0 {
			continue
		}

		u, ord := sharedGroupInfo(list)
		if len(list) >= 2 && u != "" {
			meta := fmt.Sprintf("%d명", len(list))

			if !isBday && ord > 0 {
				meta = fmt.Sprintf("%d명 · %d주년", len(list), ord)
			}

			out = append(out, calColophonLine{
				day:       d,
				mainLabel: u,
				metaLabel: meta,
				hue:       list[0].hue,
				chromatic: list[0].chromatic,
			})

			continue
		}

		for _, e := range list {
			meta := e.unit

			if !isBday {
				meta = fmt.Sprintf("%d주년", e.ordinal)
			}

			out = append(out, calColophonLine{
				day:       e.day,
				mainLabel: e.fullName,
				metaLabel: meta,
				hue:       e.hue,
				chromatic: e.chromatic,
			})
		}
	}

	return out
}

func drawPosterActiveCells(c *calCanvas, spec calPageSpec, lay calPosterLayout, byDay map[int][]calEntry, cellsTop float64, p calPosterPalette) {
	for wk := range lay.weeks {
		ry := cellsTop + float64(wk)*lay.rowH

		for colIdx := range 7 {
			day := wk*7 + colIdx - lay.firstWD + 1
			if day < 1 || day > lay.daysInM || len(byDay[day]) == 0 {
				continue
			}

			slot := calBox{X: calPosterGridX + float64(colIdx)*calPosterColW, Y: ry, W: calPosterColW, H: lay.rowH}
			drawPosterActiveCell(c, slot, day, colIdx, byDay[day], spec.isBday, p)
		}
	}
}

func posterActiveCellBox(slot calBox, colIdx int, creamCards bool) calBox {
	pad := 2.0

	if creamCards {
		pad = 2.5
	}

	padL, padR := pad, pad

	if colIdx == 0 {
		padL = 3.0
	}

	if colIdx == 6 {
		padR = 3.0
	}

	return calBox{X: slot.X + padL, Y: slot.Y + pad, W: slot.W - padL - padR, H: slot.H - 2*pad}
}

func drawPosterActiveCell(c *calCanvas, slot calBox, day, colIdx int, list []calEntry, isBday bool, p calPosterPalette) {
	if len(list) == 0 {
		return
	}

	c.Fill(slot.rect(), p.boardPaper)

	cb := posterActiveCellBox(slot, colIdx, p.creamCards)
	headH := 27.0
	capH := 45.0

	if p.creamCards {
		capH = 47.0
	}

	photoH := max(52.0, cb.H-headH-capH)
	capY := cb.Y + headH + photoH

	c.Fill(cb.rect(), p.activeBodyBG)
	c.Fill(calRect(cb.X, cb.Y, cb.W, headH), p.activeHeadBG)

	e0 := list[0]
	spot := calPosterSpotColor(e0.hue, e0.chromatic, p.obiPrimary, p.cardSpotTone)

	drawPosterActiveHeader(c, cb, headH, day, colIdx, list, isBday, p)
	drawPosterPhotoZone(c, calBox{X: cb.X, Y: cb.Y + headH, W: cb.W, H: photoH}, list, spot, p)
	drawPosterNameplate(c, cb, calBox{X: cb.X, Y: capY, W: cb.W, H: cb.bottom() - capY}, list, isBday, spot, p)
}

func drawPosterActiveHeader(c *calCanvas, cb calBox, headH float64, day, colIdx int, list []calEntry, isBday bool, p calPosterPalette) {
	if p.cornerDateTab {
		dBlk := calBox{X: cb.X, Y: cb.Y, W: 36, H: headH}
		c.Fill(dBlk.rect(), p.dateTabBG)
		c.textMid(calText{Size: 17.5, Face: calFaceDisplay, Track: -0.4}, strconv.Itoa(day), dBlk.cx(), dBlk.cy()+0.5, calAlignC, p.dateTabFG)
	} else {
		if p.hasTopRule {
			c.Fill(calRect(cb.X, cb.Y, cb.W, 3.0), p.activeTopRule)
		}

		dCol := p.activeHeadFG
		if p.creamCards {
			switch colIdx {
			case 0:
				dCol = calHex(0xC9335A)
			case 6:
				dCol = calHex(0x1B6EC2)
			}
		}

		c.textMid(calText{Size: 18, Face: calFaceDisplay, Track: -0.4}, strconv.Itoa(day), cb.X+10, cb.Y+14.5, calAlignL, dCol)
	}

	rightTag := ""

	if len(list) > 1 {
		rightTag = fmt.Sprintf("%d명", len(list))
	} else if !isBday && len(list) == 1 {
		rightTag = fmt.Sprintf("%d주년", list[0].ordinal)
	}

	if rightTag == "" {
		return
	}

	if p.cellStampTag {
		rt := calText{Size: 11.2, Face: calFaceXBold}
		rw := c.f.width(rt, rightTag) + 12
		rb := calBox{X: cb.right() - 5 - rw, Y: cb.Y + 4.5, W: rw, H: 18}
		c.Fill(rb.rect(), p.cellStampBG)
		c.textMid(rt, rightTag, rb.cx(), rb.cy(), calAlignC, p.cellStampFG)

		return
	}

	c.textMid(calText{Size: 11.5, Face: calFaceXBold}, rightTag, cb.right()-9, cb.Y+14, calAlignR, p.activeHeadSub)
}

func drawPosterPhotoZone(c *calCanvas, zone calBox, list []calEntry, spot color.RGBA, p calPosterPalette) {
	if len(list) == 0 {
		return
	}

	switch p.portraitFrame {
	case calPortraitBromideMat:
		inner := calBox{X: zone.X + 4, Y: zone.Y + 2, W: zone.W - 8, H: zone.H - 5}
		c.Fill(zone.rect(), p.photoMatCol)
		drawPosterPortraitOrMosaic(c, inner, list, p.photoMatCol, spot)
		c.StrokeInsetRect(inner, 1.0, p.photoInnerLin)
	case calPortraitPassePartout:
		inner := calBox{X: zone.X + 3, Y: zone.Y + 3, W: zone.W - 6, H: zone.H - 6}
		c.Fill(zone.rect(), p.photoMatCol)
		drawPosterPortraitOrMosaic(c, inner, list, p.photoMatCol, spot)
		c.StrokeInsetRect(inner, 1.0, p.photoInnerLin)
	case calPortraitFullBleed:
		drawPosterPortraitOrMosaic(c, zone, list, p.boardPaper, spot)
	}
}

func drawPosterPortraitOrMosaic(c *calCanvas, b calBox, list []calEntry, sepCol, fallback color.RGBA) {
	if len(list) == 0 {
		return
	}

	if len(list) == 1 {
		drawPosterPortrait(c, b, list[0], fallback, -0.07)

		return
	}

	drawPosterMultiMosaic(c, b, list, sepCol, fallback)
}

func drawPosterNameplate(c *calCanvas, cb, capBox calBox, list []calEntry, isBday bool, spot color.RGBA, p calPosterPalette) {
	if len(list) == 0 {
		return
	}

	c.Fill(capBox.rect(), p.activeCapBG)
	c.Fill(calRect(capBox.X, capBox.Y, capBox.W, 1.0), p.photoInnerLin)

	spotX := capBox.X
	spotH := capBox.H - 1

	if p.hasActiveFrame {
		spotX = capBox.X + 1.5
		spotH = capBox.H - 2.5
	}

	c.Fill(calRect(spotX, capBox.Y+1, 4.5, spotH), spot)

	if p.hasActiveFrame {
		c.StrokeInsetRect(cb, 1.5, p.activeFrame)
	}

	tx := spotX + 10.5
	maxTW := capBox.right() - 6 - tx

	if len(list) == 1 {
		e0 := list[0]
		nt := calText{Size: 15.2, Face: calFaceXBold, Track: -0.3}

		if e0.unit != "" {
			ut := calText{Size: 11.0, Face: calFaceBold}

			c.textMid(nt, c.f.clamp(nt, e0.shortName, maxTW), tx, capBox.Y+15, calAlignL, p.activeCapInk)
			c.textMid(ut, c.f.clamp(ut, e0.unit, maxTW), tx, capBox.Y+32.5, calAlignL, p.activeCapSub)

			return
		}

		c.textMid(nt, c.f.clamp(nt, e0.shortName, maxTW), tx, capBox.cy(), calAlignL, p.activeCapInk)

		return
	}

	uLbl, sOrd := sharedGroupInfo(list)
	if uLbl == "" {
		uLbl = list[0].shortName + " 외"
	}

	ut := calText{Size: 14.2, Face: calFaceXBold, Track: -0.2}
	c.textMid(ut, c.f.clamp(ut, uLbl, maxTW), tx, capBox.Y+15, calAlignL, p.activeCapInk)

	subLbl := "합동 생일"

	if !isBday && sOrd > 0 {
		subLbl = fmt.Sprintf("데뷔 %d주년", sOrd)
	} else if !isBday {
		subLbl = "합동 데뷔 기념일"
	}

	c.textMid(calText{Size: 11.0, Face: calFaceBold}, subLbl, tx, capBox.Y+32.5, calAlignL, p.activeCapSub)
}

func drawPosterPortrait(c *calCanvas, b calBox, e calEntry, fallback color.RGBA, biasY float64) {
	if e.photo != nil {
		c.PhotoFocus(e.photo, b.rect(), b, biasY)

		return
	}

	c.Fill(b.rect(), fallback)

	runes := []rune(e.shortName)
	ini := "?"

	if len(runes) > 0 {
		ini = string(runes[0])
	}

	c.textMid(calText{Size: min(b.W, b.H) * 0.45, Face: calFaceDisplay}, ini, b.cx(), b.cy(), calAlignC, calHex(0xFFFFFF))
}

func drawPosterMultiMosaic(c *calCanvas, b calBox, list []calEntry, sepCol, fallback color.RGBA) {
	shown := min(5, len(list))
	if shown == 0 {
		return
	}

	c.Fill(b.rect(), sepCol)

	const gap = 2.0

	if shown <= 3 {
		w := (b.W - float64(shown-1)*gap) / float64(shown)

		for i := range shown {
			mb := calBox{X: b.X + float64(i)*(w+gap), Y: b.Y, W: w, H: b.H}
			drawPosterPortrait(c, mb, list[i], fallback, -0.06)
		}

		return
	}

	topN := 3
	hRatio := 0.54

	if shown == 4 {
		topN = 2
		hRatio = 0.50
	}

	botN := shown - topN
	hTop := math.Round((b.H - gap) * hRatio)
	hBot := b.H - gap - hTop
	wTop := (b.W - float64(topN-1)*gap) / float64(topN)

	for i := range topN {
		mb := calBox{X: b.X + float64(i)*(wTop+gap), Y: b.Y, W: wTop, H: hTop}
		drawPosterPortrait(c, mb, list[i], fallback, -0.06)
	}

	wBot := (b.W - float64(botN-1)*gap) / float64(botN)

	for i := range botN {
		mb := calBox{X: b.X + float64(i)*(wBot+gap), Y: b.Y + hTop + gap, W: wBot, H: hBot}
		if i == botN-1 && len(list) > 5 {
			c.Fill(mb.rect(), fallback)
			c.textMid(calText{Size: min(mb.W, mb.H) * 0.36, Face: calFaceDisplay}, fmt.Sprintf("+%d", len(list)-4), mb.cx(), mb.cy(), calAlignC, calHex(0xFFFFFF))

			continue
		}

		drawPosterPortrait(c, mb, list[topN+i], fallback, -0.06)
	}
}

type calColophonGrid struct {
	shown       []calColophonLine
	overflow    int
	totalItems  int
	cols        int
	rowsPerCol  int
	colGap      float64
	colSpan     float64
	firstRowY   float64
	lineStep    float64
	compactDate bool
}

func computePosterColophonGrid(mb calBox, lines []calColophonLine) calColophonGrid {
	cols := 2

	if mb.W > 800 || len(lines) > 8 {
		cols = 3
	}

	bodyTop := mb.Y + 35
	bodyAvail := max(36.0, mb.H-35)
	maxRowsPerCol := max(2, int((bodyAvail-10)/20.0))
	maxSlots := cols * maxRowsPerCol

	shown := lines
	overflow := 0

	if len(shown) > maxSlots {
		overflow = len(shown) - (maxSlots - 1)
		shown = shown[:maxSlots-1]
	}

	totalItems := len(shown)

	if overflow > 0 {
		totalItems++
	}

	rowsPerCol := max(1, (totalItems+cols-1)/cols)
	colGap := 24.0
	colSpan := (mb.W - 32 - float64(cols-1)*colGap) / float64(cols)
	lineStep := min(27.0, (bodyAvail-14)/float64(rowsPerCol))
	blockSpan := float64(rowsPerCol-1) * lineStep

	return calColophonGrid{
		shown:       shown,
		overflow:    overflow,
		totalItems:  totalItems,
		cols:        cols,
		rowsPerCol:  rowsPerCol,
		colGap:      colGap,
		colSpan:     colSpan,
		firstRowY:   bodyTop + (bodyAvail-blockSpan)*0.44,
		lineStep:    lineStep,
		compactDate: cols == 3 && mb.W < 760,
	}
}

func drawPosterColophonHeader(c *calCanvas, spec calPageSpec, mb calBox, activeDays int, p calPosterPalette) {
	c.Fill(mb.rect(), p.indexBG)
	c.Fill(calRect(mb.X, mb.Y, mb.W, 3.0), p.indexTopRule)
	c.Fill(calRect(mb.X, mb.Y+34, mb.W, 1.0), p.gridLine)

	idxTitle := fmt.Sprintf("%d월 생일 목록", spec.month)
	sumRight := fmt.Sprintf("총 %d명", len(spec.entries))

	if !spec.isBday {
		idxTitle = fmt.Sprintf("%d월 데뷔 기념일 목록", spec.month)
		sumRight = fmt.Sprintf("%d일 · 총 %d명", activeDays, len(spec.entries))
	}

	c.textMid(calText{Size: 13.5, Face: calFaceXBold}, idxTitle, mb.X+16, mb.Y+18.5, calAlignL, p.indexHeadFG)
	c.textMid(calText{Size: 12, Face: calFaceBold}, sumRight, mb.right()-16, mb.Y+18.5, calAlignR, p.mutedInk)
}

func drawPosterColophon(c *calCanvas, spec calPageSpec, mb calBox, lines []calColophonLine, activeDays int, p calPosterPalette) {
	drawPosterColophonHeader(c, spec, mb, activeDays, p)

	g := computePosterColophonGrid(mb, lines)
	if g.totalItems == 0 {
		return
	}

	divSpan := float64(g.rowsPerCol-1) * g.lineStep

	for col := 1; col < g.cols; col++ {
		vx := math.Round(mb.X + 16 + float64(col)*g.colSpan + (float64(col)-0.5)*g.colGap)
		c.Fill(calRect(vx, g.firstRowY-9, 1.0, divSpan+18), p.gridLine)
	}

	for i := range g.totalItems {
		col := i / g.rowsPerCol
		row := i % g.rowsPerCol

		if col >= g.cols {
			break
		}

		ix := mb.X + 16 + float64(col)*(g.colSpan+g.colGap)
		iy := g.firstRowY + float64(row)*g.lineStep

		if i == len(g.shown) && g.overflow > 0 {
			c.textMid(calText{Size: 12.5, Face: calFaceBold}, fmt.Sprintf("외 %d건", g.overflow), ix+11, iy, calAlignL, p.subInk)

			continue
		}

		drawPosterColophonRow(c, spec, g.shown[i], ix, iy, g.colSpan, g.compactDate, p)
	}
}

func drawPosterColophonRow(c *calCanvas, spec calPageSpec, ln calColophonLine, ix, iy, colSpan float64, compactDate bool, p calPosterPalette) {
	spot := calPosterSpotColor(ln.hue, ln.chromatic, p.obiPrimary, p.boardSpotTone)
	c.Fill(calRect(ix, iy-4.5, 5, 9), spot)

	dStr := fmt.Sprintf("%02d.%02d %s", spec.month, ln.day, calDowKO(spec.year, spec.month, ln.day))
	nx := ix + 72

	if compactDate {
		dStr = fmt.Sprintf("%02d일", ln.day)
		nx = ix + 44
	}

	c.textMid(calText{Size: 12, Face: calFaceBold}, dStr, ix+11, iy, calAlignL, p.mutedInk)

	nt := calText{Size: 13, Face: calFaceXBold}
	mt := calText{Size: 11.5, Face: calFaceBold}
	metaText := ""

	if ln.metaLabel != "" {
		metaText = " · " + ln.metaLabel
	}

	metaW := c.f.width(mt, metaText)
	nameClamped := c.f.clamp(nt, ln.mainLabel, max(36, ix+colSpan-nx-metaW))
	c.textMid(nt, nameClamped, nx, iy, calAlignL, p.ink)

	if metaText == "" {
		return
	}

	mx := nx + c.f.width(nt, nameClamped)
	rem := ix + colSpan - mx

	if rem > 22 {
		c.textMid(mt, c.f.clamp(mt, metaText, rem), mx, iy, calAlignL, p.subInk)
	}
}

func drawPosterRosterRows(c *calCanvas, spec calPageSpec, lay calPosterLayout, byDay map[int][]calEntry, startY float64, p calPosterPalette) {
	ry := startY

	for _, d := range lay.multiDays {
		if list, ok := byDay[d]; ok {
			drawPosterRosterRow(c, spec, d, list, ry, lay.rosterRowH, p)
		}

		ry += lay.rosterRowH
	}
}

func posterRosterColumnSpans(n int) (headCols, memColSpan int) {
	switch n {
	case 6:
		return 1, 1
	case 5:
		return 2, 1
	case 4:
		return 3, 1
	case 3:
		return 1, 2
	default:
		return 3, 2
	}
}

func drawPosterRosterRow(c *calCanvas, spec calPageSpec, day int, list []calEntry, ry, rh float64, p calPosterPalette) {
	n := min(6, len(list))
	if n == 0 {
		return
	}

	headCols, memColSpan := posterRosterColumnSpans(n)

	c.Fill(calRect(calPosterGridX, ry, calPosterGridW, rh), p.boardPaper)
	c.Fill(calRect(calPosterGridX, math.Round(ry), calPosterGridW, 2.0), p.strongLine)

	bodyY := ry + 2.0
	bodyH := rh - 2.0
	headW := float64(headCols) * calPosterColW
	hb := calBox{X: calPosterGridX, Y: bodyY, W: headW, H: bodyH}

	c.Fill(hb.rect(), p.rosterHeadBG)
	c.Fill(calRect(hb.X, hb.Y, 6.0, hb.H), p.rosterBarCol)

	uLbl, sOrd := sharedGroupInfo(list)
	if uLbl == "" {
		uLbl = "합동 기념일"
	}

	dateTitle := fmt.Sprintf("%d월 %d일 (%s요일)", spec.month, day, calDowKO(spec.year, spec.month, day))
	c.textMid(calText{Size: 12.5, Face: calFaceBold}, dateTitle, hb.X+18, hb.Y+26, calAlignL, p.rosterSubFG)

	ut := calText{Size: 24, Face: calFaceDisplay, Track: -0.3}
	c.textMid(ut, c.f.clamp(ut, uLbl, hb.W-28), hb.X+18, hb.Y+58, calAlignL, p.rosterTitleFG)

	badgeStr := fmt.Sprintf("생일 · %d명", len(list))

	if !spec.isBday && sOrd > 0 {
		badgeStr = fmt.Sprintf("데뷔 %d주년 · %d명", sOrd, len(list))
	} else if !spec.isBday {
		badgeStr = fmt.Sprintf("데뷔 기념일 · %d명", len(list))
	}

	bt := calText{Size: 12, Face: calFaceXBold}
	bb := calBox{X: hb.X + 18, Y: hb.Y + 84, W: c.f.width(bt, badgeStr) + 16, H: 24}
	c.Fill(bb.rect(), p.rosterStampBG)
	c.textMid(bt, badgeStr, bb.cx(), bb.cy(), calAlignC, p.rosterStampFG)

	slotW := float64(memColSpan) * calPosterColW

	for i := range n {
		sx := calPosterGridX + headW + float64(i)*slotW
		drawPosterRosterMemberSlot(c, calBox{X: sx, Y: bodyY, W: slotW, H: bodyH}, list[i], p)
	}
}

func drawPosterRosterMemberSlot(c *calCanvas, rawSlot calBox, e calEntry, p calPosterPalette) {
	insetX, insetY := 1.0, 0.0

	if p.flushRoster {
		insetX, insetY = 0.0, 0.0
	} else if p.creamCards {
		insetX, insetY = 2.5, 2.5
	}

	if !p.flushRoster {
		c.Fill(calRect(math.Round(rawSlot.X), rawSlot.Y, 1.0, rawSlot.H), p.gridLine)
	}

	sb := calBox{X: rawSlot.X + insetX, Y: rawSlot.Y + insetY, W: rawSlot.W - 2*insetX, H: rawSlot.H - 2*insetY}
	photoH := max(48.0, sb.H-52.0)
	spot := calPosterSpotColor(e.hue, e.chromatic, p.obiPrimary, p.cardSpotTone)
	pZone := calBox{X: sb.X, Y: sb.Y, W: sb.W, H: photoH}

	switch p.portraitFrame {
	case calPortraitBromideMat:
		inP := calBox{X: pZone.X + 3, Y: pZone.Y + 3, W: pZone.W - 6, H: pZone.H - 5}
		c.Fill(pZone.rect(), p.photoMatCol)
		drawPosterPortrait(c, inP, e, spot, -0.07)
		c.StrokeInsetRect(inP, 1.0, p.photoInnerLin)
	case calPortraitPassePartout:
		padX := 3.0

		if p.flushRoster {
			padX = 4.0
		}

		inP := calBox{X: pZone.X + padX, Y: pZone.Y + 3, W: pZone.W - 2*padX, H: pZone.H - 6}
		c.Fill(pZone.rect(), p.photoMatCol)
		drawPosterPortrait(c, inP, e, spot, -0.07)
		c.StrokeInsetRect(inP, 1.0, p.photoInnerLin)
	case calPortraitFullBleed:
		drawPosterPortrait(c, pZone, e, spot, -0.07)
	}

	capB := calBox{X: sb.X, Y: sb.Y + photoH, W: sb.W, H: sb.H - photoH}
	c.Fill(capB.rect(), p.activeCapBG)
	c.Fill(calRect(capB.X, capB.Y, capB.W, 1.0), p.photoInnerLin)

	spotX := capB.X

	if p.flushRoster {
		spotX = capB.X + 1.0
	}

	c.Fill(calRect(spotX, capB.Y+1, 4.5, capB.H-1), spot)

	if p.flushRoster {
		c.Fill(calRect(math.Round(rawSlot.X), rawSlot.Y, 1.0, rawSlot.H), p.dowRule)
	}

	tx := spotX + 10.5
	avail := capB.right() - 6 - tx
	nt := calText{Size: 14.8, Face: calFaceXBold, Track: -0.2}
	ft := calText{Size: 11.0, Face: calFaceBold}

	c.textMid(nt, c.f.clamp(nt, e.shortName, avail), tx, capB.Y+15.5, calAlignL, p.activeCapInk)
	c.textMid(ft, c.f.clamp(ft, e.fullName, avail), tx, capB.Y+33.5, calAlignL, p.activeCapSub)
}
