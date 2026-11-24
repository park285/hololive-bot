package render

import (
	"image/color"
	"math"
)

type calPortraitFrame uint8

const (
	calPortraitPassePartout calPortraitFrame = iota
	calPortraitBromideMat
	calPortraitFullBleed
)

type calSpotTone uint8

const (
	calSpotDarkBoard calSpotTone = iota
	calSpotCreamCard
	calSpotLightPaper
)

type calPosterPalette struct {
	tileMonthStamp bool
	creamCards     bool
	cornerDateTab  bool
	cellStampTag   bool
	hasActiveFrame bool
	flushRoster    bool
	hatchThickness float64
	portraitFrame  calPortraitFrame
	boardSpotTone  calSpotTone
	cardSpotTone   calSpotTone

	outerCanvas color.RGBA
	boardPaper  color.RGBA
	boardBorder color.RGBA
	obiPrimary  color.RGBA
	obiSecond   color.RGBA

	numTileBG    color.RGBA
	numFG        color.RGBA
	numShadowCol color.RGBA

	ink      color.RGBA
	subInk   color.RGBA
	mutedInk color.RGBA
	sunInk   color.RGBA
	satInk   color.RGBA

	dowBG     color.RGBA
	dowFG     color.RGBA
	dowSunFG  color.RGBA
	dowSatFG  color.RGBA
	dowSunBar color.RGBA
	dowSatBar color.RGBA
	dowRule   color.RGBA

	hatchBase   color.RGBA
	hatchStripe color.RGBA
	quietCellBG color.RGBA
	quietNum    color.RGBA
	gridLine    color.RGBA
	strongLine  color.RGBA

	activeHeadBG  color.RGBA
	activeHeadFG  color.RGBA
	activeHeadSub color.RGBA
	activeTopRule color.RGBA
	hasTopRule    bool
	dateTabBG     color.RGBA
	dateTabFG     color.RGBA
	activeBodyBG  color.RGBA
	photoMatCol   color.RGBA
	photoInnerLin color.RGBA
	activeCapBG   color.RGBA
	activeCapInk  color.RGBA
	activeCapSub  color.RGBA
	activeFrame   color.RGBA

	mastStampBG color.RGBA
	mastStampFG color.RGBA
	cellStampBG color.RGBA
	cellStampFG color.RGBA

	indexBG      color.RGBA
	indexTopRule color.RGBA
	indexHeadFG  color.RGBA

	rosterHeadBG  color.RGBA
	rosterBarCol  color.RGBA
	rosterTitleFG color.RGBA
	rosterSubFG   color.RGBA
	rosterStampBG color.RGBA
	rosterStampFG color.RGBA
}

type calPosterSeed struct {
	tileMonthStamp bool
	creamCards     bool
	cornerDateTab  bool
	cellStampTag   bool
	hasActiveFrame bool
	hasTopRule     bool
	flushRoster    bool
	hatchThickness float64
	portraitFrame  calPortraitFrame
	boardSpotTone  calSpotTone
	cardSpotTone   calSpotTone

	outer, paper, border, obi1, obi2    uint32
	numTile, numFG, numShadow           uint32
	ink, subInk, muted, sunInk, satInk  uint32
	dowBG, dowFG, dowSunFG, dowSatFG    uint32
	dowSunBar, dowSatBar, dowRule       uint32
	hatchBase, hatchStripe              uint32
	quietBG, quietNum, gridLn, strongLn uint32
	headBG, headFG, headSub, topRule    uint32
	tabBG, tabFG, bodyBG                uint32
	matCol, innerLin, capBG, capInk     uint32
	capSub, frameCol                    uint32
	mastStBG, mastStFG, cellStBG        uint32
	cellStFG, idxBG, idxRule, idxHead   uint32
	rostBG, rostBar, rostFG, rostSub    uint32
	rostStBG, rostStFG                  uint32
}

