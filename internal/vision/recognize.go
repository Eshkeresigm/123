package vision

import (
	"errors"
	"image"
	"math"

	"chessvision/internal/chess"
)

// Recognition is the result of reading one screenshot.
type Recognition struct {
	Board      [64]chess.Piece
	Dist       [64]float32 // distance to the best template (0 for empty squares)
	Occupied   int
	Suspicious int     // squares whose match looks unreliable
	MaxDist    float32 // largest template distance on the board
}

// Quality returns 0..1: 1 means every square matched its template closely.
func (r *Recognition) Quality() float64 {
	if r.Occupied == 0 {
		return 0
	}
	return 1 - float64(r.Suspicious)/float64(r.Occupied)
}

// Recognize reads the position from the board area of img.
func (p *Profile) Recognize(img *image.RGBA, boardRect image.Rectangle, whiteAtBottom bool) (Recognition, error) {
	var res Recognition
	if !p.Ready() {
		return res, errors.New("профиль фигур не обучен")
	}
	if boardRect.Dx() < 80 || boardRect.Dy() < 80 || !boardRect.In(img.Rect) {
		return res, errors.New("область доски вне снимка")
	}
	for i := range res.Board {
		res.Board[i] = chess.NoPiece
	}
	susp := p.RefDist * 6
	if susp < 0.5 {
		susp = 0.5
	}
	for row := 0; row < 8; row++ {
		for col := 0; col < 8; col++ {
			r := squareRect(boardRect, col, row)
			st := analyzeSquare(img, r, 0, 0)
			sq := gridToSquare(col, row, whiteAtBottom)
			if st.Coverage < p.TOcc {
				continue
			}
			best, bestD := chess.NoPiece, float32(math.MaxFloat32)
			for c := 0; c < 12; c++ {
				for i := range p.Samples[c] {
					if d := dist2(&st.Feat, &p.Samples[c][i]); d < bestD {
						bestD, best = d, chess.Piece(c)
					}
				}
			}
			emptyD := float32(math.MaxFloat32)
			for i := range p.Empty {
				if d := dist2(&st.Feat, &p.Empty[i]); d < emptyD {
					emptyD = d
				}
			}
			if emptyD <= bestD {
				continue
			}
			res.Board[sq] = best
			res.Dist[sq] = bestD
			res.Occupied++
			if bestD > res.MaxDist {
				res.MaxDist = bestD
			}
			if bestD > susp {
				res.Suspicious++
			}
		}
	}
	return res, nil
}

