// Package gfx is a small anti-aliased 2D software renderer: paths, gradients,
// shadows and TrueType text. Everything the interface draws goes through it,
// so the picture is identical on every machine.
package gfx

import (
	"image"
	"math"
)

// Pt is a point in pixel space.
type Pt struct{ X, Y float64 }

// Path is a set of closed contours. Fill uses signed-area accumulation, so
// opposite winding cuts holes (rings) and equal winding overlaps merge.
type Path struct {
	C [][]Pt
}

// Mask is an 8-bit coverage map positioned in canvas space.
type Mask struct {
	R   image.Rectangle
	Pix []uint8 // len = R.Dx()*R.Dy()
}

// Bounds returns the path bounding box.
func (p *Path) Bounds() (minX, minY, maxX, maxY float64) {
	minX, minY = math.Inf(1), math.Inf(1)
	maxX, maxY = math.Inf(-1), math.Inf(-1)
	for _, c := range p.C {
		for _, q := range c {
			minX = math.Min(minX, q.X)
			maxX = math.Max(maxX, q.X)
			minY = math.Min(minY, q.Y)
			maxY = math.Max(maxY, q.Y)
		}
	}
	return
}

// Add appends a contour.
func (p *Path) Add(c []Pt) {
	if len(c) >= 3 {
		p.C = append(p.C, c)
	}
}

// Reversed returns a contour in opposite winding order.
func Reversed(c []Pt) []Pt {
	out := make([]Pt, len(c))
	for i, q := range c {
		out[len(c)-1-i] = q
	}
	return out
}

// Rasterize converts the path to a coverage mask limited to clip.
func (p *Path) Rasterize(clip image.Rectangle) *Mask {
	minX, minY, maxX, maxY := p.Bounds()
	if minX > maxX {
		return nil
	}
	r := image.Rect(int(math.Floor(minX)), int(math.Floor(minY)), int(math.Ceil(maxX)), int(math.Ceil(maxY))).Intersect(clip)
	if r.Empty() {
		return nil
	}
	w, h := r.Dx(), r.Dy()
	acc := make([]float32, (w+3)*h)
	stride := w + 3
	ox, oy := float64(r.Min.X), float64(r.Min.Y)
	for _, c := range p.C {
		n := len(c)
		for i := 0; i < n; i++ {
			a, b := c[i], c[(i+1)%n]
			accLine(acc, stride, w, h, a.X-ox, a.Y-oy, b.X-ox, b.Y-oy)
		}
	}
	m := &Mask{R: r, Pix: make([]uint8, w*h)}
	for y := 0; y < h; y++ {
		var sum float32
		row := acc[y*stride : y*stride+w]
		out := m.Pix[y*w : (y+1)*w]
		for x, v := range row {
			sum += v
			a := sum
			if a < 0 {
				a = -a
			}
			if a > 1 {
				a = 1
			}
			out[x] = uint8(a*255 + 0.5)
		}
	}
	return m
}

