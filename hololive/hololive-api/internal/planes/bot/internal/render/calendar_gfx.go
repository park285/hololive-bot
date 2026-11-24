package render

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/f64"
	"golang.org/x/image/vector"
)

type calPt struct {
	X, Y float64
}

type calSubpath struct {
	pts    []calPt
	closed bool
}

type calShape struct {
	subs []calSubpath
}

func (s *calShape) MoveTo(x, y float64) {
	s.subs = append(s.subs, calSubpath{pts: []calPt{{X: x, Y: y}}})
}

func (s *calShape) last() *calSubpath {
	if len(s.subs) == 0 {
		s.subs = append(s.subs, calSubpath{})
	}

	return &s.subs[len(s.subs)-1]
}

func (s *calShape) pen() calPt {
	l := s.last()
	if len(l.pts) == 0 {
		return calPt{}
	}

	return l.pts[len(l.pts)-1]
}

func (s *calShape) LineTo(x, y float64) {
	l := s.last()

	l.pts = append(l.pts, calPt{X: x, Y: y})
}

func calCurveSteps(length float64) int {
	return max(4, min(72, int(length*0.6)+3))
}

func (s *calShape) QuadTo(cx, cy, x, y float64) {
	p := s.pen()
	n := calCurveSteps(math.Hypot(cx-p.X, cy-p.Y) + math.Hypot(x-cx, y-cy))

	for i := 1; i <= n; i++ {
		t := float64(i) / float64(n)
		u := 1 - t
		s.LineTo(u*u*p.X+2*u*t*cx+t*t*x, u*u*p.Y+2*u*t*cy+t*t*y)
	}
}

func (s *calShape) CubeTo(c1x, c1y, c2x, c2y, x, y float64) {
	p := s.pen()
	n := calCurveSteps(math.Hypot(c1x-p.X, c1y-p.Y) + math.Hypot(c2x-c1x, c2y-c1y) + math.Hypot(x-c2x, y-c2y))

	for i := 1; i <= n; i++ {
		t := float64(i) / float64(n)
		u := 1 - t
		a, b, c, d := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
		s.LineTo(a*p.X+b*c1x+c*c2x+d*x, a*p.Y+b*c1y+c*c2y+d*y)
	}
}

func (s *calShape) Arc(cx, cy, rx, ry, a0, a1 float64) {
	n := calCurveSteps(math.Abs(a1-a0) * max(rx, ry))

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

func (s *calShape) Close() {
	s.last().closed = true
}

func (s *calShape) Add(o calShape) {
	s.subs = append(s.subs, o.subs...)
}

func (s *calShape) Bounds() (x0, y0, x1, y1 float64) {
	x0, y0, x1, y1 = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)

	for _, sb := range s.subs {
		for _, p := range sb.pts {
			x0 = min(x0, p.X)
			y0 = min(y0, p.Y)
			x1 = max(x1, p.X)
			y1 = max(y1, p.Y)
		}
	}

	return x0, y0, x1, y1
}

type calAffine struct {
	A, B, C, D, E, F float64
}

func calMove(tx, ty float64) calAffine {
	return calAffine{A: 1, C: tx, E: 1, F: ty}
}

func calScale(sx, sy float64) calAffine {
	return calAffine{A: sx, E: sy}
}

func calRot(a float64) calAffine {
	c, s := math.Cos(a), math.Sin(a)

	return calAffine{A: c, B: -s, D: s, E: c}
}

func (m calAffine) Then(n calAffine) calAffine {
	return calAffine{
		A: n.A*m.A + n.B*m.D,
		B: n.A*m.B + n.B*m.E,
		C: n.A*m.C + n.B*m.F + n.C,
		D: n.D*m.A + n.E*m.D,
		E: n.D*m.B + n.E*m.E,
		F: n.D*m.C + n.E*m.F + n.F,
	}
}

func calPlace(cx, cy, s, a float64) calAffine {
	return calScale(s, s).Then(calRot(a)).Then(calMove(cx, cy))
}

func (m calAffine) Pt(x, y float64) calPt {
	return calPt{X: m.A*x + m.B*y + m.C, Y: m.D*x + m.E*y + m.F}
}

