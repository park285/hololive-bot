package schedulecard

import (
	"image/color"
	"math"
)

func rect(x, y, w, h float64) shape {
	return shape{subs: []subpath{{closed: true, pts: []pt{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}}}}}
}

func rrect(x, y, w, h, r float64) shape {
	r = min(r, w/2, h/2)
	if r <= 0 {
		return rect(x, y, w, h)
	}

	var s shape

	s.MoveTo(x+r, y)
	s.LineTo(x+w-r, y)
	s.Arc(x+w-r, y+r, r, r, -math.Pi/2, 0)
	s.LineTo(x+w, y+h-r)
	s.Arc(x+w-r, y+h-r, r, r, 0, math.Pi/2)
	s.LineTo(x+r, y+h)
	s.Arc(x+r, y+h-r, r, r, math.Pi/2, math.Pi)
	s.LineTo(x, y+r)
	s.Arc(x+r, y+r, r, r, math.Pi, 3*math.Pi/2)
	s.Close()

	return s
}

func ellipse(cx, cy, rx, ry float64) shape {
	n := curveSteps(2 * math.Pi * max(rx, ry))
	pts := make([]pt, n)

	for i := range n {
		a := 2 * math.Pi * float64(i) / float64(n)

		pts[i] = pt{cx + rx*math.Cos(a), cy + ry*math.Sin(a)}
	}

	return shape{subs: []subpath{{closed: true, pts: pts}}}
}

func circle(cx, cy, r float64) shape { return ellipse(cx, cy, r, r) }

func poly(xy ...float64) shape {
	pts := make([]pt, 0, len(xy)/2)
	for i := 0; i+1 < len(xy); i += 2 {
		pts = append(pts, pt{xy[i], xy[i+1]})
	}

	return shape{subs: []subpath{{closed: true, pts: pts}}}
}

func line(xy ...float64) shape {
	s := poly(xy...)

	s.subs[0].closed = false

	return s
}

func puffyStar(cx, cy, ro, ri, angle float64) shape {
	var pts [10]pt

	for i := range 10 {
		r := ro

		if i%2 == 1 {
			r = ri
		}

		a := angle - math.Pi/2 + float64(i)*math.Pi/5

		pts[i] = pt{cx + r*math.Cos(a), cy + r*math.Sin(a)}
	}

	var s shape

	s.MoveTo((pts[9].X+pts[0].X)/2, (pts[9].Y+pts[0].Y)/2)

	for i := range 10 {
		n := pts[(i+1)%10]
		s.QuadTo(pts[i].X, pts[i].Y, (pts[i].X+n.X)/2, (pts[i].Y+n.Y)/2)
	}

	s.Close()

	return s
}

func sparkle(cx, cy, r float64) shape {
	k := r * 0.2

	var s shape

	s.MoveTo(cx, cy-r)
	s.QuadTo(cx+k, cy-k, cx+r, cy)
	s.QuadTo(cx+k, cy+k, cx, cy+r)
	s.QuadTo(cx-k, cy+k, cx-r, cy)
	s.QuadTo(cx-k, cy-k, cx, cy-r)
	s.Close()

	return s
}

func heart(cx, cy, r float64) shape {
	var s shape

	s.MoveTo(cx, cy+r*0.80)
	s.CubeTo(cx-r*0.14, cy+r*0.60, cx-r*1.02, cy+r*0.18, cx-r*1.02, cy-r*0.30)
	s.CubeTo(cx-r*1.02, cy-r*0.78, cx-r*0.40, cy-r*0.92, cx, cy-r*0.40)
	s.CubeTo(cx+r*0.40, cy-r*0.92, cx+r*1.02, cy-r*0.78, cx+r*1.02, cy-r*0.30)
	s.CubeTo(cx+r*1.02, cy+r*0.18, cx+r*0.14, cy+r*0.60, cx, cy+r*0.80)
	s.Close()

	return s
}

