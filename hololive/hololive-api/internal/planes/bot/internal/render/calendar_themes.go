package render

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	xdraw "golang.org/x/image/draw"
)

const (
	CalendarThemePrismDaylight  = "prism_daylight"
	CalendarThemeStarlightOrbit = "starlight_orbit"
)

var calendarThemePool = []string{
	CalendarThemePrismDaylight,
	CalendarThemeStarlightOrbit,
}

type calTheme struct {
	skyTop, skyBot color.RGBA
	bloomA, bloomB color.RGBA
	nebulaBand     color.RGBA
	starDim        color.RGBA
	starBri        color.RGBA
	limbGlow       color.RGBA
	limbRim        color.RGBA
	limbSub        color.RGBA

	accent      color.RGBA
	accentSoft  color.RGBA
	accentDeep  color.RGBA
	goldAccent  color.RGBA
	prismA      color.RGBA
	prismB      color.RGBA
	prismC      color.RGBA
	horizonRule color.RGBA

	dowBG, dowFG       color.RGBA
	dowSatBG, dowSatFG color.RGBA
	dowSunBG, dowSunFG color.RGBA

	quietFill    color.RGBA
	quietSat     color.RGBA
	quietSun     color.RGBA
	quietBorder  color.RGBA
	quietStrokeW float64
	quietNum     color.RGBA
	emptySlot    color.RGBA

	cardFill0     color.RGBA
	cardFill1     color.RGBA
	cardPlate     color.RGBA
	plateSep      color.RGBA
	cardBorder    color.RGBA
	cardShadow    color.RGBA
	cutoutOuter   color.RGBA
	cutoutInner   color.RGBA
	headBadgeBG   color.RGBA
	headBadgeLine color.RGBA
	headBadgeFG   color.RGBA
	cellBadgeBG   color.RGBA
	cellBadgeLine color.RGBA
	cellBadgeFG   color.RGBA

	indexFill   color.RGBA
	indexBorder color.RGBA
	indexShadow color.RGBA
	indexBarEnd color.RGBA

	ink, subInk, muted color.RGBA
	dark               bool
}

type calLightSeed struct {
	skyTop, skyBot, bloomA, bloomB, band uint32
	accent, accentSoft, accentDeep, gold uint32
	pA, pB, pC                           color.RGBA
	horizon, dowBG, dowFG, satBG, satFG  uint32
	sunBG, quietBorder, quietNum, empty  uint32
	plate, plateSep, cardBorder, shadow  uint32
	shadowA, indexShadowA, quietSatA     float64
	hBadgeBG, hBadgeLine, hBadgeFG       uint32
	cBadgeBG, cBadgeLine, cBadgeFG       uint32
	indexFill, indexBorder, indexBarEnd  uint32
	ink, subInk, muted                   uint32
}

type calOrbitSeed struct {
	skyTop, skyBot, bloomA, bloomB, band uint32
	starDim, starBri                     uint32
	limbGlow, limbRim, limbSub           color.RGBA
	accent, accentDeep                   uint32
	pA, pB, pC                           color.RGBA
	horizon, dowBG, dowFG, satBG, sunBG  uint32
	quietFill, quietSat, quietSun        uint32
	quietBorder, quietNum, empty         uint32
	card0, card1, plate, plateSep        uint32
	cardBorder, shadow, cutoutOuter      uint32
	shadowA                              float64
	headFG, indexFill, indexBorder       uint32
	indexShadow, subInk, muted           uint32
}

func buildCalendarTheme(themeID string, isBday bool) calTheme {
	if themeID == CalendarThemeStarlightOrbit {
		return buildThemeStarlightOrbit(isBday)
	}

	return buildThemePrismDaylight(isBday)
}