func calDeg(d float64) float64 {
	return d * math.Pi / 180
}

func calTransform(s calShape, m calAffine) calShape {
	out := calShape{subs: make([]calSubpath, len(s.subs))}

	for i, sb := range s.subs {
		pts := make([]calPt, len(sb.pts))

		for j, p := range sb.pts {
			pts[j] = m.Pt(p.X, p.Y)
		}

		out.subs[i] = calSubpath{pts: pts, closed: sb.closed}
	}

	return out
}

type calBox struct {
	X, Y, W, H float64
}

func (b calBox) right() float64           { return b.X + b.W }
func (b calBox) bottom() float64          { return b.Y + b.H }
func (b calBox) cx() float64              { return b.X + b.W/2 }
func (b calBox) cy() float64              { return b.Y + b.H/2 }
func (b calBox) rect() calShape           { return calRect(b.X, b.Y, b.W, b.H) }
func (b calBox) rrect(r float64) calShape { return calRRect(b.X, b.Y, b.W, b.H, r) }

type calStop struct {
	T float64
	C color.RGBA
}

type calCanvas struct {
	img  *image.RGBA
	z    *vector.Rasterizer
	s    float64
	f    *calFontSet
	w, h float64
}

func newCalCanvas(w, h int, s float64, f *calFontSet) *calCanvas {
	return &calCanvas{
		img: image.NewRGBA(image.Rect(0, 0, int(math.Round(float64(w)*s)), int(math.Round(float64(h)*s)))),
		z:   &vector.Rasterizer{},
		s:   s,
		f:   f,
		w:   float64(w),
		h:   float64(h),
	}
}

func (c *calCanvas) devRect(s calShape, pad float64) image.Rectangle {
	x0, y0, x1, y1 := s.Bounds()
	if math.IsInf(x0, 0) || math.IsNaN(x0+y0+x1+y1) {
		return image.Rectangle{}
	}

	r := image.Rect(
		int(math.Floor((x0-pad)*c.s))-1,
		int(math.Floor((y0-pad)*c.s))-1,
		int(math.Ceil((x1+pad)*c.s))+1,
		int(math.Ceil((y1+pad)*c.s))+1,
	)

	return r.Intersect(c.img.Bounds())
}

func (c *calCanvas) raster(s calShape, r image.Rectangle) *vector.Rasterizer {
	z := c.z
	z.Reset(r.Dx(), r.Dy())

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

func (c *calCanvas) Fill(s calShape, col color.RGBA) {
	r := c.devRect(s, 0)
	if r.Empty() {
		return
	}

	c.raster(s, r).Draw(c.img, r, image.NewUniform(col), image.Point{})
}

func (c *calCanvas) StrokeInsetRect(b calBox, w float64, col color.RGBA) {
	if b.W <= 2*w || b.H <= 2*w {
		return
	}

	c.Fill(calRect(b.X, b.Y, b.W, w), col)
	c.Fill(calRect(b.X, b.bottom()-w, b.W, w), col)
	c.Fill(calRect(b.X, b.Y+w, w, b.H-2*w), col)
	c.Fill(calRect(b.right()-w, b.Y+w, w, b.H-2*w), col)
}

func (c *calCanvas) FillHatch(b calBox, baseCol, stripeCol color.RGBA, pitch, thickness float64) {
	c.Clip(b.rect(), 1, func(l *calCanvas) {
		l.Fill(b.rect(), baseCol)

		span := b.W + b.H
		dx := thickness * 0.7071

		for d := -b.H; d < span; d += pitch {
			x0 := b.X + d
			y0 := b.Y + b.H
			x1 := b.X + d + b.H
			y1 := b.Y

			l.Fill(calPoly(
				calPt{X: x0, Y: y0},
				calPt{X: x0 + dx, Y: y0},
				calPt{X: x1 + dx, Y: y1},
				calPt{X: x1, Y: y1},
			), stripeCol)
		}
	})
}

func (c *calCanvas) Mask(s calShape, pad float64) (*image.Alpha, image.Rectangle) {
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

func calGradAt(stops []calStop, t float64) color.RGBA {
	if t <= stops[0].T {
		return stops[0].C
	}

	for i := 1; i < len(stops); i++ {
		if t <= stops[i].T {
			return calMix(stops[i-1].C, stops[i].C, (t-stops[i-1].T)/(stops[i].T-stops[i-1].T))
		}
	}

	return stops[len(stops)-1].C
}

func (c *calCanvas) FillLinear(s calShape, x0, y0, x1, y1 float64, stops ...calStop) {
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
			g.SetRGBA(x, y, calGradAt(stops, t))
		}
	}

	c.raster(s, r).Draw(c.img, r, g, r.Min)
}

