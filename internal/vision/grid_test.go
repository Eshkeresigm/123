package vision

import (
	"image"
	"image/draw"
	"image/png"
	"math"
	"os"

	"chessvision/internal/chess"
	"testing"
	"time"
)

func loadPNG(t testing.TB, path string) *image.RGBA {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	im, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	out := image.NewRGBA(im.Bounds())
	draw.Draw(out, out.Rect, im, im.Bounds().Min, draw.Src)
	return out
}

func TestDetectBoardRealScreenshot(t *testing.T) {
	full := loadPNG(t, "testdata/chesscom_full.png")
	t0 := time.Now()
	r, conf := DetectBoard(full)
	t.Logf("full screenshot: %v conf=%.2f in %v", r, conf, time.Since(t0))
	crop := image.NewRGBA(image.Rect(0, 0, 830, 821))
	draw.Draw(crop, crop.Rect, full, image.Point{}, draw.Src)
	t0 = time.Now()
	r2, conf2 := DetectBoard(crop)
	t.Logf("left crop: %v conf=%.2f in %v", r2, conf2, time.Since(t0))
}

func TestAutoLearnRealScreenshot(t *testing.T) {
	full := loadPNG(t, "testdata/chesscom_full.png")
	crop := image.NewRGBA(image.Rect(0, 0, 830, 821))
	draw.Draw(crop, crop.Rect, full, image.Point{}, draw.Src)
	rect, conf := DetectBoard(crop)
	t.Logf("board %v conf=%.2f", rect, conf)
	want, err := chess.ParseFEN("rnbqkbnr/pppp1p1p/4p1p1/7Q/4P3/8/PPPP1PPP/RNB1KBNR w KQkq - 0 4")
	if err != nil {
		t.Fatal(err)
	}
	autoDebug = func(m string) { t.Log(m) }
	defer func() { autoDebug = nil }()
	t0 := time.Now()
	res, err := AutoLearn(crop, rect, []RefSet{glyphRefSet()})
	t.Logf("glyph-only: %v err=%v in %v", res, err, time.Since(t0))
	if err != nil {
		t.Fatal(err)
	}
	diff := 0
	for sq := 0; sq < 64; sq++ {
		if res.Placement[sq] != want.Board[sq] {
			diff++
			t.Logf("sq %s: got %d want %d", chess.SqName(sq), res.Placement[sq], want.Board[sq])
		}
	}
	t.Logf("differences: %d", diff)
}

func resizeRGBA(src *image.RGBA, f float64) *image.RGBA {
	w, h := int(float64(src.Rect.Dx())*f), int(float64(src.Rect.Dy())*f)
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sx, sy := (float64(x)+0.5)/f-0.5, (float64(y)+0.5)/f-0.5
			x0, y0 := int(sx), int(sy)
			if sx < 0 {
				x0 = 0
			}
			if sy < 0 {
				y0 = 0
			}
			x1, y1 := min(x0+1, src.Rect.Dx()-1), min(y0+1, src.Rect.Dy()-1)
			tx, ty := sx-float64(x0), sy-float64(y0)
			if tx < 0 {
				tx = 0
			}
			if ty < 0 {
				ty = 0
			}
			for c := 0; c < 4; c++ {
				a := float64(src.Pix[src.PixOffset(x0, y0)+c])*(1-tx) + float64(src.Pix[src.PixOffset(x1, y0)+c])*tx
				b := float64(src.Pix[src.PixOffset(x0, y1)+c])*(1-tx) + float64(src.Pix[src.PixOffset(x1, y1)+c])*tx
				out.Pix[out.PixOffset(x, y)+c] = uint8(a*(1-ty) + b*ty + 0.5)
			}
		}
	}
	return out
}

func TestAutoLearnScalesAndRefs(t *testing.T) {
	full := loadPNG(t, "testdata/chesscom_full.png")
	crop := image.NewRGBA(image.Rect(0, 0, 830, 821))
	draw.Draw(crop, crop.Rect, full, image.Point{}, draw.Src)
	want, _ := chess.ParseFEN("rnbqkbnr/pppp1p1p/4p1p1/7Q/4P3/8/PPPP1PPP/RNB1KBNR w KQkq - 0 4")
	for _, refs := range []struct {
		name string
		sets []RefSet
	}{{"glyph-only", []RefSet{glyphRefSet()}}, {"default", nil}} {
		for _, f := range []float64{0.5, 0.75, 1, 1.3} {
			im := resizeRGBA(crop, f)
			t0 := time.Now()
			rect, conf := DetectBoard(im)
			res, err := AutoLearn(im, rect, refs.sets)
			diff := -1
			if err == nil {
				diff = 0
				for sq := 0; sq < 64; sq++ {
					if res.Placement[sq] != want.Board[sq] {
						diff++
					}
				}
			}
			t.Logf("%s x%.2f: board %v (%.2f) diff=%d err=%v %v [%v]", refs.name, f, rect, conf, diff, err, res, time.Since(t0))
			if diff != 0 {
				t.Errorf("%s x%.2f wrong", refs.name, f)
			}
		}
	}
}

