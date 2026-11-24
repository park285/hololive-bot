package render

import (
	"fmt"
	"image/color"
	"strconv"
	"time"
)

func drawCalendarMonthGrid(c *calCanvas, spec calPageSpec, lay calBoardLayout, byDay map[int][]calEntry, th calTheme) float64 {
	gridTop := lay.headTop + lay.headH + lay.horizonGap + lay.dowH + lay.gridTopGap

	for wk := range lay.weeks {
		cy := gridTop + float64(wk)*(lay.rowH+calGridCellGap)

		for colIdx := range 7 {
			day := wk*7 + colIdx - lay.firstWD + 1
			cb := calBox{
				X: calGridPadX + float64(colIdx)*(calGridCellW+calGridCellGap),
				Y: cy,
				W: calGridCellW,
				H: lay.rowH,
			}

			if day > lay.daysInMonth && wk == lay.weeks-1 && lay.trailing >= 3 && len(spec.entries) > 0 {
				continue
			}

			if day < 1 || day > lay.daysInMonth {
				c.Fill(cb.rrect(14), th.emptySlot)

				continue
			}

			list := byDay[day]
			if len(list) == 0 {
				drawQuietDayCell(c, cb, day, colIdx, th)

				continue
			}

			drawActiveDayCell(c, cb, day, colIdx, list, spec.isBday, th)
		}
	}

	return gridTop + float64(lay.weeks)*lay.rowH + float64(lay.weeks-1)*calGridCellGap
}

func drawQuietDayCell(c *calCanvas, cb calBox, day, colIdx int, th calTheme) {
	qFill := th.quietFill
	nCol := th.quietNum

	switch colIdx {
	case 0:
		qFill = th.quietSun
		nCol = calColAlpha(th.dowSunFG, 0.76)
	case 6:
		qFill = th.quietSat
		nCol = calColAlpha(th.dowSatFG, 0.76)
	}

	c.Fill(cb.rrect(14), qFill)
	c.Stroke(cb.rrect(14), th.quietStrokeW, th.quietBorder)
	c.textMid(calText{Size: 15.5, Face: calFaceBold}, strconv.Itoa(day), cb.X+13, cb.Y+19, calAlignL, nCol)
}

func drawActiveDayCell(c *calCanvas, cb calBox, day, colIdx int, list []calEntry, isBday bool, th calTheme) {
	if len(list) == 0 {
		return
	}

	sh := cb.rrect(15)
	e0 := list[0]
	mp := calMemberPalette(e0.hue, e0.chromatic, th)

	c.Shadow(sh, 0, 6, 14, th.cardShadow)
	c.FillLinear(sh, cb.X, cb.Y, cb.X, cb.bottom(), calStop{T: 0, C: th.cardFill0}, calStop{T: 1, C: th.cardFill1})

	scale := min(1.0, cb.H/172.0)
	plateH := 48.0 * scale
	plateTop := cb.bottom() - plateH

	c.Clip(sh, 1, func(l *calCanvas) {
		auraBottom := cb.Y + 88*scale
		l.FillLinear(calRect(cb.X, cb.Y, cb.W, auraBottom-cb.Y), cb.X, cb.Y, cb.X, auraBottom,
			calStop{T: 0, C: mp.aura},
			calStop{T: 1, C: th.cardFill0})
		l.Fill(calRect(cb.X, plateTop, cb.W, plateH), th.cardPlate)
		l.Fill(calRect(cb.X+10, plateTop, cb.W-20, 1.2), th.plateSep)
		l.Fill(calRect(cb.X, cb.Y, cb.W, 3.8), mp.bar)
	})

	c.Stroke(sh, 1.8, mp.border)

	numCol := th.ink

	switch colIdx {
	case 0:
		numCol = th.dowSunFG
	case 6:
		numCol = th.dowSatFG
	}

	c.textMid(calText{Size: 16.5 * scale, Face: calFaceXBold}, strconv.Itoa(day), cb.X+11, cb.Y+17*scale, calAlignL, numCol)

	if len(list) == 1 {
		drawSingleMemberStage(c, cb, plateTop, scale, e0, isBday, mp, th)

		return
	}

	drawMultiMemberStage(c, cb, plateTop, scale, list, isBday, th)
}

