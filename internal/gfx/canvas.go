package gfx

import (
	"image"
	"math"
)

// Color is a non-premultiplied RGBA colour.
type Color struct{ R, G, B, A uint8 }

// Hex builds an opaque colour from 0xRRGGBB.
func Hex(v uint32) Color { return Color{uint8(v >> 16), uint8(v >> 8), uint8(v), 255} }

// RGBA builds a colour from components.
func RGBA(r, g, b, a uint8) Color { return Color{r, g, b, a} }

// Alpha returns the colour with its alpha scaled by a (0..1).
func (c Color) Alpha(a float64) Color {
	return Color{c.R, c.G, c.B, uint8(clampF(float64(c.A)*a, 0, 255) + 0.5)}
}

// Mix interpolates between two colours.
func Mix(a, b Color, t float64) Color {
	t = clampF(t, 0, 1)
	l := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t + 0.5) }
	return Color{l(a.R, b.R), l(a.G, b.G), l(a.B, b.B), l(a.A, b.A)}
}

// Paint supplies a colour per pixel.
type Paint interface {
	At(x, y int) Color
}

type solid Color

func (s solid) At(x, y int) Color { return Color(s) }

// Solid is a uniform paint.
func Solid(c Color) Paint { return solid(c) }

// Stop is a gradient colour stop.
type Stop struct {
	Pos   float64
	Color Color
}

const lutN = 256

// lut is a lazily built colour ramp (a per-pixel gradient lookup is by far the
// hottest path of the renderer, so the stops are sampled once).
type lut struct {
	stops []Stop
	tab   [lutN + 1]Color
	ready bool
}

func (l *lut) at(t float64) Color {
	if !l.ready {
		for i := range l.tab {
			l.tab[i] = gradAt(l.stops, float64(i)/lutN)
		}
		l.ready = true
	}
	return l.tab[int(t*lutN+0.5)]
}

type linear struct {
	x0, y0, x1, y1 float64
	stops          []Stop
	ramp           lut
}

// Linear creates a linear gradient between two points.
func Linear(x0, y0, x1, y1 float64, stops ...Stop) Paint {
	l := &linear{x0: x0, y0: y0, x1: x1, y1: y1, stops: stops}
	l.ramp.stops = stops
	return l
}

func gradAt(stops []Stop, t float64) Color {
	if t <= stops[0].Pos {
		return stops[0].Color
	}
	for i := 1; i < len(stops); i++ {
		if t <= stops[i].Pos {
			a, b := stops[i-1], stops[i]
			return Mix(a.Color, b.Color, (t-a.Pos)/math.Max(b.Pos-a.Pos, 1e-9))
		}
	}
	return stops[len(stops)-1].Color
}

func (l *linear) At(x, y int) Color {
	dx, dy := l.x1-l.x0, l.y1-l.y0
	d := dx*dx + dy*dy
	t := 0.0
	if d > 0 {
		t = ((float64(x)+0.5-l.x0)*dx + (float64(y)+0.5-l.y0)*dy) / d
	}
	return l.ramp.at(clampF(t, 0, 1))
}

type radial struct {
	cx, cy, r float64
	stops     []Stop
	ramp      lut
}

// Radial creates a radial gradient.
func Radial(cx, cy, r float64, stops ...Stop) Paint {
	g := &radial{cx: cx, cy: cy, r: r, stops: stops}
	g.ramp.stops = stops
	return g
}

func (g *radial) At(x, y int) Color {
	d := math.Hypot(float64(x)+0.5-g.cx, float64(y)+0.5-g.cy) / g.r
	return g.ramp.at(clampF(d, 0, 1))
}

// Canvas is a premultiplied RGBA drawing surface.
type Canvas struct {
	Img   *image.RGBA
	clips []image.Rectangle

	shadows map[shadowKey]*Mask
}

type shadowKey struct {
	w, h, r float64
	bl      int
}

// NewCanvas allocates a canvas.
func NewCanvas(w, h int) *Canvas {
	return &Canvas{Img: image.NewRGBA(image.Rect(0, 0, w, h))}
}

// Bounds returns the canvas rectangle.
func (c *Canvas) Bounds() image.Rectangle { return c.Img.Bounds() }

// ResetClip drops every pushed clip rectangle (used after a failed frame).
func (c *Canvas) ResetClip() { c.clips = nil }