func TestAutoLearnDebug13(t *testing.T) {
	full := loadPNG(t, "testdata/chesscom_full.png")
	crop := image.NewRGBA(image.Rect(0, 0, 830, 821))
	draw.Draw(crop, crop.Rect, full, image.Point{}, draw.Src)
	im := resizeRGBA(crop, 1.3)
	rect, _ := DetectBoard(im)
	autoDebug = func(m string) { t.Log(m) }
	defer func() { autoDebug = nil }()
	want, _ := chess.ParseFEN("rnbqkbnr/pppp1p1p/4p1p1/7Q/4P3/8/PPPP1PPP/RNB1KBNR w KQkq - 0 4")
	res, err := AutoLearn(im, rect, nil)
	t.Log(res, err)
	for sq := 0; sq < 64; sq++ {
		if res.Placement[sq] != want.Board[sq] {
			t.Logf("%s got %d want %d", chess.SqName(sq), res.Placement[sq], want.Board[sq])
		}
	}
}

func TestRefDistAtScales(t *testing.T) {
	full := loadPNG(t, "testdata/chesscom_full.png")
	crop := image.NewRGBA(image.Rect(0, 0, 830, 821))
	draw.Draw(crop, crop.Rect, full, image.Point{}, draw.Src)
	var ref RefSet
	for _, r := range DefaultRefs() {
		if r.Name == "chesscom-neo" {
			ref = r
		}
	}
	for _, f := range []float64{1, 1.3} {
		im := resizeRGBA(crop, f)
		rect, _ := DetectBoard(im)
		// pawn on a2 (col 0,row 6), knight b1 (col1,row7), bishop c1
		for _, sq := range [][3]int{{0, 6, 0}, {1, 7, 1}, {2, 7, 2}, {0, 7, 3}, {7, 3, 4}, {4, 7, 5}} {
			r := squareRect(rect, sq[0], sq[1])
			bg, _, _ := bgSample(im, r)
			o, ok := segmentSquare(im, r, bg)
			g := o.toGrid(1)
			t.Logf("x%.1f %v ok=%v area=%.3f bh=%.2f cx=%.2f d(correct)=%.3f", f, sq, ok, o.area, o.bh, o.cx, silDist(&g, &ref.Sil[sq[2]]))
		}
	}
}

func TestIntraInter(t *testing.T) {
	full := loadPNG(t, "testdata/chesscom_full.png")
	crop := image.NewRGBA(image.Rect(0, 0, 830, 821))
	draw.Draw(crop, crop.Rect, full, image.Point{}, draw.Src)
	want, _ := chess.ParseFEN("rnbqkbnr/pppp1p1p/4p1p1/7Q/4P3/8/PPPP1PPP/RNB1KBNR w KQkq - 0 4")
	for _, f := range []float64{0.5, 0.75, 1, 1.3} {
		im := resizeRGBA(crop, f)
		rect, _ := DetectBoard(im)
		type o2 struct {
			g  [sGrid * sGrid]float32
			tp chess.PieceType
			sq int
		}
		var all []o2
		for row := 0; row < 8; row++ {
			for col := 0; col < 8; col++ {
				pc := want.Board[gridToSquare(col, row, true)]
				if pc == chess.NoPiece {
					continue
				}
				r := squareRect(rect, col, row)
				bg, _, _ := bgSample(im, r)
				o, ok := segmentSquare(im, r, bg)
				if !ok {
					t.Fatalf("not occupied %d %d", col, row)
				}
				all = append(all, o2{o.toGrid(1), pc.Type(), gridToSquare(col, row, true)})
			}
		}
		maxIntra, minInter := 0.0, 9.0
		var wi, wj [2]int
		for i := range all {
			for j := i + 1; j < len(all); j++ {
				d := silDist(&all[i].g, &all[j].g)
				if all[i].tp == all[j].tp {
					if d > maxIntra {
						maxIntra, wi = d, [2]int{all[i].sq, all[j].sq}
					}
				} else if d < minInter {
					minInter, wj = d, [2]int{all[i].sq, all[j].sq}
				}
			}
		}
		t.Logf("x%.2f: max intra %.3f (%s,%s) min inter %.3f (%s,%s)", f, maxIntra, chess.SqName(wi[0]), chess.SqName(wi[1]), minInter, chess.SqName(wj[0]), chess.SqName(wj[1]))
	}
}

func TestSymmetryStats(t *testing.T) {
	full := loadPNG(t, "testdata/chesscom_full.png")
	crop := image.NewRGBA(image.Rect(0, 0, 830, 821))
	draw.Draw(crop, crop.Rect, full, image.Point{}, draw.Src)
	want, _ := chess.ParseFEN("rnbqkbnr/pppp1p1p/4p1p1/7Q/4P3/8/PPPP1PPP/RNB1KBNR w KQkq - 0 4")
	for _, f := range []float64{0.5, 1} {
		im := resizeRGBA(crop, f)
		rect, _ := DetectBoard(im)
		sum := map[chess.PieceType][]float64{}
		for row := 0; row < 8; row++ {
			for col := 0; col < 8; col++ {
				pc := want.Board[gridToSquare(col, row, true)]
				if pc == chess.NoPiece {
					continue
				}
				r := squareRect(rect, col, row)
				bg, _, _ := bgSample(im, r)
				o, _ := segmentSquare(im, r, bg)
				sum[pc.Type()] = append(sum[pc.Type()], o.symmetry())
			}
		}
		for tp := chess.Pawn; tp <= chess.King; tp++ {
			lo, hi := 2.0, -1.0
			for _, v := range sum[tp] {
				lo, hi = math.Min(lo, v), math.Max(hi, v)
			}
			t.Logf("x%.1f type %d symmetry %.2f..%.2f", f, tp, lo, hi)
		}
	}
}
