package cvmobile

import (
	"encoding/json"
	"image"
	"image/draw"
	"sync"
	"testing"
	"time"

	"chessvision/internal/boardimg"
	"chessvision/internal/chess"
)

type fakeHost struct {
	mu       sync.Mutex
	overlays int
	hides    int
	lastRect image.Rectangle
}

func (h *fakeHost) SetSelfVisible(bool) {}
func (h *fakeHost) ShowOverlay(x, y, w, hh int, rgba []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.overlays++
	h.lastRect = image.Rect(x, y, x+w, y+hh)
	if len(rgba) != w*hh*4 {
		panic("bad overlay size")
	}
}
func (h *fakeHost) HideOverlay() { h.mu.Lock(); h.hides++; h.mu.Unlock() }

// phoneFrame builds a portrait "screen" with the board in the middle and
// returns the RGBA bytes with a padded row stride, as ImageReader delivers.
func phoneFrame(b [64]chess.Piece, white bool) (w, h, stride int, pix []byte, board image.Rectangle) {
	img, rect := boardimg.Render(boardimg.Green, b, boardimg.Options{Square: 58, Margin: 0, WhiteAtBottom: white})
	w, h = 540, 1000
	scr := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(scr.Pix); i += 4 {
		scr.Pix[i], scr.Pix[i+1], scr.Pix[i+2], scr.Pix[i+3] = 245, 245, 247, 255
	}
	off := image.Pt(36, 260)
	draw.Draw(scr, img.Rect.Add(off), img, image.Point{}, draw.Src)
	stride = w*4 + 64
	pix = make([]byte, stride*h)
	for y := 0; y < h; y++ {
		copy(pix[y*stride:], scr.Pix[y*scr.Stride:y*scr.Stride+w*4])
	}
	return w, h, stride, pix, rect.Add(off)
}

func TestSessionEndToEnd(t *testing.T) {
	host := &fakeHost{}
	s := NewSession(host)
	defer s.Close()
	pos := chess.Start()
	for _, mv := range []string{"e2e4", "e7e5", "g1f3", "b8c6"} {
		m, _ := pos.ParseUCI(mv)
		pos, _ = pos.Make(m)
	}
	w, h, stride, pix, board := phoneFrame(pos.Board, true)
	s.PushFrame(w, h, stride, pix)
	s.SetPanelRect(0, 0, 540, 60)
	s.Toggle()
	var st state
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		if err := json.Unmarshal([]byte(s.State()), &st); err != nil {
			t.Fatal(err)
		}
		if st.Running && st.Best != "" {
			break
		}
	}
	t.Logf("state: %+v", st)
	if !st.Running || st.Best == "" {
		t.Fatalf("no best move: %+v", st)
	}
	if !st.White {
		t.Fatalf("side not white")
	}
	got := image.Rect(st.Board[0], st.Board[1], st.Board[2], st.Board[3])
	if d := got.Min.Sub(board.Min); abs(d.X) > 3 || abs(d.Y) > 3 || abs(got.Dx()-board.Dx()) > 6 {
		t.Fatalf("board rect %v, want %v", got, board)
	}
	if _, err := pos.ParseUCI(st.Best); err != nil {
		t.Fatalf("illegal best move %q: %v", st.Best, err)
	}
	time.Sleep(1500 * time.Millisecond)
	host.mu.Lock()
	defer host.mu.Unlock()
	if host.overlays == 0 {
		t.Fatal("overlay was never shown")
	}
	if !host.lastRect.In(image.Rect(0, 0, w, h)) {
		t.Fatalf("overlay rect %v outside frame", host.lastRect)
	}
}

func TestPushFrameRejectsBadInput(t *testing.T) {
	s := NewSession(&fakeHost{})
	defer s.Close()
	s.PushFrame(100, 100, 10, make([]byte, 10))
	s.PushFrame(100, 100, 400, make([]byte, 100))
	if img, _ := s.plat.frame(); img != nil {
		t.Fatal("bad frame accepted")
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
