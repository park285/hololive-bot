package schedulecard

import (
	"fmt"
	"image/color"
	"math"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/assets/fonts"
)

type face uint8

const (
	faceBold face = iota
	faceXBold
	faceHand
	faceRound
	faceImpact
	faceSign
	faceFat
	faceRegular
	faceCount
)

type glyphSeg struct {
	op sfnt.SegmentOp
	a  [3]pt
}

type glyph struct {
	segs []glyphSeg
	adv  float64
}

// maxCachedGlyphs는 모델 출력 글자가 캐시 키가 되므로 글리프 캐시 메모리(글리프당 수 KB)를 묶는다.
const maxCachedGlyphs = 4096

type glyphKey struct {
	f *sfnt.Font
	r rune
}

// fontSet은 sfnt.Buffer와 글리프 캐시를 공유하므로 renderMu를 잡은 동안에만 쓴다.
type fontSet struct {
	faces [faceCount]*sfnt.Font
	jp    *sfnt.Font
	mids  [faceCount]float64
	buf   sfnt.Buffer
	cache map[glyphKey]*glyph
}

var loadFontSet = sync.OnceValues(func() (*fontSet, error) {
	data := fonts.ScheduleCard()
	files := [faceCount][]byte{
		faceRegular: data.PretendardRegular,
		faceBold:    data.PretendardBold,
		faceXBold:   data.PretendardExtraBold,
		faceHand:    data.YeonSung,
		faceRound:   data.Jua,
		faceImpact:  data.BlackHanSans,
		faceSign:    data.DoHyeon,
		faceFat:     data.BagelFatOne,
	}
	f := &fontSet{cache: map[glyphKey]*glyph{}}

	var err error

	for i, raw := range files {
		if f.faces[i], err = sfnt.Parse(raw); err != nil {
			return nil, fmt.Errorf("parse schedule card font %d: %w", i, err)
		}
	}

	if f.jp, err = sfnt.Parse(data.NotoSansJP); err != nil {
		return nil, fmt.Errorf("parse schedule card japanese font: %w", err)
	}

	for i := range f.faces {
		f.mids[i] = f.measureMid(face(i))
	}

	return f, nil
})

func (f *fontSet) lookup(src *sfnt.Font, r rune) (sfnt.GlyphIndex, bool) {
	gi, err := src.GlyphIndex(&f.buf, r)

	return gi, err == nil && gi != 0
}

func (f *fontSet) glyph(fc face, r rune) *glyph {
	src := f.faces[fc]
	if fc != faceXBold && boldSymbol(r) {
		src = f.faces[faceBold]
	}

	gi, ok := f.lookup(src, r)
	if !ok {
		src = f.faces[faceBold]
		if gi, ok = f.lookup(src, r); !ok {
			src = f.jp
			if gi, ok = f.lookup(src, r); !ok {
				return &glyph{}
			}
		}
	}

	key := glyphKey{src, r}
	if g, found := f.cache[key]; found {
		return g
	}

	if len(f.cache) >= maxCachedGlyphs {
		clear(f.cache)
	}

	upem := fixed.I(int(src.UnitsPerEm()))
	em := float64(src.UnitsPerEm()) * 64
	g := &glyph{}

	if adv, err := src.GlyphAdvance(&f.buf, gi, upem, font.HintingNone); err == nil {
		g.adv = float64(adv) / em
	}

	if segs, err := src.LoadGlyph(&f.buf, gi, upem, nil); err == nil {
		for _, sg := range segs {
			gs := glyphSeg{op: sg.Op}
			for i, a := range sg.Args {
				gs.a[i] = pt{float64(a.X) / em, float64(a.Y) / em}
			}

			g.segs = append(g.segs, gs)
		}
	}

	f.cache[key] = g

	return g
}

// covers는 r을 그릴 글꼴이 하나라도 있는지 본다. 공유 buf를 쓰지 않아 renderMu 없이 불러도 된다.
func (f *fontSet) covers(buf *sfnt.Buffer, r rune) bool {
	if gi, err := f.jp.GlyphIndex(buf, r); err == nil && gi != 0 {
		return true
	}

	for _, src := range f.faces {
		if gi, err := src.GlyphIndex(buf, r); err == nil && gi != 0 {
			return true
		}
	}

	return false
}

func (f *fontSet) drawable() func(rune) bool {
	var buf sfnt.Buffer

	return func(r rune) bool { return f.covers(&buf, r) }
}

func boldSymbol(r rune) bool {
	return r > unicode.MaxASCII && (unicode.IsSymbol(r) || unicode.Is(unicode.Pd, r) || r == '·')
}

