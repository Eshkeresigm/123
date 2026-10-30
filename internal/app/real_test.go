package app

import (
	"image"
	"image/draw"
	"image/png"
	"os"
	"testing"

	"chessvision/internal/chess"
)

// realPlatform serves a real screenshot (chess.com, middlegame, our own
// window visible on the right).
type realPlatform struct {
	fakePlatform
	own []image.Rectangle
}

func (r *realPlatform) OwnRects() []image.Rectangle   { return r.own }
func (r *realPlatform) ScreenBounds() image.Rectangle { return r.screen.Rect }

func TestRealScreenshotEndToEnd(t *testing.T) {
	f, err := os.Open("../vision/testdata/chesscom_full.png")
	if err != nil {
		t.Fatal(err)
	}
	im, err := png.Decode(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	rp := &realPlatform{}
	rp.screen = image.NewRGBA(im.Bounds())
	draw.Draw(rp.screen, rp.screen.Rect, im, im.Bounds().Min, draw.Src)
	rp.own = []image.Rectangle{image.Rect(866, 0, 1466, 821)}

	c := New(rp, t.TempDir(), t.TempDir(), "")
	c.scanEvery = 40 * 1e6
	c.SetMoveTime(300)
	c.Start()
	defer c.Close()
	c.ToggleRun() // no region, no initial position, no FEN: everything automatic
	m := c.Snapshot()
	if !m.Running || !m.Calibrated {
		t.Fatalf("auto setup failed: %q", m.Status)
	}
	want, _ := chess.ParseFEN("rnbqkbnr/pppp1p1p/4p1p1/7Q/4P3/8/PPPP1PPP/RNB1KBNR w KQkq - 0 4")
	if m.Pos.Board != want.Board || !m.Cfg.WhiteAtBottom {
		t.Fatalf("wrong position: %s (status %q)", m.Pos.FEN(), m.Status)
	}
	if m.Pos.Side != chess.White {
		t.Fatalf("side to move should be white (black just played g6): %v", m.Pos.Side)
	}
	br := m.Cfg.BoardRect
	t.Logf("board rect %v, status %q", br, m.Status)
	if br[0] < 4 || br[0] > 12 || br[2] < 810 || br[2] > 820 {
		t.Fatalf("board rectangle off: %v", br)
	}
	m = waitFor(t, "best move", func(m Model) bool { return m.Best != chess.NoMove }, c)
	t.Logf("best move %s (%s)", m.Best, m.BestSAN)
	// the scanner must keep reading the same position from the live screen
	for i := 0; i < 8; i++ {
		waitFor(t, "scan", func(mm Model) bool { return mm.Scans > m.Scans+i }, c)
	}
	m2 := c.Snapshot()
	if m2.Board != want.Board {
		t.Fatalf("live recognition drifted: %v", m2.Board)
	}
}

func loadScreen(t *testing.T, path string, own image.Rectangle) *realPlatform {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	im, err := png.Decode(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	rp := &realPlatform{}
	rp.screen = image.NewRGBA(im.Bounds())
	draw.Draw(rp.screen, rp.screen.Rect, im, im.Bounds().Min, draw.Src)
	rp.own = []image.Rectangle{own}
	return rp
}

// A mid-game board seen from Black's side, with a white arrow drawn over it.
func TestRealScreenshotBlackSide(t *testing.T) {
	rp := loadScreen(t, "../vision/testdata/chesscom_black.png", image.Rect(855, 0, 1467, 821))
	c := New(rp, t.TempDir(), t.TempDir(), "")
	c.scanEvery = 40 * 1e6
	c.SetMoveTime(300)
	c.Start()
	defer c.Close()
	c.ToggleRun()
	m := c.Snapshot()
	t.Logf("status=%q running=%v calibrated=%v white=%v rect=%v", m.Status, m.Running, m.Calibrated, m.Cfg.WhiteAtBottom, m.Cfg.BoardRect)
	t.Logf("FEN: %s", m.Pos.FEN())
	want, _ := chess.ParseFEN("r1bqkbnr/pppp1ppp/2n5/6N1/3pP3/8/PPP2PPP/RNBQKB1R b KQkq - 0 4")
	// the screenshot has a white arrow baked in on d7-d5; at most the squares
	// under it may be misread
	for sq := 0; sq < 64; sq++ {
		if m.Pos.Board[sq] != want.Board[sq] && chess.File(sq) != 3 {
			t.Errorf("square %s: got %v want %v", chess.SqName(sq), m.Pos.Board[sq], want.Board[sq])
		}
	}
	r := m.Cfg.BoardRect
	if !m.Running || !m.Calibrated || m.Cfg.WhiteAtBottom || abs2(r[0]-16) > 3 || abs2(r[1]-7) > 3 || abs2(r[2]-824) > 3 || abs2(r[3]-815) > 3 {
		t.Fatalf("wrong setup: %q rect=%v white=%v", m.Status, r, m.Cfg.WhiteAtBottom)
	}
}