func drawSingleMemberStage(c *calCanvas, cb calBox, plateTop, scale float64, e calEntry, isBday bool, mp calMemberColors, th calTheme) {
	if !isBday {
		drawCellBadge(c, cb.right()-8, cb.Y+7.5*scale, 19*scale, fmt.Sprintf("%d주년", e.ordinal), th)
	} else {
		c.Fill(calSparkle(cb.right()-14, cb.Y+17*scale, 6.0*scale), mp.bar)
	}

	stageTop := cb.Y + 26.5*scale
	stageCY := (stageTop + plateTop) / 2
	r := min(36.5*scale, (plateTop-stageTop)/2-7.0)
	drawHeroPortrait(c, cb.cx(), stageCY, r, e, mp)

	nt := calText{Size: 16.5 * scale, Face: calFaceXBold, Track: -0.3}

	if e.unit != "" {
		c.textMid(nt, c.f.clamp(nt, e.shortName, cb.W-12), cb.cx(), plateTop+16*scale, calAlignC, th.ink)

		ut := calText{Size: 10.8 * scale, Face: calFaceBold}
		c.textMid(ut, c.f.clamp(ut, e.unit, cb.W-12), cb.cx(), plateTop+34*scale, calAlignC, th.subInk)

		return
	}

	c.textMid(nt, c.f.clamp(nt, e.shortName, cb.W-12), cb.cx(), (plateTop+cb.bottom())/2, calAlignC, th.ink)
}

func drawMultiMemberStage(c *calCanvas, cb calBox, plateTop, scale float64, list []calEntry, isBday bool, th calTheme) {
	drawCellBadge(c, cb.right()-8, cb.Y+7.5*scale, 19*scale, fmt.Sprintf("%d명", len(list)), th)
	drawMultiMemberPortraits(c, cb, (cb.Y+27*scale+plateTop)/2, scale, list, th)
	drawMultiMemberLabels(c, cb, plateTop, scale, list, isBday, th)
}

func drawMultiMemberPortraits(c *calCanvas, cb calBox, stageCY, scale float64, list []calEntry, th calTheme) {
	switch len(list) {
	case 0, 1:
		return
	case 2:
		r := 21.5 * scale
		sp := 36.0 * scale
		drawCutoutPortrait(c, cb.cx()-sp/2, stageCY, r, list[0], th)
		drawCutoutPortrait(c, cb.cx()+sp/2, stageCY, r, list[1], th)
	case 3:
		r := 18.0 * scale
		sp := 29.0 * scale

		for i := range 3 {
			drawCutoutPortrait(c, cb.cx()+float64(i-1)*sp, stageCY, r, list[i], th)
		}
	default:
		shown := min(5, len(list))
		topN := 3

		if len(list) == 4 {
			topN = 2
		}

		botN := shown - topN
		r := 15.0 * scale
		spTop := 28.5 * scale
		yTop := stageCY - 14.5*scale
		yBot := stageCY + 15.5*scale
		sTop := cb.cx() - float64(topN-1)*spTop/2

		for i := range topN {
			drawCutoutPortrait(c, sTop+float64(i)*spTop, yTop, r, list[i], th)
		}

		spBot := 30.0 * scale
		sBot := cb.cx() - float64(botN-1)*spBot/2

		for i := range botN {
			cx := sBot + float64(i)*spBot
			if i == botN-1 && len(list) > 5 {
				c.Fill(calCircle(cx, yBot, r+2.4), th.cutoutInner)
				c.Fill(calCircle(cx, yBot, r), th.accent)
				c.textMid(calText{Size: 10.5 * scale, Face: calFaceXBold}, fmt.Sprintf("+%d", len(list)-4), cx, yBot, calAlignC, calHex(0xFFFFFF))

				continue
			}

			drawCutoutPortrait(c, cx, yBot, r, list[topN+i], th)
		}
	}
}

func sharedGroupInfo(list []calEntry) (sharedUnit string, sharedOrdinal int) {
	if len(list) == 0 {
		return "", 0
	}

	sharedUnit = list[0].unit
	sharedOrdinal = list[0].ordinal

	for _, item := range list[1:] {
		if item.unit != sharedUnit {
			sharedUnit = ""
		}

		if item.ordinal != sharedOrdinal {
			sharedOrdinal = 0
		}
	}

	return sharedUnit, sharedOrdinal
}

func drawMultiMemberLabels(c *calCanvas, cb calBox, plateTop, scale float64, list []calEntry, isBday bool, th calTheme) {
	if len(list) == 0 {
		return
	}

	unitLbl, sharedOrd := sharedGroupInfo(list)
	if unitLbl == "" {
		unitLbl = list[0].shortName + " 외"
	}

	ut := calText{Size: 13.5 * scale, Face: calFaceXBold}
	c.textMid(ut, c.f.clamp(ut, unitLbl, cb.W-12), cb.cx(), plateTop+16*scale, calAlignC, th.ink)

	subInfo := "생일"

	if !isBday {
		if sharedOrd > 0 {
			subInfo = fmt.Sprintf("데뷔 %d주년", sharedOrd)
		} else {
			subInfo = "데뷔 기념일"
		}
	}

	c.textMid(calText{Size: 11 * scale, Face: calFaceXBold}, subInfo, cb.cx(), plateTop+34*scale, calAlignC, th.accent)
}