func scallop(cx, cy, r, amp float64, n int) shape {
	steps := n * 16
	pts := make([]pt, steps)

	for i := range steps {
		a := 2 * math.Pi * float64(i) / float64(steps)
		rr := r + amp*math.Cos(float64(n)*a)

		pts[i] = pt{cx + rr*math.Cos(a), cy + rr*math.Sin(a)}
	}

	return shape{subs: []subpath{{closed: true, pts: pts}}}
}

func flameUnit() shape {
	var s shape

	s.MoveTo(0, 50)
	s.CubeTo(-30, 50, -50, 32, -50, 6)
	s.CubeTo(-50, -12, -42, -22, -40, -40)
	s.CubeTo(-32, -30, -27, -22, -21, -10)
	s.CubeTo(-20, -34, -10, -52, 6, -70)
	s.CubeTo(4, -50, 14, -32, 23, -16)
	s.CubeTo(29, -24, 34, -32, 36, -46)
	s.CubeTo(44, -30, 50, -12, 50, 6)
	s.CubeTo(50, 32, 30, 50, 0, 50)
	s.Close()

	return s
}

func flameCore() shape {
	var s shape

	s.MoveTo(2, 40)
	s.CubeTo(-16, 40, -25, 26, -23, 10)
	s.CubeTo(-21, -4, -8, -12, 0, -30)
	s.CubeTo(7, -12, 24, -2, 24, 13)
	s.CubeTo(24, 28, 16, 40, 2, 40)
	s.Close()

	return s
}

func drawFlame(c *canvas, cx, cy, h, angle float64, ink color.RGBA, lineW float64) {
	m := place(cx, cy, h/120, angle)
	body := xf(flameUnit(), m)

	if lineW > 0 {
		c.Fill(outline(body, lineW*2, false), ink)
	}

	_, y0, _, y1 := body.Bounds()
	c.FillLinear(body, 0, y0, 0, y1, stop{0, hex(0xC9F1FF)}, stop{0.55, hex(0x6CC8FB)}, stop{1, hex(0x3D9BF2)})
	c.Fill(xf(flameCore(), m), rgba(0xFFFFFF, 0.82))
}

func pawUnit() shape {
	var s shape

	s.MoveTo(0, 36)
	s.CubeTo(-22, 44, -42, 26, -36, 6)
	s.CubeTo(-30, -12, -12, -18, 0, -18)
	s.CubeTo(12, -18, 30, -12, 36, 6)
	s.CubeTo(42, 26, 22, 44, 0, 36)
	s.Close()

	for _, t := range [][3]float64{{-38, -22, -24}, {-14, -44, -8}, {14, -44, 8}, {38, -22, 24}} {
		s.Add(xf(ellipse(0, 0, 11.5, 16), rot(deg(t[2])).Then(move(t[0], t[1]))))
	}

	return s
}

func shieldUnit() shape {
	var s shape

	s.MoveTo(0, -60)
	s.CubeTo(-10, -50, -24, -46, -40, -54)
	s.CubeTo(-44, -40, -50, -30, -50, -12)
	s.CubeTo(-50, 24, -28, 48, 0, 62)
	s.CubeTo(28, 48, 50, 24, 50, -12)
	s.CubeTo(50, -30, 44, -40, 40, -54)
	s.CubeTo(24, -46, 10, -50, 0, -60)
	s.Close()

	return s
}

func perforated(w, h, r float64, nx, ny int) shape {
	var s shape

	edge := func(m affine, length float64, n int) {
		step := length / float64(n)

		for i := range n {
			cx := step * (float64(i) + 0.5)
			a := m.Pt(cx-r, 0)

			if len(s.subs) == 0 {
				s.MoveTo(a.X, a.Y)
			} else {
				s.LineTo(a.X, a.Y)
			}

			for k := 1; k <= 10; k++ {
				t := math.Pi - math.Pi*float64(k)/10
				p := m.Pt(cx+r*math.Cos(t), r*math.Sin(t))
				s.LineTo(p.X, p.Y)
			}
		}

		e := m.Pt(length, 0)
		s.LineTo(e.X, e.Y)
	}

	edge(move(-w/2, -h/2), w, nx)
	edge(rot(math.Pi/2).Then(move(w/2, -h/2)), h, ny)
	edge(rot(math.Pi).Then(move(w/2, h/2)), w, nx)
	edge(rot(3*math.Pi/2).Then(move(-w/2, h/2)), h, ny)
	s.Close()

	return s
}

