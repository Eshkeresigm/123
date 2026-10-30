package vision

import (
	"image"
	"math"
	"sort"
)

// Silhouettes are the style-independent description of a piece: the filled
// outline of whatever stands on the square, in square-relative coordinates.
const (
	fineGrid = 48 // stored resolution
	sGrid    = 24 // matching resolution
)

// obs is one occupied square seen on the screenshot.
type obs struct {
	col, row int
	fine     [fineGrid * fineGrid]float32 // silhouette coverage per cell
	luma     float64                      // median brightness of the piece body
	area, bh float64
	cx       float64
}

// segmentSquare extracts the silhouette of the piece on square r. It keeps
// only the ink components that reach the middle of the square (so coordinate
// labels in the corners are ignored), fills the holes (hollow white pieces)
// and reports whether the square is occupied.
func segmentSquare(img *image.RGBA, r image.Rectangle, bg [3]int) (o obs, occupied bool) {
	w, h := r.Dx(), r.Dy()
	if w < 12 || h < 12 {
		return o, false
	}
	ink := make([]bool, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			px, py := r.Min.X+x, r.Min.Y+y
			if !(image.Point{X: px, Y: py}).In(img.Rect) {
				continue
			}
			i := img.PixOffset(px, py)
			d := abs(int(img.Pix[i]) - bg[0])
			if e := abs(int(img.Pix[i+1]) - bg[1]); e > d {
				d = e
			}
			if e := abs(int(img.Pix[i+2]) - bg[2]); e > d {
				d = e
			}
			mg := max(1, w/40)
			ink[y*w+x] = d >= 26 && x >= mg && y >= mg && x < w-mg && y < h-mg
		}
	}
	// connected components (8-neighbourhood)
	comp := make([]int32, w*h)
	type cinfo struct {
		area           int
		x0, y0, x1, y1 int
	}
	var infos []cinfo
	stack := make([]int, 0, 256)
	for s := 0; s < w*h; s++ {
		if !ink[s] || comp[s] != 0 {
			continue
		}
		id := int32(len(infos) + 1)
		ci := cinfo{x0: w, y0: h, x1: -1, y1: -1}
		stack = append(stack[:0], s)
		comp[s] = id
		for len(stack) > 0 {
			p := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			x, y := p%w, p/w
			ci.area++
			ci.x0, ci.x1 = min(ci.x0, x), max(ci.x1, x)
			ci.y0, ci.y1 = min(ci.y0, y), max(ci.y1, y)
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					nx, ny := x+dx, y+dy
					if nx < 0 || ny < 0 || nx >= w || ny >= h {
						continue
					}
					q := ny*w + nx
					if ink[q] && comp[q] == 0 {
						comp[q] = id
						stack = append(stack, q)
					}
				}
			}
		}
		infos = append(infos, ci)
	}
	keep := make([]bool, len(infos)+1)
	cx0, cx1 := int(0.22*float64(w)), int(0.78*float64(w))
	cy0, cy1 := int(0.22*float64(h)), int(0.78*float64(h))
	any := false
	for i, ci := range infos {
		if ci.x1 >= cx0 && ci.x0 <= cx1 && ci.y1 >= cy0 && ci.y0 <= cy1 && ci.area >= w*h/400 {
			keep[i+1] = true
			any = true
		}
	}
	if !any {
		return o, false
	}
	// dilate (closes hairline gaps), flood the exterior, erode back
	rd := max(1, w/50)
	mask := make([]bool, w*h)
	for i, c := range comp {
		mask[i] = c > 0 && keep[c]
	}
	dil := dilate(mask, w, h, rd)
	ext := make([]bool, w*h)
	stack = stack[:0]
	push := func(x, y int) {
		p := y*w + x
		if !dil[p] && !ext[p] {
			ext[p] = true
			stack = append(stack, p)
		}
	}
	for x := 0; x < w; x++ {
		push(x, 0)
		push(x, h-1)
	}
	for y := 0; y < h; y++ {
		push(0, y)
		push(w-1, y)
	}
	for len(stack) > 0 {
		p := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		x, y := p%w, p/w
		if x > 0 {
			push(x-1, y)
		}
		if x < w-1 {
			push(x+1, y)
		}
		if y > 0 {
			push(x, y-1)
		}
		if y < h-1 {
			push(x, y+1)
		}
	}
	filled := make([]bool, w*h)
	for i := range filled {
		filled[i] = !ext[i]
	}
	sil := erode(filled, w, h, rd)

	var cnt int
	var sx float64
	minX, minY, maxX, maxY := w, h, -1, -1
	var cell [fineGrid * fineGrid]float32
	var lumas []float64
	for y := 0; y < h; y++ {
		gy := y * fineGrid / h
		for x := 0; x < w; x++ {
			if !sil[y*w+x] {
				continue
			}
			cnt++
			sx += float64(x)
			minX, maxX = min(minX, x), max(maxX, x)
			minY, maxY = min(minY, y), max(maxY, y)
			cell[gy*fineGrid+x*fineGrid/w]++
			if (x+y)&1 == 0 {
				i := img.PixOffset(r.Min.X+x, r.Min.Y+y)
				if (image.Point{X: r.Min.X + x, Y: r.Min.Y + y}).In(img.Rect) {
					lumas = append(lumas, float64(lum(img.Pix[i], img.Pix[i+1], img.Pix[i+2])))
				}
			}
		}
	}
	if cnt == 0 || len(lumas) == 0 {
		return o, false
	}
	o.area = float64(cnt) / float64(w*h)
	o.bh = float64(maxY-minY+1) / float64(h)
	o.cx = sx / float64(cnt) / float64(w)
	bw := float64(maxX-minX+1) / float64(w)
	if o.area < 0.10 || o.bh < 0.33 || bw < 0.2 {
		return o, false
	}
	var cellN [fineGrid * fineGrid]float32
	for y := 0; y < h; y++ {
		gy := y * fineGrid / h
		for x := 0; x < w; x++ {
			cellN[gy*fineGrid+x*fineGrid/w]++
		}
	}
	for gy := 0; gy < fineGrid; gy++ {
		for gx := 0; gx < fineGrid; gx++ {
			n := float64(cellN[gy*fineGrid+gx])
			if n == 0 {
				continue
			}
			v := float64(cell[gy*fineGrid+gx]) / n
			if v > 1 {
				v = 1
			}
			o.fine[gy*fineGrid+gx] = float32(v)
		}
	}
	sort.Float64s(lumas)
	o.luma = lumas[len(lumas)/2]
	return o, true
}

