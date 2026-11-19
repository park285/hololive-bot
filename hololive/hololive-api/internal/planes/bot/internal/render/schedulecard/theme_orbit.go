package schedulecard

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"strconv"
	"sync"
	"time"

	xdraw "golang.org/x/image/draw"
)

var (
	orbitWhite = hex(0xFAF7FF)
	orbitGold  = hex(0xFFF17D)
	orbitLilac = hex(0xDBD4EE)
	orbitInk   = hex(0x302557)
)

const orbitHorizon = 226.0

func orbitLimb(y float64) float64 {
	v := (2*y - orbitTop - orbitBottom) / (orbitBottom - orbitTop)

	return -240 + 430*math.Sqrt(max(0, 1-0.54*v*v))
}

var orbitBackdrop = sync.OnceValue(func() *image.RGBA {
	w, h, _ := Format3x4.size()
	c := newCanvas(w, h, 1, nil)
	sky := image.NewRGBA(image.Rect(0, 0, 200, 288))
	top, bottom := hex(0x2E2268), hex(0x1B1741)
	band, halo := hex(0x45307E), hex(0x504395)

	for y := range sky.Rect.Dy() {
		v := float64(y) / float64(sky.Rect.Dy()-1)
		base := mix(top, bottom, v)
		dy := (v*c.h - 125) / 144

		for x := range sky.Rect.Dx() {
			u := float64(x) / float64(sky.Rect.Dx()-1)
			dx, d := (u-0.84)/0.20, (u+0.3*v-0.24)/0.42
			sky.SetRGBA(x, y, mix(mix(base, band, 0.24*math.Exp(-d*d)), halo, 0.65*math.Exp(-dx*dx-dy*dy)))
		}
	}

	xdraw.ApproxBiLinear.Scale(c.img, c.img.Bounds(), sky, sky.Bounds(), draw.Src, nil)
	orbitStars(c)
	orbitSurface(c)

	return c.img
})

func orbitStars(c *canvas) {
	seed := uint32(47)
	next := func() float64 {
		seed = seed*1664525 + 1013904223

		return float64(seed>>8) / (1 << 24)
	}

	var dim, bright shape

	for range 288 {
		x, y, r := next()*c.w, next()*c.h, 0.65+next()*1.15
		star := circle(x, y, r)

		if x < orbitTitleX || y < orbitHorizon || x > orbitRight+11 {
			bright.Add(star)
		} else {
			dim.Add(star)
		}
	}

	c.Fill(dim, hex(0x453A66))
	c.Fill(bright, hex(0x655984))
}

func orbitSurface(c *canvas) {
	var edge shape

	edge.MoveTo(orbitLimb(orbitHorizon), orbitHorizon)

	for y := orbitHorizon + 8; y < c.h; y += 8 {
		edge.LineTo(orbitLimb(y), y)
	}

	edge.LineTo(orbitLimb(c.h), c.h)

	var surface shape

	surface.MoveTo(0, orbitHorizon)

	for _, p := range edge.subs[0].pts {
		surface.LineTo(p.X, p.Y)
	}

	surface.LineTo(0, c.h)
	surface.Close()

	texture := image.NewRGBA(image.Rect(0, 0, 48, 180))
	lo, hi := hex(0x2C2364), hex(0x705DAC)

	for y := range texture.Rect.Dy() {
		v := (orbitHorizon + (c.h-orbitHorizon)*float64(y)/float64(texture.Rect.Dy()-1)) / c.h
		wave := 18*v + 1.7*math.Sin(9*v)
		base := 0.20 + 0.12*math.Sin(v*math.Pi)

		for x := range texture.Rect.Dx() {
			u := float64(x) / float64(texture.Rect.Dx()-1)
			t := base + 0.25*u + 0.13*math.Sin(wave+4*u+0.5*math.Sin(8*u))
			texture.SetRGBA(x, y, mix(lo, hi, t))
		}
	}

	c.Clip(surface, 1, func(l *canvas) {
		xdraw.ApproxBiLinear.Scale(l.img, l.img.Bounds(), texture, texture.Bounds(), draw.Src, nil)
	})

	for _, rim := range [...]struct {
		dx, width float64
		color     uint32
	}{
		{-79, 1.5, 0x544287}, {-28, 1.5, 0x584691}, {7, 2, 0x7162AD},
	} {
		c.StrokeButt(xf(edge, move(rim.dx, 0)), rim.width, hex(rim.color))
	}

	c.StrokeButt(edge, 9, rgba(0xA494E8, 0.16))
	c.StrokeButt(edge, 4, hex(0xB5A8ED))
}