func makeLightTheme(s calLightSeed) calTheme {
	return calTheme{
		skyTop: calHex(s.skyTop), skyBot: calHex(s.skyBot),
		bloomA: calHex(s.bloomA), bloomB: calHex(s.bloomB),
		nebulaBand: calHex(s.band),

		accent: calHex(s.accent), accentSoft: calHex(s.accentSoft), accentDeep: calHex(s.accentDeep),
		goldAccent: calHex(s.gold), prismA: s.pA, prismB: s.pB, prismC: s.pC,
		horizonRule: calHex(s.horizon),

		dowBG: calHex(s.dowBG), dowFG: calHex(s.dowFG),
		dowSatBG: calHex(s.satBG), dowSatFG: calHex(s.satFG),
		dowSunBG: calHex(s.sunBG), dowSunFG: calHex(0xCF325E),

		quietFill: calRGBA(0xFFFFFF, 0.66), quietSat: calRGBA(0xEEF6FF, s.quietSatA), quietSun: calRGBA(0xFFF0F5, 0.72),
		quietBorder: calHex(s.quietBorder), quietStrokeW: 1.35, quietNum: calHex(s.quietNum),
		emptySlot: calRGBA(s.empty, 0.30),

		cardFill0: calHex(0xFFFFFF), cardFill1: calHex(0xFFFFFF),
		cardPlate: calHex(s.plate), plateSep: calHex(s.plateSep),
		cardBorder: calHex(s.cardBorder), cardShadow: calRGBA(s.shadow, s.shadowA),
		cutoutOuter: calRGBA(0x142038, 0.16), cutoutInner: calHex(0xFFFFFF),
		headBadgeBG: calHex(s.hBadgeBG), headBadgeLine: calHex(s.hBadgeLine), headBadgeFG: calHex(s.hBadgeFG),
		cellBadgeBG: calHex(s.cBadgeBG), cellBadgeLine: calHex(s.cBadgeLine), cellBadgeFG: calHex(s.cBadgeFG),

		indexFill: calHex(s.indexFill), indexBorder: calHex(s.indexBorder),
		indexShadow: calRGBA(s.shadow, s.indexShadowA), indexBarEnd: calHex(s.indexBarEnd),

		ink: calHex(s.ink), subInk: calHex(s.subInk), muted: calHex(s.muted),
	}
}

func buildThemePrismDaylight(isBday bool) calTheme {
	if isBday {
		return makeLightTheme(calLightSeed{
			skyTop: 0xFCEEF4, skyBot: 0xEDE0EA, bloomA: 0xFFC9DD, bloomB: 0xFFE2C6, band: 0xF8DCE8,
			accent: 0xE64978, accentSoft: 0xFFE5EE, accentDeep: 0xB82E58, gold: 0xE09224,
			pA: calRGBA(0xFF598C, 0.23), pB: calRGBA(0xFFAE52, 0.21), pC: calRGBA(0x45C4FF, 0.18),
			horizon: 0xE3BDCB, dowBG: 0xEEDCE6, dowFG: 0x4E3948, satBG: 0xDFECFC, satFG: 0x225EC7,
			sunBG: 0xFBD9E4, quietBorder: 0xDFC3D1, quietNum: 0x7E687A, empty: 0xDFC3D1,
			plate: 0xFCF5F8, plateSep: 0xEAD2DE, cardBorder: 0xEE7599, shadow: 0x7A1E42,
			shadowA: 0.16, indexShadowA: 0.11, quietSatA: 0.72,
			hBadgeBG: 0xFFEBF1, hBadgeLine: 0xF29EB8, hBadgeFG: 0xC22E5B,
			cBadgeBG: 0xFFF0F4, cBadgeLine: 0xF2A0B9, cBadgeFG: 0xC22E5B,
			indexFill: 0xFFFCFD, indexBorder: 0xE5A6BE, indexBarEnd: 0xF2845C,
			ink: 0x221724, subInk: 0x695566, muted: 0x947F91,
		})
	}

	return makeLightTheme(calLightSeed{
		skyTop: 0xEAF5FF, skyBot: 0xD8E9FA, bloomA: 0xB8E0FF, bloomB: 0xCFDDFE, band: 0xD8ECFF,
		accent: 0x1195EC, accentSoft: 0xE0F3FF, accentDeep: 0x0D6CB5, gold: 0xDC9018,
		pA: calRGBA(0x27C7FF, 0.25), pB: calRGBA(0x4E9EED, 0.21), pC: calRGBA(0x75E5EE, 0.24),
		horizon: 0xB8D3EE, dowBG: 0xDCEAF8, dowFG: 0x32496B, satBG: 0xD0E6FE, satFG: 0x185BC9,
		sunBG: 0xFADBE5, quietBorder: 0xBDD4EC, quietNum: 0x687F9E, empty: 0xB5CEE8,
		plate: 0xF5FAFF, plateSep: 0xD0E3F5, cardBorder: 0x47B2F8, shadow: 0x0D3B70,
		shadowA: 0.15, indexShadowA: 0.10, quietSatA: 0.74,
		hBadgeBG: 0xFFF4D6, hBadgeLine: 0xEDB54C, hBadgeFG: 0x8F5305,
		cBadgeBG: 0xFFF4D9, cBadgeLine: 0xEDB650, cBadgeFG: 0x8F5305,
		indexFill: 0xFCFDFE, indexBorder: 0x8FC2F2, indexBarEnd: 0x27C7FF,
		ink: 0x14243D, subInk: 0x4E6482, muted: 0x7A90AE,
	})
}