// FindBoard locates the 8x8 board inside a loosely selected screenshot. It
// looks for the two dominant square colours and returns the bounding box of
// the area covered by them.
func FindBoard(img *image.RGBA) (image.Rectangle, bool) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 100 || h < 100 {
		return b, false
	}
	// histogram of quantised colours
	hist := map[uint32]int{}
	step := 1
	if w*h > 1500*1500 {
		step = 2
	}
	key := func(r, g, bl uint8) uint32 { return uint32(r>>3)<<10 | uint32(g>>3)<<5 | uint32(bl>>3) }
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			i := img.PixOffset(x, y)
			hist[key(img.Pix[i], img.Pix[i+1], img.Pix[i+2])]++
		}
	}
	type kv struct {
		k uint32
		n int
	}
	var all []kv
	for k, n := range hist {
		all = append(all, kv{k, n})
	}
	// merge neighbouring bins by summing counts over a 3x3x3 neighbourhood
	score := func(k uint32) int {
		r0, g0, b0 := int(k>>10)&31, int(k>>5)&31, int(k)&31
		s := 0
		for dr := -1; dr <= 1; dr++ {
			for dg := -1; dg <= 1; dg++ {
				for db := -1; db <= 1; db++ {
					r, g, bl := r0+dr, g0+dg, b0+db
					if r < 0 || g < 0 || bl < 0 || r > 31 || g > 31 || bl > 31 {
						continue
					}
					s += hist[uint32(r)<<10|uint32(g)<<5|uint32(bl)]
				}
			}
		}
		return s
	}
	bestA, bestAS := uint32(0), -1
	for _, e := range all {
		if s := score(e.k); s > bestAS {
			bestA, bestAS = e.k, s
		}
	}
	toRGB := func(k uint32) (int, int, int) { return int(k>>10&31) * 8, int(k>>5&31) * 8, int(k&31) * 8 }
	ar, ag, ab := toRGB(bestA)
	bestB, bestBS := uint32(0), -1
	for _, e := range all {
		r, g, bl := toRGB(e.k)
		if abs(r-ar)+abs(g-ag)+abs(bl-ab) < 36 {
			continue
		}
		if s := score(e.k); s > bestBS {
			bestB, bestBS = e.k, s
		}
	}
	if bestBS < 0 {
		return b, false
	}
	br, bg, bb := toRGB(bestB)
	const tol = 20
	isBoard := func(i int) bool {
		r, g, bl := int(img.Pix[i]), int(img.Pix[i+1]), int(img.Pix[i+2])
		return (abs(r-ar-4) <= tol && abs(g-ag-4) <= tol && abs(bl-ab-4) <= tol) ||
			(abs(r-br-4) <= tol && abs(g-bg-4) <= tol && abs(bl-bb-4) <= tol)
	}
	cols := make([]int, w)
	rows := make([]int, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if isBoard(img.PixOffset(b.Min.X+x, b.Min.Y+y)) {
				cols[x]++
				rows[y]++
			}
		}
	}
	maxC, maxR := 0, 0
	for _, v := range cols {
		if v > maxC {
			maxC = v
		}
	}
	for _, v := range rows {
		if v > maxR {
			maxR = v
		}
	}
	first := func(v []int, thr int) (int, int) {
		lo, hi := -1, -1
		for i, x := range v {
			if x >= thr {
				if lo < 0 {
					lo = i
				}
				hi = i
			}
		}
		return lo, hi
	}
	x0, x1 := first(cols, int(float64(maxC)*0.4))
	y0, y1 := first(rows, int(float64(maxR)*0.4))
	if x0 < 0 || y0 < 0 {
		return b, false
	}
	r := image.Rect(b.Min.X+x0, b.Min.Y+y0, b.Min.X+x1+1, b.Min.Y+y1+1)
	ratio := float64(r.Dx()) / float64(r.Dy())
	if r.Dx() < 96 || r.Dy() < 96 || ratio < 0.93 || ratio > 1.07 {
		return b, false
	}
	return r, true
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// WhiteAtBottom decides the orientation of a board that shows the initial
// position: the side whose pieces are brighter is White.
func WhiteAtBottom(img *image.RGBA, boardRect image.Rectangle) bool {
	var top, bot float64
	var nt, nb int
	for row := 0; row < 8; row++ {
		if row >= 2 && row <= 5 {
			continue
		}
		for col := 0; col < 8; col++ {
			r := squareRect(boardRect, col, row)
			br, bg, bb := squareBackground(img, r)
			var sum float64
			var n int
			for y := r.Min.Y + r.Dy()/5; y < r.Max.Y-r.Dy()/5; y++ {
				for x := r.Min.X + r.Dx()/5; x < r.Max.X-r.Dx()/5; x++ {
					i := img.PixOffset(x, y)
					d := abs(int(img.Pix[i])-br) + abs(int(img.Pix[i+1])-bg) + abs(int(img.Pix[i+2])-bb)
					if d > 60 {
						sum += float64(lum(img.Pix[i], img.Pix[i+1], img.Pix[i+2]))
						n++
					}
				}
			}
			if n == 0 {
				continue
			}
			if row < 2 {
				top += sum / float64(n)
				nt++
			} else {
				bot += sum / float64(n)
				nb++
			}
		}
	}
	if nt == 0 || nb == 0 {
		return true
	}
	return bot/float64(nb) >= top/float64(nt)
}

// StartPlacement is the initial piece placement.
func StartPlacement() [64]chess.Piece {
	p := chess.Start()
	return p.Board
}

// IsStartPlacement reports whether b is exactly the initial placement.
func IsStartPlacement(b [64]chess.Piece) bool { return b == StartPlacement() }

// OccupancyMatch returns the fraction of squares whose occupied/empty state in
// the screenshot agrees with the expected placement. It needs no profile, so
// it can verify that the screen really shows the position the user claims.
func OccupancyMatch(img *image.RGBA, boardRect image.Rectangle, whiteAtBottom bool, placement [64]chess.Piece) float64 {
	agree := 0
	for row := 0; row < 8; row++ {
		for col := 0; col < 8; col++ {
			st := analyzeSquare(img, squareRect(boardRect, col, row), 0, 0)
			occupied := st.Coverage >= occCover && st.BBoxH >= occBBox
			want := placement[gridToSquare(col, row, whiteAtBottom)] != chess.NoPiece
			if occupied == want {
				agree++
			}
		}
	}
	return float64(agree) / 64
}
