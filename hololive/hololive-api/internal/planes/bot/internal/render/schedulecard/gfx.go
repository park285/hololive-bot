package schedulecard

import (
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"math"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/math/f64"
	"golang.org/x/image/vector"
)

type pt struct{ X, Y float64 }

type subpath struct {
	pts    []pt
	closed bool
}

// shape는 비영 감기 규칙으로 채운다. 반대 방향으로 감긴 하위 경로는 구멍이 된다.
type shape struct{ subs []subpath }

func (s *shape) MoveTo(x, y float64) { s.subs = append(s.subs, subpath{pts: []pt{{x, y}}}) }

func (s *shape) last() *subpath {
	if len(s.subs) == 0 {
		s.subs = append(s.subs, subpath{})
	}

	return &s.subs[len(s.subs)-1]
}

func (s *shape) pen() pt {
	l := s.last()
	if len(l.pts) == 0 {
		return pt{}
	}

	return l.pts[len(l.pts)-1]
}

func (s *shape) LineTo(x, y float64) {
	l := s.last()

	l.pts = append(l.pts, pt{x, y})
}

func curveSteps(length float64) int { return max(4, min(72, int(length*0.6)+3)) }

func (s *shape) QuadTo(cx, cy, x, y float64) {
	p := s.pen()
	n := curveSteps(math.Hypot(cx-p.X, cy-p.Y) + math.Hypot(x-cx, y-cy))

	for i := 1; i <= n; i++ {
		t := float64(i) / float64(n)
		u := 1 - t
		s.LineTo(u*u*p.X+2*u*t*cx+t*t*x, u*u*p.Y+2*u*t*cy+t*t*y)
	}
}

func (s *shape) CubeTo(c1x, c1y, c2x, c2y, x, y float64) {
	p := s.pen()
	n := curveSteps(math.Hypot(c1x-p.X, c1y-p.Y) + math.Hypot(c2x-c1x, c2y-c1y) + math.Hypot(x-c2x, y-c2y))

	for i := 1; i <= n; i++ {
		t := float64(i) / float64(n)
		u := 1 - t
		a, b, c, d := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
		s.LineTo(a*p.X+b*c1x+c*c2x+d*x, a*p.Y+b*c1y+c*c2y+d*y)
	}
}

// Arc는 a0→a1(라디안)로 타원 호를 잇는다. Y가 아래로 커지므로 양의 방향이 시계 방향이다.
func (s *shape) Arc(cx, cy, rx, ry, a0, a1 float64) {
	n := curveSteps(math.Abs(a1-a0) * max(rx, ry))

	for i := 0; i <= n; i++ {
		a := a0 + (a1-a0)*float64(i)/float64(n)
		x, y := cx+rx*math.Cos(a), cy+ry*math.Sin(a)

		if i == 0 && (len(s.subs) == 0 || len(s.last().pts) == 0) {
			s.MoveTo(x, y)

			continue
		}

		s.LineTo(x, y)
	}
}

func (s *shape) Close() { s.last().closed = true }

func (s *shape) Add(o shape) { s.subs = append(s.subs, o.subs...) }

func (s *shape) Bounds() (x0, y0, x1, y1 float64) {
	x0, y0, x1, y1 = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)

	for _, sb := range s.subs {
		for _, p := range sb.pts {
			x0, y0, x1, y1 = min(x0, p.X), min(y0, p.Y), max(x1, p.X), max(y1, p.Y)
		}
	}

	return x0, y0, x1, y1
}

type affine struct{ A, B, C, D, E, F float64 }

func move(tx, ty float64) affine { return affine{1, 0, tx, 0, 1, ty} }
func scl(sx, sy float64) affine  { return affine{sx, 0, 0, 0, sy, 0} }

func rot(a float64) affine {
	c, s := math.Cos(a), math.Sin(a)

	return affine{c, -s, 0, s, c, 0}
}

// Then은 m을 먼저, n을 나중에 적용한 변환이다.
func (m affine) Then(n affine) affine {
	return affine{
		n.A*m.A + n.B*m.D, n.A*m.B + n.B*m.E, n.A*m.C + n.B*m.F + n.C,
		n.D*m.A + n.E*m.D, n.D*m.B + n.E*m.E, n.D*m.C + n.E*m.F + n.F,
	}
}

func rotAt(a, cx, cy float64) affine { return move(-cx, -cy).Then(rot(a)).Then(move(cx, cy)) }