func makePosterPalette(s calPosterSeed) calPosterPalette {
	return calPosterPalette{
		tileMonthStamp: s.tileMonthStamp,
		creamCards:     s.creamCards,
		cornerDateTab:  s.cornerDateTab,
		cellStampTag:   s.cellStampTag,
		hasActiveFrame: s.hasActiveFrame,
		hasTopRule:     s.hasTopRule,
		flushRoster:    s.flushRoster,
		hatchThickness: s.hatchThickness,
		portraitFrame:  s.portraitFrame,
		boardSpotTone:  s.boardSpotTone,
		cardSpotTone:   s.cardSpotTone,

		outerCanvas: calHex(s.outer), boardPaper: calHex(s.paper), boardBorder: calHex(s.border),
		obiPrimary: calHex(s.obi1), obiSecond: calHex(s.obi2),
		numTileBG: calHex(s.numTile), numFG: calHex(s.numFG), numShadowCol: calHex(s.numShadow),
		ink: calHex(s.ink), subInk: calHex(s.subInk), mutedInk: calHex(s.muted),
		sunInk: calHex(s.sunInk), satInk: calHex(s.satInk),
		dowBG: calHex(s.dowBG), dowFG: calHex(s.dowFG), dowSunFG: calHex(s.dowSunFG), dowSatFG: calHex(s.dowSatFG),
		dowSunBar: calHex(s.dowSunBar), dowSatBar: calHex(s.dowSatBar), dowRule: calHex(s.dowRule),
		hatchBase: calHex(s.hatchBase), hatchStripe: calHex(s.hatchStripe),
		quietCellBG: calHex(s.quietBG), quietNum: calHex(s.quietNum),
		gridLine: calHex(s.gridLn), strongLine: calHex(s.strongLn),
		activeHeadBG: calHex(s.headBG), activeHeadFG: calHex(s.headFG), activeHeadSub: calHex(s.headSub),
		activeTopRule: calHex(s.topRule), dateTabBG: calHex(s.tabBG), dateTabFG: calHex(s.tabFG),
		activeBodyBG: calHex(s.bodyBG), photoMatCol: calHex(s.matCol), photoInnerLin: calHex(s.innerLin),
		activeCapBG: calHex(s.capBG), activeCapInk: calHex(s.capInk), activeCapSub: calHex(s.capSub),
		activeFrame: calHex(s.frameCol),
		mastStampBG: calHex(s.mastStBG), mastStampFG: calHex(s.mastStFG),
		cellStampBG: calHex(s.cellStBG), cellStampFG: calHex(s.cellStFG),
		indexBG: calHex(s.idxBG), indexTopRule: calHex(s.idxRule), indexHeadFG: calHex(s.idxHead),
		rosterHeadBG: calHex(s.rostBG), rosterBarCol: calHex(s.rostBar),
		rosterTitleFG: calHex(s.rostFG), rosterSubFG: calHex(s.rostSub),
		rosterStampBG: calHex(s.rostStBG), rosterStampFG: calHex(s.rostStFG),
	}
}

func calPosterSpotColor(hue float64, chromatic bool, fallback color.RGBA, tone calSpotTone) color.RGBA {
	if !chromatic {
		return fallback
	}

	wYel := math.Exp(-math.Pow(calHueDist(hue, 64)/26, 2))
	wGrn := math.Exp(-math.Pow(calHueDist(hue, 128)/34, 2))
	wDeep := math.Exp(-math.Pow(calHueDist(hue, 248)/34, 2))
	adjH := math.Mod(hue-10*wYel+360, 360)

	switch tone {
	case calSpotCreamCard:
		return calHSL(adjH, 0.58-0.06*wGrn, 0.45-0.05*wYel-0.04*wGrn+0.06*wDeep)
	case calSpotLightPaper:
		return calHSL(adjH, 0.64-0.06*wGrn, 0.45-0.06*wYel-0.05*wGrn+0.06*wDeep)
	case calSpotDarkBoard:
		return calHSL(adjH, 0.54-0.05*wGrn, 0.62-0.04*wYel-0.03*wGrn+0.07*wDeep)
	}

	return fallback
}

