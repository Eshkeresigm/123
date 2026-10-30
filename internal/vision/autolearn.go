package vision

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"image"

	"chessvision/internal/chess"
)

// AutoResult is the outcome of recognising an unknown piece set on a board
// whose position is not known in advance.
type AutoResult struct {
	Placement     [64]chess.Piece
	WhiteAtBottom bool
	Side          chess.Color // side to move when SideKnown
	SideKnown     bool
	Conf          float64 // 0..1
	Cost          float64 // mean silhouette distance of the chosen naming
	Set           string  // reference set that fitted best
	Pieces        int
	Margin        float64 // relative cost gap to the next different naming
	Clusters      int
}

var autoDebug func(string)

type piece struct {
	obs
	white bool
	idx   int
}

var scales = []float64{0.7, 0.78, 0.86, 0.93, 1, 1.07, 1.15, 1.25}

// AutoLearn names every piece on the board without knowing the position:
// shapes are matched against reference silhouettes, identical-looking pieces
// are grouped, and the naming must form a legal chess position. The colour of
// a piece comes from its brightness, the board orientation from where the
// bright pieces stand.
func AutoLearn(img *image.RGBA, rect image.Rectangle, refs []RefSet) (AutoResult, error) {
	var res AutoResult
	if rect.Dx() < 96 || rect.Dy() < 96 {
		return res, errors.New("область доски слишком мала")
	}
	if len(refs) == 0 {
		refs = DefaultRefs()
	}
	// segmentation
	var bgs [64][3]int
	var have [64]bool
	var obsAll []piece
	var par [2][3][32]int
	var np [2]int
	for row := 0; row < 8; row++ {
		for col := 0; col < 8; col++ {
			bg, _, ok := bgSample(img, squareRect(rect, col, row))
			if !ok {
				continue
			}
			i := row*8 + col
			bgs[i], have[i] = bg, true
			k := (row + col) & 1
			for c := 0; c < 3; c++ {
				par[k][c][np[k]] = bg[c]
			}
			np[k]++
		}
	}
	occ := map[int]int{} // grid index -> index into obsAll
	for row := 0; row < 8; row++ {
		for col := 0; col < 8; col++ {
			i := row*8 + col
			if !have[i] {
				continue
			}
			o, ok := segmentSquare(img, squareRect(rect, col, row), bgs[i])
			if !ok {
				continue
			}
			o.col, o.row = col, row
			occ[i] = len(obsAll)
			obsAll = append(obsAll, piece{obs: o, idx: len(obsAll)})
		}
	}
	n := len(obsAll)
	res.Pieces = n
	if n < 2 {
		return res, errors.New("на доске не видно фигур")
	}
	// colour: two brightness classes
	lumas := make([]float64, n)
	for i := range obsAll {
		lumas[i] = obsAll[i].luma
	}
	thr, sep := otsu(lumas)
	if sep < 0.18 {
		return res, errors.New("не удалось отличить белые фигуры от чёрных")
	}
	var wRow, bRow float64
	var nw, nb int
	for i := range obsAll {
		obsAll[i].white = obsAll[i].luma > thr
		if obsAll[i].white {
			wRow += float64(obsAll[i].row)
			nw++
		} else {
			bRow += float64(obsAll[i].row)
			nb++
		}
	}
	if nw == 0 || nb == 0 {
		return res, errors.New("видны фигуры только одного цвета")
	}
	wRow /= float64(nw)
	bRow /= float64(nb)
	whiteBottom := wRow > bRow
	orientSure := math.Abs(wRow-bRow) >= 0.8

	// shape grids at every scale
	grids := make([][]([sGrid * sGrid]float32), len(scales))
	for si, m := range scales {
		grids[si] = make([][sGrid * sGrid]float32, n)
		for i := range obsAll {
			grids[si][i] = obsAll[i].toGrid(m)
		}
	}
	// group identical-looking pieces (same shape, either colour)
	g1 := grids[4] // scale 1
	var clusters [][]int
	dcache := map[[2]int]float64{}
	pd := func(a, b int) float64 {
		k := [2]int{min(a, b), max(a, b)}
		if v, ok := dcache[k]; ok {
			return v
		}
		v := silDist(&g1[a], &g1[b])
		dcache[k] = v
		return v
	}
	base := make([][]int, n)
	for i := range base {
		base[i] = []int{i}
	}
	merge := func(tau float64) [][]int {
		cl := make([][]int, len(base))
		for i := range base {
			cl[i] = append([]int(nil), base[i]...)
		}
		for {
			bi, bj, bd := -1, -1, tau
			for i := 0; i < len(cl); i++ {
				for j := i + 1; j < len(cl); j++ {
					var s float64
					for _, a := range cl[i] {
						for _, b := range cl[j] {
							s += pd(a, b)
						}
					}
					if d := s / float64(len(cl[i])*len(cl[j])); d < bd {
						bi, bj, bd = i, j, d
					}
				}
			}
			if bi < 0 {
				return cl
			}
			cl[bi] = append(cl[bi], cl[bj]...)
			cl = append(cl[:bj], cl[bj+1:]...)
		}
	}
	for _, tau := range []float64{0.15, 0.18, 0.21, 0.25} {
		clusters = merge(tau)
		if len(clusters) <= 9 {
			break
		}
	}
	sort.Slice(clusters, func(i, j int) bool { return len(clusters[i]) > len(clusters[j]) })
	if autoDebug != nil {
		for k, cl := range clusters {
			var names []string
			for _, i := range cl {
				names = append(names, fmt.Sprintf("%s%c", chess.SqName(gridToSquare(obsAll[i].col, obsAll[i].row, whiteBottom)), map[bool]byte{true: 'w', false: 'b'}[obsAll[i].white]))
			}
			autoDebug(fmt.Sprintf("cluster %d: %v", k, names))
		}
	}

	symOf := make([]float64, len(clusters))
	for k, cl := range clusters {
		v := make([]float64, len(cl))
		for j, i := range cl {
			v[j] = obsAll[i].symmetry()
		}
		sort.Float64s(v)
		symOf[k] = v[len(v)/2]
	}
	type cand struct {
		cost  float64
		types []chess.PieceType // per cluster
		set   string
	}
	var cands []cand
	const keepN = 40
	capOf := [6]int{8, 2, 2, 2, 1, 1}
	for _, rs := range refs {
		for si := range scales {
			// cost of naming cluster k as type t
			cost := make([][6]float64, len(clusters))
			for k, cl := range clusters {
				for t := 0; t < 6; t++ {
					var s float64
					for _, i := range cl {
						s += silDist(&grids[si][i], &rs.Sil[t])
					}
					cost[k][t] = s/float64(len(cl)) + symPenalty(symOf[k], t)
				}
			}
			minRest := make([]float64, len(clusters)+1)
			for k := len(clusters) - 1; k >= 0; k-- {
				m := math.MaxFloat64
				for t := 0; t < 6; t++ {
					m = math.Min(m, cost[k][t])
				}
				minRest[k] = minRest[k+1] + m*float64(len(clusters[k]))
			}
			// depth-first search with pruning
			assign := make([]chess.PieceType, len(clusters))
			var cnt [2][6]int
			var cntTypeClusters [6]int
			worst := math.MaxFloat64
			var rec func(k int, acc float64)
			rec = func(k int, acc float64) {
				if len(cands) >= keepN && acc+minRest[k] >= worst {
					return
				}
				if k == len(clusters) {
					if cnt[0][chess.King] != 1 || cnt[1][chess.King] != 1 {
						return
					}
					c := cand{cost: acc, types: append([]chess.PieceType(nil), assign...), set: rs.Name}
					cands = append(cands, c)
					sort.Slice(cands, func(i, j int) bool { return cands[i].cost < cands[j].cost })
					if len(cands) > keepN {
						cands = cands[:keepN]
					}
					if len(cands) >= keepN {
						worst = cands[len(cands)-1].cost
					}
					return
				}
				order := [6]int{0, 1, 2, 3, 4, 5}
				sort.Slice(order[:], func(a, b int) bool { return cost[k][order[a]] < cost[k][order[b]] })
				for _, t := range order {
					var add [2]int
					for _, i := range clusters[k] {
						if obsAll[i].white {
							add[0]++
						} else {
							add[1]++
						}
					}
					if cnt[0][t]+add[0] > 0 && t == int(chess.King) && cnt[0][t]+add[0] > 1 {
						continue
					}
					if t == int(chess.King) && cnt[1][t]+add[1] > 1 {
						continue
					}
					pen := 0.0
					for c := 0; c < 2; c++ {
						if over := cnt[c][t] + add[c] - capOf[t]; over > 0 {
							pen += 0.5 * float64(min(over, add[c]))
						}
					}
					if cntTypeClusters[t] > 0 {
						pen += 0.06
					}
					assign[k] = chess.PieceType(t)
					cnt[0][t] += add[0]
					cnt[1][t] += add[1]
					cntTypeClusters[t]++
					rec(k+1, acc+cost[k][t]*float64(len(clusters[k]))+pen)
					cntTypeClusters[t]--
					cnt[0][t] -= add[0]
					cnt[1][t] -= add[1]
				}
			}
			rec(0, 0)
		}
	}
	if len(cands) == 0 {
		return res, errors.New("не удалось подобрать названия фигур")
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].cost < cands[j].cost })

	// pick the cheapest naming that forms a legal position; try both
	// orientations when the colour layout is ambiguous
	type outcome struct {
		board  [64]chess.Piece
		cost   float64
		set    string
		bottom bool
	}
	var good []outcome
	build := func(c cand, bottom bool) ([64]chess.Piece, bool) {
		var board [64]chess.Piece
		for i := range board {
			board[i] = chess.NoPiece
		}
		for k, cl := range clusters {
			for _, i := range cl {
				o := obsAll[i]
				col := chess.Black
				if o.white {
					col = chess.White
				}
				board[gridToSquare(o.col, o.row, bottom)] = chess.MakePiece(col, c.types[k])
			}
		}
		for _, side := range []chess.Color{chess.White, chess.Black} {
			if _, err := chess.FromBoard(board, side, 15, -1); err == nil {
				return board, true
			}
		}
		return board, false
	}
	orients := []bool{whiteBottom}
	if !orientSure {
		orients = append(orients, !whiteBottom)
	}
	for _, c := range cands {
		for oi, bottom := range orients {
			if b, ok := build(c, bottom); ok {
				cost := c.cost / float64(n)
				if oi > 0 {
					cost += 0.05
				}
				good = append(good, outcome{b, cost, c.set, bottom})
			}
		}
		if len(good) >= 12 {
			break
		}
	}
	if len(good) == 0 {
		return res, errors.New("распознанные фигуры не образуют допустимую позицию")
	}
	sort.Slice(good, func(i, j int) bool { return good[i].cost < good[j].cost })
	best := good[0]
	res.Placement, res.WhiteAtBottom, res.Cost, res.Set = best.board, best.bottom, best.cost, best.set
	// confidence: margin to the best different alternative, and absolute fit
	margin := 1.0
	for _, g := range good[1:] {
		if g.board != best.board {
			margin = (g.cost - best.cost) / (best.cost + 0.05)
			break
		}
	}
	res.Margin, res.Clusters = margin, len(clusters)
	fit := 1 - math.Min(1, best.cost/0.5)
	res.Conf = math.Max(0, math.Min(1, 0.55*math.Min(1, margin)+0.45*fit))

	// last-move highlight tells whose turn it is
	var cls [2][3]int
	for k := 0; k < 2; k++ {
		for c := 0; c < 3; c++ {
			cls[k][c] = medianInts(par[k][c][:np[k]])
		}
	}
	var hl []int
	for i := 0; i < 64; i++ {
		if !have[i] {
			continue
		}
		k := ((i / 8) + (i % 8)) & 1
		d := 0
		for c := 0; c < 3; c++ {
			if e := abs(bgs[i][c] - cls[k][c]); e > d {
				d = e
			}
		}
		if d >= 28 {
			hl = append(hl, i)
		}
	}
	if len(hl) == 2 {
		for _, i := range hl {
			sq := gridToSquare(i%8, i/8, res.WhiteAtBottom)
			if pc := res.Placement[sq]; pc != chess.NoPiece {
				res.Side, res.SideKnown = pc.Color().Opp(), true
			}
		}
	}
	return res, nil
}