func place(cx, cy, scale, angle float64) affine {
	return scl(scale, scale).Then(rot(angle)).Then(move(cx, cy))
}

func (m affine) Pt(x, y float64) pt { return pt{m.A*x + m.B*y + m.C, m.D*x + m.E*y + m.F} }

func xf(s shape, m affine) shape {
	out := shape{subs: make([]subpath, len(s.subs))}

	for i, sb := range s.subs {
		pts := make([]pt, len(sb.pts))
		for j, p := range sb.pts {
			pts[j] = m.Pt(p.X, p.Y)
		}

		out.subs[i] = subpath{pts: pts, closed: sb.closed}
	}

	return out
}

func deg(d float64) float64 { return d * math.Pi / 180 }

func hex(v uint32) color.RGBA {
	r, g, b := channels(v)

	return color.RGBA{R: r, G: g, B: b, A: 255}
}

func channels(v uint32) (r, g, b uint8) {
	var raw [4]byte

	binary.BigEndian.PutUint32(raw[:], v)

	return raw[1], raw[2], raw[3]
}

func rgba(v uint32, a float64) color.RGBA {
	a = max(0, min(1, a))

	r, g, b := channels(v)

	return color.RGBA{
		R: uint8(float64(r)*a + 0.5),
		G: uint8(float64(g)*a + 0.5),
		B: uint8(float64(b)*a + 0.5),
		A: uint8(255*a + 0.5),
	}
}

func mix(a, b color.RGBA, t float64) color.RGBA {
	t = max(0, min(1, t))

	l := func(x, y uint8) uint8 { return uint8(float64(x)*(1-t) + float64(y)*t + 0.5) }

	return color.RGBA{R: l(a.R, b.R), G: l(a.G, b.G), B: l(a.B, b.B), A: l(a.A, b.A)}
}

type canvas struct {
	img  *image.RGBA
	s    float64
	f    *fontSet
	w, h float64
}

func newCanvas(w, h int, s float64, f *fontSet) *canvas {
	return &canvas{
		img: image.NewRGBA(image.Rect(0, 0, int(float64(w)*s), int(float64(h)*s))),
		s:   s, f: f, w: float64(w), h: float64(h),
	}
}

func (c *canvas) devRect(s shape, pad float64) image.Rectangle {
	x0, y0, x1, y1 := s.Bounds()
	if math.IsInf(x0, 0) || math.IsNaN(x0+y0+x1+y1) {
		return image.Rectangle{}
	}

	r := image.Rect(
		int(math.Floor((x0-pad)*c.s))-1, int(math.Floor((y0-pad)*c.s))-1,
		int(math.Ceil((x1+pad)*c.s))+1, int(math.Ceil((y1+pad)*c.s))+1,
	)

	return r.Intersect(c.img.Bounds())
}

func (c *canvas) raster(s shape, r image.Rectangle) *vector.Rasterizer {
	z := vector.NewRasterizer(r.Dx(), r.Dy())

	z.DrawOp = draw.Over

	ox, oy := float64(r.Min.X), float64(r.Min.Y)

	for _, sb := range s.subs {
		if len(sb.pts) < 2 {
			continue
		}

		for i, p := range sb.pts {
			x, y := float32(p.X*c.s-ox), float32(p.Y*c.s-oy)

			if i == 0 {
				z.MoveTo(x, y)
			} else {
				z.LineTo(x, y)
			}
		}

		z.ClosePath()
	}

	return z
}

func (c *canvas) Fill(s shape, col color.RGBA) {
	r := c.devRect(s, 0)
	if r.Empty() {
		return
	}

	c.raster(s, r).Draw(c.img, r, image.NewUniform(col), image.Point{})
}

func (c *canvas) Mask(s shape, pad float64) (*image.Alpha, image.Rectangle) {
	r := c.devRect(s, pad)
	if r.Empty() {
		return nil, r
	}

	m := image.NewAlpha(r)
	z := c.raster(s, r)

	z.DrawOp = draw.Src
	z.Draw(m, r, image.Opaque, image.Point{})

	return m, r
}

type stop struct {
	T float64
	C color.RGBA
}

