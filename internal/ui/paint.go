// Package ui draws ChessVision's interface with the software renderer and
// turns mouse input into controller commands. It never touches the OS, so
// the exact same picture can be rendered (and tested) anywhere.
package ui

import (
	"image"
	"math"

	"chessvision/internal/gfx"
)

// P paints in logical units: every coordinate is multiplied by S.
type P struct {
	C *gfx.Canvas
	S float64
}

func (p P) Round(x, y, w, h, r float64, paint gfx.Paint) {
	p.C.FillRound(x*p.S, y*p.S, w*p.S, h*p.S, r*p.S, paint)
}

func (p P) RoundC(x, y, w, h, r float64, col gfx.Color) { p.Round(x, y, w, h, r, gfx.Solid(col)) }

func (p P) Corners(x, y, w, h, tl, tr, br, bl float64, paint gfx.Paint) {
	s := p.S
	p.C.FillContour(gfx.RoundRect(x*s, y*s, w*s, h*s, tl*s, tr*s, br*s, bl*s), paint)
}

func (p P) Stroke(x, y, w, h, r, th float64, col gfx.Color) {
	p.C.StrokeRound(x*p.S, y*p.S, w*p.S, h*p.S, r*p.S, math.Max(th*p.S, 1), gfx.Solid(col))
}

func (p P) Rect(x, y, w, h float64, col gfx.Color) {
	p.C.FillRect(x*p.S, y*p.S, w*p.S, h*p.S, gfx.Solid(col))
}

func (p P) Circle(cx, cy, r float64, paint gfx.Paint) {
	p.C.FillCircle(cx*p.S, cy*p.S, r*p.S, paint)
}

func (p P) CircleC(cx, cy, r float64, col gfx.Color) { p.Circle(cx, cy, r, gfx.Solid(col)) }

func (p P) Ring(cx, cy, r, th float64, col gfx.Color) {
	p.C.StrokeCircle(cx*p.S, cy*p.S, r*p.S, math.Max(th*p.S, 1), gfx.Solid(col))
}

func (p P) Line(x0, y0, x1, y1, w float64, col gfx.Color) {
	p.C.Line(x0*p.S, y0*p.S, x1*p.S, y1*p.S, math.Max(w*p.S, 1), gfx.Solid(col))
}

// Poly fills a polygon given in logical coordinates.
func (p P) Poly(pts []gfx.Pt, paint gfx.Paint) {
	out := make([]gfx.Pt, len(pts))
	for i, q := range pts {
		out[i] = gfx.Pt{X: q.X * p.S, Y: q.Y * p.S}
	}
	p.C.FillContour(out, paint)
}

// Shadow paints a soft shadow below a rounded rectangle.
func (p P) Shadow(x, y, w, h, r, blur, dx, dy float64, col gfx.Color) {
	p.C.Shadow(x*p.S, y*p.S, w*p.S, h*p.S, r*p.S, blur*p.S, dx*p.S, dy*p.S, col)
}

// Lin is a linear gradient in logical coordinates.
func (p P) Lin(x0, y0, x1, y1 float64, stops ...gfx.Stop) gfx.Paint {
	return gfx.Linear(x0*p.S, y0*p.S, x1*p.S, y1*p.S, stops...)
}

// Rad is a radial gradient in logical coordinates.
func (p P) Rad(cx, cy, r float64, stops ...gfx.Stop) gfx.Paint {
	return gfx.Radial(cx*p.S, cy*p.S, r*p.S, stops...)
}

// Clip restricts drawing to a logical rectangle until Unclip.
func (p P) Clip(x, y, w, h float64) {
	p.C.PushClip(image.Rect(int(math.Floor(x*p.S)), int(math.Floor(y*p.S)), int(math.Ceil((x+w)*p.S)), int(math.Ceil((y+h)*p.S))))
}

func (p P) Unclip() { p.C.PopClip() }

// Text draws s with its vertical middle at cy.
func (p P) Text(f *gfx.Font, size, x, cy float64, s string, col gfx.Color) {
	p.C.DrawText(f, size*p.S, x*p.S, (cy+0.34*size)*p.S, s, col)
}

// TextR right-aligns at x.
func (p P) TextR(f *gfx.Font, size, x, cy float64, s string, col gfx.Color) {
	p.Text(f, size, x-p.W(f, size, s), cy, s, col)
}

// TextC centres on x.
func (p P) TextC(f *gfx.Font, size, x, cy float64, s string, col gfx.Color) {
	p.Text(f, size, x-p.W(f, size, s)/2, cy, s, col)
}

// W measures text in logical units.
func (p P) W(f *gfx.Font, size float64, s string) float64 { return f.TextWidth(s, size) }

// Fit shortens s with an ellipsis to fit maxW logical units.
func (p P) Fit(f *gfx.Font, size, maxW float64, s string) string { return f.Fit(s, size, maxW) }

func mixAlpha(c gfx.Color, a float64) gfx.Color { return c.Alpha(a) }