// accLine adds the signed-area contribution of one segment (font-rs style
// accumulation). Coordinates are relative to the mask; the segment is
// clipped to [0,w]x[0,h] with exact handling of the left/right edges.
func accLine(acc []float32, stride, w, h int, x0, y0, x1, y1 float64) {
	if y0 == y1 {
		return
	}
	fw := float64(w)
	// clip in x by splitting at the borders; outside parts become vertical edges
	if (x0 < 0 && x1 < 0) || (x0 > fw && x1 > fw) {
		cx := 0.0
		if x0 > fw {
			cx = fw
		}
		accSeg(acc, stride, h, cx, y0, cx, y1)
		return
	}
	if x0 >= 0 && x0 <= fw && x1 >= 0 && x1 <= fw {
		accSeg(acc, stride, h, x0, y0, x1, y1)
		return
	}
	// crossing a border: split at x=0 and x=w as needed
	type pt struct{ x, y float64 }
	pts := []pt{{x0, y0}}
	dx, dy := x1-x0, y1-y0
	var ts []float64
	if (x0 < 0) != (x1 < 0) {
		ts = append(ts, (0-x0)/dx)
	}
	if (x0 > fw) != (x1 > fw) {
		ts = append(ts, (fw-x0)/dx)
	}
	if len(ts) == 2 && ts[0] > ts[1] {
		ts[0], ts[1] = ts[1], ts[0]
	}
	for _, t := range ts {
		pts = append(pts, pt{x0 + dx*t, y0 + dy*t})
	}
	pts = append(pts, pt{x1, y1})
	for i := 0; i+1 < len(pts); i++ {
		a, b := pts[i], pts[i+1]
		mx := (a.x + b.x) / 2
		switch {
		case mx < 0:
			accSeg(acc, stride, h, 0, a.y, 0, b.y)
		case mx > fw:
			accSeg(acc, stride, h, fw, a.y, fw, b.y)
		default:
			accSeg(acc, stride, h, clampF(a.x, 0, fw), a.y, clampF(b.x, 0, fw), b.y)
		}
	}
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func accSeg(acc []float32, stride, h int, x0, y0, x1, y1 float64) {
	if y0 == y1 {
		return
	}
	fw := float64(stride - 3)
	dir := float32(1)
	if y0 > y1 {
		x0, y0, x1, y1 = x1, y1, x0, y0
		dir = -1
	}
	if y1 <= 0 || y0 >= float64(h) {
		return
	}
	dxdy := (x1 - x0) / (y1 - y0)
	x := x0
	if y0 < 0 {
		x -= y0 * dxdy
		y0 = 0
	}
	yEnd := y1
	if yEnd > float64(h) {
		yEnd = float64(h)
	}
	for y := int(y0); y < h && float64(y) < yEnd; y++ {
		dy := math.Min(float64(y+1), yEnd) - math.Max(float64(y), y0)
		xn := x + dxdy*dy
		d := float32(dy) * dir
		// rounding can push x a hair outside the mask: keep it inside
		x = clampF(x, 0, fw)
		xn = clampF(xn, 0, fw)
		xa, xb := x, xn
		if xa > xb {
			xa, xb = xb, xa
		}
		xaf := math.Floor(xa)
		xai := int(xaf)
		xbc := math.Ceil(xb)
		xbi := int(xbc)
		row := y * stride
		if xbi <= xai+1 {
			xmf := float32(0.5*(x+xn) - xaf)
			acc[row+xai] += d - d*xmf
			acc[row+xai+1] += d * xmf
		} else {
			s := float32(1 / (xb - xa))
			x0f := float32(xa - xaf)
			a0 := 0.5 * s * (1 - x0f) * (1 - x0f)
			x1f := float32(xb - xbc + 1)
			am := 0.5 * s * x1f * x1f
			acc[row+xai] += d * a0
			if xbi == xai+2 {
				acc[row+xai+1] += d * (1 - a0 - am)
			} else {
				a1 := s * (1.5 - x0f)
				acc[row+xai+1] += d * (a1 - a0)
				for xi := xai + 2; xi < xbi-1; xi++ {
					acc[row+xi] += d * s
				}
				a2 := a1 + float32(xbi-xai-3)*s
				acc[row+xbi-1] += d * (1 - a2 - am)
			}
			acc[row+xbi] += d * am
		}
		x = xn
	}
}

// ---- path builders -------------------------------------------------------

func arcSteps(r float64) int {
	n := int(math.Ceil(math.Sqrt(r) * 3.2))
	if n < 4 {
		n = 4
	}
	if n > 48 {
		n = 48
	}
	return n
}

// RoundRect returns a rounded rectangle contour (clockwise on screen).
func RoundRect(x, y, w, h, rTL, rTR, rBR, rBL float64) []Pt {
	lim := math.Min(w, h) / 2
	rTL, rTR, rBR, rBL = math.Min(rTL, lim), math.Min(rTR, lim), math.Min(rBR, lim), math.Min(rBL, lim)
	var c []Pt
	arc := func(cx, cy, r, a0 float64) {
		if r <= 0.01 {
			c = append(c, Pt{cx, cy})
			return
		}
		n := arcSteps(r)
		for i := 0; i <= n; i++ {
			a := a0 + (math.Pi/2)*float64(i)/float64(n)
			c = append(c, Pt{cx + r*math.Cos(a), cy + r*math.Sin(a)})
		}
	}
	arc(x+rTL, y+rTL, rTL, math.Pi)       // top-left
	arc(x+w-rTR, y+rTR, rTR, 1.5*math.Pi) // top-right
	arc(x+w-rBR, y+h-rBR, rBR, 0)         // bottom-right
	arc(x+rBL, y+h-rBL, rBL, 0.5*math.Pi) // bottom-left
	return c
}

// Rect is a plain rectangle contour.
func Rect(x, y, w, h float64) []Pt {
	return []Pt{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}}
}

