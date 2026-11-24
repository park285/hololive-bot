package render

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/assets/fonts"
)

func calChannels(v uint32) (r, g, b uint8) {
	var raw [4]byte

	binary.BigEndian.PutUint32(raw[:], v)

	return raw[1], raw[2], raw[3]
}

func calHex(v uint32) color.RGBA {
	r, g, b := calChannels(v)

	return color.RGBA{R: r, G: g, B: b, A: 255}
}

func calRGB24(c color.RGBA) uint32 {
	return uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
}

func calRGBA(v uint32, a float64) color.RGBA {
	a = max(0, min(1, a))

	r, g, b := calChannels(v)

	return color.RGBA{
		R: uint8(float64(r)*a + 0.5),
		G: uint8(float64(g)*a + 0.5),
		B: uint8(float64(b)*a + 0.5),
		A: uint8(255*a + 0.5),
	}
}

func calColAlpha(c color.RGBA, a float64) color.RGBA {
	return calRGBA(calRGB24(c), a)
}

func calMix(a, b color.RGBA, t float64) color.RGBA {
	t = max(0, min(1, t))

	lerp := func(x, y uint8) uint8 {
		return uint8(float64(x)*(1-t) + float64(y)*t + 0.5)
	}

	return color.RGBA{R: lerp(a.R, b.R), G: lerp(a.G, b.G), B: lerp(a.B, b.B), A: lerp(a.A, b.A)}
}

func calRGBToHSV(r, g, b float64) (h, s, v float64) {
	mx, mn := max(r, g, b), min(r, g, b)

	v = mx

	if mx == 0 || mx == mn {
		return 0, 0, v
	}

	d := mx - mn

	s = d / mx

	switch mx {
	case r:
		h = math.Mod((g-b)/d, 6)
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}

	h *= 60
	if h < 0 {
		h += 360
	}

	return h, s, v
}

func calHSL(h, s, l float64) color.RGBA {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}

	s = max(0, min(1, s))
	l = max(0, min(1, l))

	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2

	var r, g, b float64

	switch {
	case h < 60:
		r, g = c, x
	case h < 120:
		r, g = x, c
	case h < 180:
		g, b = c, x
	case h < 240:
		g, b = x, c
	case h < 300:
		r, b = x, c
	default:
		r, b = c, x
	}

	return color.RGBA{
		R: uint8((r+m)*255 + 0.5),
		G: uint8((g+m)*255 + 0.5),
		B: uint8((b+m)*255 + 0.5),
		A: 255,
	}
}

func calSamplePixelHue(px color.Color, rad float64) (hue, weight float64, ok bool) {
	if px == nil {
		return 0, 0, false
	}

	r, g, bl, a := px.RGBA()
	if a < 16384 {
		return 0, 0, false
	}

	h, s, v := calRGBToHSV(float64(r)/65535, float64(g)/65535, float64(bl)/65535)
	if s < 0.24 || v < 0.22 {
		return 0, 0, false
	}

	w := s * v
	if rad < 0.42 && (h <= 44 || h >= 345) {
		w *= 0.16
	} else if h > 10 && h < 45 && s < 0.56 {
		w *= 0.24
	}

	return h, w, true
}

func calDominantHue(img image.Image) (float64, bool) {
	if img == nil {
		return 214, false
	}

	var (
		hist    [36]float64
		samples float64
		totalW  float64
	)

	b := img.Bounds()
	wSpan, hSpan := float64(max(1, b.Dx())), float64(max(1, b.Dy()))
	cx, cy := float64(b.Min.X)+wSpan/2, float64(b.Min.Y)+hSpan/2
	step := max(1, b.Dx()/96, b.Dy()/96)

	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			samples++

			rad := math.Hypot((float64(x)-cx)/wSpan, (float64(y)-cy)/hSpan)

			h, w, ok := calSamplePixelHue(img.At(x, y), rad)
			if !ok {
				continue
			}

			hist[int(h/10)%36] += w
			totalW += w
		}
	}

	if samples == 0 || totalW/samples < 0.032 {
		return 214, false
	}

	best := 0

	for i := range hist {
		if hist[i]+hist[(i+1)%36] > hist[best]+hist[(best+1)%36] {
			best = i
		}
	}

	if hist[best] <= 0 {
		return 214, false
	}

	return float64(best)*10 + 10, true
}

