package vision

import (
	"image"
	"math"
	"runtime"
	"sort"
	"sync"
)

// bgSample returns the median colour of a square's quiet side strips and the
// share of strip samples that agree with it. It does not allocate.
func bgSample(img *image.RGBA, r image.Rectangle) (bg [3]int, purity float64, ok bool) {
	w, h := r.Dx(), r.Dy()
	if w < 4 || h < 4 {
		return bg, 0, false
	}
	var smp [3][32]uint8
	n := 0
	put := func(x, y int) {
		if (image.Point{X: x, Y: y}).In(img.Rect) {
			i := img.PixOffset(x, y)
			smp[0][n], smp[1][n], smp[2][n] = img.Pix[i], img.Pix[i+1], img.Pix[i+2]
			n++
		}
	}
	for k := 0; k < 4; k++ {
		t := 0.38 + 0.08*float64(k)
		for _, f := range [2]float64{0.06, 0.10} {
			put(r.Min.X+int(f*float64(w)), r.Min.Y+int(t*float64(h)))
			put(r.Min.X+int((1-f)*float64(w)), r.Min.Y+int(t*float64(h)))
			put(r.Min.X+int(t*float64(w)), r.Min.Y+int(f*float64(h)))
			put(r.Min.X+int(t*float64(w)), r.Min.Y+int((1-f)*float64(h)))
		}
	}
	if n < 8 {
		return bg, 0, false
	}
	minShare := 1.0
	for c := 0; c < 3; c++ {
		v := smp[c][:n]
		for i := 1; i < len(v); i++ {
			x := v[i]
			j := i - 1
			for j >= 0 && v[j] > x {
				v[j+1] = v[j]
				j--
			}
			v[j+1] = x
		}
		med := int(v[n/2])
		bg[c] = med
		in := 0
		for _, x := range v {
			if abs(int(x)-med) <= 8 {
				in++
			}
		}
		if sh := float64(in) / float64(n); sh < minShare {
			minShare = sh
		}
	}
	return bg, minShare, true
}

func medianInts(v []int) int {
	for i := 1; i < len(v); i++ {
		x := v[i]
		j := i - 1
		for j >= 0 && v[j] > x {
			v[j+1] = v[j]
			j--
		}
		v[j+1] = x
	}
	return v[len(v)/2]
}

// gridScore rates how much rc looks like an 8x8 checkerboard: every square
// has a uniform background, and the backgrounds alternate between two colours
// with a clear contrast. Pieces do not matter. The result is 0..~1.
func gridScore(img *image.RGBA, rc image.Rectangle, full bool) float64 {
	var bgs [64][3]int
	var pur [64]float64
	var have [64]bool
	var par [2][3][32]int
	var np [2]int
	for row := 0; row < 8; row++ {
		for col := 0; col < 8; col++ {
			if !full && (row+col)%2 == 1 && (row%2 == 1) {
				continue // coarse pass: 3/8 of squares, still both parities
			}
			i := row*8 + col
			bg, p, ok := bgSample(img, squareRect(rc, col, row))
			if !ok {
				continue
			}
			bgs[i], pur[i], have[i] = bg, p, true
			k := (row + col) & 1
			for c := 0; c < 3; c++ {
				par[k][c][np[k]] = bg[c]
			}
			np[k]++
		}
	}
	if np[0] < 6 || np[1] < 6 {
		return 0
	}
	var cls [2][3]int
	for k := 0; k < 2; k++ {
		for c := 0; c < 3; c++ {
			cls[k][c] = medianInts(par[k][c][:np[k]])
		}
	}
	contrast := 0
	for c := 0; c < 3; c++ {
		if d := abs(cls[0][c] - cls[1][c]); d > contrast {
			contrast = d
		}
	}
	if contrast < 14 {
		return 0
	}
	var score float64
	cnt := 0
	for i := 0; i < 64; i++ {
		if !have[i] {
			continue
		}
		cnt++
		k := ((i / 8) + (i % 8)) & 1
		d := 0
		for c := 0; c < 3; c++ {
			if e := abs(bgs[i][c] - cls[k][c]); e > d {
				d = e
			}
		}
		// distance to the *other* class must be clearly larger, so a flat
		// region (both classes equal) cannot score
		o := 0
		for c := 0; c < 3; c++ {
			if e := abs(bgs[i][c] - cls[1-k][c]); e > o {
				o = e
			}
		}
		if d <= 14 && o >= contrast/2 {
			score += 0.5 + 0.5*pur[i]
		} else if d <= 28 {
			score += 0.15 * pur[i]
		}
	}
	return score / float64(cnt) * math.Min(1, float64(contrast)/40)
}

type gcand struct {
	r image.Rectangle
	s float64
}