// Ellipse returns an ellipse contour.
func Ellipse(cx, cy, rx, ry float64) []Pt {
	n := arcSteps(math.Max(rx, ry)) * 4
	c := make([]Pt, n)
	for i := range c {
		a := 2 * math.Pi * float64(i) / float64(n)
		c[i] = Pt{cx + rx*math.Cos(a), cy + ry*math.Sin(a)}
	}
	return c
}

// Capsule is a thick line with round caps.
func Capsule(x0, y0, x1, y1, w float64) []Pt {
	r := w / 2
	dx, dy := x1-x0, y1-y0
	l := math.Hypot(dx, dy)
	if l < 1e-6 {
		return Ellipse(x0, y0, r, r)
	}
	ang := math.Atan2(dy, dx)
	n := arcSteps(r) * 2
	var c []Pt
	for i := 0; i <= n; i++ {
		a := ang - math.Pi/2 + math.Pi*float64(i)/float64(n)
		c = append(c, Pt{x1 + r*math.Cos(a), y1 + r*math.Sin(a)})
	}
	for i := 0; i <= n; i++ {
		a := ang + math.Pi/2 + math.Pi*float64(i)/float64(n)
		c = append(c, Pt{x0 + r*math.Cos(a), y0 + r*math.Sin(a)})
	}
	return c
}

// Arrow returns a polygon of a thick arrow from (x0,y0) to (x1,y1).
func Arrow(x0, y0, x1, y1, shaft, head float64) []Pt {
	dx, dy := x1-x0, y1-y0
	l := math.Hypot(dx, dy)
	if l < 1e-6 {
		return nil
	}
	ux, uy := dx/l, dy/l
	nx, ny := -uy, ux
	hl := math.Min(head, l*0.7)
	hw := head * 0.62
	bx, by := x1-ux*hl, y1-uy*hl
	hs := shaft / 2
	return []Pt{
		{x0 + nx*hs, y0 + ny*hs},
		{bx + nx*hs, by + ny*hs},
		{bx + nx*hw, by + ny*hw},
		{x1, y1},
		{bx - nx*hw, by - ny*hw},
		{bx - nx*hs, by - ny*hs},
		{x0 - nx*hs, y0 - ny*hs},
	}
}

// Blur softens a mask with three box-blur passes (≈ Gaussian). The returned
// mask is larger than the input by 3*radius on every side.
func (m *Mask) Blur(radius int) *Mask {
	if m == nil || radius <= 0 {
		return m
	}
	pad := radius * 3
	w, h := m.R.Dx()+2*pad, m.R.Dy()+2*pad
	buf := make([]float32, w*h)
	for y := 0; y < m.R.Dy(); y++ {
		for x := 0; x < m.R.Dx(); x++ {
			buf[(y+pad)*w+x+pad] = float32(m.Pix[y*m.R.Dx()+x])
		}
	}
	tmp := make([]float32, w*h)
	for pass := 0; pass < 3; pass++ {
		boxBlurH(buf, tmp, w, h, radius)
		boxBlurV(tmp, buf, w, h, radius)
	}
	out := &Mask{R: image.Rect(m.R.Min.X-pad, m.R.Min.Y-pad, m.R.Max.X+pad, m.R.Max.Y+pad), Pix: make([]uint8, w*h)}
	for i, v := range buf {
		if v > 255 {
			v = 255
		}
		out.Pix[i] = uint8(v + 0.5)
	}
	return out
}

func boxBlurH(src, dst []float32, w, h, r int) {
	div := float32(2*r + 1)
	for y := 0; y < h; y++ {
		row := src[y*w : (y+1)*w]
		out := dst[y*w : (y+1)*w]
		var sum float32
		for x := -r; x <= r; x++ {
			if x >= 0 && x < w {
				sum += row[x]
			}
		}
		for x := 0; x < w; x++ {
			out[x] = sum / div
			if x+r+1 < w {
				sum += row[x+r+1]
			}
			if x-r >= 0 {
				sum -= row[x-r]
			}
		}
	}
}

