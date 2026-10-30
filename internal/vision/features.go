// Package vision recognises a chess position on a screenshot of a board.
// It needs no piece templates shipped with the program: the piece set that
// is currently on the screen is learned from a known position (normally the
// initial one) and then matched by nearest neighbour on shape/brightness
// features, which works for any theme or piece style.
package vision

import (
	"image"
	"math"
	"sort"
)

// G is the feature grid size (G x G cells per square).
const G = 16

// FeatLen is the length of a feature vector.
const FeatLen = 2 * G * G

// Feat is the descriptor of one square: coverage and coverage-weighted brightness.
type Feat [FeatLen]float32

// squareStat is everything we measure on one square.
type squareStat struct {
	Coverage float32 // ink fraction in the central area
	BBoxW    float32 // bounding box of strong ink, fraction of the square
	BBoxH    float32
	Feat     Feat
}

func lum(r, g, b uint8) float32 {
	return (0.299*float32(r) + 0.587*float32(g) + 0.114*float32(b)) / 255
}

func median(v []int) int {
	sort.Ints(v)
	return v[len(v)/2]
}

// squareBackground estimates the empty-square colour from two quiet side
// strips (pieces rarely reach them, coordinate labels sit in corners).
func squareBackground(img *image.RGBA, r image.Rectangle) (br, bg, bb int) {
	w, h := r.Dx(), r.Dy()
	var rs, gs, bs []int
	y0, y1 := r.Min.Y+int(0.35*float64(h)), r.Min.Y+int(0.65*float64(h))
	if y1 <= y0 {
		y1 = y0 + 1
	}
	strip := func(xa, xb int) {
		if xb <= xa {
			xb = xa + 1
		}
		for y := y0; y < y1; y++ {
			for x := xa; x < xb; x++ {
				if !(image.Point{x, y}).In(img.Rect) {
					continue
				}
				i := img.PixOffset(x, y)
				rs = append(rs, int(img.Pix[i]))
				gs = append(gs, int(img.Pix[i+1]))
				bs = append(bs, int(img.Pix[i+2]))
			}
		}
	}
	strip(r.Min.X+int(0.04*float64(w)), r.Min.X+int(0.12*float64(w)))
	strip(r.Min.X+int(0.88*float64(w)), r.Min.X+int(0.96*float64(w)))
	if len(rs) == 0 {
		return 128, 128, 128
	}
	return median(rs), median(gs), median(bs)
}

const (
	inkLow  = 14.0
	inkHigh = 30.0
)

// cellWeight emphasises the centre of the square (where the piece body is)
// over the border (coordinates, move hints, capture rings).
var cellWeight [G * G]float32

func init() {
	for y := 0; y < G; y++ {
		for x := 0; x < G; x++ {
			dx := math.Abs((float64(x)+0.5)/G - 0.5)
			dy := math.Abs((float64(y)+0.5)/G - 0.5)
			d := math.Max(dx, dy) // 0 .. 0.5
			w := 1.0
			if d > 0.28 {
				w = 1 - (d-0.28)/(0.5-0.28)*0.65
			}
			cellWeight[y*G+x] = float32(math.Sqrt(w))
		}
	}
}

// syntheticEmpty returns descriptors of squares that hold no piece but
// are not perfectly blank: plain squares and move-hint dots.
func syntheticEmpty() []Feat {
	out := []Feat{{}}
	for _, r := range []float64{0.07, 0.10, 0.13, 0.16, 0.19, 0.22} {
		for _, l := range []float32{0.2, 0.75} {
			var f Feat
			for y := 0; y < G; y++ {
				for x := 0; x < G; x++ {
					in := 0
					for sy := 0; sy < 4; sy++ {
						for sx := 0; sx < 4; sx++ {
							px := (float64(x) + (float64(sx)+0.5)/4) / G
							py := (float64(y) + (float64(sy)+0.5)/4) / G
							if math.Hypot(px-0.5, py-0.5) <= r {
								in++
							}
						}
					}
					a := float32(in) / 16
					f[y*G+x] = a * cellWeight[y*G+x]
					f[G*G+y*G+x] = a * l * cellWeight[y*G+x]
				}
			}
			out = append(out, f)
		}
	}
	return out
}

// glowReds are the saturated reds sites paint behind a king that is in check.
var glowReds = [][3]float64{{255, 0, 0}, {231, 0, 0}, {200, 0, 0}, {170, 0, 0}}

// isRedGlowPixel: strongly red. Brown board squares stay well below this
// (their r-g is at most ~55), a king glow is far above.
func isRedGlowPixel(r, g, b uint8) bool {
	return int(r) > int(g)+75 && int(r) > int(b)+75
}

// hasRedGlow reports whether the square carries a red "check" glow: a sizeable
// share of strongly red pixels.
func hasRedGlow(img *image.RGBA, r image.Rectangle) bool {
	red, n := 0, 0
	for y := r.Min.Y; y < r.Max.Y; y += 2 {
		for x := r.Min.X; x < r.Max.X; x += 2 {
			if !(image.Point{x, y}).In(img.Rect) {
				continue
			}
			i := img.PixOffset(x, y)
			n++
			if isRedGlowPixel(img.Pix[i], img.Pix[i+1], img.Pix[i+2]) {
				red++
			}
		}
	}
	return n > 0 && float64(red)/float64(n) > 0.06
}