func buildPosterPalette(themeID string, isBday bool) calPosterPalette {
	switch themeID {
	case CalendarThemeCreamBromide:
		return buildCreamBromidePalette(isBday)
	case CalendarThemeKurokamiRisograph:
		return buildKurokamiRisographPalette(isBday)
	case CalendarThemeHoloStage:
		return buildHoloStagePalette(isBday)
	case CalendarThemeShirokamiRisograph:
		return buildShirokamiRisographPalette(isBday)
	case CalendarThemeSakuraSoraPress:
		return buildSakuraSoraPressPalette(isBday)
	default:
		return buildMuseumArchivePalette(isBday)
	}
}

func buildCreamBromidePalette(isBday bool) calPosterPalette {
	if isBday {
		return makePosterPalette(calPosterSeed{
			tileMonthStamp: true, creamCards: true, cellStampTag: true, hasTopRule: true,
			hatchThickness: 2.2, portraitFrame: calPortraitBromideMat,
			boardSpotTone: calSpotDarkBoard, cardSpotTone: calSpotCreamCard,
			outer: 0x130E16, paper: 0x1E1623, border: 0x3D2E47, obi1: 0xC9335A, obi2: 0xF2B63D,
			numTile: 0xC9335A, numFG: 0xFFF7F5, numShadow: 0xF2B63D,
			ink: 0xF6EFE6, subInk: 0xC6B7CF, muted: 0x94839F, sunInk: 0xED7B93, satInk: 0x7EB4F2,
			dowBG: 0x271D2E, dowFG: 0xDFD4E6, dowSunFG: 0xF58CA2, dowSatFG: 0x8CC0FA,
			dowSunBar: 0xC9335A, dowSatBar: 0x3B82D6, dowRule: 0x463552,
			hatchBase: 0x19121E, hatchStripe: 0x231A29, quietBG: 0x261D2C, quietNum: 0x8F7E9B,
			gridLn: 0x3B2D45, strongLn: 0x523F61,
			headBG: 0xF6EFE6, headFG: 0x1F1624, headSub: 0x6B5675, topRule: 0xC9335A,
			bodyBG: 0xF6EFE6, matCol: 0xF6EFE6, innerLin: 0xD8CCBE,
			capBG: 0xECE2D4, capInk: 0x1F1624, capSub: 0x5E4C68,
			mastStBG: 0xF2B63D, mastStFG: 0x1F1624, cellStBG: 0xF2B63D, cellStFG: 0x1F1624,
			idxBG: 0x211926, idxRule: 0xF2B63D, idxHead: 0xF2B63D,
			rostBG: 0x2A1F31, rostBar: 0xC9335A, rostFG: 0xF6EFE6, rostSub: 0xC6B7CF,
			rostStBG: 0xF2B63D, rostStFG: 0x1F1624,
		})
	}

	return makePosterPalette(calPosterSeed{
		tileMonthStamp: true, creamCards: true, cellStampTag: true, hasTopRule: true,
		hatchThickness: 2.2, portraitFrame: calPortraitBromideMat,
		boardSpotTone: calSpotDarkBoard, cardSpotTone: calSpotCreamCard,
		outer: 0x0C121D, paper: 0x141D2E, border: 0x2D3F5E, obi1: 0x1B6EC2, obi2: 0xEEB438,
		numTile: 0x1B6EC2, numFG: 0xF7FAFF, numShadow: 0xEEB438,
		ink: 0xF4F0E8, subInk: 0xB4C2D8, muted: 0x8294B2, sunInk: 0xE87E92, satInk: 0x7AB6F5,
		dowBG: 0x1B273D, dowFG: 0xD2DDF0, dowSunFG: 0xF0899C, dowSatFG: 0x88C2FF,
		dowSunBar: 0xD4526E, dowSatBar: 0x2E85E6, dowRule: 0x33486B,
		hatchBase: 0x111927, hatchStripe: 0x192336, quietBG: 0x1B273C, quietNum: 0x7E92B2,
		gridLn: 0x2C3E5C, strongLn: 0x3D557D,
		headBG: 0xF4F0E8, headFG: 0x131D2E, headSub: 0x526480, topRule: 0x1B6EC2,
		bodyBG: 0xF4F0E8, matCol: 0xF4F0E8, innerLin: 0xD2CCC0,
		capBG: 0xE6E0D3, capInk: 0x131D2E, capSub: 0x4A5B78,
		mastStBG: 0xEEB438, mastStFG: 0x131D2E, cellStBG: 0xEEB438, cellStFG: 0x131D2E,
		idxBG: 0x162032, idxRule: 0xEEB438, idxHead: 0xEEB438,
		rostBG: 0x1D2A42, rostBar: 0x1B6EC2, rostFG: 0xF4F0E8, rostSub: 0xB4C2D8,
		rostStBG: 0xEEB438, rostStFG: 0x131D2E,
	})
}

