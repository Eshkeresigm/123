package ui

import (
	"testing"

	"chessvision/internal/chess"
	"chessvision/internal/gfx"
)

func TestArrowsNoPanic(t *testing.T) {
	for _, s := range []float64{0.5, 0.9833, 1, 1.25, 1.5, 2} {
		c := gfx.NewCanvas(int(1120*s), int(760*s))
		u := &UI{S: s, anim: map[string]float64{}}
		p := P{C: c, S: s}
		for from := 0; from < 64; from += 3 {
			for to := 0; to < 64; to += 5 {
				if from == to {
					continue
				}
				mv := chess.NewMove(from, to)
				for _, g := range []float64{0, 0.01, 0.2, 0.5, 1} {
					c.PushClip(c.Img.Bounds())
					u.drawArrow(p, 92, 124, 464, mv, g, 0.7)
					c.PopClip()
				}
			}
		}
	}
}
