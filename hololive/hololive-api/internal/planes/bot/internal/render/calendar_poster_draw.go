package render

import (
	"fmt"
	"image"
	"image/draw"
	"math"
	"slices"
	"strconv"
	"time"
)

const (
	calPosterBoardX = 20.0
	calPosterBoardW = 984.0
	calPosterGridX  = 36.0
	calPosterColW   = 136.0
	calPosterGridW  = 952.0
	calPosterObiH   = 7.0
)

type calPosterLayout struct {
	boardTop   float64
	mastH      float64
	dowH       float64
	rowH       float64
	extraIdxH  float64
	rosterRowH float64
	boardBot   float64
	outerBot   float64
	weeks      int
	firstWD    int
	daysInM    int
	trailing   int
	multiDays  []int
	totalH     int
}

func (l *calPosterLayout) recompute(hasEntries bool, colophonCount int) {
	l.extraIdxH = 0
	if hasEntries && l.trailing < 3 {
		rows := min(5, max(2, (colophonCount+2)/3))

		l.extraIdxH = max(104.0, 38.0+float64(rows)*24.0+14.0)
	}

	rosterTotal := float64(len(l.multiDays)) * l.rosterRowH
	tableH := l.dowH + float64(l.weeks)*l.rowH + l.extraIdxH + rosterTotal
	boardH := calPosterObiH + l.mastH + tableH + l.boardBot

	l.totalH = int(math.Ceil(l.boardTop + boardH + l.outerBot))
}

func computePosterLayout(year, month int, byDay map[int][]calEntry, hasEntries bool, colophonCount int) calPosterLayout {
	first := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	firstWD := int(first.Weekday())
	daysInM := first.AddDate(0, 1, -1).Day()
	weeks := (firstWD + daysInM + 6) / 7
	trailing := weeks*7 - (firstWD + daysInM)

	var multi []int

	for d, list := range byDay {
		if len(list) >= 2 && len(list) <= 6 {
			multi = append(multi, d)
		}
	}

	slices.Sort(multi)

	if len(multi) > 2 {
		multi = multi[:2]
	}

	lay := calPosterLayout{
		boardTop: 20, mastH: 112, dowH: 36, rowH: 158,
		rosterRowH: 136, boardBot: 18, outerBot: 20,
		weeks: weeks, firstWD: firstWD, daysInM: daysInM,
		trailing: trailing, multiDays: multi,
	}

	lay.recompute(hasEntries, colophonCount)
	tightenPosterLayout(&lay, hasEntries, colophonCount)

	return lay
}

func tightenPosterLayout(lay *calPosterLayout, hasEntries bool, colophonCount int) {
	if lay.totalH > calendarOutputHeight && len(lay.multiDays) > 1 {
		lay.multiDays = lay.multiDays[:1]
		lay.recompute(hasEntries, colophonCount)
	}

	if lay.totalH > calendarOutputHeight {
		lay.boardTop = 16
		lay.mastH = 100
		lay.dowH = 32
		lay.rosterRowH = 128
		lay.boardBot = 14
		lay.outerBot = 16
		lay.recompute(hasEntries, colophonCount)
	}

	if lay.totalH > calendarOutputHeight {
		excess := float64(lay.totalH - calendarOutputHeight)

		lay.rowH = max(132, lay.rowH-math.Ceil(excess/float64(max(1, lay.weeks))))
		lay.recompute(hasEntries, colophonCount)
	}

	if lay.totalH > calendarOutputHeight && len(lay.multiDays) > 0 {
		lay.multiDays = nil
		lay.recompute(hasEntries, colophonCount)
	}

	lay.totalH = min(lay.totalH, calendarOutputHeight)
}

