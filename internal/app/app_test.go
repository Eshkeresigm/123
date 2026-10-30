package app

import (
	"image"
	"image/draw"
	"sync"
	"testing"
	"time"

	"chessvision/internal/boardimg"
	"chessvision/internal/chess"
)

// fakePlatform emulates a desktop showing a chess website.
type fakePlatform struct {
	mu        sync.Mutex
	screen    *image.RGBA
	region    image.Rectangle
	clip      string
	ovRect    image.Rectangle
	ovImg     *image.RGBA
	ovShown   int
	captureOK bool
}

func (f *fakePlatform) Capture(r image.Rectangle) (*image.RGBA, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	scr := image.NewRGBA(f.screen.Rect)
	draw.Draw(scr, scr.Rect, f.screen, image.Point{}, draw.Src)
	if f.ovImg != nil {
		// premultiplied "over" compositing like the OS does
		ov := f.ovImg
		for y := 0; y < ov.Rect.Dy(); y++ {
			for x := 0; x < ov.Rect.Dx(); x++ {
				oi := y*ov.Stride + x*4
				a := int(ov.Pix[oi+3])
				if a == 0 {
					continue
				}
				si := scr.PixOffset(f.ovRect.Min.X+x, f.ovRect.Min.Y+y)
				for k := 0; k < 3; k++ {
					scr.Pix[si+k] = uint8(int(ov.Pix[oi+k]) + int(scr.Pix[si+k])*(255-a)/255)
				}
			}
		}
	}
	out := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(out, out.Rect, scr, r.Min, draw.Src)
	return out, nil
}
func (f *fakePlatform) PickRegion() (image.Rectangle, bool, error) { return f.region, true, nil }
func (f *fakePlatform) GetClipboard() (string, error)              { return f.clip, nil }
func (f *fakePlatform) SetClipboard(s string) error                { f.clip = s; return nil }
func (f *fakePlatform) ShowOverlay(r image.Rectangle, img *image.RGBA) {
	f.mu.Lock()
	f.ovRect, f.ovImg = r, img
	f.ovShown++
	f.mu.Unlock()
}
func (f *fakePlatform) HideOverlay() {
	f.mu.Lock()
	f.ovImg = nil
	f.mu.Unlock()
}

func (f *fakePlatform) show(board [64]chess.Piece, last chess.Move, white bool) {
	img, rect := boardimg.Render(boardimg.Green, board, boardimg.Options{Square: 62, Margin: 14, WhiteAtBottom: white, LastMove: last})
	f.mu.Lock()
	defer f.mu.Unlock()
	f.screen = image.NewRGBA(image.Rect(0, 0, 900, 700))
	for i := 0; i < len(f.screen.Pix); i += 4 {
		f.screen.Pix[i], f.screen.Pix[i+1], f.screen.Pix[i+2], f.screen.Pix[i+3] = 38, 36, 33, 255
	}
	off := image.Pt(200, 80)
	draw.Draw(f.screen, img.Rect.Add(off), img, image.Point{}, draw.Src)
	f.region = image.Rect(200+rect.Min.X-10, 80+rect.Min.Y-10, 200+rect.Max.X+10, 80+rect.Max.Y+10)
}

func waitFor(t *testing.T, what string, cond func(Model) bool, c *Controller) Model {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		m := c.Snapshot()
		if cond(m) {
			return m
		}
		time.Sleep(30 * time.Millisecond)
	}
	m := c.Snapshot()
	t.Fatalf("timeout waiting for %s; status=%q running=%v pos=%v best=%v scans=%d", what, m.Status, m.Running, m.Pos.FEN(), m.Best, m.Scans)
	return m
}

func TestEndToEnd(t *testing.T) {
	for _, white := range []bool{true, false} {
		name := "white"
		if !white {
			name = "black"
		}
		t.Run(name, func(t *testing.T) { runEndToEnd(t, white) })
	}
}