func makeOrbitTheme(s calOrbitSeed) calTheme {
	return calTheme{
		skyTop: calHex(s.skyTop), skyBot: calHex(s.skyBot),
		bloomA: calHex(s.bloomA), bloomB: calHex(s.bloomB),
		nebulaBand: calHex(s.band), starDim: calHex(s.starDim), starBri: calHex(s.starBri),
		limbGlow: s.limbGlow, limbRim: s.limbRim, limbSub: s.limbSub,

		accent: calHex(s.accent), accentSoft: calRGBA(s.accent, 0.19), accentDeep: calHex(s.accentDeep),
		goldAccent: calHex(0xFFF17D), prismA: s.pA, prismB: s.pB, prismC: s.pC,
		horizonRule: calHex(s.horizon),

		dowBG: calRGBA(s.dowBG, 0.85), dowFG: calHex(s.dowFG),
		dowSatBG: calRGBA(s.satBG, 0.85), dowSatFG: calHex(0x92C6FF),
		dowSunBG: calRGBA(s.sunBG, 0.85), dowSunFG: calHex(0xFFA6C6),

		quietFill: calRGBA(s.quietFill, 0.50), quietSat: calRGBA(s.quietSat, 0.52), quietSun: calRGBA(s.quietSun, 0.52),
		quietBorder: calRGBA(s.quietBorder, 0.30), quietStrokeW: 1.25, quietNum: calHex(s.quietNum),
		emptySlot: calRGBA(s.empty, 0.34),

		cardFill0: calHex(s.card0), cardFill1: calHex(s.card1),
		cardPlate: calHex(s.plate), plateSep: calRGBA(s.plateSep, 0.26),
		cardBorder: calHex(s.cardBorder), cardShadow: calRGBA(s.shadow, s.shadowA),
		cutoutOuter: calHex(s.cutoutOuter), cutoutInner: calHex(0xFAF7FF),
		headBadgeBG: calHex(0xFFF17D), headBadgeLine: calHex(0xFFF17D), headBadgeFG: calHex(s.headFG),
		cellBadgeBG: calRGBA(0xFFF17D, 0.15), cellBadgeLine: calRGBA(0xFFF17D, 0.72), cellBadgeFG: calHex(0xFFF396),

		indexFill: calRGBA(s.indexFill, 0.86), indexBorder: calRGBA(s.indexBorder, 0.48),
		indexShadow: calRGBA(s.indexShadow, 0.44), indexBarEnd: calHex(0xFFF17D),

		ink: calHex(0xFAF7FF), subInk: calHex(s.subInk), muted: calHex(s.muted),
		dark: true,
	}
}