func buildKurokamiRisographPalette(isBday bool) calPosterPalette {
	if isBday {
		return makePosterPalette(calPosterSeed{
			cornerDateTab: true, cellStampTag: true, hasActiveFrame: true,
			hatchThickness: 2.2, portraitFrame: calPortraitPassePartout,
			boardSpotTone: calSpotDarkBoard, cardSpotTone: calSpotDarkBoard,
			outer: 0x151110, paper: 0x211A18, border: 0x453732, obi1: 0xE65A46, obi2: 0xF4DFC4,
			numFG: 0xF5ECE2, numShadow: 0xE65A46,
			ink: 0xF5ECE2, subInk: 0xC7B8AA, muted: 0x968577, sunInk: 0xEB735E, satInk: 0x78B0CC,
			dowBG: 0x2A211E, dowFG: 0xD8C9BC, dowSunFG: 0xEB735E, dowSatFG: 0x78B0CC,
			dowSunBar: 0xE65A46, dowSatBar: 0x5C98B8, dowRule: 0x4A3B36,
			hatchBase: 0x1D1715, hatchStripe: 0x271F1C, quietBG: 0x271F1C, quietNum: 0x8F7E71,
			gridLn: 0x3E312C, strongLn: 0x594740,
			headBG: 0x382C28, headFG: 0xF5ECE2, headSub: 0xF4DFC4,
			tabBG: 0xE65A46, tabFG: 0x241916,
			bodyBG: 0x382C28, matCol: 0xEFE4D6, innerLin: 0xC9BCB0,
			capBG: 0x312623, capInk: 0xF5ECE2, capSub: 0xC7B8AA, frameCol: 0xE65A46,
			mastStBG: 0xF4DFC4, mastStFG: 0x241916, cellStBG: 0xF4DFC4, cellStFG: 0x241916,
			idxBG: 0x241C1A, idxRule: 0xF4DFC4, idxHead: 0xF4DFC4,
			rostBG: 0x2F2421, rostBar: 0xE65A46, rostFG: 0xF5ECE2, rostSub: 0xC7B8AA,
			rostStBG: 0xF4DFC4, rostStFG: 0x241916,
		})
	}

	return makePosterPalette(calPosterSeed{
		cornerDateTab: true, cellStampTag: true, hasActiveFrame: true,
		hatchThickness: 2.2, portraitFrame: calPortraitPassePartout,
		boardSpotTone: calSpotDarkBoard, cardSpotTone: calSpotDarkBoard,
		outer: 0x0E1517, paper: 0x162124, border: 0x33474D, obi1: 0xE0A83B, obi2: 0xE8E3D3,
		numFG: 0xF2EFE6, numShadow: 0xE0A83B,
		ink: 0xF2EFE6, subInk: 0xB5C4C2, muted: 0x829694, sunInk: 0xE37A68, satInk: 0x6EB2CF,
		dowBG: 0x1D2B2F, dowFG: 0xCAD6D4, dowSunFG: 0xE37A68, dowSatFG: 0x6EB2CF,
		dowSunBar: 0xD96B59, dowSatBar: 0x579DBA, dowRule: 0x384E54,
		hatchBase: 0x131D20, hatchStripe: 0x1B282C, quietBG: 0x1D2B2F, quietNum: 0x7D9391,
		gridLn: 0x31454B, strongLn: 0x466169,
		headBG: 0x2A3D42, headFG: 0xF2EFE6, headSub: 0xE0A83B,
		tabBG: 0xE0A83B, tabFG: 0x162225,
		bodyBG: 0x2A3D42, matCol: 0xEDEAE0, innerLin: 0xC2BFB4,
		capBG: 0x233338, capInk: 0xF2EFE6, capSub: 0xB5C4C2, frameCol: 0xE0A83B,
		mastStBG: 0xE8E3D3, mastStFG: 0x162225, cellStBG: 0xE8E3D3, cellStFG: 0x162225,
		idxBG: 0x192528, idxRule: 0xE8E3D3, idxHead: 0xE0A83B,
		rostBG: 0x213136, rostBar: 0xE0A83B, rostFG: 0xF2EFE6, rostSub: 0xB5C4C2,
		rostStBG: 0xE8E3D3, rostStFG: 0x162225,
	})
}