// DetectBoard finds an 8x8 checkerboard in img regardless of the position on
// it. It returns the board rectangle and a confidence (0..1).
func DetectBoard(img *image.RGBA) (image.Rectangle, float64) {
	if f := MinBoardFraction; f > defaultMinBoardFraction {
		// a caller that knows boards are big (a phone screen) gets a much
		// faster search first; fall back to the full range when it finds nothing
		if r, c := detectBoard(img, f); c >= 0.62 {
			return r, c
		}
	}
	return detectBoard(img, defaultMinBoardFraction)
}

// defaultMinBoardFraction is the smallest board side, as a fraction of the
// shorter image side, that DetectBoard looks for.
const defaultMinBoardFraction = 0.18

// MinBoardFraction can be raised by programs whose screens always show a large
// board (the phone app) to make the first search pass much cheaper.
var MinBoardFraction = defaultMinBoardFraction

func detectBoard(img *image.RGBA, minFrac float64) (image.Rectangle, float64) {
	b := img.Bounds()
	maxS := min(b.Dx(), b.Dy())
	if maxS < 96 {
		return b, 0
	}
	minS := math.Max(96, minFrac*float64(maxS))
	// downscale for the coarse search on big images (a full screen)
	k := 1
	for float64(minS)/float64(k+1) >= 56 {
		k++
	}
	small := img
	if k > 1 {
		small = boxDown(img, k)
	}
	sb := small.Bounds()
	var sizes []int
	for s := minS / float64(k); s < float64(min(sb.Dx(), sb.Dy())); s *= 1.04 {
		sizes = append(sizes, int(s))
	}
	sizes = append(sizes, min(sb.Dx(), sb.Dy()))
	var mu sync.Mutex
	var all []gcand
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	last := -1
	for _, s := range sizes {
		if s == last || s < 48 {
			continue
		}
		last = s
		wg.Add(1)
		sem <- struct{}{}
		go func(s int) {
			defer wg.Done()
			defer func() { <-sem }()
			st := max(2, s/16)
			var local []gcand
			for y := sb.Min.Y; y+s <= sb.Max.Y; y += st {
				for x := sb.Min.X; x+s <= sb.Max.X; x += st {
					r := image.Rect(x, y, x+s, y+s)
					local = append(local, gcand{r, gridScore(small, r, false)})
				}
			}
			sort.Slice(local, func(i, j int) bool { return local[i].s > local[j].s })
			if len(local) > 4 {
				local = local[:4]
			}
			mu.Lock()
			all = append(all, local...)
			mu.Unlock()
		}(s)
	}
	wg.Wait()
	sort.Slice(all, func(i, j int) bool { return all[i].s > all[j].s })
	var seeds []image.Rectangle
	for _, c := range all {
		if c.s <= 0 {
			break
		}
		dup := false
		for _, o := range seeds {
			if abs(c.r.Min.X-o.Min.X) < o.Dx()/24 && abs(c.r.Min.Y-o.Min.Y) < o.Dy()/24 && abs(c.r.Dx()-o.Dx()) < o.Dx()/24 {
				dup = true
				break
			}
		}
		if !dup {
			r := c.r
			seeds = append(seeds, image.Rect(r.Min.X*k, r.Min.Y*k, r.Max.X*k, r.Max.Y*k).Intersect(b))
			if len(seeds) == 24 {
				break
			}
		}
	}
	valid := func(r image.Rectangle) bool {
		if !r.In(b) || r.Dx() < 96 || r.Dy() < 96 {
			return false
		}
		ar := float64(r.Dx()) / float64(r.Dy())
		return ar > 0.92 && ar < 1.08
	}
	bestRect, bestScore := b, 0.0
	for _, seed := range seeds {
		cur := seed
		if !valid(cur) {
			continue
		}
		cs := gridScore(img, cur, true)
		step := max(2, cur.Dx()/32)
		for step >= 1 {
			improved := false
			try := func(c image.Rectangle) {
				if !valid(c) {
					return
				}
				if s := gridScore(img, c, true); s > cs+1e-9 {
					cur, cs, improved = c, s, true
				}
			}
			for edge := 0; edge < 4; edge++ {
				for _, d := range []int{-step, step} {
					c := cur
					switch edge {
					case 0:
						c.Min.X += d
					case 1:
						c.Min.Y += d
					case 2:
						c.Max.X += d
					default:
						c.Max.Y += d
					}
					try(c)
				}
			}
			for _, d := range [][4]int{{-step, 0, -step, 0}, {step, 0, step, 0}, {0, -step, 0, -step}, {0, step, 0, step}, {-step, -step, step, step}, {step, step, -step, -step}} {
				try(image.Rect(cur.Min.X+d[0], cur.Min.Y+d[1], cur.Max.X+d[2], cur.Max.Y+d[3]))
			}
			if !improved {
				step /= 2
			}
		}
		// prefer larger boards when scores are close (a sub-grid of a board
		// can score nearly as well)
		adj := cs * (1 + 0.04*float64(cur.Dx())/float64(maxS))
		if adj > bestScore {
			bestScore, bestRect = adj, cur
		}
	}
	if bestScore <= 0 {
		return b, 0
	}
	// a grid shifted (or one row/column too large or small) by whole squares
	// is a local optimum of the hill climb: test those neighbours explicitly
	cur, cs := bestRect, gridScore(img, bestRect, true)
	for iter := 0; iter < 4; iter++ {
		sq := cur.Dx() / 8
		improved := false
		for _, d := range [][4]int{{sq, 0, sq, 0}, {-sq, 0, -sq, 0}, {0, sq, 0, sq}, {0, -sq, 0, -sq},
			{sq, 0, 0, 0}, {-sq, 0, 0, 0}, {0, 0, sq, 0}, {0, 0, -sq, 0},
			{0, sq, 0, 0}, {0, -sq, 0, 0}, {0, 0, 0, sq}, {0, 0, 0, -sq},
			{sq, sq, sq, sq}, {-sq, -sq, -sq, -sq}, {sq, -sq, sq, -sq}, {-sq, sq, -sq, sq}} {
			c := image.Rect(cur.Min.X+d[0], cur.Min.Y+d[1], cur.Max.X+d[2], cur.Max.Y+d[3])
			if !valid(c) {
				continue
			}
			if s := gridScore(img, c, true); s > cs+0.01 {
				cur, cs, improved = c, s, true
			}
		}
		if !improved {
			break
		}
	}
	out := refineEdges(img, cur)
	if al, ok := AlignRect(img, out); ok {
		out = refineEdges(img, al)
	}
	return out, math.Min(1, cs)
}

