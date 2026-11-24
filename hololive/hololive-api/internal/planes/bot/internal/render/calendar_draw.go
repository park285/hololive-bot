package render

import (
	"cmp"
	"context"
	"fmt"
	"image"
	"math"
	"slices"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

const (
	calGridPadX    = 40.0
	calGridCellW   = 128.0
	calGridCellGap = 8.0
	calSupersample = 2.0
)

type calEntry struct {
	day       int
	kind      domain.CelebrationKind
	shortName string
	fullName  string
	unit      string
	ordinal   int
	photo     image.Image
	hue       float64
	chromatic bool
}

type calPageSpec struct {
	year    int
	month   int
	themeID string
	isBday  bool
	entries []calEntry
	empty   string
}

type calBoardLayout struct {
	headTop     float64
	headH       float64
	horizonGap  float64
	dowH        float64
	gridTopGap  float64
	rowH        float64
	extraIndexH float64
	detailCardH float64
	detailGap   float64
	footH       float64
	weeks       int
	firstWD     int
	daysInMonth int
	trailing    int
	multiDays   []int
	totalH      int
}

type calHueSample struct {
	hue       float64
	chromatic bool
}

func buildCalEntries(ctx context.Context, stringsStore *messagestrings.Store, entries []domain.CalendarEntry, photos map[string]image.Image) []calEntry {
	m := newCalendarMetrics(1)

	m.strings = stringsStore

	unknown := cmp.Or(m.unknownName(ctx), "알 수 없음")
	hueCache := make(map[string]calHueSample)
	out := make([]calEntry, 0, len(entries))

	for _, e := range entries {
		out = append(out, buildSingleCalEntry(e, unknown, photos, hueCache))
	}

	return out
}

func buildSingleCalEntry(e domain.CalendarEntry, unknown string, photos map[string]image.Image, hueCache map[string]calHueSample) calEntry {
	ce := calEntry{
		day:       e.Day,
		kind:      e.Kind,
		shortName: unknown,
		fullName:  unknown,
		ordinal:   e.Ordinal,
	}

	if e.Member == nil {
		return ce
	}

	ce.shortName = cmp.Or(e.Member.ShortKoreanName, e.Member.NameKo, e.Member.Name, unknown)
	ce.fullName = cmp.Or(e.Member.NameKo, e.Member.ShortKoreanName, e.Member.Name, unknown)

	if len(e.Member.Units) > 0 {
		ce.unit = e.Member.Units[0]
	}

	if e.Member.Photo == "" {
		return ce
	}

	ce.photo = photos[e.Member.Photo]
	if ce.photo == nil {
		return ce
	}

	cached, ok := hueCache[e.Member.Photo]
	if !ok {
		cached.hue, cached.chromatic = calDominantHue(ce.photo)
		hueCache[e.Member.Photo] = cached
	}

	ce.hue = cached.hue
	ce.chromatic = cached.chromatic

	return ce
}

func (l *calBoardLayout) updateHeight(totalEntries int) {
	gridH := float64(l.weeks)*l.rowH + float64(l.weeks-1)*calGridCellGap

	l.extraIndexH = 0

	if l.trailing < 3 && totalEntries > 0 {
		shown := min(totalEntries, 15)
		rows := (shown + 2) / 3

		l.extraIndexH = l.detailGap + 40 + float64(rows)*26 + 12
	}

	detailTotal := 0.0

	if len(l.multiDays) > 0 {
		detailTotal = float64(len(l.multiDays)) * (l.detailCardH + l.detailGap)
	}

	l.totalH = int(math.Ceil(l.headTop + l.headH + l.horizonGap + l.dowH + l.gridTopGap + gridH + l.extraIndexH + detailTotal + l.footH))
}

func computeCalBoardLayout(year, month int, byDay map[int][]calEntry, totalEntries int) calBoardLayout {
	first := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	firstWD := int(first.Weekday())
	daysInMonth := first.AddDate(0, 1, -1).Day()
	weeks := (firstWD + daysInMonth + 6) / 7
	trailing := weeks*7 - (firstWD + daysInMonth)

	var candMulti []int

	for d, list := range byDay {
		if len(list) >= 2 && len(list) <= 6 {
			candMulti = append(candMulti, d)
		}
	}

	slices.Sort(candMulti)

	var multiDays []int

	if len(candMulti) <= 2 {
		multiDays = candMulti
	}

	lay := calBoardLayout{
		headTop: 28, headH: 144, horizonGap: 18, dowH: 34, gridTopGap: 10,
		rowH: 172, detailCardH: 158, detailGap: 14, footH: 44,
		weeks: weeks, firstWD: firstWD, daysInMonth: daysInMonth,
		trailing: trailing, multiDays: multiDays,
	}

	lay.updateHeight(totalEntries)
	tightenCalBoardLayout(&lay, totalEntries)

	return lay
}

func tightenCalBoardLayout(lay *calBoardLayout, totalEntries int) {
	if lay.totalH > calendarOutputHeight && len(lay.multiDays) > 1 {
		lay.multiDays = lay.multiDays[:1]
		lay.updateHeight(totalEntries)
	}

	if lay.totalH > calendarOutputHeight {
		lay.headTop = 22
		lay.headH = 128
		lay.horizonGap = 14
		lay.dowH = 30
		lay.gridTopGap = 8
		lay.detailCardH = 144
		lay.detailGap = 10
		lay.footH = 38
		lay.updateHeight(totalEntries)
	}

	if lay.totalH > calendarOutputHeight {
		excess := float64(lay.totalH - calendarOutputHeight)

		lay.rowH = max(136, lay.rowH-math.Ceil(excess/float64(lay.weeks)))
		lay.updateHeight(totalEntries)
	}

	if lay.totalH > calendarOutputHeight && len(lay.multiDays) > 0 {
		lay.multiDays = nil
		lay.updateHeight(totalEntries)
	}

	lay.totalH = min(lay.totalH, calendarOutputHeight)
}

func renderCalendarPagePNG(fs *calFontSet, spec calPageSpec) ([]byte, error) {
	th := buildCalendarTheme(spec.themeID, spec.isBday)
	byDay := make(map[int][]calEntry)

	for _, e := range spec.entries {
		if e.day >= 1 && e.day <= 31 {
			byDay[e.day] = append(byDay[e.day], e)
		}
	}

	lay := computeCalBoardLayout(spec.year, spec.month, byDay, len(spec.entries))
	c := newCalCanvas(calendarOutputWidth, lay.totalH, calSupersample, fs)

	paintCalendarBackdrop(c, th)
	drawCalendarPageHeader(c, spec, lay, th)
	drawCalendarWeekdays(c, lay, th)

	gridBottom := drawCalendarMonthGrid(c, spec, lay, byDay, th)

	if len(spec.entries) == 0 {
		drawCalendarEmptyBanner(c, spec, lay, th)
	} else {
		gridBottom = drawCalendarMonthIndex(c, spec, lay, gridBottom, th)
		drawCalendarMultiDayCards(c, spec, lay, byDay, gridBottom, th)
	}

	drawCalendarPageFooter(c, spec, lay, th)

	return c.EncodePNG(calendarOutputWidth, lay.totalH)
}

func drawCalendarPageHeader(c *calCanvas, spec calPageSpec, lay calBoardLayout, th calTheme) {
	w := float64(calendarOutputWidth)
	hb := calBox{X: calGridPadX, Y: lay.headTop, W: w - calGridPadX*2, H: lay.headH}
	numScale := min(1.0, lay.headH/144.0)

	inkTop, inkRight, inkBot := drawHeaderNumeral(c, hb, spec.month, numScale, th)
	divX := inkRight + 22
	c.Fill(calRRect(divX, inkTop+1, 3.2, max(24, inkBot-inkTop-2), 1.6), calColAlpha(th.accent, 0.46))

	drawHeaderLockup(c, spec, divX+22, inkTop, inkBot, numScale, th)
	drawCalendarPrismEmblem(c, hb.right(), hb.cy(), th)

	hy := lay.headTop + lay.headH + 3
	c.Fill(calRect(calGridPadX+8, hy-0.8, w-calGridPadX*2-16, 1.5), th.horizonRule)

	finialCol := th.accent
	if th.dark {
		finialCol = th.goldAccent
	}

	c.Fill(calSparkle(calGridPadX, hy, 6.2), finialCol)
	c.Fill(calSparkle(w-calGridPadX, hy, 6.2), finialCol)
}

func drawHeaderNumeral(c *calCanvas, hb calBox, month int, numScale float64, th calTheme) (inkTop, inkRight, inkBot float64) {
	numStr := fmt.Sprintf("%02d", month)
	numT := calText{Size: 104 * numScale, Face: calFaceXBold, Track: -3.0 * numScale}
	rawNum := c.textShape(numT, numStr, 0, 0, calAlignL)
	nx0, ny0, _, ny1 := rawNum.Bounds()

	ns := calTransform(rawNum, calMove(hb.X-nx0, hb.cy()-(ny0+ny1)/2))
	inkLeft, inkTop, inkRight, inkBot := ns.Bounds()

	if th.dark {
		c.Glow((inkLeft+inkRight)/2, hb.cy(), 82*numScale, calRGB24(th.accent), 0.24)
		c.Fill(calTransform(ns, calMove(0, 4)), calRGBA(0x08061D, 0.55))
		c.FillLinear(ns, inkLeft, inkTop, inkLeft, inkBot,
			calStop{T: 0, C: calHex(0xFFFFFF)},
			calStop{T: 1, C: calMix(calHex(0xFFFFFF), th.accent, 0.34)})

		return inkTop, inkRight, inkBot
	}

	c.Fill(calTransform(ns, calMove(3, 4)), calColAlpha(th.accent, 0.16))
	c.FillLinear(ns, inkLeft, inkTop, inkLeft, inkBot,
		calStop{T: 0, C: th.accent},
		calStop{T: 1, C: th.accentDeep})

	return inkTop, inkRight, inkBot
}

func drawHeaderLockup(c *calCanvas, spec calPageSpec, tx, inkTop, inkBot, numScale float64, th calTheme) {
	kickerT := calText{Size: 15.5 * numScale, Face: calFaceBold, Track: 0.6}
	kickerStr := fmt.Sprintf("%04d.%02d", spec.year, spec.month)
	pillH := 25.0 * numScale
	pillCY := inkTop + pillH/2 + 1
	c.textMid(kickerT, kickerStr, tx, pillCY, calAlignL, th.subInk)

	countLabel := fmt.Sprintf("생일 %d명", len(spec.entries))
	mainTitle := fmt.Sprintf("%d월 생일 달력", spec.month)

	if !spec.isBday {
		countLabel = fmt.Sprintf("데뷔 기념일 %d명", len(spec.entries))
		mainTitle = fmt.Sprintf("%d월 데뷔 기념일 달력", spec.month)
	}

	kw := c.f.width(kickerT, kickerStr)
	pillT := calText{Size: 13.2 * numScale, Face: calFaceXBold}
	pillW := c.f.width(pillT, countLabel) + 26
	pillX := tx + kw + 12
	pillShape := calRRect(pillX, pillCY-pillH/2, pillW, pillH, pillH/2)

	c.Fill(pillShape, th.headBadgeBG)
	c.Stroke(pillShape, 1.2, th.headBadgeLine)
	c.textMid(pillT, countLabel, pillX+pillW/2, pillCY, calAlignC, th.headBadgeFG)

	titleT := calText{Size: 42 * numScale, Face: calFaceXBold, Track: -0.9}
	rawTitle := c.textShape(titleT, mainTitle, tx, 0, calAlignL)
	_, _, _, ty1 := rawTitle.Bounds()
	c.Fill(calTransform(rawTitle, calMove(0, inkBot-ty1)), th.ink)
}

func drawCalendarWeekdays(c *calCanvas, lay calBoardLayout, th calTheme) {
	dowTop := lay.headTop + lay.headH + lay.horizonGap
	dows := [7]string{"일", "월", "화", "수", "목", "금", "토"}

	for i, label := range dows {
		dx := calGridPadX + float64(i)*(calGridCellW+calGridCellGap)
		db := calBox{X: dx, Y: dowTop, W: calGridCellW, H: lay.dowH}
		bg, fg := th.dowBG, th.dowFG

		switch i {
		case 0:
			bg, fg = th.dowSunBG, th.dowSunFG
		case 6:
			bg, fg = th.dowSatBG, th.dowSatFG
		}

		c.Fill(db.rrect(10), bg)
		c.textMid(calText{Size: 14.5, Face: calFaceXBold}, label, db.cx(), db.cy(), calAlignC, fg)
	}
}

func drawCalendarPageFooter(c *calCanvas, spec calPageSpec, lay calBoardLayout, th calTheme) {
	w := float64(calendarOutputWidth)
	fy := float64(lay.totalH) - 18
	finialCol := th.accent

	if th.dark {
		finialCol = th.goldAccent
	}

	c.Fill(calRect(calGridPadX, fy-14, w-calGridPadX*2-14, 1.2), calColAlpha(th.horizonRule, 0.78))
	c.Fill(calSparkle(w-calGridPadX, fy-13.4, 4.6), finialCol)
	c.textMid(calText{Size: 12, Face: calFaceBold}, "한국 표준시(KST) 기준", calGridPadX, fy, calAlignL, th.muted)
	c.textMid(calText{Size: 12, Face: calFaceBold, Track: 0.3}, fmt.Sprintf("%d년 %d월", spec.year, spec.month), w-calGridPadX, fy, calAlignR, th.muted)
}
