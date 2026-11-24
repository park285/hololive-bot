package render

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	xdraw "golang.org/x/image/draw"
)

const (
	CalendarThemePrismDaylight      = "prism_daylight"
	CalendarThemeShirokamiRisograph = "shirokami_risograph"
	CalendarThemeSakuraSoraPress    = "sakura_sora_press"
	CalendarThemeMuseumArchive      = "museum_archive"
	CalendarThemeCreamBromide       = "cream_bromide"
	CalendarThemeKurokamiRisograph  = "kurokami_risograph"
	CalendarThemeHoloStage          = "holo_stage"
)

var calendarThemePool = []string{
	CalendarThemePrismDaylight,
	CalendarThemeShirokamiRisograph,
	CalendarThemeSakuraSoraPress,
	CalendarThemeMuseumArchive,
	CalendarThemeCreamBromide,
	CalendarThemeKurokamiRisograph,
	CalendarThemeHoloStage,
}

func isFlatPosterTheme(themeID string) bool {
	return themeID != CalendarThemePrismDaylight
}

type calTheme struct {
	skyTop, skyBot color.RGBA
	bloomA, bloomB color.RGBA
	nebulaBand     color.RGBA

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
}

func drawCalendarPrismEmblem(c *calCanvas, rightEdge, cy float64, th calTheme) {
	cx := rightEdge - 118
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