// boxDown shrinks an image by an integer factor with a box filter.
func boxDown(img *image.RGBA, k int) *image.RGBA {
	b := img.Bounds()
	w, h := b.Dx()/k, b.Dy()/k
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var r, g, bl, n int
			for dy := 0; dy < k; dy++ {
				for dx := 0; dx < k; dx++ {
					i := img.PixOffset(b.Min.X+x*k+dx, b.Min.Y+y*k+dy)
					r += int(img.Pix[i])
					g += int(img.Pix[i+1])
					bl += int(img.Pix[i+2])
					n++
				}
			}
			o := out.PixOffset(x, y)
			out.Pix[o], out.Pix[o+1], out.Pix[o+2], out.Pix[o+3] = uint8(r/n), uint8(g/n), uint8(bl/n), 255
		}
	}
	return out
}

// refineEdges snaps the grid to the real square boundaries: every grid line
// between two squares is a sharp colour step, so the position and size that
// maximise the step energy along the 9 vertical and 9 horizontal lines give a
// pixel-accurate board rectangle.
func refineEdges(img *image.RGBA, rc image.Rectangle) image.Rectangle {
	b := img.Bounds()
	sq := rc.Dx() / 8
	d := max(3, int(0.09*float64(sq)))
	step := func(xa, ya, xb, yb int) int {
		if !(image.Point{X: xa, Y: ya}).In(b) || !(image.Point{X: xb, Y: yb}).In(b) {
			return 0
		}
		i, j := img.PixOffset(xa, ya), img.PixOffset(xb, yb)
		m := 0
		for c := 0; c < 3; c++ {
			if e := abs(int(img.Pix[i+c]) - int(img.Pix[j+c])); e > m {
				m = e
			}
		}
		return m
	}
	// energy of every candidate boundary position along each axis
	energy := func(lo, hi, a0, a1 int, vertical bool) map[int]float64 {
		e := map[int]float64{}
		for p := lo; p <= hi; p++ {
			var s float64
			for q := a0; q < a1; q++ {
				if vertical {
					s += float64(step(p, q, p-1, q))
				} else {
					s += float64(step(q, p, q, p-1))
				}
			}
			e[p] = s
		}
		return e
	}
	solve := func(min0, max0 int, ev map[int]float64) (int, int) {
		best, bx, bw := -1.0, min0, max0-min0
		w0 := max0 - min0
		for x0 := min0 - d; x0 <= min0+d; x0++ {
			for w := w0 - d; w <= w0+d; w++ {
				if w < 96 {
					continue
				}
				var s float64
				for k := 0; k <= 8; k++ {
					p := x0 + int(math.Round(float64(k)*float64(w)/8))
					wt := 1.0
					if k == 0 || k == 8 {
						wt = 0.5
					}
					s += wt * ev[p]
				}
				// prefer the original on ties
				s -= 1e-6 * float64(abs(x0-min0)+abs(w-w0))
				if s > best {
					best, bx, bw = s, x0, w
				}
			}
		}
		return bx, bx + bw
	}
	ex := energy(rc.Min.X-2*d, rc.Max.X+2*d, rc.Min.Y, rc.Max.Y, true)
	x0, x1 := solve(rc.Min.X, rc.Max.X, ex)
	ey := energy(rc.Min.Y-2*d, rc.Max.Y+2*d, x0, x1, false)
	y0, y1 := solve(rc.Min.Y, rc.Max.Y, ey)
	out := image.Rect(x0, y0, x1, y1)
	if !out.In(b) {
		return rc
	}
	return out
}