type envelopeStyle struct {
	fill, ink, seal color.RGBA
	lineW           float64
}

func drawEnvelope(c *canvas, cx, cy, w, angle float64, st envelopeStyle) {
	h := w * 0.68
	m := place(cx, cy, 1, angle)
	body := xf(rrect(-w/2, -h/2, w, h, w*0.06), m)
	c.Fill(body, st.fill)
	c.Stroke(body, st.lineW, st.ink)
	c.Stroke(xf(line(-w/2+st.lineW, -h/2+st.lineW, 0, h*0.08, w/2-st.lineW, -h/2+st.lineW), m), st.lineW, st.ink)
	c.Fill(xf(heart(0, h*0.10, w*0.11), m), st.seal)
}

func stripes(c *canvas, b box, band, gap, angle float64, col color.RGBA) {
	d := math.Hypot(b.W, b.H)
	m := rotAt(angle, b.cx(), b.cy())

	for t := -d; t < d; t += band + gap {
		c.Fill(xf(rect(b.cx()+t, b.cy()-d, band, 2*d), m), col)
	}
}

func dots(c *canvas, b box, step, r float64, col color.RGBA) {
	var s shape

	for j, yy := 0, b.Y; yy < b.bottom()+step; j, yy = j+1, yy+step*0.866 {
		off := 0.0

		if j%2 == 1 {
			off = step / 2
		}

		for xx := b.X + off; xx < b.right()+step; xx += step {
			s.Add(circle(xx, yy, r))
		}
	}

	c.Fill(s, col)
}

func checker(c *canvas, b box, cell float64, col color.RGBA) {
	var s shape

	for j, yy := 0, b.Y; yy < b.bottom(); j, yy = j+1, yy+cell {
		for i, xx := 0, b.X; xx < b.right(); i, xx = i+1, xx+cell {
			if (i+j)%2 == 0 {
				s.Add(rect(xx, yy, cell, cell))
			}
		}
	}

	c.Fill(s, col)
}

func seigaiha(c *canvas, b box, r float64, bg, ink color.RGBA, lw float64) {
	for j, yy := 0, b.Y; yy < b.bottom()+r*1.5; j, yy = j+1, yy+r*0.5 {
		off := 0.0

		if j%2 == 1 {
			off = r
		}

		for xx := b.X - r + off; xx < b.right()+r; xx += 2 * r {
			c.Fill(circle(xx, yy, r), bg)

			for _, k := range []float64{1, 0.68, 0.36} {
				c.Stroke(circle(xx, yy, r*k-lw/2), lw, ink)
			}
		}
	}
}

func washiShape(w, h float64) shape {
	const teeth = 5

	var s shape

	s.MoveTo(-w/2, -h/2)
	s.LineTo(w/2, -h/2)

	for i := 1; i <= teeth*2; i++ {
		x := w / 2

		if i%2 == 1 {
			x -= h * 0.09
		}

		s.LineTo(x, -h/2+h*float64(i)/(teeth*2))
	}

	s.LineTo(-w/2, h/2)

	for i := 1; i <= teeth*2; i++ {
		x := -w / 2

		if i%2 == 1 {
			x += h * 0.09
		}

		s.LineTo(x, h/2-h*float64(i)/(teeth*2))
	}

	s.Close()

	return s
}

func washi(c *canvas, m affine, w, h float64, base color.RGBA, pattern func(l *canvas, m affine)) {
	tape := xf(washiShape(w, h), m)
	c.Shadow(tape, 0, 1.5, 3, rgba(0x7A4B50, 0.16))
	c.Clip(tape, 0.9, func(l *canvas) {
		l.Fill(tape, base)
		pattern(l, m)
	})
}

type saturnStyle struct {
	body, band, ring, ink color.RGBA
}