func runEndToEnd(t *testing.T, white bool) {
	fp := &fakePlatform{}
	start := chess.Start()
	fp.show(start.Board, chess.NoMove, white)
	c := New(fp, t.TempDir(), t.TempDir(), "")
	c.scanEvery = 40 * time.Millisecond
	c.Start()
	defer c.Close()
	c.SetMoveTime(250)
	c.SetOverlay(true)

	c.SelectRegion()
	m := c.Snapshot()
	if !m.Cfg.HasRegion || !m.Cfg.HasBoard {
		t.Fatalf("region not set: %q", m.Status)
	}
	if m.Calibrated {
		t.Fatal("must not be calibrated before learning")
	}
	// one press does everything: finds the board, learns the pieces, starts
	c.ToggleRun()
	m = c.Snapshot()
	if !m.Running || !m.Calibrated || m.Cfg.WhiteAtBottom != white {
		t.Fatalf("auto setup failed (white=%v): %q learned=%d running=%v", white, m.Status, m.Learned, m.Running)
	}
	// user's turn if user is white; otherwise the engine waits for the opponent
	pos := start
	play := func(uci string) {
		mv, err := pos.ParseUCI(uci)
		if err != nil {
			t.Fatal(err)
		}
		pos, _ = pos.Make(mv)
		fp.show(pos.Board, mv, white)
	}
	if white {
		m = waitFor(t, "first best move", func(m Model) bool { return m.Best != chess.NoMove }, c)
		if !start.IsLegal(m.Best) {
			t.Fatalf("illegal best %s", m.Best)
		}
		// play the suggested move, then an opponent reply
		pos2, _ := pos.Make(m.Best)
		pos = pos2
		fp.show(pos.Board, m.Best, white)
		waitFor(t, "position update", func(m Model) bool { return m.HavePos && m.Pos.Board == pos.Board }, c)
		play("e7e5")
		m = waitFor(t, "second best move", func(m Model) bool { return m.Pos.Board == pos.Board && m.Best != chess.NoMove && !m.Analyzing }, c)
		if !pos.IsLegal(m.Best) {
			t.Fatalf("illegal best %s in %s", m.Best, pos.FEN())
		}
		if m.BestSAN == "" || len(m.History) == 0 {
			t.Fatalf("missing SAN/history: %+v", m.History)
		}
		// overlay must have been shown and must not disturb recognition
		fp.mu.Lock()
		shown := fp.ovShown
		fp.mu.Unlock()
		if shown == 0 {
			t.Fatal("overlay never shown")
		}
		time.Sleep(400 * time.Millisecond)
		m = c.Snapshot()
		if m.Pos.Board != pos.Board {
			t.Fatalf("overlay corrupted recognition: %s vs %s", m.Pos.FEN(), pos.FEN())
		}
	} else {
		// black user: white (opponent) moves first; engine should answer afterwards
		play("e2e4")
		m = waitFor(t, "best move for black", func(m Model) bool { return m.Pos.Board == pos.Board && m.Best != chess.NoMove && !m.Analyzing }, c)
		if !pos.IsLegal(m.Best) || pos.Side != chess.Black {
			t.Fatalf("bad answer %s in %s", m.Best, pos.FEN())
		}
	}
	c.CopyFEN()
	if fp.clip == "" {
		t.Error("FEN not copied")
	}
	// pause hides overlay and stops analysis
	c.ToggleRun()
	if c.Snapshot().Running {
		t.Error("still running after pause")
	}
}

func TestCalibrateMidGameAuto(t *testing.T) {
	fp := &fakePlatform{}
	p, _ := chess.ParseFEN("r1bqkbnr/pppp1ppp/2n5/4p3/4P3/5N2/PPPP1PPP/RNBQKB1R b KQkq - 2 3")
	fp.show(p.Board, chess.NoMove, true)
	c := New(fp, t.TempDir(), t.TempDir(), "")
	c.SelectRegion()
	c.Calibrate() // not the initial position: pieces are named by shape
	m := c.Snapshot()
	if !m.Calibrated {
		t.Fatalf("mid-game auto calibration failed: %q (learned %d)", m.Status, m.Learned)
	}
	for sq := 0; sq < 64; sq++ {
		if m.Pos.Board[sq] != p.Board[sq] {
			t.Fatalf("auto position differs at %s: got %d want %d", chess.SqName(sq), m.Pos.Board[sq], p.Board[sq])
		}
	}
	// FEN calibration still works as a manual override
	full, _ := chess.ParseFEN("r1bqkb1r/pppp1ppp/2n2n2/4p3/2B1P3/5N2/PPPP1PPP/RNBQK2R w KQkq - 4 4")
	fp.show(full.Board, chess.NoMove, true)
	c.SelectRegion()
	fp.clip = full.FEN()
	c.CalibrateFEN()
	m = c.Snapshot()
	if !m.Calibrated {
		t.Fatalf("FEN calibration failed: %q (learned %d)", m.Status, m.Learned)
	}
	c.SetMoveTime(200)
	c.ToggleRun()
	waitFor(t, "analysis after FEN calibration", func(m Model) bool { return m.Best != chess.NoMove }, c)
	c.Close()
}

