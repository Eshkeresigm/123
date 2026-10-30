package ui

import (
	"image"

	"chessvision/assets"
	"chessvision/internal/gfx"
)

// RenderIcon draws the application logo (as in the title bar) at size×size.
func RenderIcon(size int) *image.RGBA {
	sym, err := gfx.LoadFont(assets.Symbols)
	c := gfx.NewCanvas(size, size)
	if err != nil {
		return c.Img
	}
	s := float64(size) / 34
	p := P{C: c, S: s}
	p.Round(0, 0, 34, 34, 11, p.Lin(0, 0, 34, 34, gfx.Stop{Pos: 0, Color: colViolet}, gfx.Stop{Pos: 1, Color: colCyan}))
	path := sym.GlyphPath(0x265E, 25*s, 0, 0)
	minX, minY, maxX, maxY := path.Bounds()
	ox, oy := 17*s-(minX+maxX)/2, 17.5*s-(minY+maxY)/2
	for _, cc := range path.C {
		for i := range cc {
			cc[i].X += ox
			cc[i].Y += oy
		}
	}
	c.Fill(path, gfx.Solid(gfx.Hex(0xFFFFFF)))
	return c.Img
}