func paintOrbitPage(c *canvas, v view, page orbitPage, number, pages int) {
	bg := orbitBackdrop()
	xdraw.ApproxBiLinear.Scale(c.img, c.img.Bounds(), bg, bg.Bounds(), draw.Src, nil)
	orbitHeader(c, v)

	for i := range page.groups {
		g := &page.groups[i]
		if g.total > 1 {
			orbitWash(c, g)
		}

		if i > 0 {
			c.Fill(rect(orbitTitleX, g.y-1, orbitRight-orbitTitleX, 2), hex(0x8E7DAD))
			c.Fill(rect(orbitTitleX, g.y-1.5, 25, 3), hex(0xBEAED2))
		}

		cy := orbitDateDisc(c, g)
		if len(g.rows) == 0 {
			c.Fill(rrect(orbitTitleX, cy-2, 30, 4, 2), hex(0xB3A4C9))
		}

		for j := range g.rows {
			r := &g.rows[j]
			if j > 0 {
				prev := &g.rows[j-1]
				y := (prev.y + prev.h + r.y) / 2
				c.Fill(rect(orbitTitleX, y-0.5, orbitRight-orbitTitleX, 1), hex(0x5C4D79))
			}

			orbitEntry(c, r)
		}
	}

	orbitFooter(c, v.updated, number, pages)
}

func orbitHeader(c *canvas, v view) {
	const nameWidth = 636

	t := c.f.fitSize(txt{Size: 88, Face: faceXBold}, v.name, nameWidth, 40)
	c.textMid(t, c.f.clamp(t, v.name, nameWidth), 59, 86, alignL, orbitWhite)

	t = c.f.fitSize(txt{Size: 48, Face: faceBold}, v.period, nameWidth, 32)
	c.textMid(t, v.period, 64, 173, alignL, orbitGold)

	const cx, cy, r = 872.0, 114.0, 92.0

	ring := func(a, b, start, end, width float64, ink color.RGBA) {
		var s shape

		s.Arc(0, 0, a, b, start, end)
		c.Stroke(xf(s, place(cx, cy+6, 1, -0.30)), width, ink)
	}
	ring(177, 64, 0, 2*math.Pi, 2, hex(0x7669A9))
	ring(170, 58, 0, 2*math.Pi, 3, hex(0xB8AFF0))
	c.Fill(circle(cx, cy, r+5), hex(0xE3DBFA))
	c.Medallion(nil, cx, cy, r, 0, 0xC3C7FB, 0)

	if v.photo != nil {
		b := v.photo.Bounds()
		k := 2 * r / float64(max(b.Dx(), b.Dy()))
		w, h := float64(b.Dx())*k, float64(b.Dy())*k
		c.Photo(v.photo, circle(cx, cy, r), box{cx - w/2, cy - h/2, w, h}, 0)
	}

	ring(170, 58, 0.045*math.Pi, 0.94*math.Pi, 6, orbitGold)
	c.Fill(sparkle(716, 50, 13), hex(0xF6E699))
	c.Fill(sparkle(1019, 167, 16), hex(0xE1D9FA))
	c.Fill(sparkle(997, 194, 6), hex(0x968ACA))
	c.Fill(rect(57, orbitHorizon-1, 963, 2), hex(0x786B9B))

	ends := sparkle(57, orbitHorizon, 7)
	ends.Add(sparkle(1020, orbitHorizon, 7))
	c.Fill(ends, hex(0xCDC0EB))
}

var orbitTints = func() (shades [23]image.Uniform) {
	for i := range shades {
		shades[i].C = rgba(0xB4A8F1, float64(i)/255)
	}

	return shades
}()

func orbitWash(c *canvas, g *orbitGroup) {
	start, end := g.y+3, g.y+g.h-3
	for y := int(math.Ceil(start * c.s)); y < int(end*c.s); y++ {
		yy := float64(y) / c.s
		fade := min(1, (yy-start)/16, (end-yy)/16)
		a := int(math.Round(22 * fade * (1 - 0.48*(yy-start)/(end-start))))
		x := int(math.Round((orbitLimb(yy) + 7) * c.s))
		draw.Draw(c.img, image.Rect(x, y, c.img.Rect.Max.X, y+1), &orbitTints[a], image.Point{}, draw.Over)
	}
}