func buildHoloStagePalette(isBday bool) calPosterPalette {
	if isBday {
		return makePosterPalette(calPosterSeed{
			hasTopRule: true, hatchThickness: 2.2, portraitFrame: calPortraitFullBleed,
			boardSpotTone: calSpotDarkBoard, cardSpotTone: calSpotDarkBoard,
			outer: 0x0F131D, paper: 0x181E2C, border: 0x36425E, obi1: 0xD93864, obi2: 0xF5CA4E,
			numFG: 0xF5F7FC, numShadow: 0xD93864,
			ink: 0xF5F7FC, subInk: 0xB8C2D8, muted: 0x8491AE, sunInk: 0xF27995, satInk: 0x70B2F8,
			dowBG: 0x20283A, dowFG: 0xD4DDF0, dowSunFG: 0xF78AA3, dowSatFG: 0x7DB8FA,
			dowSunBar: 0xD93864, dowSatBar: 0x2B82E3, dowRule: 0x3A4766,
			hatchBase: 0x151B28, hatchStripe: 0x1E2638, quietBG: 0x1F2638, quietNum: 0x7E8CA8,
			gridLn: 0x313C57, strongLn: 0x49587D,
			headBG: 0xD93864, headFG: 0xFFFFFF, headSub: 0xFFE8EE, topRule: 0xD93864,
			bodyBG: 0x2B354D, matCol: 0x181E2C, innerLin: 0x3D4B6B,
			capBG: 0x252E44, capInk: 0xF5F7FC, capSub: 0xB8C2D8,
			mastStBG: 0xF5CA4E, mastStFG: 0x161B28,
			idxBG: 0x1B2232, idxRule: 0xF5CA4E, idxHead: 0xF5CA4E,
			rostBG: 0x232C40, rostBar: 0xD93864, rostFG: 0xF5F7FC, rostSub: 0xB8C2D8,
			rostStBG: 0xF5CA4E, rostStFG: 0x161B28,
		})
	}

	return makePosterPalette(calPosterSeed{
		hasTopRule: true, hatchThickness: 2.2, portraitFrame: calPortraitFullBleed,
		boardSpotTone: calSpotDarkBoard, cardSpotTone: calSpotDarkBoard,
		outer: 0x0E131F, paper: 0x171F30, border: 0x324363, obi1: 0x186CC4, obi2: 0xF2C14B,
		numFG: 0xF3F7FD, numShadow: 0x186CC4,
		ink: 0xF3F7FD, subInk: 0xB4C3DC, muted: 0x8093B2, sunInk: 0xEE7B94, satInk: 0x70B5FA,
		dowBG: 0x1E2940, dowFG: 0xD0DCF0, dowSunFG: 0xF2859C, dowSatFG: 0x85C2FF,
		dowSunBar: 0xD44C6E, dowSatBar: 0x2988ED, dowRule: 0x36496B,
		hatchBase: 0x141C2B, hatchStripe: 0x1C273B, quietBG: 0x1D283E, quietNum: 0x7A8DAE,
		gridLn: 0x2E3F5E, strongLn: 0x445B85,
		headBG: 0x186CC4, headFG: 0xFFFFFF, headSub: 0xFFF0B8, topRule: 0x186CC4,
		bodyBG: 0x283754, matCol: 0x171F30, innerLin: 0x394F75,
		capBG: 0x222F48, capInk: 0xF3F7FD, capSub: 0xB4C3DC,
		mastStBG: 0xF2C14B, mastStFG: 0x141C2B,
		idxBG: 0x1A2336, idxRule: 0xF2C14B, idxHead: 0xF2C14B,
		rostBG: 0x212D45, rostBar: 0x186CC4, rostFG: 0xF3F7FD, rostSub: 0xB4C3DC,
		rostStBG: 0xF2C14B, rostStFG: 0x141C2B,
	})
}