type calMemberColors struct {
	bar    color.RGBA
	ring   color.RGBA
	border color.RGBA
	aura   color.RGBA
	dot    color.RGBA
}

func calHueDist(a, b float64) float64 {
	d := math.Mod(math.Abs(a-b), 360)
	if d > 180 {
		return 360 - d
	}

	return d
}

func calMemberPalette(hue float64, hasChromatic bool, th calTheme) calMemberColors {
	if !hasChromatic {
		if th.dark {
			return calMemberColors{
				bar:    th.accent,
				ring:   calMix(th.accent, calHex(0xFAF7FF), 0.25),
				border: calMix(th.cardBorder, calHex(0xFAF7FF), 0.18),
				aura:   th.accent,
				dot:    th.accent,
			}
		}

		return calMemberColors{
			bar:    th.accent,
			ring:   th.accent,
			border: th.cardBorder,
			aura:   th.accentSoft,
			dot:    th.accent,
		}
	}

	wYel := math.Exp(-math.Pow(calHueDist(hue, 64)/26, 2))
	wGrn := math.Exp(-math.Pow(calHueDist(hue, 128)/34, 2))
	wDeep := math.Exp(-math.Pow(calHueDist(hue, 248)/34, 2))
	adjH := math.Mod(hue-10*wYel+360, 360)

	if th.dark {
		col := calHSL(adjH, 0.80, 0.68-0.04*wYel+0.09*wDeep)

		return calMemberColors{
			bar:    col,
			ring:   col,
			border: calMix(col, calHex(0xFAF7FF), 0.20),
			aura:   col,
			dot:    col,
		}
	}

	col := calHSL(adjH, 0.72-0.08*wGrn, 0.50-0.06*wYel-0.05*wGrn+0.07*wDeep)

	return calMemberColors{
		bar:    col,
		ring:   col,
		border: calHSL(adjH, 0.54-0.06*wGrn, 0.68-0.04*wYel+0.04*wDeep),
		aura:   calMix(calHSL(adjH, 0.58-0.12*wYel, 0.95), calHex(0xFFFFFF), 0.22),
		dot:    col,
	}
}

type calFace uint8

const (
	calFaceRegular calFace = iota
	calFaceBold
	calFaceXBold
	calFaceCount
)

type calGlyphSeg struct {
	op sfnt.SegmentOp
	a  [3]calPt
}

type calGlyph struct {
	segs []calGlyphSeg
	adv  float64
}

type calGlyphKey struct {
	f *sfnt.Font
	r rune
}

const maxCachedCalendarGlyphs = 4096

type calFontSet struct {
	faces [calFaceCount]*sfnt.Font
	jp    *sfnt.Font
	mids  [calFaceCount]float64
	mu    sync.Mutex
	buf   sfnt.Buffer
	cache map[calGlyphKey]*calGlyph
}

var loadCalendarFontSet = sync.OnceValues(func() (*calFontSet, error) {
	data := fonts.ScheduleCard()
	files := [calFaceCount][]byte{
		calFaceRegular: data.PretendardRegular,
		calFaceBold:    data.PretendardBold,
		calFaceXBold:   data.PretendardExtraBold,
	}

	fs := &calFontSet{cache: make(map[calGlyphKey]*calGlyph)}

	for i, raw := range files {
		parsed, err := sfnt.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("parse calendar font %d: %w", i, err)
		}

		fs.faces[i] = parsed
	}

	jpFont, err := sfnt.Parse(data.NotoSansJP)
	if err != nil {
		return nil, fmt.Errorf("parse calendar japanese font: %w", err)
	}

	fs.jp = jpFont

	for i := range fs.faces {
		fs.mids[i] = fs.measureMid(calFace(i))
	}

	return fs, nil
})