func drawCellBadge(c *calCanvas, rightX, topY, h float64, tag string, th calTheme) {
	bt := calText{Size: 10.6 * (h / 19.0), Face: calFaceXBold}
	bw := c.f.width(bt, tag) + 14
	bx := rightX - bw
	pill := calRRect(bx, topY, bw, h, h/2)

	c.Fill(pill, th.cellBadgeBG)
	c.Stroke(pill, 1.15, th.cellBadgeLine)
	c.textMid(bt, tag, bx+bw/2, topY+h/2, calAlignC, th.cellBadgeFG)
}

func drawHeroPortrait(c *calCanvas, cx, cy, r float64, e calEntry, mp calMemberColors) {
	c.Shadow(calCircle(cx, cy, r+3.8), 0, 3, 6, calRGBA(0x142038, 0.14))
	c.Fill(calCircle(cx, cy, r+4.0), mp.ring)
	c.Fill(calCircle(cx, cy, r+1.8), calHex(0xFFFFFF))
	drawPortraitFill(c, cx, cy, r, e, mp.ring)
}

func drawCutoutPortrait(c *calCanvas, cx, cy, r float64, e calEntry, th calTheme) {
	c.Shadow(calCircle(cx, cy, r+2.2), 0, 2, 4, th.cutoutOuter)
	c.Fill(calCircle(cx, cy, r+2.4), th.cutoutInner)

	mp := calMemberPalette(e.hue, e.chromatic, th)
	drawPortraitFill(c, cx, cy, r, e, mp.ring)
}

func drawPortraitFill(c *calCanvas, cx, cy, r float64, e calEntry, fallbackCol color.RGBA) {
	if e.photo != nil {
		c.Photo(e.photo, calCircle(cx, cy, r), calBox{X: cx - r, Y: cy - r, W: 2 * r, H: 2 * r})

		return
	}

	c.Fill(calCircle(cx, cy, r), fallbackCol)

	runes := []rune(e.shortName)
	ini := "?"

	if len(runes) > 0 {
		ini = string(runes[0])
	}

	c.textMid(calText{Size: r * 0.84, Face: calFaceXBold}, ini, cx, cy, calAlignC, calHex(0xFFFFFF))
}

func drawCalendarEmptyBanner(c *calCanvas, spec calPageSpec, lay calBoardLayout, th calTheme) {
	if spec.empty == "" {
		return
	}

	gridTop := lay.headTop + lay.headH + lay.horizonGap + lay.dowH + lay.gridTopGap
	gridH := float64(lay.weeks)*lay.rowH + float64(lay.weeks-1)*calGridCellGap
	cy := gridTop + gridH/2
	bw, bh := 460.0, 84.0
	bb := calBox{X: float64(calendarOutputWidth)/2 - bw/2, Y: cy - bh/2, W: bw, H: bh}
	sh := bb.rrect(20)

	c.Shadow(sh, 0, 6, 16, th.cardShadow)
	c.Fill(sh, th.indexFill)
	c.Stroke(sh, 1.6, th.indexBorder)
	c.textMid(calText{Size: 18, Face: calFaceXBold}, spec.empty, bb.cx(), bb.cy(), calAlignC, th.ink)
}

func calDowKO(year, month, day int) string {
	wd := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC).Weekday()
	ko := [...]string{"일", "월", "화", "수", "목", "금", "토"}

	return ko[wd]
}

func resolveIndexBox(lay calBoardLayout, gridBottom float64) (calBox, float64) {
	if lay.trailing >= 3 {
		startCol := 7 - lay.trailing
		mb := calBox{
			X: calGridPadX + float64(startCol)*(calGridCellW+calGridCellGap),
			Y: gridBottom - lay.rowH,
			W: float64(lay.trailing)*calGridCellW + float64(lay.trailing-1)*calGridCellGap,
			H: lay.rowH,
		}

		return mb, gridBottom
	}

	mb := calBox{
		X: calGridPadX,
		Y: gridBottom + lay.detailGap,
		W: float64(calendarOutputWidth) - calGridPadX*2,
		H: lay.extraIndexH - lay.detailGap,
	}

	return mb, mb.bottom()
}

