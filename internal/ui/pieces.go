package ui

import (
	"image"

	"chessvision/internal/chess"
	"chessvision/internal/gfx"
)

type spriteKey struct {
	pc   chess.Piece
	size int
}

var pieceRune = map[chess.PieceType]rune{
	chess.King: 0x265A, chess.Queen: 0x265B, chess.Rook: 0x265C,
	chess.Bishop: 0x265D, chess.Knight: 0x265E, chess.Pawn: 0x265F,
}

// sprite returns a size×size premultiplied sprite of the piece with a soft shadow.
func (u *UI) sprite(pc chess.Piece, size int) *image.RGBA {
	k := spriteKey{pc, size}
	if s, ok := u.sprites[k]; ok {
		return s
	}
	c := gfx.NewCanvas(size, size)
	sz := float64(size)
	path := u.sym.GlyphPath(pieceRune[pc.Type()], sz*0.80, 0, 0)
	minX, minY, maxX, maxY := path.Bounds()
	// centre horizontally, sit on a common baseline
	ox := sz/2 - (minX+maxX)/2
	oy := sz*0.88 - maxY
	_ = minY
	for _, ct := range path.C {
		for i := range ct {
			ct[i].X += ox
			ct[i].Y += oy
		}
	}
	mask := path.Rasterize(c.Bounds())
	if mask != nil {
		blurR := size / 26
		if blurR < 1 {
			blurR = 1
		}
		c.FillMaskAt(mask.Blur(blurR), 0, int(sz*0.03)+1, gfx.Solid(gfx.RGBA(0, 4, 20, 120)))
		fill, line, lw := gfx.Hex(0xFFFFFF), gfx.Hex(0x1E2540), sz*0.022
		if pc.Color() == chess.Black {
			fill, line, lw = gfx.Hex(0x1A1E33), gfx.Hex(0xDDE2F3), sz*0.016
		}
		c.FillMaskAtZero(mask.Dilate(lw), gfx.Solid(line))
		c.FillMaskAtZero(mask, gfx.Solid(fill))
		if pc.Color() == chess.White {
			// subtle shading on the lower half for volume
			c.PushClip(image.Rect(0, int(sz*0.55), size, size))
			c.FillMaskAtZero(mask, gfx.Solid(gfx.RGBA(90, 105, 160, 46)))
			c.PopClip()
		} else {
			c.PushClip(image.Rect(0, 0, size, int(sz*0.42)))
			c.FillMaskAtZero(mask, gfx.Solid(gfx.RGBA(150, 165, 220, 38)))
			c.PopClip()
		}
	}
	u.sprites[k] = c.Img
	return c.Img
}