func gradAt(stops []stop, t float64) color.RGBA {
	if t <= stops[0].T {
		return stops[0].C
	}

	for i := 1; i < len(stops); i++ {
		if t <= stops[i].T {
			return mix(stops[i-1].C, stops[i].C, (t-stops[i-1].T)/(stops[i].T-stops[i-1].T))
		}
	}

	return stops[len(stops)-1].C
}

func (c *canvas) FillLinear(s shape, x0, y0, x1, y1 float64, stops ...stop) {
	r := c.devRect(s, 0)
	if r.Empty() || len(stops) == 0 {
		return
	}

	g := image.NewRGBA(r)
	dx, dy := (x1-x0)*c.s, (y1-y0)*c.s
	den := max(dx*dx+dy*dy, 1e-9)

	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			t := ((float64(x)-x0*c.s)*dx + (float64(y)-y0*c.s)*dy) / den
			g.SetRGBA(x, y, gradAt(stops, t))
		}
	}

	c.raster(s, r).Draw(c.img, r, g, r.Min)
}

func (c *canvas) Glow(cx, cy, radius float64, v uint32, alpha float64) {
	r := image.Rect(int((cx-radius)*c.s), int((cy-radius)*c.s), int((cx+radius)*c.s)+1, int((cy+radius)*c.s)+1).Intersect(c.img.Bounds())
	if r.Empty() {
		return
	}

	m := image.NewAlpha(r)

	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			d := math.Hypot(float64(x)/c.s-cx, float64(y)/c.s-cy) / radius
			if d >= 1 {
				continue
			}

			t := 1 - d

			m.Pix[m.PixOffset(x, y)] = alphaByte(alpha * t * t * (3 - 2*t))
		}
	}

	draw.DrawMask(c.img, r, image.NewUniform(hex(v)), image.Point{}, m, r.Min, draw.Over)
}

func alphaByte(a float64) uint8 { return uint8(max(0, min(1, a))*255 + 0.5) }

func outline(s shape, w float64, butt bool) shape {
	h := w / 2
	n := max(10, min(48, int(h*4)+8))
	ring := make([]pt, n)

	for i := range n {
		a := 2 * math.Pi * float64(i) / float64(n)

		ring[i] = pt{h * math.Cos(a), h * math.Sin(a)}
	}

	var o shape

	for _, sb := range s.subs {
		pts := closedPoints(sb)

		for i := 0; i+1 < len(pts); i++ {
			p, q := pts[i], pts[i+1]
			l := math.Hypot(q.X-p.X, q.Y-p.Y)

			if l < 1e-6 {
				continue
			}

			nx, ny := -(q.Y-p.Y)/l*h, (q.X-p.X)/l*h

			o.subs = append(o.subs, subpath{closed: true, pts: []pt{
				{p.X - nx, p.Y - ny}, {q.X - nx, q.Y - ny}, {q.X + nx, q.Y + ny}, {p.X + nx, p.Y + ny},
			}})
		}

		for i, p := range pts {
			end := i == 0 || i == len(pts)-1
			if (sb.closed && i == len(pts)-1) || (butt && !sb.closed && end) {
				continue
			}

			joint := make([]pt, n)

			for k, d := range ring {
				joint[k] = pt{p.X + d.X, p.Y + d.Y}
			}

			o.subs = append(o.subs, subpath{closed: true, pts: joint})
		}
	}

	return o
}

func closedPoints(sb subpath) []pt {
	if sb.closed && len(sb.pts) > 1 {
		return append(append(make([]pt, 0, len(sb.pts)+1), sb.pts...), sb.pts[0])
	}

	return sb.pts
}

func (c *canvas) Stroke(s shape, w float64, col color.RGBA)     { c.Fill(outline(s, w, false), col) }
func (c *canvas) StrokeButt(s shape, w float64, col color.RGBA) { c.Fill(outline(s, w, true), col) }

func dash(s shape, on, off float64) shape {
	var o shape

	for _, sb := range s.subs {
		pts := closedPoints(sb)
		drawing, left := true, on

		if len(pts) > 0 {
			o.MoveTo(pts[0].X, pts[0].Y)
		}

		for i := 0; i+1 < len(pts); i++ {
			p, q := pts[i], pts[i+1]
			l := math.Hypot(q.X-p.X, q.Y-p.Y)
			pos := 0.0

			for l-pos > left {
				pos += left

				x, y := p.X+(q.X-p.X)*pos/l, p.Y+(q.Y-p.Y)*pos/l

				if drawing {
					o.LineTo(x, y)

					left = off
				} else {
					o.MoveTo(x, y)

					left = on
				}

				drawing = !drawing
			}

			left -= l - pos

			if drawing {
				o.LineTo(q.X, q.Y)
			}
		}
	}

	return o
}

