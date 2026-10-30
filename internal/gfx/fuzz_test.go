package gfx

import (
	"image"
	"math/rand"
	"testing"
)

// Random polygons against random clips must never panic (rasteriser rounding).
func TestRasterizeFuzz(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 3000; i++ {
		n := 3 + rng.Intn(6)
		var pts []Pt
		for k := 0; k < n; k++ {
			pts = append(pts, Pt{X: rng.Float64()*400 - 100, Y: rng.Float64()*400 - 100})
		}
		p := &Path{}
		p.Add(pts)
		clip := image.Rect(rng.Intn(100), rng.Intn(100), 100+rng.Intn(200), 100+rng.Intn(200))
		m := p.Rasterize(clip)
		if m != nil {
			m.Blur(1 + rng.Intn(4)).Minus(m.Dilate(1.5))
		}
	}
}
