package app

import (
	"image"
	"math"

	"chessvision/internal/chess"
	"chessvision/internal/gfx"
)

// overlayArrowAlpha is the opacity of the white arrow body. It stays below the
// un-blending limit (see unblend) so the pieces under it remain recoverable.
const overlayArrowAlpha = 0.80

func pxwOf(sx, sy float64) float64 { return math.Max(1.5, math.Min(sx, sy)*0.028) }

// renderOverlay draws the suggested move as an arrow over a w×h board.
func renderOverlay(w, h int, whiteAtBottom bool, m chess.Move) *image.RGBA {
	return renderOverlayT(w, h, whiteAtBottom, m, 1, 1)
}

// renderOverlayT draws the arrow extended to the fraction grow (0..1) of its
// length with its opacity scaled by k (0..1); (1,1) is the resting picture.
func renderOverlayT(w, h int, whiteAtBottom bool, m chess.Move, grow, k float64) *image.RGBA {
	c := gfx.NewCanvas(w, h)
	sx, sy := float64(w)/8, float64(h)/8
	centre := func(sq int) (float64, float64) {
		f, r := float64(chess.File(sq)), float64(chess.Rank(sq))
		if whiteAtBottom {
			return (f + 0.5) * sx, (7 - r + 0.5) * sy
		}
		return (7 - f + 0.5) * sx, (r + 0.5) * sy
	}
	x0, y0 := centre(m.From())
	x1, y1 := centre(m.To())
	ex, ey := x0+(x1-x0)*grow, y0+(y1-y0)*grow
	white := gfx.Hex(0xFFFFFF)
	ink := gfx.Hex(0x1B2340)
	// squares: white-edged frames with an ink outline (readable on any theme)
	for i, sq := range []int{m.From(), m.To()} {
		cx, cy := centre(sq)
		a := 0.55
		if i == 1 {
			a = 0.85
		}
		a *= k
		x, y, w2, h2 := cx-sx/2+2, cy-sy/2+2, sx-4, sy-4
		th := math.Max(2, math.Min(sx, sy)*0.045)
		c.FillRound(x, y, w2, h2, sx*0.12, gfx.Solid(white.Alpha(a*0.22)))
		c.StrokeRound(x-th*0.6, y-th*0.6, w2+th*1.2, h2+th*1.2, sx*0.14, th*0.7, gfx.Solid(ink.Alpha(a*0.55)))
		c.StrokeRound(x, y, w2, h2, sx*0.12, th, gfx.Solid(white.Alpha(a*overlayArrowAlpha)))
	}
	// arrow
	sc := 0.35 + 0.65*grow
	shaft := math.Min(sx, sy) * 0.17 * sc
	head := math.Min(sx, sy) * 0.46 * sc
	poly := gfx.Arrow(x0, y0, ex, ey, shaft, head)
	if poly != nil {
		pxw := math.Max(1.5, math.Min(sx, sy)*0.028)
		c.FillOutlined(poly, white.Alpha(overlayArrowAlpha*k), ink.Alpha(0.62*k), gfx.RGBA(20, 28, 70, uint8(80*k)), pxw, math.Max(3, pxw*2.5), int(math.Max(2, pxw*1.5)))
	}
	r0 := math.Min(sx, sy) * 0.17 * 0.9 * sc
	c.FillCircle(x0, y0, r0+pxwOf(sx, sy), gfx.Solid(ink.Alpha(0.62*k)))
	c.FillCircle(x0, y0, r0, gfx.Solid(white.Alpha(overlayArrowAlpha*k)))
	return c.Img
}

// unblend removes a known premultiplied overlay from a capture of the same
// area, recovering the pixels that lie underneath it.
func unblend(img, ov *image.RGBA) {
	if img.Rect.Dx() != ov.Rect.Dx() || img.Rect.Dy() != ov.Rect.Dy() {
		return
	}
	for y := 0; y < img.Rect.Dy(); y++ {
		ip := y * img.Stride
		op := y * ov.Stride
		for x := 0; x < img.Rect.Dx(); x++ {
			a := int(ov.Pix[op+3])
			if a != 0 && a < 235 {
				for k := 0; k < 3; k++ {
					v := (int(img.Pix[ip+k]) - int(ov.Pix[op+k])) * 255 / (255 - a)
					if v < 0 {
						v = 0
					} else if v > 255 {
						v = 255
					}
					img.Pix[ip+k] = uint8(v)
				}
			}
			ip += 4
			op += 4
		}
	}
}