// With no region selected at all the program must find the board itself.
func TestAutoFindsBoardWithoutRegion(t *testing.T) {
	fp := &fakePlatform{}
	p, _ := chess.ParseFEN("r1bqkb1r/pppp1ppp/2n2n2/4p3/2B1P3/5N2/PPPP1PPP/RNBQK2R w KQkq - 4 4")
	fp.show(p.Board, chess.NoMove, false)
	c := New(fp, t.TempDir(), t.TempDir(), "")
	c.SetMoveTime(200)
	c.ToggleRun() // straight from a fresh start
	m := c.Snapshot()
	if !m.Running || !m.Calibrated || m.Cfg.WhiteAtBottom {
		t.Fatalf("no automatic setup: %q running=%v calibrated=%v white=%v", m.Status, m.Running, m.Calibrated, m.Cfg.WhiteAtBottom)
	}
	c.Start()
	defer c.Close()
	waitFor(t, "analysis", func(m Model) bool { return m.Best != chess.NoMove }, c)
}

// A loosely selected area (extra margins, side panel, different sizes and
// styles) must still calibrate on the initial position.
func TestCalibrateLooseRegion(t *testing.T) {
	type tc struct {
		name   string
		style  boardimg.Style
		square int
		white  bool
		pad    int
		panel  bool
	}
	cases := []tc{
		{"green-62-tight", boardimg.Green, 62, true, 0, false},
		{"green-62-loose", boardimg.Green, 62, true, 90, true},
		{"brown-48-loose-black", boardimg.Brown, 48, false, 60, true},
		{"brown-90-loose", boardimg.Brown, 90, true, 140, true},
		{"green-40-loose-black", boardimg.Green, 40, false, 50, true},
		{"green-75-offcenter", boardimg.Green, 75, true, 200, true},
	}
	for _, k := range cases {
		t.Run(k.name, func(t *testing.T) {
			fp := &fakePlatform{}
			start := chess.Start()
			img, rect := boardimg.Render(k.style, start.Board, boardimg.Options{Square: k.square, Margin: 14, WhiteAtBottom: k.white})
			fp.screen = image.NewRGBA(image.Rect(0, 0, img.Rect.Dx()+700, img.Rect.Dy()+500))
			for i := 0; i < len(fp.screen.Pix); i += 4 {
				fp.screen.Pix[i], fp.screen.Pix[i+1], fp.screen.Pix[i+2], fp.screen.Pix[i+3] = 38, 36, 33, 255
			}
			off := image.Pt(240, 120)
			draw.Draw(fp.screen, img.Rect.Add(off), img, image.Point{}, draw.Src)
			if k.panel {
				// bright panel with "text" lines to the right of the board
				px := off.X + img.Rect.Dx() + 10
				for y := off.Y; y < off.Y+img.Rect.Dy() && y < fp.screen.Rect.Max.Y; y++ {
					for x := px; x < px+260 && x < fp.screen.Rect.Max.X; x++ {
						v := uint8(230)
						if (y/9)%2 == 0 && x%7 < 4 {
							v = 90
						}
						i := fp.screen.PixOffset(x, y)
						fp.screen.Pix[i], fp.screen.Pix[i+1], fp.screen.Pix[i+2] = v, v, v
					}
				}
			}
			br := rect.Add(off)
			fp.region = image.Rect(br.Min.X-k.pad, br.Min.Y-k.pad, br.Max.X+k.pad+(map[bool]int{true: 200, false: 0}[k.panel]), br.Max.Y+k.pad).Intersect(fp.screen.Rect)
			c := New(fp, t.TempDir(), t.TempDir(), "")
			c.SelectRegion()
			c.Calibrate()
			m := c.Snapshot()
			if !m.Calibrated || m.Cfg.WhiteAtBottom != k.white {
				t.Fatalf("calibration failed: %q learned=%d white=%v", m.Status, m.Learned, m.Cfg.WhiteAtBottom)
			}
			got := image.Rect(m.Cfg.BoardRect[0], m.Cfg.BoardRect[1], m.Cfg.BoardRect[2], m.Cfg.BoardRect[3])
			d := func(a, b int) int {
				if a > b {
					return a - b
				}
				return b - a
			}
			tol := k.square / 8
			if d(got.Min.X, br.Min.X) > tol || d(got.Min.Y, br.Min.Y) > tol || d(got.Max.X, br.Max.X) > tol || d(got.Max.Y, br.Max.Y) > tol {
				t.Fatalf("board rect off: got %v want %v", got, br)
			}
		})
	}
}
