package vision

import (
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"chessvision/assets"
	"chessvision/internal/chess"
	"chessvision/internal/gfx"
)

type style struct {
	name        string
	light, dark gfx.Color
	font        *gfx.Font
	whiteFill   gfx.Color
	whiteLine   gfx.Color
	blackFill   gfx.Color
	blackLine   gfx.Color
	glyphScale  float64
	coords      bool
	hints       bool
	highlight   bool
}

var glyphs = map[chess.PieceType]rune{chess.King: 0x265A, chess.Queen: 0x265B, chess.Rook: 0x265C, chess.Bishop: 0x265D, chess.Knight: 0x265E, chess.Pawn: 0x265F}

func mustFont(t testing.TB, data []byte) *gfx.Font {
	f, err := gfx.LoadFont(data)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func styles(t testing.TB) []style {
	dj := mustFont(t, assets.Symbols)
	list := []style{
		{name: "green-dejavu", light: gfx.Hex(0xEEEED2), dark: gfx.Hex(0x769656), font: dj, whiteFill: gfx.Hex(0xFFFFFF), whiteLine: gfx.Hex(0x222222), blackFill: gfx.Hex(0x2B2B2B), blackLine: gfx.Hex(0x000000), glyphScale: 0.80, coords: true, hints: true, highlight: true},
		{name: "brown-dejavu", light: gfx.Hex(0xF0D9B5), dark: gfx.Hex(0xB58863), font: dj, whiteFill: gfx.Hex(0xFAFAFA), whiteLine: gfx.Hex(0x303030), blackFill: gfx.Hex(0x1C1C1C), blackLine: gfx.Hex(0x000000), glyphScale: 0.86, coords: true, highlight: true},
		{name: "blue-dejavu", light: gfx.Hex(0xDEE3E6), dark: gfx.Hex(0x8CA2AD), font: dj, whiteFill: gfx.Hex(0xFFFFFF), whiteLine: gfx.Hex(0x1E2A33), blackFill: gfx.Hex(0x33414B), blackLine: gfx.Hex(0x10181D), glyphScale: 0.78, hints: true},
	}
	if data, err := os.ReadFile("/usr/share/fonts/truetype/freefont/FreeSerif.ttf"); err == nil {
		if f, err := gfx.LoadFont(data); err == nil && f.Has(0x265A) {
			list = append(list, style{name: "wood-freeserif", light: gfx.Hex(0xE8D0A9), dark: gfx.Hex(0xA97A4F), font: f, whiteFill: gfx.Hex(0xFFFFFF), whiteLine: gfx.Hex(0x4A3A2A), blackFill: gfx.Hex(0x111111), blackLine: gfx.Hex(0x000000), glyphScale: 0.82, coords: true})
		}
	}
	return list
}

type renderOpts struct {
	sq            int
	margin        int
	whiteAtBottom bool
	noise         int
	lastMove      [2]int // squares, -1 for none
	dotsOn        []int
	uiAround      bool
	hasCheck      bool
	check         int // king square highlighted in red (like chess.com / lichess)
}

func renderBoard(st style, board [64]chess.Piece, o renderOpts, ui *gfx.Font) (*image.RGBA, image.Rectangle) {
	size := o.sq*8 + 2*o.margin
	c := gfx.NewCanvas(size, size)
	c.Clear(gfx.Hex(0x302E2B))
	if o.uiAround {
		c.FillRect(0, 0, float64(size), float64(o.margin), gfx.Solid(gfx.Hex(0x262421)))
	}
	sqf := float64(o.sq)
	for row := 0; row < 8; row++ {
		for col := 0; col < 8; col++ {
			sq := gridToSquare(col, row, o.whiteAtBottom)
			x, y := float64(o.margin)+float64(col)*sqf, float64(o.margin)+float64(row)*sqf
			light := (chess.File(sq)+chess.Rank(sq))%2 == 1
			col0 := st.dark
			if light {
				col0 = st.light
			}
			c.FillRect(x, y, sqf, sqf, gfx.Solid(col0))
			if st.highlight && (sq == o.lastMove[0] || sq == o.lastMove[1]) && o.lastMove[0] != o.lastMove[1] {
				c.FillRect(x, y, sqf, sqf, gfx.Solid(gfx.RGBA(255, 235, 60, 105)))
			}
			if o.hasCheck && sq == o.check {
				cx, cy := x+sqf/2, y+sqf/2
				c.FillRect(x, y, sqf, sqf, gfx.Radial(cx, cy, sqf*0.72,
					gfx.Stop{Pos: 0, Color: gfx.RGBA(255, 20, 20, 235)},
					gfx.Stop{Pos: 0.55, Color: gfx.RGBA(235, 40, 40, 150)},
					gfx.Stop{Pos: 1, Color: gfx.RGBA(220, 50, 50, 0)}))
			}
			if st.coords && ui != nil {
				if col == 0 {
					c.DrawText(ui, sqf*0.2, x+sqf*0.06, y+sqf*0.24, string(rune('1'+chess.Rank(sq))), gfx.Mix(st.light, st.dark, 0.7))
				}
				if row == 7 {
					c.DrawText(ui, sqf*0.2, x+sqf*0.82, y+sqf*0.94, string(rune('a'+chess.File(sq))), gfx.Mix(st.light, st.dark, 0.7))
				}
			}
		}
	}
	for row := 0; row < 8; row++ {
		for col := 0; col < 8; col++ {
			sq := gridToSquare(col, row, o.whiteAtBottom)
			cx, cy := float64(o.margin)+(float64(col)+0.5)*sqf, float64(o.margin)+(float64(row)+0.5)*sqf
			pc := board[sq]
			if pc == chess.NoPiece {
				if st.hints {
					for _, d := range o.dotsOn {
						if d == sq {
							c.FillCircle(cx, cy, sqf*0.16, gfx.Solid(gfx.RGBA(20, 85, 30, 60)))
						}
					}
				}
				continue
			}
			path := st.font.GlyphPath(glyphs[pc.Type()], sqf*st.glyphScale, 0, 0)
			minX, minY, maxX, maxY := path.Bounds()
			ox, oy := cx-(minX+maxX)/2, cy-(minY+maxY)/2
			for _, ct := range path.C {
				for i := range ct {
					ct[i].X += ox
					ct[i].Y += oy
				}
			}
			mask := path.Rasterize(c.Bounds())
			fill, line := st.whiteFill, st.whiteLine
			if pc.Color() == chess.Black {
				fill, line = st.blackFill, st.blackLine
			}
			c.FillMaskAtZero(mask.Dilate(sqf*0.018), gfx.Solid(line))
			c.FillMaskAtZero(mask, gfx.Solid(fill))
		}
	}
	img := c.Img
	if o.noise > 0 {
		seed := uint32(7)
		for i := 0; i < len(img.Pix); i += 4 {
			for k := 0; k < 3; k++ {
				seed = seed*1664525 + 1013904223
				d := int(seed>>24)%(2*o.noise+1) - o.noise
				v := int(img.Pix[i+k]) + d
				if v < 0 {
					v = 0
				}
				if v > 255 {
					v = 255
				}
				img.Pix[i+k] = uint8(v)
			}
		}
	}
	return img, image.Rect(o.margin, o.margin, o.margin+o.sq*8, o.margin+o.sq*8)
}

// randomPositions plays random legal moves from the start and returns snapshots.
func randomPositions(n int, seed uint64) []chess.Position {
	rng := seed
	next := func(k int) int {
		rng = rng*6364136223846793005 + 1442695040888963407
		return int((rng >> 33) % uint64(k))
	}
	var out []chess.Position
	for len(out) < n {
		p := chess.Start()
		plies := 6 + next(90)
		for i := 0; i < plies; i++ {
			mv := p.LegalMoves()
			if len(mv) == 0 {
				break
			}
			q, _ := p.Make(mv[next(len(mv))])
			p = q
			if i > 4 && next(12) == 0 {
				out = append(out, p)
			}
		}
		out = append(out, p)
	}
	return out[:n]
}

func learnFrom(t *testing.T, st style, o renderOpts, ui *gfx.Font) *Profile {
	img, rect := renderBoard(st, StartPlacement(), o, ui)
	prof := &Profile{}
	if err := prof.Learn(img, rect, StartPlacement(), o.whiteAtBottom); err != nil {
		t.Fatal(err)
	}
	if !prof.Ready() {
		t.Fatalf("%s: profile not ready, missing %v", st.name, prof.Missing())
	}
	return prof
}

func checkPositions(t *testing.T, st style, prof *Profile, o renderOpts, ui *gfx.Font, positions []chess.Position, tag string) {
	t.Helper()
	bad := 0
	for i, p := range positions {
		oo := o
		oo.lastMove = [2]int{-1, -1}
		if i%2 == 0 {
			oo.lastMove = [2]int{12 + i%20, 28 + i%17}
		}
		if i%3 == 0 {
			oo.dotsOn = []int{20, 21, 35, 36, 37}
		}
		img, rect := renderBoard(st, p.Board, oo, ui)
		got, err := prof.Recognize(img, rect, oo.whiteAtBottom)
		if err != nil {
			t.Fatal(err)
		}
		if got.Board != p.Board {
			bad++
			if bad <= 2 {
				t.Errorf("%s/%s: position %d misread\n want %s\n  got %s", st.name, tag, i, fenOf(p.Board), fenOf(got.Board))
				if dir := os.Getenv("VISION_DEBUG"); dir != "" {
					f, _ := os.Create(filepath.Join(dir, st.name+"-"+tag+".png"))
					png.Encode(f, img)
					f.Close()
				}
			}
		}
	}
	if bad > 0 {
		t.Errorf("%s/%s: %d of %d positions misread", st.name, tag, bad, len(positions))
	}
}

func fenOf(b [64]chess.Piece) string {
	p := chess.Empty()
	for sq, pc := range b {
		if pc != chess.NoPiece {
			p.Set(sq, pc)
		}
	}
	return p.FEN()
}

func TestLearnAndRecognise(t *testing.T) {
	ui := mustFont(t, assets.UIRegular)
	positions := randomPositions(40, 99)
	for _, st := range styles(t) {
		t.Run(st.name, func(t *testing.T) {
			base := renderOpts{sq: 64, margin: 10, whiteAtBottom: true, lastMove: [2]int{-1, -1}}
			prof := learnFrom(t, st, base, ui)
			checkPositions(t, st, prof, base, ui, positions, "same-size")
			// different scales with the same profile
			for _, sq := range []int{44, 52, 80, 100} {
				o := base
				o.sq = sq
				checkPositions(t, st, prof, o, ui, positions[:15], "scale")
			}
			// flipped board
			o := base
			o.whiteAtBottom = false
			checkPositions(t, st, prof, o, ui, positions[:15], "flipped")
			// pixel noise
			o = base
			o.noise = 6
			checkPositions(t, st, prof, o, ui, positions[:15], "noise")
		})
	}
}

func TestProfileSaveLoad(t *testing.T) {
	ui := mustFont(t, assets.UIRegular)
	st := styles(t)[0]
	base := renderOpts{sq: 60, margin: 6, whiteAtBottom: true, lastMove: [2]int{-1, -1}}
	prof := learnFrom(t, st, base, ui)
	path := filepath.Join(t.TempDir(), "p.bin")
	if err := prof.Save(path); err != nil {
		t.Fatal(err)
	}
	prof2, err := LoadProfile(path)
	if err != nil {
		t.Fatal(err)
	}
	checkPositions(t, st, prof2, base, ui, randomPositions(8, 5), "reloaded")
}

func TestFindBoardAndOrientation(t *testing.T) {
	ui := mustFont(t, assets.UIRegular)
	for _, st := range styles(t) {
		for _, white := range []bool{true, false} {
			o := renderOpts{sq: 70, margin: 28, whiteAtBottom: white, lastMove: [2]int{-1, -1}, uiAround: true}
			img, want := renderBoard(st, StartPlacement(), o, ui)
			got, ok := FindBoard(img)
			if !ok {
				t.Errorf("%s: board not found", st.name)
				continue
			}
			if d := math.Max(math.Max(math.Abs(float64(got.Min.X-want.Min.X)), math.Abs(float64(got.Min.Y-want.Min.Y))), math.Max(math.Abs(float64(got.Max.X-want.Max.X)), math.Abs(float64(got.Max.Y-want.Max.Y)))); d > 3 {
				t.Errorf("%s: found %v want %v", st.name, got, want)
			}
			if WhiteAtBottom(img, got) != white {
				t.Errorf("%s: orientation wrong (white=%v)", st.name, white)
			}
			// learning on the auto-found rectangle must also work
			prof := &Profile{}
			if err := prof.Learn(img, got, StartPlacement(), white); err != nil {
				t.Fatal(err)
			}
			rec, _ := prof.Recognize(img, got, white)
			if rec.Board != StartPlacement() {
				t.Errorf("%s: auto-fit start position misread", st.name)
			}
		}
	}
}

func TestEmptyBoardAndPartialProfile(t *testing.T) {
	ui := mustFont(t, assets.UIRegular)
	st := styles(t)[0]
	o := renderOpts{sq: 60, margin: 4, whiteAtBottom: true, lastMove: [2]int{-1, -1}}
	prof := learnFrom(t, st, o, ui)
	var empty [64]chess.Piece
	for i := range empty {
		empty[i] = chess.NoPiece
	}
	img, rect := renderBoard(st, empty, o, ui)
	rec, _ := prof.Recognize(img, rect, true)
	if rec.Occupied != 0 {
		t.Errorf("empty board read as %d pieces", rec.Occupied)
	}
	if (&Profile{}).Ready() {
		t.Error("empty profile cannot be ready")
	}
}

// A king in check is drawn on a red glow by most sites; it must not change
// what is recognised (this used to leave a stale suggestion on the screen).
func TestCheckGlowRecognition(t *testing.T) {
	ui := mustFont(t, assets.UIRegular)
	positions := randomPositions(40, 5)
	for _, st := range styles(t) {
		t.Run(st.name, func(t *testing.T) {
			base := renderOpts{sq: 64, margin: 10, whiteAtBottom: true, lastMove: [2]int{-1, -1}}
			prof := learnFrom(t, st, base, ui)
			bad := 0
			for i, p := range positions {
				for _, col := range []chess.Color{chess.White, chess.Black} {
					o := base
					o.hasCheck, o.check = true, p.KingSq(col)
					if i%2 == 1 {
						o.whiteAtBottom = false
					}
					img, rect := renderBoard(st, p.Board, o, ui)
					got, err := prof.Recognize(img, rect, o.whiteAtBottom)
					if err != nil {
						t.Fatal(err)
					}
					if got.Board != p.Board {
						bad++
						if bad <= 3 {
							t.Errorf("%s: glow on %v king misread\n want %s\n  got %s", st.name, col, fenOf(p.Board), fenOf(got.Board))
						}
					}
				}
			}
			if bad > 0 {
				t.Errorf("%s: %d of %d misread", st.name, bad, 2*len(positions))
			}
		})
	}
}