// Clip returns the active clip rectangle.
func (c *Canvas) Clip() image.Rectangle {
	if n := len(c.clips); n > 0 {
		return c.clips[n-1]
	}
	return c.Img.Bounds()
}

// PushClip restricts drawing to r (intersected with the current clip).
func (c *Canvas) PushClip(r image.Rectangle) {
	c.clips = append(c.clips, c.Clip().Intersect(r))
}

// PopClip restores the previous clip.
func (c *Canvas) PopClip() {
	if len(c.clips) > 0 {
		c.clips = c.clips[:len(c.clips)-1]
	}
}

// Clear fills the whole canvas.
func (c *Canvas) Clear(col Color) {
	pr, pg, pb, pa := premul(col)
	pix := c.Img.Pix
	for i := 0; i < len(pix); i += 4 {
		pix[i], pix[i+1], pix[i+2], pix[i+3] = pr, pg, pb, pa
	}
}

func premul(c Color) (r, g, b, a uint8) {
	if c.A == 255 {
		return c.R, c.G, c.B, 255
	}
	return uint8((int(c.R)*int(c.A) + 127) / 255), uint8((int(c.G)*int(c.A) + 127) / 255), uint8((int(c.B)*int(c.A) + 127) / 255), c.A
}

// blend composites colour col with coverage cov (0..255) at pixel offset i.
func blend(pix []uint8, i int, col Color, cov int) {
	a := (int(col.A)*cov + 127) / 255
	if a <= 0 {
		return
	}
	ia := 255 - a
	if a == 255 {
		pix[i], pix[i+1], pix[i+2], pix[i+3] = col.R, col.G, col.B, 255
		return
	}
	pix[i] = uint8((int(col.R)*a + int(pix[i])*ia + 127) / 255)
	pix[i+1] = uint8((int(col.G)*a + int(pix[i+1])*ia + 127) / 255)
	pix[i+2] = uint8((int(col.B)*a + int(pix[i+2])*ia + 127) / 255)
	pix[i+3] = uint8((255*a + int(pix[i+3])*ia + 127) / 255)
}

// FillMaskAt paints a mask whose rectangle is relative to (ox,oy).
func (c *Canvas) FillMaskAt(m *Mask, ox, oy int, p Paint) {
	if m == nil {
		return
	}
	r := m.R.Add(image.Pt(ox, oy))
	c.fillMask(&Mask{R: r, Pix: m.Pix}, p)
}

func (c *Canvas) fillMask(m *Mask, p Paint) {
	if m == nil {
		return
	}
	clip := c.Clip().Intersect(m.R)
	if clip.Empty() {
		return
	}
	sol, isSolid := p.(solid)
	w := m.R.Dx()
	stride := c.Img.Stride
	for y := clip.Min.Y; y < clip.Max.Y; y++ {
		row := m.Pix[(y-m.R.Min.Y)*w:]
		base := y*stride + 0
		for x := clip.Min.X; x < clip.Max.X; x++ {
			cov := int(row[x-m.R.Min.X])
			if cov == 0 {
				continue
			}
			col := Color(sol)
			if !isSolid {
				col = p.At(x, y)
			}
			blend(c.Img.Pix, base+x*4, col, cov)
		}
	}
}

// Fill paints a path.
func (c *Canvas) Fill(p *Path, paint Paint) {
	c.fillMask(p.Rasterize(c.Clip()), paint)
}

// FillContour paints a single contour.
func (c *Canvas) FillContour(pts []Pt, paint Paint) {
	c.Fill(&Path{C: [][]Pt{pts}}, paint)
}

// FillRect paints an axis-aligned rectangle (fast path for pixel-aligned ones).
func (c *Canvas) FillRect(x, y, w, h float64, paint Paint) {
	if x == math.Floor(x) && y == math.Floor(y) && w == math.Floor(w) && h == math.Floor(h) {
		r := image.Rect(int(x), int(y), int(x+w), int(y+h)).Intersect(c.Clip())
		if r.Empty() {
			return
		}
		sol, isSolid := paint.(solid)
		for yy := r.Min.Y; yy < r.Max.Y; yy++ {
			base := yy * c.Img.Stride
			for xx := r.Min.X; xx < r.Max.X; xx++ {
				col := Color(sol)
				if !isSolid {
					col = paint.At(xx, yy)
				}
				blend(c.Img.Pix, base+xx*4, col, 255)
			}
		}
		return
	}
	c.FillContour(Rect(x, y, w, h), paint)
}

