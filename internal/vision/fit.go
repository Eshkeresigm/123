package vision

import (
	"image"
	"math"
	"runtime"
	"sort"
	"sync"

	"chessvision/internal/chess"
)

// Occupancy thresholds shared by the board fitter and OccupancyMatch.
const (
	occCover = 0.06
	occBBox  = 0.25
)

// squareInk measures how much a square differs from its own background in
// the central area (ink coverage, height of the strong-ink bbox) and how
// uniform the side strips are (purity: 1 when the grid is aligned, lower when
// the strips cross a square boundary). It does not allocate.
func squareInk(img *image.RGBA, r image.Rectangle, stride int) (cov, bboxH, purity float64) {
	w, h := r.Dx(), r.Dy()
	if w < 4 || h < 4 {
		return 0, 0, 0
	}
	// 32 fixed sample points in the four quiet side strips
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
	if n == 0 {
		return 0, 0, 0
	}
	var bg [3]int
	minShare := float64(1)
	for c := 0; c < 3; c++ {
		v := smp[c][:n]
		for i := 1; i < len(v); i++ { // insertion sort
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
	purity = minShare

	x0, x1 := r.Min.X+int(0.12*float64(w)), r.Min.X+int(0.88*float64(w))
	cy0, cy1 := r.Min.Y+int(0.12*float64(h)), r.Min.Y+int(0.88*float64(h))
	var sum, cnt float64
	minY, maxY := math.MaxInt32, -1
	for y := cy0; y < cy1; y += stride {
		for x := x0; x < x1; x += stride {
			if !(image.Point{X: x, Y: y}).In(img.Rect) {
				continue
			}
			i := img.PixOffset(x, y)
			d := abs(int(img.Pix[i]) - bg[0])
			if e := abs(int(img.Pix[i+1]) - bg[1]); e > d {
				d = e
			}
			if e := abs(int(img.Pix[i+2]) - bg[2]); e > d {
				d = e
			}
			var a float64
			switch {
			case d >= inkHigh:
				a = 1
			case d > inkLow:
				a = (float64(d) - inkLow) / (inkHigh - inkLow)
			}
			sum += a
			cnt++
			if a > 0.5 {
				if y < minY {
					minY = y
				}
				if y > maxY {
					maxY = y
				}
			}
		}
	}
	if cnt == 0 {
		return 0, 0, purity
	}
	if maxY >= minY {
		bboxH = float64(maxY-minY+stride) / float64(h)
	}
	return sum / cnt, bboxH, purity
}

// fitScore is a smooth alignment score of a candidate board rectangle against
// a known placement: occupied squares should be full of ink, empty ones blank.
// It also returns the plain occupied/empty agreement (0..1).
func fitScore(img *image.RGBA, rc image.Rectangle, white bool, placement *[64]chess.Piece, stride int) (score, agree float64) {
	ok, total := 0, 0
	for row := 0; row < 8; row++ {
		for col := 0; col < 8; col++ {
			if stride > 1 && (row+col)%2 == 1 { // coarse pass: half of the squares
				continue
			}
			total++
			cov, bh, pur := squareInk(img, squareRect(rc, col, row), stride)
			score += 0.8 * pur
			want := placement[gridToSquare(col, row, white)] != chess.NoPiece
			occ := cov >= occCover && bh >= occBBox
			if occ == want {
				ok++
			}
			if want {
				score += math.Min(cov, 0.30) / 0.30
			} else {
				score += 1 - math.Min(cov/0.12, 1)
			}
		}
	}
	return score, float64(ok) / float64(total)
}

// FitBoard finds the board rectangle (and checks the occupancy) for a known
// placement. It starts from several guesses (FindBoard result, the whole
// image, its centred square) and refines each edge by hill climbing, so it
// tolerates loose selections, coordinates, frames and side panels.
func FitBoard(img *image.RGBA, white bool, placement [64]chess.Piece) (image.Rectangle, float64) {
	r, a := fitBoard(img, white, placement, 0.3)
	if a < 0.86 {
		if r2, a2 := fitBoard(img, white, placement, 0); a2 > a {
			return r2, a2
		}
	}
	return r, a
}

func fitBoard(img *image.RGBA, white bool, placement [64]chess.Piece, minFrac float64) (image.Rectangle, float64) {
	b := img.Bounds()
	seeds := coarseSeeds(img, white, &placement, minFrac)
	if r, ok := FindBoard(img); ok {
		seeds = append(seeds, r)
	}
	valid := func(r image.Rectangle) bool {
		if !r.In(b) || r.Dx() < 96 || r.Dy() < 96 {
			return false
		}
		ar := float64(r.Dx()) / float64(r.Dy())
		return ar > 0.9 && ar < 1.1
	}
	bestRect, bestScore := b, -1.0
	for _, seed := range seeds {
		// make the seed squarish and valid
		cur := seed
		if !valid(cur) {
			s := min(cur.Dx(), cur.Dy())
			cx, cy := (cur.Min.X+cur.Max.X)/2, (cur.Min.Y+cur.Max.Y)/2
			cur = image.Rect(cx-s/2, cy-s/2, cx-s/2+s, cy-s/2+s).Intersect(b)
			if !valid(cur) {
				continue
			}
		}
		cs, _ := fitScore(img, cur, white, &placement, 1)
		step := max(2, cur.Dx()/24)
		for step >= 1 {
			improved := false
			for edge := 0; edge < 4; edge++ {
				for _, dir := range []int{-1, 1} {
					cand := cur
					switch edge {
					case 0:
						cand.Min.X += dir * step
					case 1:
						cand.Min.Y += dir * step
					case 2:
						cand.Max.X += dir * step
					case 3:
						cand.Max.Y += dir * step
					}
					if !valid(cand) {
						continue
					}
					if s, _ := fitScore(img, cand, white, &placement, 1); s > cs+1e-9 {
						cur, cs, improved = cand, s, true
					}
				}
			}
			// also try moving/zooming the whole square
			for _, d := range []image.Rectangle{
				{Min: image.Pt(-step, 0), Max: image.Pt(-step, 0)}, {Min: image.Pt(step, 0), Max: image.Pt(step, 0)},
				{Min: image.Pt(0, -step), Max: image.Pt(0, -step)}, {Min: image.Pt(0, step), Max: image.Pt(0, step)},
				{Min: image.Pt(-step, -step), Max: image.Pt(step, step)}, {Min: image.Pt(step, step), Max: image.Pt(-step, -step)},
			} {
				cand := image.Rect(cur.Min.X+d.Min.X, cur.Min.Y+d.Min.Y, cur.Max.X+d.Max.X, cur.Max.Y+d.Max.Y)
				if !valid(cand) {
					continue
				}
				if s, _ := fitScore(img, cand, white, &placement, 1); s > cs+1e-9 {
					cur, cs, improved = cand, s, true
				}
			}
			if !improved {
				step /= 2
			}
		}
		if cs > bestScore {
			bestScore, bestRect = cs, cur
		}
	}
	if bestScore < 0 {
		return b, 0
	}
	_, agree := fitScore(img, bestRect, white, &placement, 1)
	return bestRect, agree
}

// WhiteBrighter reports whether the learned white pieces are brighter than
// the black ones (a sanity check of the orientation used for learning).
func (p *Profile) WhiteBrighter() (bool, bool) {
	mean := func(c0 int) (float64, bool) {
		var a, al float64
		for c := c0; c < c0+6; c++ {
			for _, f := range p.Samples[c] {
				for i := 0; i < G*G; i++ {
					a += float64(f[i])
					al += float64(f[G*G+i])
				}
			}
		}
		if a <= 0 {
			return 0, false
		}
		return al / a, true
	}
	w, ok1 := mean(0)
	k, ok2 := mean(6)
	if !ok1 || !ok2 {
		return true, false
	}
	return w >= k, math.Abs(w-k) > 0.06
}

type cand struct {
	r image.Rectangle
	s float64
}

// coarseSeeds scans squares of many sizes and positions with a cheap,
// subsampled score and returns the best few distinct candidates.
func coarseSeeds(img *image.RGBA, white bool, placement *[64]chess.Piece, minFrac float64) []image.Rectangle {
	b := img.Bounds()
	maxS := min(b.Dx(), b.Dy())
	var sizes []int
	for s := math.Max(96, minFrac*float64(maxS)); s < float64(maxS); s *= 1.04 {
		sizes = append(sizes, int(s)&^7)
	}
	if maxS >= 96 {
		sizes = append(sizes, maxS&^7)
	}
	var mu sync.Mutex
	var all []cand
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	last := -1
	for _, s := range sizes {
		if s == last || s < 96 {
			continue
		}
		last = s
		wg.Add(1)
		sem <- struct{}{}
		go func(s int) {
			defer wg.Done()
			defer func() { <-sem }()
			stride := max(2, s/64)
			st := max(2, s/24)
			var local []cand
			for y := b.Min.Y; y+s <= b.Max.Y; y += st {
				for x := b.Min.X; x+s <= b.Max.X; x += st {
					r := image.Rect(x, y, x+s, y+s)
					sc, _ := fitScore(img, r, white, placement, stride)
					local = append(local, cand{r, sc})
				}
			}
			sort.Slice(local, func(i, j int) bool { return local[i].s > local[j].s })
			if len(local) > 3 {
				local = local[:3]
			}
			mu.Lock()
			all = append(all, local...)
			mu.Unlock()
		}(s)
	}
	wg.Wait()
	sort.Slice(all, func(i, j int) bool { return all[i].s > all[j].s })
	var out []image.Rectangle
	for _, c := range all {
		dup := false
		for _, o := range out {
			if abs(c.r.Min.X-o.Min.X) < o.Dx()/6 && abs(c.r.Min.Y-o.Min.Y) < o.Dy()/6 && abs(c.r.Dx()-o.Dx()) < o.Dx()/6 {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, c.r)
			if len(out) == 8 {
				break
			}
		}
	}
	return out
}