func boxBlurV(src, dst []float32, w, h, r int) {
	div := float32(2*r + 1)
	for x := 0; x < w; x++ {
		var sum float32
		for y := -r; y <= r; y++ {
			if y >= 0 && y < h {
				sum += src[y*w+x]
			}
		}
		for y := 0; y < h; y++ {
			dst[y*w+x] = sum / div
			if y+r+1 < h {
				sum += src[(y+r+1)*w+x]
			}
			if y-r >= 0 {
				sum -= src[(y-r)*w+x]
			}
		}
	}
}

// Dilate grows a mask outward by about r pixels (used for piece outlines).
func (m *Mask) Dilate(r float64) *Mask {
	if m == nil || r <= 0 {
		return m
	}
	pad := int(math.Ceil(r)) + 1
	w, h := m.R.Dx()+2*pad, m.R.Dy()+2*pad
	out := &Mask{R: image.Rect(m.R.Min.X-pad, m.R.Min.Y-pad, m.R.Max.X+pad, m.R.Max.Y+pad), Pix: make([]uint8, w*h)}
	ir := int(math.Ceil(r))
	type off struct {
		dx, dy int
		wgt    float64
	}
	var offs []off
	for dy := -ir; dy <= ir; dy++ {
		for dx := -ir; dx <= ir; dx++ {
			d := math.Hypot(float64(dx), float64(dy))
			if d <= r+0.5 {
				wt := 1.0
				if d > r-0.5 {
					wt = r + 0.5 - d
				}
				offs = append(offs, off{dx, dy, wt})
			}
		}
	}
	mw, mh := m.R.Dx(), m.R.Dy()
	for y := 0; y < mh; y++ {
		for x := 0; x < mw; x++ {
			v := m.Pix[y*mw+x]
			if v == 0 {
				continue
			}
			for _, o := range offs {
				ox, oy := x+pad+o.dx, y+pad+o.dy
				nv := uint8(float64(v) * o.wgt)
				if idx := oy*w + ox; out.Pix[idx] < nv {
					out.Pix[idx] = nv
				}
			}
		}
	}
	return out
}

// Shifted returns a copy of the mask moved by (dx, dy).
func (m *Mask) Shifted(dx, dy int) *Mask {
	if m == nil {
		return nil
	}
	return &Mask{R: m.R.Add(image.Pt(dx, dy)), Pix: m.Pix}
}

// Minus returns m with the coverage of o removed (over m's rectangle).
func (m *Mask) Minus(o *Mask) *Mask {
	if m == nil || o == nil {
		return m
	}
	out := &Mask{R: m.R, Pix: make([]uint8, len(m.Pix))}
	w := m.R.Dx()
	ow := o.R.Dx()
	for y := m.R.Min.Y; y < m.R.Max.Y; y++ {
		for x := m.R.Min.X; x < m.R.Max.X; x++ {
			v := int(m.Pix[(y-m.R.Min.Y)*w+(x-m.R.Min.X)])
			if image.Pt(x, y).In(o.R) {
				v -= int(o.Pix[(y-o.R.Min.Y)*ow+(x-o.R.Min.X)])
			}
			if v > 0 {
				out.Pix[(y-m.R.Min.Y)*w+(x-m.R.Min.X)] = uint8(v)
			}
		}
	}
	return out
}

// FillOutlined paints a polygon as a solid shape with a crisp edge line and a
// soft drop shadow that never shows through the (translucent) fill.
func (c *Canvas) FillOutlined(poly []Pt, fill, edge, shadow Color, edgeW, blur float64, dy int) {
	path := &Path{}
	path.Add(poly)
	m := path.Rasterize(c.Clip())
	if m == nil {
		return
	}
	grown := m.Dilate(edgeW)
	if shadow.A > 0 && blur >= 1 {
		sh := grown.Blur(int(blur)).Shifted(0, dy).Minus(grown)
		c.FillMaskAtZero(sh, Solid(shadow))
	}
	if edge.A > 0 {
		c.FillMaskAtZero(grown.Minus(m), Solid(edge))
	}
	c.FillMaskAtZero(m, Solid(fill))
}