func renderFlatPosterPagePNG(fs *calFontSet, spec calPageSpec, byDay map[int][]calEntry) ([]byte, error) {
	p := buildPosterPalette(spec.themeID, spec.isBday)
	colophonLines := buildPosterColophonLines(spec.entries, spec.isBday)
	hasEntries := len(spec.entries) > 0
	lay := computePosterLayout(spec.year, spec.month, byDay, hasEntries, len(colophonLines))

	c := newCalCanvas(calendarOutputWidth, lay.totalH, calSupersample, fs)
	draw.Draw(c.img, c.img.Bounds(), image.NewUniform(p.outerCanvas), image.Point{}, draw.Src)

	rosterTotal := float64(len(lay.multiDays)) * lay.rosterRowH
	gridTop := lay.boardTop + calPosterObiH + lay.mastH
	tableH := lay.dowH + float64(lay.weeks)*lay.rowH + lay.extraIdxH + rosterTotal
	boardH := calPosterObiH + lay.mastH + tableH + lay.boardBot

	bd := calBox{X: calPosterBoardX, Y: lay.boardTop, W: calPosterBoardW, H: boardH}
	c.Fill(bd.rect(), p.boardPaper)

	splitW := calPosterGridX + calPosterColW*5 - calPosterBoardX
	c.Fill(calRect(bd.X, bd.Y, splitW, calPosterObiH), p.obiPrimary)
	c.Fill(calRect(bd.X+splitW, bd.Y, bd.W-splitW, calPosterObiH), p.obiSecond)

	drawPosterMasthead(c, spec, len(byDay), lay.boardTop+calPosterObiH, lay.mastH, p)

	tb := calBox{X: calPosterGridX, Y: gridTop, W: calPosterGridW, H: tableH}
	c.Fill(tb.rect(), p.quietCellBG)

	drawPosterWeekdaysAndBaseCells(c, lay, byDay, hasEntries, gridTop, p)
	drawPosterGridLines(c, lay, hasEntries, gridTop, p)

	cellsTop := gridTop + lay.dowH

	if !hasEntries {
		drawPosterEmptyBanner(c, spec, lay, cellsTop, p)
	} else {
		idxBox := resolvePosterColophonBox(lay, cellsTop)
		drawPosterColophon(c, spec, idxBox, colophonLines, len(byDay), p)
		drawPosterActiveCells(c, spec, lay, byDay, cellsTop, p)
		drawPosterRosterRows(c, spec, lay, byDay, cellsTop+float64(lay.weeks)*lay.rowH+lay.extraIdxH, p)
	}

	c.StrokeInsetRect(tb, 1.5, p.strongLine)
	c.StrokeInsetRect(bd, 1.5, p.boardBorder)

	return c.EncodePNG(calendarOutputWidth, lay.totalH)
}

func drawPosterMasthead(c *calCanvas, spec calPageSpec, activeDays int, topY, mastH float64, p calPosterPalette) {
	scale := min(1.0, mastH/112.0)
	numStr := fmt.Sprintf("%02d", spec.month)

	if p.tileMonthStamp {
		tile := calBox{X: calPosterGridX, Y: topY + 18*scale, W: 108, H: 74 * scale}
		c.Fill(calRect(tile.X+4, tile.Y+4, tile.W, tile.H), p.numShadowCol)
		c.Fill(tile.rect(), p.numTileBG)

		numT := calText{Size: 60 * scale, Face: calFaceDisplay, Track: -2.0}
		sh := c.textShape(numT, numStr, 0, 0, calAlignL)
		x0, y0, x1, y1 := sh.Bounds()
		c.Fill(calTransform(sh, calMove(tile.cx()-(x0+x1)/2, tile.cy()-(y0+y1)/2)), p.numFG)
	} else {
		numT := calText{Size: 88 * scale, Face: calFaceDisplay, Track: -3.0}
		sh := c.textShape(numT, numStr, 0, 0, calAlignL)
		x0, y0, _, y1 := sh.Bounds()
		placed := calTransform(sh, calMove(calPosterGridX-x0, topY+mastH/2-(y0+y1)/2+2))
		c.Fill(calTransform(placed, calMove(3.5, 3.5)), p.numShadowCol)
		c.Fill(placed, p.numFG)
	}

	tx := calPosterGridX + calPosterColW
	kicker := fmt.Sprintf("%d년 %d월  ·  한국 표준시(KST) 기준", spec.year, spec.month)
	c.textMid(calText{Size: 13.5, Face: calFaceBold, Track: 0.2}, kicker, tx, topY+31*scale, calAlignL, p.subInk)

	mainTitle, stampText := posterMastheadLabels(spec, activeDays)
	c.textMid(calText{Size: 42 * scale, Face: calFaceDisplay, Track: -0.6}, mainTitle, tx, topY+73*scale, calAlignL, p.ink)

	rx := calPosterGridX + calPosterGridW
	stT := calText{Size: 14, Face: calFaceXBold, Track: 0.1}
	stW := max(116.0, c.f.width(stT, stampText)+28)
	stBox := calBox{X: rx - stW, Y: topY + 28*scale, W: stW, H: 32}

	c.Fill(stBox.rect(), p.mastStampBG)
	c.textMid(stT, stampText, stBox.cx(), stBox.cy(), calAlignC, p.mastStampFG)
	drawPosterMastheadSpotBar(c, spec.entries, stBox, p)
}

