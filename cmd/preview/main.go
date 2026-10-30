// preview renders the interface in several states to PNG files so the design
// can be inspected without Windows.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"time"

	"chessvision/internal/app"
	"chessvision/internal/boardimg"
	"chessvision/internal/chess"
	"chessvision/internal/gfx"
	"chessvision/internal/ui"
)

type fake struct {
	mu     sync.Mutex
	screen *image.RGBA
	region image.Rectangle
	clip   string
}

func (f *fake) Capture(r image.Rectangle) (*image.RGBA, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(out, out.Rect, f.screen, r.Min, draw.Src)
	return out, nil
}
func (f *fake) PickRegion() (image.Rectangle, bool, error)   { return f.region, true, nil }
func (f *fake) GetClipboard() (string, error)                { return f.clip, nil }
func (f *fake) SetClipboard(s string) error                  { f.clip = s; return nil }
func (f *fake) ShowOverlay(r image.Rectangle, i *image.RGBA) {}
func (f *fake) HideOverlay()                                 {}
func (f *fake) show(b [64]chess.Piece, last chess.Move, white bool) {
	img, rect := boardimg.Render(boardimg.Green, b, boardimg.Options{Square: 62, Margin: 14, WhiteAtBottom: white, LastMove: last})
	f.mu.Lock()
	f.screen = image.NewRGBA(image.Rect(0, 0, 900, 700))
	draw.Draw(f.screen, img.Rect.Add(image.Pt(200, 80)), img, image.Point{}, draw.Src)
	f.region = image.Rect(190+rect.Min.X, 70+rect.Min.Y, 210+rect.Max.X, 90+rect.Max.Y)
	f.mu.Unlock()
}

func render(u *ui.UI, scale float64, path string) {
	u.SetScale(scale)
	w, h := u.PixelSize()
	c := gfx.NewCanvas(w, h)
	for i := 0; i < 40; i++ { // let easing and the arrow animation settle
		u.Render(c)
		time.Sleep(20 * time.Millisecond)
	}
	f, _ := os.Create(path)
	defer f.Close()
	png.Encode(f, c.Img)
	fmt.Println("wrote", path)
}

func main() {
	out := flag.String("out", ".", "output dir")
	flag.Parse()
	fp := &fake{}
	start := chess.Start()
	fp.show(start.Board, chess.NoMove, true)
	dir, _ := os.MkdirTemp("", "cv")
	ctl := app.New(fp, dir, dir, "")
	u := ui.New(ctl)

	render(u, 1, filepath.Join(*out, "ui1_empty.png"))

	ctl.SelectRegion()
	render(u, 1, filepath.Join(*out, "ui2_region.png"))
	ctl.Calibrate()
	ctl.SetMoveTime(600)
	ctl.Start()
	ctl.ToggleRun()
	// play a few moves
	pos := start
	for _, mv := range []string{"e2e4", "e7e5", "g1f3", "b8c6", "f1c4", "g8f6"} {
		m, _ := pos.ParseUCI(mv)
		pos, _ = pos.Make(m)
		fp.show(pos.Board, m, true)
		time.Sleep(450 * time.Millisecond)
	}
	for i := 0; i < 80; i++ {
		s := ctl.Snapshot()
		if s.Best != chess.NoMove && !s.Analyzing {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	render(u, 1, filepath.Join(*out, "ui3_running.png"))
	render(u, 1.5, filepath.Join(*out, "ui4_running_150.png"))
	render(u, 0.9833, filepath.Join(*out, "ui5_scaled.png"))
	ctl.SetWhiteAtBottom(false)
	ctl.Close()
}