func buildThemeStarlightOrbit(isBday bool) calTheme {
	if isBday {
		return makeOrbitTheme(calOrbitSeed{
			skyTop: 0x2E1E5C, skyBot: 0x171234, bloomA: 0x5E2E72, bloomB: 0x442F7C, band: 0x522B78,
			starDim: 0x504076, starBri: 0x826FA8,
			limbGlow: calRGBA(0xC996E8, 0.14), limbRim: calRGBA(0xDFBEFA, 0.36), limbSub: calRGBA(0x8E6CB8, 0.28),
			accent: 0xFF7CA9, accentDeep: 0xFF5C93,
			pA: calRGBA(0xFF7CA9, 0.22), pB: calRGBA(0xFFF17D, 0.17), pC: calRGBA(0x9E8AF5, 0.20),
			horizon: 0x715B98, dowBG: 0x2B2054, dowFG: 0xE0D7F5, satBG: 0x232B63, sunBG: 0x462250,
			quietFill: 0x181136, quietSat: 0x191844, quietSun: 0x24133D,
			quietBorder: 0x8A74C8, quietNum: 0x9687BF, empty: 0x0B0820,
			card0: 0x422E7E, card1: 0x312160, plate: 0x24184A, plateSep: 0xC4B4F5,
			cardBorder: 0xFF8EB6, shadow: 0x060417, cutoutOuter: 0x2A1E54, shadowA: 0.48,
			headFG: 0x281B4E, indexFill: 0x2D215C, indexBorder: 0xA698E8,
			indexShadow: 0x050312, subInk: 0xDBD1F2, muted: 0x9588BF,
		})
	}

	return makeOrbitTheme(calOrbitSeed{
		skyTop: 0x292166, skyBot: 0x151336, bloomA: 0x374B9E, bloomB: 0x4C3A8F, band: 0x354396,
		starDim: 0x484178, starBri: 0x766FAD,
		limbGlow: calRGBA(0xA494E8, 0.15), limbRim: calRGBA(0xC8BCFA, 0.38), limbSub: calRGBA(0x7668B2, 0.28),
		accent: 0x5AD4FF, accentDeep: 0x2EB8F5,
		pA: calRGBA(0x5AD4FF, 0.22), pB: calRGBA(0x9D8DF7, 0.21), pC: calRGBA(0xFFF17D, 0.16),
		horizon: 0x6963A3, dowBG: 0x24215C, dowFG: 0xDDD9F5, satBG: 0x1F316E, sunBG: 0x3F2256,
		quietFill: 0x131338, quietSat: 0x151D4A, quietSun: 0x211544,
		quietBorder: 0x7882D4, quietNum: 0x8E8BC2, empty: 0x090922,
		card0: 0x3A3686, card1: 0x2A2768, plate: 0x1E1B4E, plateSep: 0xB0BAFA,
		cardBorder: 0x6ED8FF, shadow: 0x05051A, cutoutOuter: 0x24205C, shadowA: 0.50,
		headFG: 0x231C4F, indexFill: 0x262563, indexBorder: 0xA0A6EE,
		indexShadow: 0x050516, subInk: 0xD8D4F2, muted: 0x8F8CC4,
	})
}

func paintCalendarBackdrop(c *calCanvas, th calTheme) {
	mw, mh := 200, max(240, int(c.h*200/c.w))
	sky := image.NewRGBA(image.Rect(0, 0, mw, mh))

	for y := range mh {
		v := float64(y) / float64(mh-1)
		base := calMix(th.skyTop, th.skyBot, v)

		for x := range mw {
			u := float64(x) / float64(mw-1)
			dBand := (u + 0.28*v - 0.30) / 0.38
			d1 := math.Hypot((u-0.84)/0.34, (v-0.11)/0.22)
			d2 := math.Hypot((u-0.16)/0.40, (v-0.62)/0.36)

			col := calMix(base, th.nebulaBand, 0.28*math.Exp(-dBand*dBand))

			col = calMix(col, th.bloomA, 0.56*math.Exp(-d1*d1))
			col = calMix(col, th.bloomB, 0.38*math.Exp(-d2*d2))
			sky.SetRGBA(x, y, col)
		}
	}

	xdraw.ApproxBiLinear.Scale(c.img, c.img.Bounds(), sky, sky.Bounds(), draw.Src, nil)

	if !th.dark {
		return
	}

	seed := uint32(47)
	next := func() float64 {
		seed = seed*1664525 + 1013904223

		return float64(seed>>8) / (1 << 24)
	}

	var dim, bri calShape

	for range 260 {
		sx, sy, sr := next()*c.w, next()*c.h, 0.6+next()*1.15
		if sy < 210 || sx < 38 || sx > c.w-38 {
			bri.Add(calCircle(sx, sy, sr))
		} else {
			dim.Add(calCircle(sx, sy, sr*0.85))
		}
	}

	c.Fill(dim, th.starDim)
	c.Fill(bri, th.starBri)

	var limb, subLimb calShape

	limb.Arc(c.w*0.52, c.h+540, 1180, 920, -0.76*math.Pi, -0.24*math.Pi)
	subLimb.Arc(c.w*0.52, c.h+568, 1180, 920, -0.75*math.Pi, -0.25*math.Pi)
	c.Stroke(limb, 9.0, th.limbGlow)
	c.Stroke(limb, 2.0, th.limbRim)
	c.Stroke(subLimb, 1.2, th.limbSub)
}