func (f *fontSet) measureMid(fc face) float64 {
	sum, n := 0.0, 0

	for _, r := range "0한" {
		top, bot := math.Inf(1), math.Inf(-1)

		for _, sg := range f.glyph(fc, r).segs {
			k := 1

			switch sg.op {
			case sfnt.SegmentOpQuadTo:
				k = 2
			case sfnt.SegmentOpCubeTo:
				k = 3
			case sfnt.SegmentOpMoveTo, sfnt.SegmentOpLineTo:
			}

			for _, p := range sg.a[:k] {
				top, bot = min(top, p.Y), max(bot, p.Y)
			}
		}

		if !math.IsInf(top, 0) {
			sum -= (top + bot) / 2
			n++
		}
	}

	if n == 0 {
		return 0.355
	}

	return sum / float64(n)
}

type txt struct {
	Size  float64
	Face  face
	Track float64
}

func (t txt) scaled(k float64) txt {
	t.Size *= k
	t.Track *= k

	return t
}

func (f *fontSet) width(t txt, s string) float64 {
	w, n := 0.0, 0

	for _, r := range s {
		w += f.glyph(t.Face, r).adv * t.Size
		n++
	}

	if n > 1 {
		w += t.Track * float64(n-1)
	}

	return w
}

func (f *fontSet) mid(t txt) float64 { return f.mids[t.Face] * t.Size }

type align uint8

const (
	alignL align = iota
	alignC
	alignR
)

func (c *canvas) textShape(t txt, s string, x, y float64, al align) shape {
	f := c.f

	switch al {
	case alignC:
		x -= f.width(t, s) / 2
	case alignR:
		x -= f.width(t, s)
	case alignL:
	}

	var out shape

	for _, r := range s {
		g := f.glyph(t.Face, r)
		appendGlyph(&out, g, x, y, t.Size)

		x += g.adv*t.Size + t.Track
	}

	if len(out.subs) > 0 {
		out.Close()
	}

	return out
}

func appendGlyph(out *shape, g *glyph, x, y, size float64) {
	for _, sg := range g.segs {
		a := sg.a
		px := func(i int) (float64, float64) { return x + a[i].X*size, y + a[i].Y*size }

		switch sg.op {
		case sfnt.SegmentOpMoveTo:
			if len(out.subs) > 0 {
				out.Close()
			}

			out.MoveTo(px(0))
		case sfnt.SegmentOpLineTo:
			out.LineTo(px(0))
		case sfnt.SegmentOpQuadTo:
			cx, cy := px(0)
			ex, ey := px(1)
			out.QuadTo(cx, cy, ex, ey)
		case sfnt.SegmentOpCubeTo:
			c1x, c1y := px(0)
			c2x, c2y := px(1)
			ex, ey := px(2)
			out.CubeTo(c1x, c1y, c2x, c2y, ex, ey)
		}
	}
}

func (c *canvas) text(t txt, s string, x, y float64, al align, col color.RGBA) {
	c.Fill(c.textShape(t, s, x, y, al), col)
}

func (c *canvas) textMid(t txt, s string, x, y float64, al align, col color.RGBA) {
	c.text(t, s, x, y+c.f.mid(t), al, col)
}

func (c *canvas) textMidShape(t txt, s string) shape {
	return c.textShape(t, s, 0, c.f.mid(t), alignC)
}

func (f *fontSet) fitSize(t txt, s string, maxW, minSize float64) txt {
	for t.Size > minSize && f.width(t, s) > maxW {
		t.Size--
	}

	return t
}

func (f *fontSet) wrap(t txt, s string, maxW float64, maxLines int) (lines []string, cut bool) {
	rem := strings.TrimSpace(s)

	for len(lines) < maxLines {
		if f.width(t, rem) <= maxW {
			return append(lines, rem), cut
		}

		if len(lines) == maxLines-1 {
			return append(lines, f.clamp(t, rem, maxW)), true
		}

		runes := []rune(rem)

		end, brk := breakPoint(f, t, runes, maxW)
		if end <= 0 {
			return append(lines, f.clamp(t, rem, maxW)), true
		}

		switch {
		case brk > 0 && brk <= end:
			end = brk
		case runes[end] != ' ':
			cut = true
		}

		lines = append(lines, strings.TrimSpace(string(runes[:end])))
		rem = strings.TrimSpace(string(runes[end:]))
	}

	return lines, cut
}

func breakPoint(f *fontSet, t txt, runes []rune, maxW float64) (end, brk int) {
	w := 0.0

	for i, r := range runes {
		switch {
		case r == ' ':
			brk = i + 1
		case r == '(' && i > 0:
			brk = i
		}

		w += f.glyph(t.Face, r).adv * t.Size

		// width와 같은 순서로 더해야 width가 maxW를 넘는 글에서 반드시 끊을 자리를 찾는다.
		if w+t.Track*float64(i) > maxW {
			return i, brk
		}
	}

	return len(runes), brk
}

func (f *fontSet) clamp(t txt, s string, maxW float64) string {
	if f.width(t, s) <= maxW {
		return s
	}

	runes := []rune(s)
	for len(runes) > 0 {
		runes = runes[:len(runes)-1]
		if cand := strings.TrimRight(string(runes), " ") + "…"; f.width(t, cand) <= maxW {
			return cand
		}
	}

	return ""
}