func orbitDateDisc(c *canvas, g *orbitGroup) float64 {
	split := len(g.rows) != 0 && (g.start != 0 || g.rows[len(g.rows)-1].ordinal+1 != g.total)
	below := 55.0

	switch {
	case split:
		below = 134
	case g.total >= 3:
		below = 98
	}

	cy := g.y + g.h/2
	if len(g.rows) > 0 {
		cy = g.rows[0].y + 27
	}

	cy = max(g.y+53, min(cy, g.y+g.h-below))

	cx := orbitLimb(cy)
	c.Fill(rect(cx+50, cy-1, orbitTitleX-12-cx-50, 2), hex(0x9C8CBB))
	c.Fill(sparkle(orbitTitleX-12, cy, 4), hex(0xC1ACD5))
	c.Fill(circle(cx, cy, 53), hex(0x9485BA))
	c.Fill(circle(cx, cy, 51), hex(0x302656))

	fill := hex(0xE5DCF7)

	if day := g.date.Weekday(); day == time.Saturday || day == time.Sunday {
		fill = orbitGold
	}

	c.Fill(circle(cx, cy, 48), fill)
	c.textMid(txt{Size: 44, Face: faceXBold}, g.date.Format("02"), cx, cy-13, alignC, orbitInk)
	c.textMid(txt{Size: 34, Face: faceBold}, dowKO[weekday(g.date)], cx, cy+22, alignC, orbitInk)

	if g.total >= 3 || split {
		orbitCount(c, g, cx, cy+76, split)
	}

	return cy
}

func orbitCount(c *canvas, g *orbitGroup, cx, cy float64, split bool) {
	t := txt{Size: 32, Face: faceBold}
	unit := "회"

	if g.mixed {
		unit = "건"
	}

	count := strconv.Itoa(g.total) + unit
	span := ""
	h := 40.0

	if split {
		count += " 중"

		span = strconv.Itoa(g.start + 1)

		if last := g.rows[len(g.rows)-1].ordinal; last != g.start {
			span += "–" + strconv.Itoa(last+1)
		}

		span += "번째"
		h += 36
	}

	half := max(c.f.width(t, count), c.f.width(t, span))/2 + 12

	cx = min(max(cx, half+6), orbitTitleX-8-half)
	c.Fill(rrect(cx-half, cy-20, 2*half, h, 18), hex(0x74628F))
	c.Fill(rrect(cx-half+2, cy-18, 2*half-4, h-4, 16), hex(0x332953))
	c.textMid(t, count, cx, cy, alignC, hex(0xE8DFFA))

	if span != "" {
		c.textMid(t, span, cx, cy+36, alignC, hex(0xE8DFFA))
	}
}

func orbitEntry(c *canvas, r *orbitRow) {
	mid := r.y + 27
	ink := orbitWhite

	if r.status == StatusOff || r.status == StatusCancelled {
		ink = orbitLilac
	}

	for j, text := range r.title {
		y := mid + orbitTitleLH*float64(j)

		if r.status == StatusCancelled {
			textStruck(c, orbitTitleText, text, orbitTitleX, y, ink)
		} else {
			c.textMid(orbitTitleText, text, orbitTitleX, y, alignL, ink)
		}
	}

	top := r.y + float64(len(r.title))*orbitTitleLH + 24
	for j, text := range r.detail {
		c.textMid(orbitDetailText, text, orbitTitleX, top+orbitDetailLH*float64(j), alignL, orbitLilac)
	}

	orbitClock(c, r.label, r.kind, mid)
}

func orbitClock(c *canvas, label string, kind timeKind, mid float64) {
	if kind == timeNone || label == "" {
		return
	}

	ink := orbitGold

	if kind == timeCancelled {
		ink = orbitLilac
	}

	var width float64

	if len(label) == len("00:00") && label[2] == ':' {
		width = 151

		var digits shape

		base := mid + c.f.mid(orbitClockText)

		for i, offset := range [...]float64{16, 50, 75, 100, 134} {
			digits.Add(c.textShape(orbitClockText, label[i:i+1], orbitRight-width+offset, base, alignC))
		}

		c.Fill(digits, ink)
	} else {
		width = c.f.width(orbitClockText, label)
		c.textMid(orbitClockText, label, orbitRight-width, mid, alignL, ink)
	}

	x := orbitRight - width

	switch kind {
	case timeCancelled:
		strike(c, x, mid, width, orbitClockText, ink)
	case timeUnsure:
		c.StrokeButt(dash(line(x, mid+24, orbitRight, mid+24), 6, 5), 1.8, hex(0xCEBC80))
	case timeNone, timePlain, timeChanged:
	}
}

func orbitFooter(c *canvas, updated string, number, pages int) {
	const y = orbitBottom + 6

	c.Fill(rect(orbitTitleX, y-1, orbitRight-18-orbitTitleX, 2), hex(0x716294))
	c.Fill(sparkle(orbitRight, y, 7), hex(0xD2C3E7))

	right := orbitRight

	if pages > 1 {
		counter := strconv.Itoa(number) + " / " + strconv.Itoa(pages) + "쪽"
		t := txt{Size: 31, Face: faceBold}

		right -= c.f.width(t, counter) + 24
		c.textMid(t, counter, orbitRight, c.h-24, alignR, orbitWhite)
	}

	t := c.f.fitSize(orbitDetailText.scaled(29.0/36.0), updated, right-orbitTitleX, 18)
	c.textMid(t, updated, orbitTitleX, c.h-24, alignL, orbitLilac)
}
