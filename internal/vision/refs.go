package vision

import (
	"bytes"
	_ "embed"
	"encoding/gob"
	"fmt"
	"image"
	"sync"

	"chessvision/assets"
	"chessvision/internal/chess"
	"chessvision/internal/gfx"
)

// RefSet holds one silhouette per piece type (indexed by chess.PieceType) in
// square-relative coordinates: the shape prior used to name pieces on a
// screen whose piece style has not been seen yet.
type RefSet struct {
	Name string
	Sil  [6][sGrid * sGrid]float32
}

//go:embed refsets.gob
var refsGob []byte

var (
	refsOnce sync.Once
	refsAll  []RefSet
)

// DefaultRefs returns the built-in silhouette sets: a generic glyph set drawn
// from the embedded symbol font plus any recorded real-world sets.
func DefaultRefs() []RefSet {
	refsOnce.Do(func() {
		refsAll = append(refsAll, glyphRefSet())
		if len(refsGob) > 0 {
			var extra []RefSet
			if err := gob.NewDecoder(bytes.NewReader(refsGob)).Decode(&extra); err == nil {
				refsAll = append(refsAll, extra...)
			}
		}
	})
	return refsAll
}

func glyphRefSet() RefSet {
	rs := RefSet{Name: "glyph"}
	sym, err := gfx.LoadFont(assets.Symbols)
	if err != nil {
		return rs
	}
	runes := [6]rune{0x265F, 0x265E, 0x265D, 0x265C, 0x265B, 0x265A}
	const N = 96
	kp := sym.GlyphPath(runes[chess.King], 100, 0, 0)
	_, ky0, _, ky1 := kp.Bounds()
	fs := 100 * 0.84 * N / (ky1 - ky0)
	for t := 0; t < 6; t++ {
		p := sym.GlyphPath(runes[t], fs, 0, 0)
		x0, y0, x1, y1 := p.Bounds()
		ox, oy := N/2-(x0+x1)/2, 0.92*N-y1
		for _, c := range p.C {
			for i := range c {
				c[i].X += ox
				c[i].Y += oy
			}
		}
		_ = y0
		cv := gfx.NewCanvas(N, N)
		cv.Fill(p, gfx.Solid(gfx.Hex(0xFFFFFF)))
		const k = N / sGrid
		for gy := 0; gy < sGrid; gy++ {
			for gx := 0; gx < sGrid; gx++ {
				var a float32
				for yy := 0; yy < k; yy++ {
					for xx := 0; xx < k; xx++ {
						a += float32(cv.Img.Pix[cv.Img.PixOffset(gx*k+xx, gy*k+yy)+3]) / 255
					}
				}
				rs.Sil[t][gy*sGrid+gx] = a / (k * k)
			}
		}
	}
	return rs
}

// averageGrid builds a RefSet entry from observed silhouettes.
func gridOf(o *obs) [sGrid * sGrid]float32 { return o.toGrid(1) }

// BuildRefSet records the silhouettes of a known position as a reference set.
func BuildRefSet(name string, img *image.RGBA, rect image.Rectangle, board [64]chess.Piece, whiteAtBottom bool) (RefSet, error) {
	rs := RefSet{Name: name}
	var cnt [6]int
	for row := 0; row < 8; row++ {
		for col := 0; col < 8; col++ {
			pc := board[gridToSquare(col, row, whiteAtBottom)]
			if pc == chess.NoPiece {
				continue
			}
			r := squareRect(rect, col, row)
			bg, _, ok := bgSample(img, r)
			if !ok {
				continue
			}
			o, ok := segmentSquare(img, r, bg)
			if !ok {
				return rs, fmt.Errorf("пустая клетка на месте фигуры (%d,%d)", col, row)
			}
			g := gridOf(&o)
			t := int(pc.Type())
			for i := range g {
				rs.Sil[t][i] += g[i]
			}
			cnt[t]++
		}
	}
	for t := 0; t < 6; t++ {
		if cnt[t] == 0 {
			return rs, fmt.Errorf("нет фигуры типа %d", t)
		}
		for i := range rs.Sil[t] {
			rs.Sil[t][i] /= float32(cnt[t])
		}
	}
	return rs, nil
}

// EncodeRefSets serialises sets for embedding.
func EncodeRefSets(sets []RefSet) ([]byte, error) {
	var b bytes.Buffer
	err := gob.NewEncoder(&b).Encode(sets)
	return b.Bytes(), err
}