func drawCalendarMonthIndex(c *calCanvas, spec calPageSpec, lay calBoardLayout, gridBottom float64, th calTheme) float64 {
	mb, nextBottom := resolveIndexBox(lay, gridBottom)
	drawIndexCardChrome(c, mb, spec, th)

	cols := 2

	if len(spec.entries) > 6 || lay.trailing < 3 {
		cols = 3
	}

	maxSlots := cols * 5
	shown := spec.entries
	overflow := 0

	if len(shown) > maxSlots {
		overflow = len(shown) - (maxSlots - 1)
		shown = shown[:maxSlots-1]
	}

	totalItems := len(shown)

	if overflow > 0 {
		totalItems++
	}

	colGap := 22.0
	rowsPerCol := (totalItems + cols - 1) / cols
	colW := (mb.W - 36 - float64(cols-1)*colGap) / float64(cols)
	bodyTop := mb.Y + 38
	bodyAvail := mb.H - 46
	rH := min(28.0, bodyAvail/float64(max(1, rowsPerCol)))
	startY := bodyTop + (bodyAvail-float64(rowsPerCol)*rH)/2 + rH/2

	for col := 1; col < cols; col++ {
		vx := mb.X + 18 + float64(col)*colW + (float64(col)-0.5)*colGap
		c.Fill(calRect(vx, bodyTop+6, 1.0, max(8, bodyAvail-12)), th.quietBorder)
	}

	compactDate := cols == 3 && mb.W < 760

	for i := range totalItems {
		col := i / rowsPerCol
		row := i % rowsPerCol

		if col >= cols {
			break
		}

		ix := mb.X + 18 + float64(col)*(colW+colGap)
		iy := startY + float64(row)*rH

		if i == len(shown) && overflow > 0 {
			c.textMid(calText{Size: 12.5, Face: calFaceBold}, fmt.Sprintf("외 %d명", overflow), ix+12, iy, calAlignL, th.subInk)

			continue
		}

		drawIndexEntryRow(c, spec, shown[i], ix, iy, colW, compactDate, th)
	}

	return nextBottom
}

func drawIndexCardChrome(c *calCanvas, mb calBox, spec calPageSpec, th calTheme) {
	mSh := mb.rrect(15)
	c.Shadow(mSh, 0, 5, 13, th.indexShadow)
	c.Fill(mSh, th.indexFill)
	c.Clip(mSh, 1, func(l *calCanvas) {
		l.Fill(calRect(mb.X, mb.Y, mb.W, 38), calColAlpha(th.accent, 0.10))
		l.FillLinear(calRect(mb.X, mb.Y, mb.W, 3.4), mb.X, mb.Y, mb.right(), mb.Y,
			calStop{T: 0, C: th.accent},
			calStop{T: 1, C: th.indexBarEnd})
	})
	c.Stroke(mSh, 1.5, th.indexBorder)

	idxTitle := fmt.Sprintf("%d월 생일 목록", spec.month)
	if !spec.isBday {
		idxTitle = fmt.Sprintf("%d월 데뷔 기념일 목록", spec.month)
	}

	c.textMid(calText{Size: 14.2, Face: calFaceXBold}, idxTitle, mb.X+18, mb.Y+20, calAlignL, th.ink)
	c.textMid(calText{Size: 12.5, Face: calFaceBold}, fmt.Sprintf("총 %d명", len(spec.entries)), mb.right()-18, mb.Y+20, calAlignR, th.subInk)
}

func drawIndexEntryRow(c *calCanvas, spec calPageSpec, e calEntry, ix, iy, colW float64, compactDate bool, th calTheme) {
	mp := calMemberPalette(e.hue, e.chromatic, th)
	c.Fill(calCircle(ix+3.5, iy, 3.3), mp.dot)

	dateStr := fmt.Sprintf("%02d.%02d %s", spec.month, e.day, calDowKO(spec.year, spec.month, e.day))
	dateW := 60.0

	if compactDate {
		dateStr = fmt.Sprintf("%02d일", e.day)
		dateW = 34.0
	}

	c.textMid(calText{Size: 12.2, Face: calFaceBold}, dateStr, ix+12, iy, calAlignL, th.subInk)

	nameX := ix + 12 + dateW

	if spec.isBday {
		nt := calText{Size: 13.5, Face: calFaceXBold}
		nClamped := c.f.clamp(nt, e.shortName, ix+colW-nameX)
		c.textMid(nt, nClamped, nameX, iy, calAlignL, th.ink)

		remW := ix + colW - (nameX + c.f.width(nt, nClamped) + 8)

		if remW > 28 && e.unit != "" {
			ut := calText{Size: 11.5, Face: calFaceRegular}
			c.textMid(ut, c.f.clamp(ut, e.unit, remW), ix+colW, iy, calAlignR, th.subInk)
		}

		return
	}

	ordStr := fmt.Sprintf("%d주년", e.ordinal)
	ot := calText{Size: 11.8, Face: calFaceXBold}
	ow := c.f.width(ot, ordStr)

	c.textMid(ot, ordStr, ix+colW, iy, calAlignR, th.accent)

	nt := calText{Size: 13.2, Face: calFaceXBold}
	c.textMid(nt, c.f.clamp(nt, e.shortName, ix+colW-ow-6-nameX), nameX, iy, calAlignL, th.ink)
}