func (c *calCanvas) Glow(cx, cy, radius float64, v uint32, alpha float64) {
	r := image.Rect(
		int((cx-radius)*c.s),
		int((cy-radius)*c.s),
		int((cx+radius)*c.s)+1,
		int((cy+radius)*c.s)+1,
	).Intersect(c.img.Bounds())
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
			a := alpha * t * t * (3 - 2*t)

			m.Pix[m.PixOffset(x, y)] = uint8(max(0, min(1, a))*255 + 0.5)
		}
	}

	draw.DrawMask(c.img, r, image.NewUniform(calHex(v)), image.Point{}, m, r.Min, draw.Over)
}

func calClosedPoints(sb calSubpath) []calPt {
	if sb.closed && len(sb.pts) > 1 {
		out := make([]calPt, 0, len(sb.pts)+1)

		out = append(out, sb.pts...)

		return append(out, sb.pts[0])
	}

	return sb.pts
}

func calOutline(s calShape, w float64) calShape {
	h := w / 2
	n := max(10, min(48, int(h*4)+8))
	ring := make([]calPt, n)

	for i := range n {
		a := 2 * math.Pi * float64(i) / float64(n)

		ring[i] = calPt{X: h * math.Cos(a), Y: h * math.Sin(a)}
	}

	var out calShape

	for _, sb := range s.subs {
		pts := calClosedPoints(sb)

		for i := 0; i+1 < len(pts); i++ {
			p, q := pts[i], pts[i+1]
			l := math.Hypot(q.X-p.X, q.Y-p.Y)

			if l < 1e-6 {
				continue
			}

			nx, ny := -(q.Y-p.Y)/l*h, (q.X-p.X)/l*h

			out.subs = append(out.subs, calSubpath{
				closed: true,
				pts: []calPt{
					{X: p.X - nx, Y: p.Y - ny},
					{X: q.X - nx, Y: q.Y - ny},
					{X: q.X + nx, Y: q.Y + ny},
					{X: p.X + nx, Y: p.Y + ny},
				},
			})
		}

		for i, p := range pts {
			if sb.closed && i == len(pts)-1 {
				continue
			}

			joint := make([]calPt, n)

			for k, d := range ring {
				joint[k] = calPt{X: p.X + d.X, Y: p.Y + d.Y}
			}

			out.subs = append(out.subs, calSubpath{closed: true, pts: joint})
		}
	}

	return out
}

func (c *calCanvas) Stroke(s calShape, w float64, col color.RGBA) {
	c.Fill(calOutline(s, w), col)
}

func (c *calCanvas) Shadow(s calShape, dx, dy, blur float64, col color.RGBA) {
	m, r := c.Mask(calTransform(s, calMove(dx, dy)), blur*2)
	if m == nil {
		return
	}

	calBoxBlur(m, max(1, int(blur*c.s/2)))
	draw.DrawMask(c.img, r, image.NewUniform(col), image.Point{}, m, r.Min, draw.Over)
}