// otsu returns the brightness threshold splitting v into two classes and the
// distance between the class means.
func otsu(v []float64) (thr, sep float64) {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	bestVar := -1.0
	for i := 1; i < len(s); i++ {
		if s[i] == s[i-1] {
			continue
		}
		var a, b float64
		for _, x := range s[:i] {
			a += x
		}
		for _, x := range s[i:] {
			b += x
		}
		ma, mb := a/float64(i), b/float64(len(s)-i)
		w0, w1 := float64(i), float64(len(s)-i)
		if vr := w0 * w1 * (mb - ma) * (mb - ma); vr > bestVar {
			bestVar = vr
			thr = (s[i-1] + s[i]) / 2
			sep = mb - ma
		}
	}
	return thr, sep
}

// DescribeAuto renders a short summary for logs/tests.
func (r AutoResult) String() string {
	return fmt.Sprintf("conf=%.2f margin=%.3f clusters=%d cost=%.3f set=%s pieces=%d bottomWhite=%v side=%v/%v", r.Conf, r.Margin, r.Clusters, r.Cost, r.Set, r.Pieces, r.WhiteAtBottom, r.Side, r.SideKnown)
}

// symPenalty penalises naming a symmetric silhouette a knight and an
// asymmetric one anything else (the knight is the only lopsided piece in
// every piece set).
func symPenalty(sym float64, t int) float64 {
	clamp := func(x float64) float64 { return math.Max(0, math.Min(1, x)) }
	if t == int(chess.Knight) {
		return 0.25 * clamp((sym-0.80)/0.15)
	}
	return 0.25 * clamp((0.80-sym)/0.15)
}