func drawCalendarMultiDayCards(c *calCanvas, spec calPageSpec, lay calBoardLayout, byDay map[int][]calEntry, startTop float64, th calTheme) {
	if len(lay.multiDays) == 0 {
		return
	}

	sy := startTop + lay.detailGap
	scale := min(1.0, lay.detailCardH/158.0)

	for _, d := range lay.multiDays {
		list := byDay[d]
		if len(list) == 0 {
			continue
		}

		sb := calBox{
			X: calGridPadX,
			Y: sy,
			W: float64(calendarOutputWidth) - calGridPadX*2,
			H: lay.detailCardH,
		}
		drawSingleMultiDayCard(c, sb, scale, spec, d, list, th)

		sy += lay.detailCardH + lay.detailGap
	}
}

func drawSingleMultiDayCard(c *calCanvas, sb calBox, scale float64, spec calPageSpec, day int, list []calEntry, th calTheme) {
	if len(list) == 0 {
		return
	}

	sSh := sb.rrect(16)

	c.Shadow(sSh, 0, 5, 13, th.cardShadow)
	c.FillLinear(sSh, sb.X, sb.Y, sb.X, sb.bottom(), calStop{T: 0, C: th.cardFill0}, calStop{T: 1, C: th.cardFill1})
	c.Clip(sSh, 1, func(l *calCanvas) {
		l.Fill(calRect(sb.X, sb.Y, sb.W, 38*scale), calColAlpha(th.accent, 0.11))
		l.FillLinear(calRect(sb.X, sb.Y, sb.W, 3.6), sb.X, sb.Y, sb.right(), sb.Y,
			calStop{T: 0, C: th.accent},
			calStop{T: 1, C: th.indexBarEnd})
	})
	c.Stroke(sSh, 1.6, th.indexBorder)

	sharedUnit, sharedOrd := sharedGroupInfo(list)
	headTitle := fmt.Sprintf("%d월 %d일 (%s)", spec.month, day, calDowKO(spec.year, spec.month, day))

	if sharedUnit != "" {
		headTitle += " · " + sharedUnit
	}

	c.textMid(calText{Size: 15.2 * scale, Face: calFaceXBold}, headTitle, sb.X+18, sb.Y+19*scale, calAlignL, th.ink)

	rightTag := fmt.Sprintf("생일 · %d명", len(list))

	if !spec.isBday {
		if sharedOrd > 0 {
			rightTag = fmt.Sprintf("데뷔 %d주년 · %d명", sharedOrd, len(list))
		} else {
			rightTag = fmt.Sprintf("데뷔 기념일 · %d명", len(list))
		}
	}

	drawCellBadge(c, sb.right()-16, sb.Y+9*scale, 21*scale, rightTag, th)

	slotW := (sb.W - 24) / float64(len(list))
	ix := sb.X + 12
	nameT := calText{Size: 14.8 * scale, Face: calFaceXBold}
	fullT := calText{Size: 11 * scale, Face: calFaceRegular}

	for i, e := range list {
		if i > 0 {
			c.Fill(calRect(ix+float64(i)*slotW, sb.Y+50*scale, 1.0, sb.H-64*scale), th.plateSep)
		}

		scx := ix + float64(i)*slotW + slotW/2
		mp := calMemberPalette(e.hue, e.chromatic, th)
		drawHeroPortrait(c, scx, sb.Y+82*scale, 29.5*scale, e, mp)
		c.textMid(nameT, c.f.clamp(nameT, e.shortName, slotW-12), scx, sb.Y+127*scale, calAlignC, th.ink)
		c.textMid(fullT, c.f.clamp(fullT, e.fullName, slotW-12), scx, sb.Y+144*scale, calAlignC, th.subInk)
	}
}