func dilate(m []bool, w, h, r int) []bool {
	// separable square dilation
	tmp := make([]bool, len(m))
	out := make([]bool, len(m))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			for dx := -r; dx <= r && !tmp[y*w+x]; dx++ {
				if nx := x + dx; nx >= 0 && nx < w && m[y*w+nx] {
					tmp[y*w+x] = true
				}
			}
		}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			for dy := -r; dy <= r && !out[y*w+x]; dy++ {
				if ny := y + dy; ny >= 0 && ny < h && tmp[ny*w+x] {
					out[y*w+x] = true
				}
			}
		}
	}
	return out
}

func erode(m []bool, w, h, r int) []bool {
	inv := make([]bool, len(m))
	for i, v := range m {
		inv[i] = !v
	}
	d := dilate(inv, w, h, r)
	out := make([]bool, len(m))
	for i := range out {
		out[i] = !d[i]
	}
	return out
}

// toGrid converts the fine silhouette to the matching grid, scaled by m around
// the piece's feet (0.5, 0.92) and shifted horizontally by dx cells (fraction
// of the square), using bilinear sampling.
func (o *obs) toGrid(m float64) (g [sGrid * sGrid]float32) {
	for gy := 0; gy < sGrid; gy++ {
		v := (float64(gy) + 0.5) / sGrid
		sy := 0.92 + (v-0.92)/m
		for gx := 0; gx < sGrid; gx++ {
			u := (float64(gx) + 0.5) / sGrid
			sx := 0.5 + (u-0.5)/m
			g[gy*sGrid+gx] = sampleFine(&o.fine, sx, sy)
		}
	}
	return g
}

func sampleFine(f *[fineGrid * fineGrid]float32, x, y float64) float32 {
	if x < 0 || y < 0 || x >= 1 || y >= 1 {
		return 0
	}
	fx, fy := x*fineGrid-0.5, y*fineGrid-0.5
	x0, y0 := int(math.Floor(fx)), int(math.Floor(fy))
	tx, ty := float32(fx-float64(x0)), float32(fy-float64(y0))
	at := func(xx, yy int) float32 {
		if xx < 0 || yy < 0 || xx >= fineGrid || yy >= fineGrid {
			return 0
		}
		return f[yy*fineGrid+xx]
	}
	a := at(x0, y0)*(1-tx) + at(x0+1, y0)*tx
	b := at(x0, y0+1)*(1-tx) + at(x0+1, y0+1)*tx
	return a*(1-ty) + b*ty
}

// silDist is 1-IoU of two silhouettes, minimised over small shifts.
func silDist(a, b *[sGrid * sGrid]float32) float64 {
	best := 1.0
	const sh = 2
	for dy := -sh; dy <= sh; dy++ {
		for dx := -sh; dx <= sh; dx++ {
			var inter, uni float32
			for y := 0; y < sGrid; y++ {
				yy := y + dy
				for x := 0; x < sGrid; x++ {
					xx := x + dx
					va := a[y*sGrid+x]
					var vb float32
					if xx >= 0 && xx < sGrid && yy >= 0 && yy < sGrid {
						vb = b[yy*sGrid+xx]
					}
					if va < vb {
						inter += va
						uni += vb
					} else {
						inter += vb
						uni += va
					}
				}
			}
			if uni > 0 {
				if d := 1 - float64(inter/uni); d < best {
					best = d
				}
			}
		}
	}
	return best
}

// symmetry returns the IoU of the silhouette with its mirror image about the
// vertical axis through the middle of its bounding box (1 = symmetric).
func (o *obs) symmetry() float64 {
	minX, maxX := fineGrid, -1
	for gy := 0; gy < fineGrid; gy++ {
		for gx := 0; gx < fineGrid; gx++ {
			if o.fine[gy*fineGrid+gx] > 0.5 {
				minX, maxX = min(minX, gx), max(maxX, gx)
			}
		}
	}
	if maxX < minX {
		return 0
	}
	var inter, uni float64
	for gy := 0; gy < fineGrid; gy++ {
		for gx := minX; gx <= maxX; gx++ {
			a := float64(o.fine[gy*fineGrid+gx])
			b := float64(o.fine[gy*fineGrid+(minX+maxX-gx)])
			inter += math.Min(a, b)
			uni += math.Max(a, b)
		}
	}
	if uni == 0 {
		return 0
	}
	return inter / uni
}