func (f *calFontSet) lookupLocked(src *sfnt.Font, r rune) (sfnt.GlyphIndex, bool) {
	gi, err := src.GlyphIndex(&f.buf, r)

	return gi, err == nil && gi != 0
}

func (f *calFontSet) glyph(fc calFace, r rune) *calGlyph {
	f.mu.Lock()
	defer f.mu.Unlock()

	src := f.faces[fc]

	gi, ok := f.lookupLocked(src, r)
	if !ok {
		src = f.faces[calFaceBold]
		if gi, ok = f.lookupLocked(src, r); !ok {
			src = f.jp
			if gi, ok = f.lookupLocked(src, r); !ok {
				return &calGlyph{}
			}
		}
	}

	key := calGlyphKey{f: src, r: r}
	if g, found := f.cache[key]; found {
		return g
	}

	if len(f.cache) >= maxCachedCalendarGlyphs {
		clear(f.cache)
	}

	upem := fixed.I(int(src.UnitsPerEm()))
	em := float64(src.UnitsPerEm()) * 64
	g := &calGlyph{}

	if adv, err := src.GlyphAdvance(&f.buf, gi, upem, font.HintingNone); err == nil {
		g.adv = float64(adv) / em
	}

	if segs, err := src.LoadGlyph(&f.buf, gi, upem, nil); err == nil {
		g.segs = make([]calGlyphSeg, 0, len(segs))

		for _, sg := range segs {
			gs := calGlyphSeg{op: sg.Op}

			for i, a := range sg.Args {
				gs.a[i] = calPt{X: float64(a.X) / em, Y: float64(a.Y) / em}
			}

			g.segs = append(g.segs, gs)
		}
	}

	f.cache[key] = g

	return g
}

func (f *calFontSet) measureMid(fc calFace) float64 {
	sum, count := 0.0, 0

	for _, r := range "0한" {
		top, bot := math.Inf(1), math.Inf(-1)

		for _, sg := range f.glyph(fc, r).segs {
			limit := 1

			switch sg.op {
			case sfnt.SegmentOpQuadTo:
				limit = 2
			case sfnt.SegmentOpCubeTo:
				limit = 3
			case sfnt.SegmentOpMoveTo, sfnt.SegmentOpLineTo:
			}

			for _, p := range sg.a[:limit] {
				top = min(top, p.Y)
				bot = max(bot, p.Y)
			}
		}

		if !math.IsInf(top, 0) {
			sum -= (top + bot) / 2
			count++
		}
	}

	if count == 0 {
		return 0.355
	}

	return sum / float64(count)
}

type calText struct {
	Size  float64
	Face  calFace
	Track float64
}

func (f *calFontSet) width(t calText, s string) float64 {
	w, count := 0.0, 0

	for _, r := range s {
		w += f.glyph(t.Face, r).adv * t.Size
		count++
	}

	if count > 1 {
		w += t.Track * float64(count-1)
	}

	return w
}

func (f *calFontSet) mid(t calText) float64 {
	return f.mids[t.Face] * t.Size
}

func (f *calFontSet) clamp(t calText, s string, maxW float64) string {
	if maxW <= 0 {
		return ""
	}

	if f.width(t, s) <= maxW {
		return s
	}

	runes := []rune(s)
	for len(runes) > 0 {
		runes = runes[:len(runes)-1]

		trimmed := strings.TrimRight(string(runes), " ")
		if trimmed == "" {
			return ""
		}

		cand := trimmed + "…"
		if f.width(t, cand) <= maxW {
			return cand
		}
	}

	return ""
}

type calAlign uint8

const (
	calAlignL calAlign = iota
	calAlignC
	calAlignR
)