func drawSaturn(c *canvas, cx, cy, r, tilt float64, st saturnStyle) {
	m := place(cx, cy, 1, tilt)
	arc := func(a0, a1 float64) shape {
		var s shape

		s.Arc(0, 0, r*1.75, r*0.5, a0, a1)

		return xf(s, m)
	}

	back, front := arc(math.Pi, 2*math.Pi), arc(0, math.Pi)
	c.StrokeButt(back, r*0.30, st.ink)
	c.StrokeButt(back, r*0.18, st.ring)

	planet := circle(cx, cy, r)
	c.Fill(circle(cx, cy, r+r*0.07), st.ink)
	c.Clip(planet, 1, func(l *canvas) {
		l.Fill(planet, st.body)
		l.Fill(xf(rrect(-r*1.3, -r*0.50, r*2.6, r*0.22, r*0.11), m), st.band)
		l.Fill(xf(rrect(-r*1.3, r*0.02, r*2.6, r*0.16, r*0.08), m), st.band)
		l.Fill(xf(rrect(-r*1.3, r*0.44, r*2.6, r*0.12, r*0.06), m), st.band)
	})
	c.StrokeButt(front, r*0.30, st.ink)
	c.StrokeButt(front, r*0.18, st.ring)
}

type rocketParts struct {
	body, nose, finL, finR, fire, fireIn, nozzle shape
}

func newRocketParts() rocketParts {
	var p rocketParts

	p.body.MoveTo(0, -62)
	p.body.CubeTo(22, -42, 27, -6, 21, 34)
	p.body.LineTo(-21, 34)
	p.body.CubeTo(-27, -6, -22, -42, 0, -62)
	p.body.Close()

	p.nose.MoveTo(0, -62)
	p.nose.CubeTo(10, -53, 17, -43, 21, -30)
	p.nose.LineTo(-21, -30)
	p.nose.CubeTo(-17, -43, -10, -53, 0, -62)
	p.nose.Close()

	p.finL.MoveTo(-22, 4)
	p.finL.CubeTo(-38, 14, -44, 32, -41, 50)
	p.finL.LineTo(-20, 34)
	p.finL.Close()

	p.finR.MoveTo(22, 4)
	p.finR.CubeTo(38, 14, 44, 32, 41, 50)
	p.finR.LineTo(20, 34)
	p.finR.Close()

	p.fire.MoveTo(-13, 40)
	p.fire.CubeTo(-16, 56, -5, 62, 0, 78)
	p.fire.CubeTo(5, 62, 16, 56, 13, 40)
	p.fire.Close()

	p.fireIn.MoveTo(-6, 40)
	p.fireIn.CubeTo(-8, 50, -2, 54, 0, 62)
	p.fireIn.CubeTo(2, 54, 8, 50, 6, 40)
	p.fireIn.Close()

	p.nozzle = rrect(-13, 33, 26, 9, 3)

	return p
}

func drawRocket(c *canvas, cx, cy, scale, angle float64, ink color.RGBA) {
	m := place(cx, cy, scale, angle)
	p := newRocketParts()

	var sil shape

	for _, part := range []shape{p.body, p.finL, p.finR, p.fire, p.nozzle} {
		sil.Add(part)
	}

	sil = xf(sil, m)
	c.Shadow(sil, 0, 3*scale, 5*scale, rgba(0x5C63C8, 0.35))
	c.Fill(outline(sil, 13*scale, false), ink)
	c.Fill(outline(sil, 8*scale, false), hex(0xFFFFFF))

	c.Fill(xf(p.fire, m), hex(0xFFA9D2))
	c.Fill(xf(p.fireIn, m), hex(0xFFF06A))
	c.Fill(xf(p.finL, m), hex(0x8FB6FF))
	c.Fill(xf(p.finR, m), hex(0x8FB6FF))
	c.Fill(xf(p.nozzle, m), hex(0xC7CBF5))
	c.Fill(xf(p.body, m), hex(0xFFFFFF))
	c.Fill(xf(p.nose, m), hex(0xFFA9D2))
	c.Fill(xf(circle(0, -8, 12.5), m), ink)
	c.Fill(xf(circle(0, -8, 9), m), hex(0x7FD4FF))
	c.Fill(xf(circle(-3, -11, 3), m), rgba(0xFFFFFF, 0.9))
}