func (c *canvas) Shadow(s shape, dx, dy, blur float64, col color.RGBA) {
	m, r := c.Mask(xf(s, move(dx, dy)), blur*2)
	if m == nil {
		return
	}

	boxBlur(m, max(1, int(blur*c.s/2)))
	draw.DrawMask(c.img, r, image.NewUniform(col), image.Point{}, m, r.Min, draw.Over)
}

func boxBlur(m *image.Alpha, rad int) {
	w, h := m.Rect.Dx(), m.Rect.Dy()
	tmp := make([]uint8, max(w, h))

	pass := func(n, start, step int) {
		for range 3 {
			sum := 0

			for i := -rad; i <= rad; i++ {
				sum += int(m.Pix[start+min(max(i, 0), n-1)*step])
			}

			for i := range n {
				tmp[i] = uint8(float64(sum) / float64(2*rad+1))

				sum += int(m.Pix[start+min(i+rad+1, n-1)*step]) - int(m.Pix[start+max(i-rad, 0)*step])
			}

			for i := range n {
				m.Pix[start+i*step] = tmp[i]
			}
		}
	}

	for y := range h {
		pass(w, y*m.Stride, 1)
	}

	for x := range w {
		pass(h, x, m.Stride)
	}
}

func (c *canvas) Clip(s shape, opacity float64, fn func(l *canvas)) {
	m, r := c.Mask(s, 0)
	if m == nil {
		return
	}

	l := &canvas{img: image.NewRGBA(r), s: c.s, f: c.f, w: c.w, h: c.h}
	fn(l)

	if opacity < 1 {
		for i, v := range m.Pix {
			m.Pix[i] = uint8(float64(v) * opacity)
		}
	}

	draw.DrawMask(c.img, r, l.img, r.Min, m, r.Min, draw.Over)
}

func (c *canvas) Photo(src image.Image, clip shape, b box, angle float64) {
	m, r := c.Mask(clip, 0)
	if m == nil || src == nil {
		return
	}

	sb := src.Bounds()
	sw, sh := float64(sb.Dx()), float64(sb.Dy())
	k := max(b.W*c.s/sw, b.H*c.s/sh)
	scx := float64(sb.Min.X) + sw/2
	scy := float64(sb.Min.Y) + sh/2
	cx, cy := b.cx()*c.s, b.cy()*c.s
	cs, sn := math.Cos(angle)*k, math.Sin(angle)*k
	aff := f64.Aff3{cs, -sn, cx - (cs*scx - sn*scy), sn, cs, cy - (sn*scx + cs*scy)}

	l := image.NewRGBA(r)
	xdraw.CatmullRom.Transform(l, aff, src, sb, xdraw.Src, nil)
	draw.DrawMask(c.img, r, l, r.Min, m, r.Min, draw.Over)
}

func (c *canvas) Vignette(clip shape, cx, cy, r0, r1 float64, v uint32, alpha float64) {
	m, r := c.Mask(clip, 0)
	if m == nil {
		return
	}

	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := m.PixOffset(x, y)
			if m.Pix[i] == 0 {
				continue
			}

			t := min(1, (math.Hypot(float64(x)/c.s-cx, float64(y)/c.s-cy)-r0)/(r1-r0))
			if t <= 0 {
				m.Pix[i] = 0

				continue
			}

			m.Pix[i] = alphaByte(alpha * t * t * (3 - 2*t) * float64(m.Pix[i]) / 255)
		}
	}

	draw.DrawMask(c.img, r, image.NewUniform(hex(v)), image.Point{}, m, r.Min, draw.Over)
}

func (c *canvas) Medallion(photo image.Image, cx, cy, r, angle float64, tint uint32, alpha float64) {
	clip := circle(cx, cy, r)

	if photo == nil {
		c.Fill(clip, hex(tint))

		return
	}

	c.Photo(photo, clip, box{cx - r, cy - r, 2 * r, 2 * r}, angle)
	c.Vignette(clip, cx, cy, r*0.74, r*1.04, tint, alpha)
}

func (c *canvas) Downscale(w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), c.img, c.img.Bounds(), xdraw.Src, nil)

	return dst
}
