package ui

import (
	"math"

	"chessvision/internal/gfx"
)

// All icons are drawn from primitives, centred on (cx,cy) in a box of size s.

func (p P) arc(cx, cy, r, a0, a1, w float64, col gfx.Color) {
	n := int(math.Max(6, math.Abs(a1-a0)*r/1.5))
	px, py := cx+r*math.Cos(a0), cy+r*math.Sin(a0)
	for i := 1; i <= n; i++ {
		a := a0 + (a1-a0)*float64(i)/float64(n)
		x, y := cx+r*math.Cos(a), cy+r*math.Sin(a)
		p.Line(px, py, x, y, w, col)
		px, py = x, y
	}
}

func (p P) iconPlay(cx, cy, s float64, col gfx.Color) {
	p.Poly([]gfx.Pt{{X: cx - s*0.28, Y: cy - s*0.42}, {X: cx + s*0.46, Y: cy}, {X: cx - s*0.28, Y: cy + s*0.42}}, gfx.Solid(col))
}

func (p P) iconPause(cx, cy, s float64, col gfx.Color) {
	p.RoundC(cx-s*0.34, cy-s*0.4, s*0.24, s*0.8, s*0.07, col)
	p.RoundC(cx+s*0.10, cy-s*0.4, s*0.24, s*0.8, s*0.07, col)
}

func (p P) iconRegion(cx, cy, s float64, col gfx.Color) {
	h := s * 0.46
	l := s * 0.3
	w := math.Max(1.8, s*0.11)
	for _, sx := range []float64{-1, 1} {
		for _, sy := range []float64{-1, 1} {
			x, y := cx+sx*h, cy+sy*h
			p.Line(x, y, x-sx*l, y, w, col)
			p.Line(x, y, x, y-sy*l, w, col)
		}
	}
	p.CircleC(cx, cy, s*0.07, col)
}

func (p P) iconTarget(cx, cy, s float64, col gfx.Color) {
	w := math.Max(1.8, s*0.1)
	p.Ring(cx, cy, s*0.34, w, col)
	p.CircleC(cx, cy, s*0.1, col)
	for _, d := range [][2]float64{{0, -1}, {0, 1}, {-1, 0}, {1, 0}} {
		p.Line(cx+d[0]*s*0.34, cy+d[1]*s*0.34, cx+d[0]*s*0.52, cy+d[1]*s*0.52, w, col)
	}
}

func (p P) iconPaste(cx, cy, s float64, col gfx.Color) {
	w := math.Max(1.6, s*0.09)
	p.Stroke(cx-s*0.34, cy-s*0.36, s*0.68, s*0.8, s*0.12, w, col)
	p.RoundC(cx-s*0.16, cy-s*0.48, s*0.32, s*0.2, s*0.07, col)
	p.Line(cx-s*0.14, cy+0.02*s, cx+s*0.14, cy+0.02*s, w, col)
	p.Line(cx-s*0.14, cy+s*0.2, cx+s*0.08, cy+s*0.2, w, col)
}

func (p P) iconCopy(cx, cy, s float64, col gfx.Color) {
	w := math.Max(1.6, s*0.09)
	p.Stroke(cx-s*0.1, cy-s*0.42, s*0.52, s*0.58, s*0.1, w, col)
	p.Stroke(cx-s*0.42, cy-s*0.12, s*0.52, s*0.58, s*0.1, w, col)
}

func (p P) iconReset(cx, cy, s float64, col gfx.Color) {
	w := math.Max(1.8, s*0.1)
	p.arc(cx, cy, s*0.34, -math.Pi*0.15, math.Pi*1.55, w, col)
	ax, ay := cx+s*0.34*math.Cos(-math.Pi*0.15), cy+s*0.34*math.Sin(-math.Pi*0.15)
	p.Poly([]gfx.Pt{{X: ax - s*0.2, Y: ay - s*0.02}, {X: ax + s*0.2, Y: ay - s*0.02}, {X: ax + s*0.02, Y: ay + s*0.24}}, gfx.Solid(col))
}

func (p P) iconDownload(cx, cy, s float64, col gfx.Color) {
	w := math.Max(1.8, s*0.1)
	p.Line(cx, cy-s*0.4, cx, cy+s*0.12, w, col)
	p.Line(cx-s*0.24, cy-s*0.12, cx, cy+s*0.14, w, col)
	p.Line(cx+s*0.24, cy-s*0.12, cx, cy+s*0.14, w, col)
	p.Line(cx-s*0.36, cy+s*0.38, cx+s*0.36, cy+s*0.38, w, col)
}

func (p P) iconClose(cx, cy, s float64, col gfx.Color) {
	w := math.Max(1.6, s*0.1)
	p.Line(cx-s*0.3, cy-s*0.3, cx+s*0.3, cy+s*0.3, w, col)
	p.Line(cx+s*0.3, cy-s*0.3, cx-s*0.3, cy+s*0.3, w, col)
}

func (p P) iconMin(cx, cy, s float64, col gfx.Color) {
	p.Line(cx-s*0.32, cy+s*0.1, cx+s*0.32, cy+s*0.1, math.Max(1.6, s*0.1), col)
}

func (p P) iconPin(cx, cy, s float64, on bool, col gfx.Color) {
	w := math.Max(1.6, s*0.1)
	// a push-pin: head, body, needle
	p.Line(cx-s*0.16, cy-s*0.34, cx+s*0.16, cy-s*0.34, w, col)
	p.Line(cx-s*0.1, cy-s*0.34, cx-s*0.1, cy-s*0.04, w, col)
	p.Line(cx+s*0.1, cy-s*0.34, cx+s*0.1, cy-s*0.04, w, col)
	p.Line(cx-s*0.26, cy-s*0.04, cx+s*0.26, cy-s*0.04, w, col)
	p.Line(cx, cy-s*0.04, cx, cy+s*0.4, w, col)
	if on {
		p.CircleC(cx, cy-s*0.2, s*0.07, col)
	}
}

func (p P) iconCheck(cx, cy, s float64, col gfx.Color) {
	w := math.Max(1.8, s*0.14)
	p.Line(cx-s*0.3, cy+s*0.02, cx-s*0.08, cy+s*0.26, w, col)
	p.Line(cx-s*0.08, cy+s*0.26, cx+s*0.34, cy-s*0.24, w, col)
}

func (p P) iconArrowRight(cx, cy, s float64, col gfx.Color) {
	w := math.Max(1.8, s*0.12)
	p.Line(cx-s*0.4, cy, cx+s*0.38, cy, w, col)
	p.Line(cx+s*0.12, cy-s*0.26, cx+s*0.4, cy, w, col)
	p.Line(cx+s*0.12, cy+s*0.26, cx+s*0.4, cy, w, col)
}

func (p P) iconSpinner(cx, cy, r, t float64, col gfx.Color) {
	a0 := t * 5
	p.Ring(cx, cy, r, 2, col.Alpha(0.18))
	p.arc(cx, cy, r-1, a0, a0+math.Pi*1.1, 2.2, col)
}