func posterMastheadLabels(spec calPageSpec, activeDays int) (mainTitle, stampText string) {
	if len(spec.entries) == 0 {
		return fmt.Sprintf("%d월 기념일 달력", spec.month), "기념일 없음"
	}

	if spec.isBday {
		return fmt.Sprintf("%d월 생일 달력", spec.month), fmt.Sprintf("생일 %d명", len(spec.entries))
	}

	return fmt.Sprintf("%d월 데뷔 기념일 달력", spec.month), fmt.Sprintf("%d일 · 총 %d명", activeDays, len(spec.entries))
}

func drawPosterMastheadSpotBar(c *calCanvas, entries []calEntry, stBox calBox, p calPosterPalette) {
	spotCount := min(12, len(entries))
	if spotCount == 0 {
		return
	}

	gap := 2.0
	segW := (stBox.W - float64(spotCount-1)*gap) / float64(spotCount)
	barY := stBox.bottom() + 5

	for i := range spotCount {
		e := entries[i]
		col := calPosterSpotColor(e.hue, e.chromatic, p.obiPrimary, p.boardSpotTone)
		c.Fill(calRect(stBox.X+float64(i)*(segW+gap), barY, segW, 6.0), col)
	}
}

func drawPosterWeekdaysAndBaseCells(c *calCanvas, lay calPosterLayout, byDay map[int][]calEntry, hasEntries bool, gridTop float64, p calPosterPalette) {
	dows := [7]string{"일", "월", "화", "수", "목", "금", "토"}

	for i, dl := range dows {
		cx := calPosterGridX + float64(i)*calPosterColW
		fg := p.dowFG

		switch i {
		case 0:
			fg = p.dowSunFG
		case 6:
			fg = p.dowSatFG
		}

		c.Fill(calRect(cx, gridTop, calPosterColW, lay.dowH), p.dowBG)
		c.textMid(calText{Size: 14, Face: calFaceXBold, Track: 0.5}, dl, cx+12, gridTop+lay.dowH/2-0.5, calAlignL, fg)
	}

	cellsTop := gridTop + lay.dowH

	for wk := range lay.weeks {
		ry := cellsTop + float64(wk)*lay.rowH

		for colIdx := range 7 {
			day := wk*7 + colIdx - lay.firstWD + 1
			cb := calBox{X: calPosterGridX + float64(colIdx)*calPosterColW, Y: ry, W: calPosterColW, H: lay.rowH}

			if day > lay.daysInM && wk == lay.weeks-1 && lay.trailing >= 3 && hasEntries {
				continue
			}

			if day < 1 || day > lay.daysInM {
				c.FillHatch(cb, p.hatchBase, p.hatchStripe, 12, p.hatchThickness)

				continue
			}

			if len(byDay[day]) == 0 {
				drawPosterQuietCell(c, cb, day, colIdx, p)
			} else {
				c.Fill(cb.rect(), p.boardPaper)
			}
		}
	}
}