// FillRound paints a rounded rectangle with uniform radius.
func (c *Canvas) FillRound(x, y, w, h, r float64, paint Paint) {
	c.FillContour(RoundRect(x, y, w, h, r, r, r, r), paint)
}

// StrokeRound draws a rounded-rectangle outline of the given thickness
// (the stroke lies inside the rectangle).
func (c *Canvas) StrokeRound(x, y, w, h, r, th float64, paint Paint) {
	var p Path
	p.Add(RoundRect(x, y, w, h, r, r, r, r))
	ir := math.Max(r-th, 0)
	p.Add(Reversed(RoundRect(x+th, y+th, w-2*th, h-2*th, ir, ir, ir, ir)))
	c.Fill(&p, paint)
}

// FillCircle paints a disc.
func (c *Canvas) FillCircle(cx, cy, r float64, paint Paint) {
	c.FillContour(Ellipse(cx, cy, r, r), paint)
}

// StrokeCircle draws a ring.
func (c *Canvas) StrokeCircle(cx, cy, r, th float64, paint Paint) {
	var p Path
	p.Add(Ellipse(cx, cy, r, r))
	p.Add(Reversed(Ellipse(cx, cy, r-th, r-th)))
	c.Fill(&p, paint)
}

// Line draws a thick line with round caps.
func (c *Canvas) Line(x0, y0, x1, y1, w float64, paint Paint) {
	c.FillContour(Capsule(x0, y0, x1, y1, w), paint)
}

// Shadow paints a soft shadow of a rounded rectangle. The blurred mask is
// cached per shape, so repeated frames cost one blit instead of a blur.
func (c *Canvas) Shadow(x, y, w, h, r, blur float64, dx, dy float64, col Color) {
	bl := int(blur/3 + 0.5)
	if bl < 1 {
		bl = 1
	}
	pad := bl * 3
	ox, oy := int(math.Floor(x+dx+0.5)), int(math.Floor(y+dy+0.5))
	est := image.Rect(ox-pad-1, oy-pad-1, ox+int(w)+pad+2, oy+int(h)+pad+2)
	if !est.Overlaps(c.Clip()) {
		return
	}
	key := shadowKey{math.Round(w*4) / 4, math.Round(h*4) / 4, math.Round(r*4) / 4, bl}
	m := c.shadows[key]
	if m == nil {
		base := (&Path{C: [][]Pt{RoundRect(0, 0, key.w, key.h, key.r, key.r, key.r, key.r)}}).Rasterize(image.Rect(-1<<20, -1<<20, 1<<20, 1<<20))
		if base == nil {
			return
		}
		m = base.Blur(bl)
		if c.shadows == nil || len(c.shadows) > 200 {
			c.shadows = map[shadowKey]*Mask{}
		}
		c.shadows[key] = m
	}
	c.fillMask(m.Shifted(ox, oy), Solid(col))
}

// DrawRGBA composites src (premultiplied) at pt using the given opacity.
func (c *Canvas) DrawRGBA(src *image.RGBA, pt image.Point, opacity float64) {
	r := src.Bounds().Add(pt).Intersect(c.Clip())
	if r.Empty() {
		return
	}
	op := int(clampF(opacity, 0, 1)*255 + 0.5)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		si := (y-pt.Y-src.Rect.Min.Y)*src.Stride + (r.Min.X-pt.X-src.Rect.Min.X)*4
		di := y*c.Img.Stride + r.Min.X*4
		for x := r.Min.X; x < r.Max.X; x++ {
			sa := int(src.Pix[si+3]) * op / 255
			if sa > 0 {
				ia := 255 - sa
				for k := 0; k < 3; k++ {
					c.Img.Pix[di+k] = uint8(int(src.Pix[si+k])*op/255 + int(c.Img.Pix[di+k])*ia/255)
				}
				c.Img.Pix[di+3] = uint8(sa + int(c.Img.Pix[di+3])*ia/255)
			}
			si += 4
			di += 4
		}
	}
}

// FillMaskAtZero paints a mask at its own absolute position.
func (c *Canvas) FillMaskAtZero(m *Mask, p Paint) { c.fillMask(m, p) }