func buildShirokamiRisographPalette(isBday bool) calPosterPalette {
	if isBday {
		return makePosterPalette(calPosterSeed{
			cornerDateTab: true, cellStampTag: true, hasActiveFrame: true, flushRoster: true,
			hatchThickness: 1.8, portraitFrame: calPortraitPassePartout,
			boardSpotTone: calSpotLightPaper, cardSpotTone: calSpotLightPaper,
			outer: 0xDDD3C5, paper: 0xF6F1E8, border: 0x3D2F2A, obi1: 0xC83E2B, obi2: 0xE39B32,
			numFG: 0x261C19, numShadow: 0xE8946B,
			ink: 0x261C19, subInk: 0x5E4E46, muted: 0x7D6C63, sunInk: 0xC83E2B, satInk: 0x266B9E,
			dowBG: 0xE6DCCE, dowFG: 0x382B26, dowSunFG: 0xC83E2B, dowSatFG: 0x266B9E,
			dowSunBar: 0xC83E2B, dowSatBar: 0x266B9E, dowRule: 0xA69586,
			hatchBase: 0xE8DFD2, hatchStripe: 0xDDD2C2, quietBG: 0xEDE5D8, quietNum: 0x8C7B70,
			gridLn: 0xD4C8B8, strongLn: 0x4A3A34,
			headBG: 0x2B211E, tabBG: 0xC83E2B, tabFG: 0xFFFDF9,
			bodyBG: 0xFFFDF9, matCol: 0xDFD3C3, innerLin: 0x8A7869,
			capBG: 0xFFFDF9, capInk: 0x241B18, capSub: 0x5E4E46, frameCol: 0xC83E2B,
			mastStBG: 0x2B211E, mastStFG: 0xFAF5EC, cellStBG: 0xF4DFC4, cellStFG: 0x241B18,
			idxBG: 0xF3ECE1, idxRule: 0xE39B32, idxHead: 0xB83523,
			rostBG: 0x2B211E, rostBar: 0xC83E2B, rostFG: 0xFAF5EC, rostSub: 0xD4C4B6,
			rostStBG: 0xF4DFC4, rostStFG: 0x241B18,
		})
	}

	return makePosterPalette(calPosterSeed{
		cornerDateTab: true, cellStampTag: true, hasActiveFrame: true, flushRoster: true,
		hatchThickness: 1.8, portraitFrame: calPortraitPassePartout,
		boardSpotTone: calSpotLightPaper, cardSpotTone: calSpotLightPaper,
		outer: 0xCFDAD6, paper: 0xF2F5F1, border: 0x26383B, obi1: 0x176873, obi2: 0xDE9E2C,
		numFG: 0x162426, numShadow: 0xE3AB3B,
		ink: 0x162426, subInk: 0x45595C, muted: 0x63787B, sunInk: 0xC44536, satInk: 0x1F6A9C,
		dowBG: 0xDCE5E0, dowFG: 0x223538, dowSunFG: 0xC44536, dowSatFG: 0x1F6A9C,
		dowSunBar: 0xC44536, dowSatBar: 0x1F6A9C, dowRule: 0x8FA3A2,
		hatchBase: 0xDEE7E2, hatchStripe: 0xD0DCD6, quietBG: 0xE6EDE9, quietNum: 0x6E8486,
		gridLn: 0xC5D2CD, strongLn: 0x31474B,
		headBG: 0x19282B, tabBG: 0xDE9E2C, tabFG: 0x162426,
		bodyBG: 0xFCFDFB, matCol: 0xD3DFDA, innerLin: 0x687E80,
		capBG: 0xFCFDFB, capInk: 0x162426, capSub: 0x45595C, frameCol: 0x176873,
		mastStBG: 0x19282B, mastStFG: 0xF4F7F4, cellStBG: 0xEAE2CB, cellStFG: 0x162426,
		idxBG: 0xEBF1EE, idxRule: 0xDE9E2C, idxHead: 0x145B65,
		rostBG: 0x19282B, rostBar: 0xDE9E2C, rostFG: 0xF4F7F4, rostSub: 0xB6C7C6,
		rostStBG: 0xEAE2CB, rostStFG: 0x162426,
	})
}