func calBoxBlur(m *image.Alpha, rad int) {
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

func (c *calCanvas) Clip(s calShape, opacity float64, fn func(l *calCanvas)) {
	m, r := c.Mask(s, 0)
	if m == nil {
		return
	}

	l := &calCanvas{img: image.NewRGBA(r), z: c.z, s: c.s, f: c.f, w: c.w, h: c.h}
	fn(l)

	if opacity < 1 {
		for i, v := range m.Pix {
			m.Pix[i] = uint8(float64(v) * opacity)
		}
	}

	draw.DrawMask(c.img, r, l.img, r.Min, m, r.Min, draw.Over)
}

func (c *calCanvas) Photo(src image.Image, clip calShape, b calBox) {
	m, r := c.Mask(clip, 0)
	if m == nil || src == nil {
		return
	}

	sb := src.Bounds()
	sw, sh := float64(max(1, sb.Dx())), float64(max(1, sb.Dy()))
	k := max(b.W*c.s/sw, b.H*c.s/sh)
	scx, scy := float64(sb.Min.X)+sw/2, float64(sb.Min.Y)+sh/2
	cx, cy := b.cx()*c.s, b.cy()*c.s
	aff := f64.Aff3{k, 0, cx - k*scx, 0, k, cy - k*scy}
	l := image.NewRGBA(r)

	xdraw.CatmullRom.Transform(l, aff, src, sb, xdraw.Src, nil)
	draw.DrawMask(c.img, r, l, r.Min, m, r.Min, draw.Over)
}

func calIsWhiteMattePixel(src image.Image, x, y int) bool {
	b := src.Bounds()

	px := src.At(b.Min.X+x, b.Min.Y+y)
	if px == nil {
		return true
	}

	r, g, bl, a := px.RGBA()

	if a < 32768 {
		return true
	}

	return r > 60000 && g > 60000 && bl > 60000
}

func calCircularSafeRadius(src image.Image) (bool, float64) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()

	if w < 16 || h < 16 {
		return false, 0
	}

	corners := 0

	for _, corner := range [4][2]int{{2, 2}, {w - 3, 2}, {2, h - 3}, {w - 3, h - 3}} {
		if calIsWhiteMattePixel(src, corner[0], corner[1]) {
			corners++
		}
	}

	if corners < 3 {
		return false, 0
	}

	cx, cy := float64(w)/2, float64(h)/2
	minR := math.Hypot(cx, cy)

	for _, dir := range [4][2]float64{{-1, -1}, {1, -1}, {-1, 1}, {1, 1}} {
		for step := 100; step >= 40; step-- {
			t := float64(step) / 100.0
			px := max(0, min(w-1, int(math.Round(cx+dir[0]*cx*t))))
			py := max(0, min(h-1, int(math.Round(cy+dir[1]*cy*t))))

			if !calIsWhiteMattePixel(src, px, py) {
				minR = min(minR, math.Hypot(float64(px)-cx, float64(py)-cy))

				break
			}
		}
	}

	return true, minR * 0.965
}

func (c *calCanvas) PhotoFocus(src image.Image, clip calShape, b calBox, biasY float64) {
	m, r := c.Mask(clip, 0)
	if m == nil || src == nil {
		return
	}

	sb := src.Bounds()
	sw, sh := float64(max(1, sb.Dx())), float64(max(1, sb.Dy()))
	k := max(b.W*c.s/sw, b.H*c.s/sh)

	hasMatte, safeR := calCircularSafeRadius(src)
	if hasMatte && safeR > 8 {
		k = max(k, math.Hypot(b.W*c.s/2, b.H*c.s/2)/safeR)
	}

	scx := float64(sb.Min.X) + sw/2
	scy := float64(sb.Min.Y) + sh*(0.5+biasY)

	if hasMatte && safeR > 8 {
		halfW := (b.W * c.s / k) / 2
		halfH := (b.H * c.s / k) / 2
		maxOffY := max(0, math.Sqrt(max(0, safeR*safeR-halfW*halfW))-halfH)
		centerY := float64(sb.Min.Y) + sh/2

		scy = max(centerY-maxOffY, min(centerY+maxOffY, scy))
	} else {
		halfVisH := (b.H * c.s / k) / 2

		scy = max(float64(sb.Min.Y)+halfVisH, min(float64(sb.Max.Y)-halfVisH, scy))
	}

	cx, cy := b.cx()*c.s, b.cy()*c.s
	aff := f64.Aff3{k, 0, cx - k*scx, 0, k, cy - k*scy}
	l := image.NewRGBA(r)

	xdraw.CatmullRom.Transform(l, aff, src, sb, xdraw.Src, nil)
	draw.DrawMask(c.img, r, l, r.Min, m, r.Min, draw.Over)
}