func drawCalendarPrismEmblem(c *calCanvas, rightEdge, cy float64, th calTheme) {
	if th.dark {
		drawOrbitEmblem(c, rightEdge-126, cy, th)

		return
	}

	drawDaylightEmblem(c, rightEdge-118, cy, th)
}

func drawOrbitEmblem(c *calCanvas, cx, cy float64, th calTheme) {
	ring := func(rx, ry, a0, a1, w float64, col color.RGBA) {
		var s calShape

		s.Arc(0, 0, rx, ry, a0, a1)
		c.Stroke(calTransform(s, calPlace(cx, cy+3, 1, -0.28)), w, col)
	}

	ring(130, 44, 0, 2*math.Pi, 1.5, calColAlpha(th.subInk, 0.28))
	ring(114, 37, 0, 2*math.Pi, 2.1, calColAlpha(th.accent, 0.44))

	t1 := calRoundedTri(cx-6, cy, 50, 8, calDeg(-8))
	t2 := calRoundedTri(cx+18, cy+8, 36, 6, calDeg(14))

	c.Fill(t1, th.prismA)
	c.Stroke(t1, 1.8, calColAlpha(th.accent, 0.68))
	c.Fill(t2, th.prismB)
	c.Stroke(t2, 1.5, calColAlpha(th.goldAccent, 0.68))

	ring(114, 37, 0.06*math.Pi, 0.92*math.Pi, 4.0, th.goldAccent)

	c.Fill(calSparkle(cx-90, cy-32, 11.5), th.goldAccent)
	c.Fill(calSparkle(cx+86, cy-23, 13.5), calHex(0xFAF7FF))
	c.Fill(calSparkle(cx+60, cy+34, 7.8), th.accent)
	c.Fill(calSparkle(cx-56, cy+32, 6.2), calColAlpha(th.subInk, 0.78))
}

func drawDaylightEmblem(c *calCanvas, cx, cy float64, th calTheme) {
	c.Glow(cx, cy, 108, calRGB24(th.accent), 0.12)

	var fullOrbit, foreArc calShape

	fullOrbit.Arc(0, 0, 118, 39, 0, 2*math.Pi)
	foreArc.Arc(0, 0, 118, 39, 0.06*math.Pi, 0.94*math.Pi)
	c.Stroke(calTransform(fullOrbit, calPlace(cx, cy+4, 1, -0.25)), 1.3, calColAlpha(th.accent, 0.16))

	tBack := calRoundedTri(cx-24, cy+7, 54, 9, calDeg(-16))
	tMid := calRoundedTri(cx+5, cy-4, 56, 9, calDeg(0))
	tInner := calRoundedTri(cx+5, cy-4, 53.5, 8.2, calDeg(0))
	tFront := calRoundedTri(cx+27, cy+13, 37, 6, calDeg(18))

	c.Fill(tBack, th.prismB)
	c.Fill(tMid, th.prismA)
	c.Stroke(tInner, 1.1, calRGBA(0xFFFFFF, 0.78))
	c.Stroke(tMid, 1.8, calColAlpha(th.accent, 0.46))
	c.Fill(tFront, th.prismC)
	c.Stroke(tFront, 1.4, calColAlpha(th.goldAccent, 0.48))

	c.Stroke(calTransform(foreArc, calPlace(cx, cy+4, 1, -0.25)), 2.4, calColAlpha(th.accent, 0.36))

	c.Fill(calSparkle(cx-78, cy-29, 10.5), th.accent)
	c.Fill(calSparkle(cx+76, cy-25, 11.5), th.goldAccent)
	c.Fill(calSparkle(cx+56, cy+32, 7.2), calColAlpha(th.accent, 0.68))
}