func buildSakuraSoraPressPalette(isBday bool) calPosterPalette {
	if isBday {
		return makePosterPalette(calPosterSeed{
			cellStampTag: true, hasActiveFrame: true, flushRoster: true,
			hatchThickness: 1.8, portraitFrame: calPortraitPassePartout,
			boardSpotTone: calSpotLightPaper, cardSpotTone: calSpotLightPaper,
			outer: 0xE2CAD5, paper: 0xFBF2F6, border: 0x5E3447, obi1: 0xC92D5B, obi2: 0xF2BE38,
			numFG: 0xC92D5B, numShadow: 0xF2BE38,
			ink: 0x281621, subInk: 0x66485A, muted: 0x87677B, sunInk: 0xC92D5B, satInk: 0x2266B5,
			dowBG: 0xF0DCE5, dowFG: 0x4A293B, dowSunFG: 0xC92D5B, dowSatFG: 0x2266B5,
			dowSunBar: 0xC92D5B, dowSatBar: 0x2266B5, dowRule: 0xC49AB0,
			hatchBase: 0xF1E1E9, hatchStripe: 0xE6D0DC, quietBG: 0xF5E6EE, quietNum: 0x8F6E82,
			gridLn: 0xDEC3D1, strongLn: 0x6E3E54,
			headBG: 0xC92D5B, headFG: 0xFFFFFF, tabFG: 0xFFFFFF,
			bodyBG: 0xFFFFFF, matCol: 0xE8CCD8, innerLin: 0xB38197,
			capBG: 0xFFFFFF, capInk: 0x281621, capSub: 0x66485A, frameCol: 0xC92D5B,
			mastStBG: 0xC92D5B, mastStFG: 0xFFFFFF, cellStBG: 0xFFE999, cellStFG: 0x281621,
			idxBG: 0xFDF7FA, idxRule: 0xC92D5B, idxHead: 0xB82550,
			rostBG: 0x3D1E2E, rostBar: 0xF2BE38, rostFG: 0xFFF7FA, rostSub: 0xDFC3D1,
			rostStBG: 0xF2BE38, rostStFG: 0x281621,
		})
	}

	return makePosterPalette(calPosterSeed{
		cellStampTag: true, hasActiveFrame: true, flushRoster: true,
		hatchThickness: 1.8, portraitFrame: calPortraitPassePartout,
		boardSpotTone: calSpotLightPaper, cardSpotTone: calSpotLightPaper,
		outer: 0xC6DCF0, paper: 0xF0F7FD, border: 0x26496E, obi1: 0x1569BA, obi2: 0xF0BC34,
		numFG: 0x1569BA, numShadow: 0xF0BC34,
		ink: 0x13243A, subInk: 0x466180, muted: 0x6480A1, sunInk: 0xCC3B5E, satInk: 0x1569BA,
		dowBG: 0xD9EAF8, dowFG: 0x203E5F, dowSunFG: 0xCC3B5E, dowSatFG: 0x1569BA,
		dowSunBar: 0xCC3B5E, dowSatBar: 0x1569BA, dowRule: 0x91B6DB,
		hatchBase: 0xDFEDF9, hatchStripe: 0xCEE1F2, quietBG: 0xE4F0FA, quietNum: 0x6280A3,
		gridLn: 0xC0D7ED, strongLn: 0x315A85,
		headBG: 0x1569BA, headFG: 0xFFFFFF, tabFG: 0xFFFFFF,
		bodyBG: 0xFFFFFF, matCol: 0xCCE1F4, innerLin: 0x7DA6CC,
		capBG: 0xFFFFFF, capInk: 0x13243A, capSub: 0x466180, frameCol: 0x1569BA,
		mastStBG: 0x1569BA, mastStFG: 0xFFFFFF, cellStBG: 0xFFE78C, cellStFG: 0x13243A,
		idxBG: 0xF7FBFF, idxRule: 0x1569BA, idxHead: 0x125CA6,
		rostBG: 0x162F4D, rostBar: 0xF0BC34, rostFG: 0xF5FAFF, rostSub: 0xB8CFE8,
		rostStBG: 0xF0BC34, rostStFG: 0x13243A,
	})
}

