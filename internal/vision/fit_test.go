package vision

import (
	"image"
	"image/draw"
	"testing"
	"time"

	"chessvision/internal/boardimg"
	"chessvision/internal/chess"
)

func TestFitBoardSpeedAccuracy(t *testing.T) {
	cases := []struct {
		style  boardimg.Style
		square int
		white  bool
		pad    int
		panel  bool
	}{
		{boardimg.Green, 62, true, 0, false},
		{boardimg.Green, 62, true, 90, true},
		{boardimg.Brown, 48, false, 60, true},
		{boardimg.Brown, 90, true, 140, true},
		{boardimg.Green, 40, false, 50, true},
		{boardimg.Green, 75, true, 200, true},
	}
	for _, k := range cases {
		start := chess.Start()
		img, rect := boardimg.Render(k.style, start.Board, boardimg.Options{Square: k.square, Margin: 14, WhiteAtBottom: k.white})
		scr := image.NewRGBA(image.Rect(0, 0, img.Rect.Dx()+700, img.Rect.Dy()+500))
		for i := 0; i < len(scr.Pix); i += 4 {
			scr.Pix[i], scr.Pix[i+1], scr.Pix[i+2], scr.Pix[i+3] = 38, 36, 33, 255
		}
		off := image.Pt(240, 120)
		draw.Draw(scr, img.Rect.Add(off), img, image.Point{}, draw.Src)
		if k.panel {
			px := off.X + img.Rect.Dx() + 10
			for y := off.Y; y < off.Y+img.Rect.Dy(); y++ {
				for x := px; x < px+260 && x < scr.Rect.Max.X; x++ {
					v := uint8(230)
					if (y/9)%2 == 0 && x%7 < 4 {
						v = 90
					}
					i := scr.PixOffset(x, y)
					scr.Pix[i], scr.Pix[i+1], scr.Pix[i+2] = v, v, v
				}
			}
		}
		br := rect.Add(off)
		extra := 0
		if k.panel {
			extra = 200
		}
		reg := image.Rect(br.Min.X-k.pad, br.Min.Y-k.pad, br.Max.X+k.pad+extra, br.Max.Y+k.pad).Intersect(scr.Rect)
		crop := image.NewRGBA(image.Rect(0, 0, reg.Dx(), reg.Dy()))
		draw.Draw(crop, crop.Rect, scr, reg.Min, draw.Src)
		t0 := time.Now()
		got, agree := FitBoard(crop, k.white, start.Board)
		want := br.Sub(reg.Min)
		t.Logf("sq=%d pad=%d: got %v want %v agree=%.2f in %v", k.square, k.pad, got, want, agree, time.Since(t0))
		if agree < 0.95 {
			t.Errorf("agreement too low")
		}
		tol := k.square / 10
		d := func(a, b int) int {
			if a > b {
				return a - b
			}
			return b - a
		}
		if d(got.Min.X, want.Min.X) > tol || d(got.Min.Y, want.Min.Y) > tol || d(got.Max.X, want.Max.X) > tol || d(got.Max.Y, want.Max.Y) > tol {
			t.Errorf("rect off")
		}
		if time.Since(t0) > 4*time.Second {
			t.Errorf("too slow")
		}
	}
}