func drawPosterQuietCell(c *calCanvas, cb calBox, day, colIdx int, p calPosterPalette) {
	c.Fill(cb.rect(), p.quietCellBG)

	nCol := p.quietNum

	switch colIdx {
	case 0:
		nCol = p.sunInk
	case 6:
		nCol = p.satInk
	}

	c.textMid(calText{Size: 20, Face: calFaceDisplay, Track: -0.5}, strconv.Itoa(day), cb.X+11, cb.Y+18, calAlignL, nCol)
}

func drawPosterGridLines(c *calCanvas, lay calPosterLayout, hasEntries bool, gridTop float64, p calPosterPalette) {
	c.Fill(calRect(calPosterGridX, gridTop+lay.dowH-1.5, calPosterGridW, 1.5), p.dowRule)
	c.Fill(calRect(calPosterGridX+1, gridTop+lay.dowH-3.0, calPosterColW-1, 3.0), p.dowSunBar)
	c.Fill(calRect(calPosterGridX+6*calPosterColW+1, gridTop+lay.dowH-3.0, calPosterColW-2, 3.0), p.dowSatBar)

	cellsTop := gridTop + lay.dowH

	for wk := 1; wk < lay.weeks; wk++ {
		y := math.Round(cellsTop + float64(wk)*lay.rowH)
		c.Fill(calRect(calPosterGridX, y, calPosterGridW, 1.0), p.gridLine)
	}

	mergeStart := 7

	if lay.trailing >= 3 && hasEntries {
		mergeStart = 7 - lay.trailing
	}

	for colIdx := 1; colIdx < 7; colIdx++ {
		x := math.Round(calPosterGridX + float64(colIdx)*calPosterColW)
		h := lay.dowH + float64(lay.weeks)*lay.rowH

		if colIdx > mergeStart {
			h = lay.dowH + float64(lay.weeks-1)*lay.rowH
		}

		c.Fill(calRect(x, gridTop, 1.0, h), p.gridLine)
	}
}

func drawPosterEmptyBanner(c *calCanvas, spec calPageSpec, lay calPosterLayout, cellsTop float64, p calPosterPalette) {
	if spec.empty == "" {
		return
	}

	cy := cellsTop + float64(lay.weeks)*lay.rowH/2
	bb := calBox{X: float64(calendarOutputWidth)/2 - 232, Y: cy - 40, W: 464, H: 80}

	c.Fill(calRect(bb.X+4, bb.Y+4, bb.W, bb.H), p.strongLine)
	c.Fill(bb.rect(), p.indexBG)
	c.Fill(calRect(bb.X, bb.Y, bb.W, 3.5), p.obiPrimary)
	c.StrokeInsetRect(bb, 1.5, p.strongLine)
	c.textMid(calText{Size: 17.5, Face: calFaceXBold}, spec.empty, bb.cx(), bb.cy()+1, calAlignC, p.ink)
}

func resolvePosterColophonBox(lay calPosterLayout, cellsTop float64) calBox {
	if lay.trailing >= 3 {
		startCol := 7 - lay.trailing

		return calBox{
			X: calPosterGridX + float64(startCol)*calPosterColW + 1.0,
			Y: cellsTop + float64(lay.weeks-1)*lay.rowH,
			W: float64(lay.trailing)*calPosterColW - 1.0,
			H: lay.rowH,
		}
	}

	return calBox{
		X: calPosterGridX,
		Y: cellsTop + float64(lay.weeks)*lay.rowH,
		W: calPosterGridW,
		H: lay.extraIdxH,
	}
}
