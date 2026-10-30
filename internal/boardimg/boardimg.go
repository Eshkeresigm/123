// Package boardimg renders synthetic board screenshots. It is used by the
// end-to-end tests (and the preview tool) to emulate a chess website.
package boardimg

import (
	"image"

	"chessvision/assets"
	"chessvision/internal/chess"
	"chessvision/internal/gfx"
)

// Style describes a board theme and piece look.
type Style struct {
	Light, Dark gfx.Color
	WhiteFill   gfx.Color
	WhiteLine   gfx.Color
	BlackFill   gfx.Color
	BlackLine   gfx.Color
	GlyphScale  float64
	Coords      bool
	Highlight   bool
}

// Green is a lichess/chess.com-like theme.
var Green = Style{Light: gfx.Hex(0xEEEED2), Dark: gfx.Hex(0x769656), WhiteFill: gfx.Hex(0xFFFFFF), WhiteLine: gfx.Hex(0x222222), BlackFill: gfx.Hex(0x2B2B2B), BlackLine: gfx.Hex(0x000000), GlyphScale: 0.80, Coords: true, Highlight: true}

// Brown is a classic wooden-look theme.
var Brown = Style{Light: gfx.Hex(0xF0D9B5), Dark: gfx.Hex(0xB58863), WhiteFill: gfx.Hex(0xFAFAFA), WhiteLine: gfx.Hex(0x303030), BlackFill: gfx.Hex(0x1C1C1C), BlackLine: gfx.Hex(0x000000), GlyphScale: 0.86, Coords: true, Highlight: true}

var glyphs = map[chess.PieceType]rune{chess.King: 0x265A, chess.Queen: 0x265B, chess.Rook: 0x265C, chess.Bishop: 0x265D, chess.Knight: 0x265E, chess.Pawn: 0x265F}

var fonts struct{ sym, ui *gfx.Font }

func load() {
	if fonts.sym == nil {
		fonts.sym, _ = gfx.LoadFont(assets.Symbols)
		fonts.ui, _ = gfx.LoadFont(assets.UIRegular)
	}
}

// Options control one rendering.
type Options struct {
	Square        int
	Margin        int
	WhiteAtBottom bool
	LastMove      chess.Move
	Dots          []int
}

// Render draws board (indexed a1=0) and returns the image plus the board rectangle.
func Render(st Style, board [64]chess.Piece, o Options) (*image.RGBA, image.Rectangle) {
	load()
	sqf := float64(o.Square)
	size := o.Square*8 + 2*o.Margin
	c := gfx.NewCanvas(size, size)
	c.Clear(gfx.Hex(0x302E2B))
	cell := func(col, row int) int {
		if o.WhiteAtBottom {
			return (7-row)*8 + col
		}
		return row*8 + (7 - col)
	}
	for row := 0; row < 8; row++ {
		for col := 0; col < 8; col++ {
			sq := cell(col, row)
			x, y := float64(o.Margin)+float64(col)*sqf, float64(o.Margin)+float64(row)*sqf
			col0 := st.Dark
			if (chess.File(sq)+chess.Rank(sq))%2 == 1 {
				col0 = st.Light
			}
			c.FillRect(x, y, sqf, sqf, gfx.Solid(col0))
			if st.Highlight && o.LastMove != chess.NoMove && (sq == o.LastMove.From() || sq == o.LastMove.To()) {
				c.FillRect(x, y, sqf, sqf, gfx.Solid(gfx.RGBA(255, 235, 60, 105)))
			}
			if st.Coords {
				if col == 0 {
					c.DrawText(fonts.ui, sqf*0.2, x+sqf*0.06, y+sqf*0.24, string(rune('1'+chess.Rank(sq))), gfx.Mix(st.Light, st.Dark, 0.7))
				}
				if row == 7 {
					c.DrawText(fonts.ui, sqf*0.2, x+sqf*0.82, y+sqf*0.94, string(rune('a'+chess.File(sq))), gfx.Mix(st.Light, st.Dark, 0.7))
				}
			}
		}
	}
	for row := 0; row < 8; row++ {
		for col := 0; col < 8; col++ {
			sq := cell(col, row)
			cx, cy := float64(o.Margin)+(float64(col)+0.5)*sqf, float64(o.Margin)+(float64(row)+0.5)*sqf
			pc := board[sq]
			if pc == chess.NoPiece {
				for _, d := range o.Dots {
					if d == sq {
						c.FillCircle(cx, cy, sqf*0.16, gfx.Solid(gfx.RGBA(20, 85, 30, 60)))
					}
				}
				continue
			}
			path := fonts.sym.GlyphPath(glyphs[pc.Type()], sqf*st.GlyphScale, 0, 0)
			minX, _, maxX, maxY := path.Bounds()
			// real piece sets stand on a common baseline
			ox, oy := cx-(minX+maxX)/2, cy+0.40*sqf-maxY
			for _, ct := range path.C {
				for i := range ct {
					ct[i].X += ox
					ct[i].Y += oy
				}
			}
			mask := path.Rasterize(c.Bounds())
			fill, line := st.WhiteFill, st.WhiteLine
			if pc.Color() == chess.Black {
				fill, line = st.BlackFill, st.BlackLine
			}
			c.FillMaskAtZero(mask.Dilate(sqf*0.018), gfx.Solid(line))
			c.FillMaskAtZero(mask, gfx.Solid(fill))
		}
	}
	return c.Img, image.Rect(o.Margin, o.Margin, o.Margin+o.Square*8, o.Margin+o.Square*8)
}