// GridScore rates how well rc looks like a chessboard grid (0..1) in img.
func GridScore(img *image.RGBA, rc image.Rectangle) float64 {
	if !rc.In(img.Bounds()) || rc.Dx() < 96 || rc.Dy() < 96 {
		return 0
	}
	return gridScore(img, rc, true)
}

// boardClasses returns the two square colours of the board rc and their contrast.
func boardClasses(img *image.RGBA, rc image.Rectangle) (cls [2][3]int, contrast int, ok bool) {
	var par [2][3][32]int
	var np [2]int
	for row := 0; row < 8; row++ {
		for col := 0; col < 8; col++ {
			bg, _, good := bgSample(img, squareRect(rc, col, row))
			if !good {
				continue
			}
			k := (row + col) & 1
			if np[k] < 32 {
				for c := 0; c < 3; c++ {
					par[k][c][np[k]] = bg[c]
				}
				np[k]++
			}
		}
	}
	if np[0] < 6 || np[1] < 6 {
		return cls, 0, false
	}
	for k := 0; k < 2; k++ {
		for c := 0; c < 3; c++ {
			cls[k][c] = medianInts(par[k][c][:np[k]])
		}
	}
	for c := 0; c < 3; c++ {
		if d := abs(cls[0][c] - cls[1][c]); d > contrast {
			contrast = d
		}
	}
	return cls, contrast, contrast >= 14
}

// AlignRect corrects a board rectangle that is off by whole squares. A
// checkerboard looks the same after a shift by one square, so detection can
// land one file or rank away; the tell-tale is that the squares just outside
// one edge continue the checker pattern while the squares along the opposite
// inner edge do not belong to it (page background). The rectangle is moved by
// one square in that case. The bool reports whether anything changed.
func AlignRect(img *image.RGBA, rc image.Rectangle) (image.Rectangle, bool) {
	changed := false
	for iter := 0; iter < 3; iter++ {
		cls, contrast, ok := boardClasses(img, rc)
		if !ok {
			return rc, changed
		}
		sq := rc.Dx() / 8
		if sq < 12 {
			return rc, changed
		}
		match := func(x, y int, parity int) bool {
			bg, pur, good := bgSample(img, image.Rect(x, y, x+sq, y+sq))
			if !good || pur < 0.5 {
				return false
			}
			d, o := 0, 0
			for c := 0; c < 3; c++ {
				d = max(d, abs(bg[c]-cls[parity][c]))
				o = max(o, abs(bg[c]-cls[1-parity][c]))
			}
			return d <= 16 && o >= contrast/2
		}
		// share of the 8 squares of a column (col index -1..8) / row that match
		colShare := func(col int) float64 {
			n := 0
			for r := 0; r < 8; r++ {
				if match(rc.Min.X+col*sq, rc.Min.Y+r*sq, (r+col)&1) {
					n++
				}
			}
			return float64(n) / 8
		}
		rowShare := func(row int) float64 {
			n := 0
			for c := 0; c < 8; c++ {
				if match(rc.Min.X+c*sq, rc.Min.Y+row*sq, (row+c)&1) {
					n++
				}
			}
			return float64(n) / 8
		}
		// share of a line that matches when parity is taken either way: the
		// outer line must continue the pattern, so its parity is fixed.
		const hi, lo = 0.75, 0.6
		move := image.Point{}
		switch {
		case colShare(-1) >= hi && colShare(7) < lo:
			move.X = -sq
		case colShare(8) >= hi && colShare(0) < lo:
			move.X = sq
		}
		switch {
		case rowShare(-1) >= hi && rowShare(7) < lo:
			move.Y = -sq
		case rowShare(8) >= hi && rowShare(0) < lo:
			move.Y = sq
		}
		if move == (image.Point{}) {
			return rc, changed
		}
		nr := rc.Add(move)
		if !nr.In(img.Bounds()) {
			return rc, changed
		}
		rc, changed = nr, true
	}
	return rc, changed
}
