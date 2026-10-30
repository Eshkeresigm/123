package vision

import (
	"image"
	"testing"

	"chessvision/assets"
	"chessvision/internal/chess"
)

// A rectangle that is off by whole squares (the checker pattern repeats) must
// be moved back; a correct one must stay.
func TestAlignRect(t *testing.T) {
	ui := mustFont(t, assets.UIRegular)
	for _, st := range styles(t) {
		for _, white := range []bool{true, false} {
			o := renderOpts{sq: 70, margin: 90, whiteAtBottom: white, lastMove: [2]int{-1, -1}}
			p := chess.Start()
			img, rect := renderBoard(st, p.Board, o, ui)
			// page background around the board is the renderer's dark margin
			if got, ch := AlignRect(img, rect); ch || got != rect {
				t.Errorf("%s: correct rect was changed to %v", st.name, got)
			}
			sq := rect.Dx() / 8
			for _, d := range []image.Point{{-sq, 0}, {sq, 0}, {0, -sq}, {0, sq}, {-sq, sq}} {
				bad := rect.Add(d)
				got, ch := AlignRect(img, bad)
				if !ch || got != rect {
					t.Errorf("%s white=%v: shift %v not repaired: got %v want %v", st.name, white, d, got, rect)
				}
			}
		}
	}
}

// The real screenshots where the board was found one square too far left.
func TestAlignRectRealShots(t *testing.T) {
	for _, tc := range []struct {
		f    string
		own  image.Rectangle
		want image.Rectangle
	}{
		{"shot3", image.Rect(843, 0, 1471, 822), image.Rect(20, 6, 825, 814)},
		{"shot4", image.Rect(998, 0, 1622, 811), image.Rect(178, 10, 980, 811)},
		{"chesscom_black", image.Rect(855, 0, 1467, 821), image.Rect(16, 7, 824, 815)},
	} {
		img := loadPNG(t, "testdata/"+tc.f+".png")
		fillRect(img, tc.own, 38)
		sq := tc.want.Dx() / 8
		for _, d := range []image.Point{{-sq, 0}, {sq, 0}, {0, 0}} {
			bad := tc.want.Add(d)
			if !bad.In(img.Bounds()) {
				continue
			}
			got, _ := AlignRect(img, bad)
			if abs(got.Min.X-tc.want.Min.X) > 3 || abs(got.Min.Y-tc.want.Min.Y) > 3 {
				t.Errorf("%s: shift %v not repaired: got %v want %v", tc.f, d, got, tc.want)
			}
		}
	}
}