func buildMuseumArchivePalette(isBday bool) calPosterPalette {
	spot := uint32(0xD12445)
	headTitleCol := uint32(0xB81D3A)

	if !isBday {
		spot = 0x1658D6
		headTitleCol = 0x124BB8
	}

	return makePosterPalette(calPosterSeed{
		cornerDateTab: true, cellStampTag: true, hasActiveFrame: true, flushRoster: true,
		hatchThickness: 1.8, portraitFrame: calPortraitPassePartout,
		boardSpotTone: calSpotLightPaper, cardSpotTone: calSpotLightPaper,
		outer: 0xD8D5CC, paper: 0xF7F5EF, border: 0x1E1E1C, obi1: spot, obi2: 0x1E1E1C,
		numFG: 0x191918, numShadow: spot,
		ink: 0x191918, subInk: 0x52514C, muted: 0x706E67, sunInk: 0xC92442, satInk: 0x1658D6,
		dowBG: 0xE5E2D9, dowFG: 0x191918, dowSunFG: 0xC92442, dowSatFG: 0x1658D6,
		dowSunBar: 0xC92442, dowSatBar: 0x1658D6, dowRule: 0x1E1E1C,
		hatchBase: 0xE7E4DC, hatchStripe: 0xD9D5CA, quietBG: 0xEEECE5, quietNum: 0x7A7870,
		gridLn: 0xCFCBC0, strongLn: 0x1E1E1C,
		headBG: 0x1E1E1C, tabBG: spot, tabFG: 0xFFFFFF,
		bodyBG: 0xFFFFFF, matCol: 0xDDD9CE, innerLin: 0x8C887E,
		capBG: 0xFFFFFF, capInk: 0x191918, capSub: 0x52514C, frameCol: 0x1E1E1C,
		mastStBG: spot, mastStFG: 0xFFFFFF, cellStBG: 0xF2C438, cellStFG: 0x191918,
		idxBG: 0xF3F1EA, idxRule: spot, idxHead: headTitleCol,
		rostBG: 0x1E1E1C, rostBar: spot, rostFG: 0xF7F5EF, rostSub: 0xC2BFB6,
		rostStBG: 0xF2C438, rostStFG: 0x191918,
	})
}