func (c *calCanvas) textShape(t calText, s string, x, y float64, al calAlign) calShape {
	f := c.f

	switch al {
	case calAlignC:
		x -= f.width(t, s) / 2
	case calAlignR:
		x -= f.width(t, s)
	case calAlignL:
	}

	var out calShape

	for _, r := range s {
		g := f.glyph(t.Face, r)

		for _, sg := range g.segs {
			a := sg.a
			px := func(i int) (float64, float64) { return x + a[i].X*t.Size, y + a[i].Y*t.Size }

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

		x += g.adv*t.Size + t.Track
	}

	if len(out.subs) > 0 {
		out.Close()
	}

	return out
}

func (c *calCanvas) textMid(t calText, s string, x, y float64, al calAlign, col color.RGBA) {
	if s == "" {
		return
	}

	c.Fill(c.textShape(t, s, x, y+c.f.mid(t), al), col)
}

func (c *calCanvas) EncodePNG(outW, outH int) ([]byte, error) {
	dst := image.NewRGBA(image.Rect(0, 0, outW, outH))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), c.img, c.img.Bounds(), xdraw.Src, nil)

	var buf bytes.Buffer

	if err := png.Encode(&buf, dst); err != nil {
		return nil, fmt.Errorf("encode calendar png: %w", err)
	}

	return buf.Bytes(), nil
}

func calRect(x, y, w, h float64) calShape {
	return calShape{subs: []calSubpath{{closed: true, pts: []calPt{{X: x, Y: y}, {X: x + w, Y: y}, {X: x + w, Y: y + h}, {X: x, Y: y + h}}}}}
}

func calPoly(pts ...calPt) calShape {
	return calShape{subs: []calSubpath{{closed: true, pts: pts}}}
}

func calRRect(x, y, w, h, r float64) calShape {
	r = min(r, w/2, h/2)
	if r <= 0 {
		return calRect(x, y, w, h)
	}

	var s calShape

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

func calCircle(cx, cy, r float64) calShape {
	n := calCurveSteps(2 * math.Pi * r)
	pts := make([]calPt, n)

	for i := range n {
		a := 2 * math.Pi * float64(i) / float64(n)

		pts[i] = calPt{X: cx + r*math.Cos(a), Y: cy + r*math.Sin(a)}
	}

	return calShape{subs: []calSubpath{{closed: true, pts: pts}}}
}

func calSparkle(cx, cy, r float64) calShape {
	k := r * 0.19

	var s calShape

	s.MoveTo(cx, cy-r)
	s.QuadTo(cx+k, cy-k, cx+r, cy)
	s.QuadTo(cx+k, cy+k, cx, cy+r)
	s.QuadTo(cx-k, cy+k, cx-r, cy)
	s.QuadTo(cx-k, cy-k, cx, cy-r)
	s.Close()

	return s
}

func calRoundedTri(cx, cy, r, cr, angle float64) calShape {
	var verts [3]calPt

	for i := range 3 {
		a := angle + float64(i)*2*math.Pi/3

		verts[i] = calPt{X: cx + r*math.Cos(a), Y: cy + r*math.Sin(a)}
	}

	var s calShape

	for i := range 3 {
		prev := verts[(i+2)%3]
		cur := verts[i]
		nxt := verts[(i+1)%3]
		dx1, dy1 := prev.X-cur.X, prev.Y-cur.Y
		l1 := math.Hypot(dx1, dy1)
		dx2, dy2 := nxt.X-cur.X, nxt.Y-cur.Y
		l2 := math.Hypot(dx2, dy2)
		d := min(cr, l1*0.35, l2*0.35)
		p1 := calPt{X: cur.X + dx1/l1*d, Y: cur.Y + dy1/l1*d}
		p2 := calPt{X: cur.X + dx2/l2*d, Y: cur.Y + dy2/l2*d}

		if i == 0 {
			s.MoveTo(p1.X, p1.Y)
		} else {
			s.LineTo(p1.X, p1.Y)
		}

		s.QuadTo(cur.X, cur.Y, p2.X, p2.Y)
	}

	s.Close()

	return s
}