// cornerBackground estimates the plain square colour from the four corners,
// which a centred glow barely reaches.
func cornerBackground(img *image.RGBA, r image.Rectangle) (br, bg, bb int) {
	w, h := r.Dx(), r.Dy()
	var rs, gs, bs []int
	sz := max(2, int(0.14*float64(min(w, h))))
	for _, c := range [][2]int{{r.Min.X + 1, r.Min.Y + 1}, {r.Max.X - sz - 1, r.Min.Y + 1}, {r.Min.X + 1, r.Max.Y - sz - 1}, {r.Max.X - sz - 1, r.Max.Y - sz - 1}} {
		for y := c[1]; y < c[1]+sz; y++ {
			for x := c[0]; x < c[0]+sz; x++ {
				if !(image.Point{x, y}).In(img.Rect) {
					continue
				}
				i := img.PixOffset(x, y)
				rs = append(rs, int(img.Pix[i]))
				gs = append(gs, int(img.Pix[i+1]))
				bs = append(bs, int(img.Pix[i+2]))
			}
		}
	}
	if len(rs) == 0 {
		return 128, 128, 128
	}
	return median(rs), median(gs), median(bs)
}

// glowResidual is the colour distance from pixel p to the nearest blend of the
// square colour with a glow red (0 = pure glow / background).
func glowResidual(pr, pg, pb, br, bg, bb int) float64 {
	best := math.MaxFloat64
	bx, by, bz := float64(br), float64(bg), float64(bb)
	px, py, pz := float64(pr), float64(pg), float64(pb)
	for _, rd := range glowReds {
		ex, ey, ez := rd[0]-bx, rd[1]-by, rd[2]-bz
		den := ex*ex + ey*ey + ez*ez
		if den == 0 {
			continue
		}
		a := ((px-bx)*ex + (py-by)*ey + (pz-bz)*ez) / den
		a = math.Max(0, math.Min(1, a))
		d := math.Max(math.Abs(px-(bx+a*ex)), math.Max(math.Abs(py-(by+a*ey)), math.Abs(pz-(bz+a*ez))))
		if d < best {
			best = d
		}
	}
	return best
}

// analyzeSquare measures the square r; (sx, sy) shifts the sampling window
// in pixels (used for training augmentation).
func analyzeSquare(img *image.RGBA, r image.Rectangle, sx, sy float64) squareStat {
	var st squareStat
	br, bg, bb := squareBackground(img, r)
	glow := hasRedGlow(img, r)
	if glow {
		br, bg, bb = cornerBackground(img, r)
	}
	w, h := float64(r.Dx()), float64(r.Dy())
	var cellA, cellAL [G * G]float32
	var cellN [G * G]float32
	var inkSum, centerN float32
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	cx0, cx1 := 0.12, 0.88
	for y := 0; y < int(math.Ceil(h)); y++ {
		for x := 0; x < int(math.Ceil(w)); x++ {
			px, py := r.Min.X+x+int(math.Round(sx)), r.Min.Y+y+int(math.Round(sy))
			fx, fy := (float64(x)+0.5)/w, (float64(y)+0.5)/h
			if fx >= 1 || fy >= 1 {
				continue
			}
			cell := int(fy*G)*G + int(fx*G)
			var a, l float32
			if (image.Point{px, py}).In(img.Rect) {
				i := img.PixOffset(px, py)
				pr, pg, pb := img.Pix[i], img.Pix[i+1], img.Pix[i+2]
				d := math.Max(math.Abs(float64(int(pr)-br)), math.Max(math.Abs(float64(int(pg)-bg)), math.Abs(float64(int(pb)-bb))))
				if glow && d > inkLow {
					if gd := glowResidual(int(pr), int(pg), int(pb), br, bg, bb); gd < d {
						d = gd
					}
				}
				switch {
				case d >= inkHigh:
					a = 1
				case d > inkLow:
					a = float32((d - inkLow) / (inkHigh - inkLow))
				}
				l = lum(pr, pg, pb)
			}
			cellA[cell] += a
			cellAL[cell] += a * l
			cellN[cell]++
			if fx >= cx0 && fx <= cx1 && fy >= cx0 && fy <= cx1 {
				inkSum += a
				centerN++
				if a > 0.5 {
					minX, maxX = math.Min(minX, fx), math.Max(maxX, fx)
					minY, maxY = math.Min(minY, fy), math.Max(maxY, fy)
				}
			}
		}
	}
	for i := 0; i < G*G; i++ {
		if cellN[i] > 0 {
			st.Feat[i] = cellA[i] / cellN[i] * cellWeight[i]
			st.Feat[G*G+i] = cellAL[i] / cellN[i] * cellWeight[i]
		}
	}
	if centerN > 0 {
		st.Coverage = inkSum / centerN
	}
	if maxX >= minX {
		st.BBoxW = float32(maxX - minX)
		st.BBoxH = float32(maxY - minY)
	}
	return st
}

func dist2(a, b *Feat) float32 {
	var s float32
	for i := 0; i < FeatLen; i++ {
		d := a[i] - b[i]
		s += d * d
	}
	return s
}
