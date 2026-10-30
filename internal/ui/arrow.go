package ui

import (
	"math"

	"chessvision/internal/chess"
	"chessvision/internal/gfx"
)

const (
	arrowInSec  = 0.42 // appearance of a new arrow
	arrowOutSec = 0.22 // disappearance of the previous one
)

// arrowAnim animates the best-move arrow: the new one grows out of the source
// square while the previous one fades away.
type arrowAnim struct {
	cur  chess.Move
	t    float64 // 0..1 progress of the current arrow
	old  chess.Move
	oldA float64 // remaining opacity of the previous arrow
}

func easeOut(t float64) float64 {
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	t = 1 - t
	return 1 - t*t*t
}

// stepArrow advances the animation for the given target move.
func (u *UI) stepArrow(best chess.Move) {
	a := &u.arrow
	if best != a.cur {
		// the arrow that is currently visible becomes the fading one
		if a.cur != chess.NoMove {
			vis := math.Min(1, easeOut(a.t)*2.2)
			if vis > a.oldA || a.old == chess.NoMove {
				a.old, a.oldA = a.cur, vis
			}
		}
		a.cur, a.t = best, 0
		if best == chess.NoMove {
			a.t = 1
		}
	}
	if a.cur != chess.NoMove && a.t < 1 {
		a.t = math.Min(1, a.t+u.dt/arrowInSec)
		u.animating = true
	}
	if a.old != chess.NoMove {
		a.oldA -= u.dt / arrowOutSec
		if a.oldA <= 0 {
			a.old, a.oldA = chess.NoMove, 0
		} else {
			u.animating = true
		}
	}
}

// arrowVis is the current arrow's opacity factor (0..1).
func (u *UI) arrowVis() float64 {
	if u.arrow.cur == chess.NoMove {
		return 0
	}
	return math.Min(1, easeOut(u.arrow.t)*2.2)
}

// drawArrow draws one best-move arrow; grow (0..1) is how far it has extended.
func (u *UI) drawArrow(p P, bx, by, size float64, mv chess.Move, grow, alpha float64) {
	if mv == chess.NoMove || alpha <= 0.005 {
		return
	}
	s := size / 8
	x0, y0 := u.squareRect(bx, by, size, mv.From())
	x1, y1 := u.squareRect(bx, by, size, mv.To())
	x0, y0, x1, y1 = x0+s/2, y0+s/2, x1+s/2, y1+s/2
	ex, ey := x0+(x1-x0)*grow, y0+(y1-y0)*grow
	k := 0.35 + 0.65*grow
	poly := gfx.Arrow(x0, y0, ex, ey, s*0.17*k, s*0.46*k)
	if poly == nil {
		return
	}
	sp := make([]gfx.Pt, len(poly))
	for i, q := range poly {
		sp[i] = gfx.Pt{X: q.X * p.S, Y: q.Y * p.S}
	}
	// pearl-white arrow: crisp ink edge + soft shadow so it reads on any square
	p.C.FillOutlined(sp, colWhite.Alpha(alpha), colInk.Alpha(0.70*alpha), gfx.RGBA(20, 28, 70, uint8(90*alpha)),
		1.5*p.S, 5*p.S, int(2.5*p.S))
	// origin disc
	r := s * 0.17 * k
	p.CircleC(x0, y0+1.2, r+1.8, gfx.RGBA(20, 28, 70, uint8(70*alpha)))
	p.CircleC(x0, y0, r+1.4, colInk.Alpha(0.70*alpha))
	p.CircleC(x0, y0, r, colWhite.Alpha(alpha))
}
