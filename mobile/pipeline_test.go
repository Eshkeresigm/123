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

// ovHost records the arrow picture like the Android view would show it.
type ovHost struct {
	mu   sync.Mutex
	rgba []byte
	r    image.Rectangle
}

func (h *ovHost) SetSelfVisible(bool) {}
func (h *ovHost) ShowOverlay(x, y, w, hh int, rgba []byte) {
	h.mu.Lock()
	h.rgba, h.r = append([]byte(nil), rgba...), image.Rect(x, y, x+w, y+hh)
	h.mu.Unlock()
}
func (h *ovHost) HideOverlay() { h.mu.Lock(); h.rgba, h.r = nil, image.Rectangle{}; h.mu.Unlock() }

// androidFrame imitates what MediaProjection delivers: the screen at 1080 px
// width with the arrow window stretched to screen size, then scaled down to
// 720 px by the compositor (bilinear).
func androidFrame(base *image.RGBA, host *ovHost) *image.RGBA {
	const k = 1.5 // screen px per frame px
	scr := image.NewRGBA(base.Rect)
	copy(scr.Pix, base.Pix)
	host.mu.Lock()
	rgba, r := host.rgba, host.r
	host.mu.Unlock()
	if rgba != nil {
		w, h := r.Dx(), r.Dy()
		sx0, sy0 := int(float64(r.Min.X)*k), int(float64(r.Min.Y)*k)
		sw, sh := int(float64(w)*k), int(float64(h)*k)
		at := func(x, y int) (float64, float64, float64, float64) {
			x, y = max(0, min(w-1, x)), max(0, min(h-1, y))
			i := (y*w + x) * 4
			return float64(rgba[i]), float64(rgba[i+1]), float64(rgba[i+2]), float64(rgba[i+3])
		}
		for y := 0; y < sh; y++ {
			for x := 0; x < sw; x++ {
				fx, fy := (float64(x)+0.5)/k-0.5, (float64(y)+0.5)/k-0.5
				x0, y0 := int(fx+1)-1, int(fy+1)-1
				tx, ty := fx-float64(x0), fy-float64(y0)
				var c [4]float64
				for j, p := range [4][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
					wt := (1 - tx + float64(p[0])*(2*tx-1)) * (1 - ty + float64(p[1])*(2*ty-1))
					a, b, cc, d := at(x0+p[0], y0+p[1])
					c[0] += a * wt
					c[1] += b * wt
					c[2] += cc * wt
					c[3] += d * wt
					_ = j
				}
				o := scr.PixOffset(sx0+x, sy0+y)
				if o < 0 || o+3 >= len(scr.Pix) {
					continue
				}
				for q := 0; q < 3; q++ {
					v := c[q] + float64(scr.Pix[o+q])*(1-c[3]/255)
					scr.Pix[o+q] = uint8(max(0, min(255, v+0.5)))
				}
			}
		}
	}
	// compositor downscale 1080 -> 720
	out := image.NewRGBA(image.Rect(0, 0, int(float64(base.Rect.Dx())/k), int(float64(base.Rect.Dy())/k)))
	for y := 0; y < out.Rect.Dy(); y++ {
		for x := 0; x < out.Rect.Dx(); x++ {
			fx, fy := (float64(x)+0.5)*k-0.5, (float64(y)+0.5)*k-0.5
			x0, y0 := int(fx), int(fy)
			tx, ty := fx-float64(x0), fy-float64(y0)
			x1, y1 := min(x0+1, scr.Rect.Dx()-1), min(y0+1, scr.Rect.Dy()-1)
			for q := 0; q < 4; q++ {
				a := float64(scr.Pix[scr.PixOffset(x0, y0)+q])*(1-tx) + float64(scr.Pix[scr.PixOffset(x1, y0)+q])*tx
				b := float64(scr.Pix[scr.PixOffset(x0, y1)+q])*(1-tx) + float64(scr.Pix[scr.PixOffset(x1, y1)+q])*tx
				out.Pix[out.PixOffset(x, y)+q] = uint8(a*(1-ty) + b*ty + 0.5)
			}
		}
	}
	return out
}

func TestArrowOverBoardKeepsReadingStable(t *testing.T) {
	host := &ovHost{}
	s := NewSession(host)
	defer s.Close()
	pos := chess.Start()
	for _, mv := range []string{"e2e4", "e7e5", "g1f3", "b8c6"} {
		m, _ := pos.ParseUCI(mv)
		pos, _ = pos.Make(m)
	}
	render := func(p chess.Position, last chess.Move) *image.RGBA {
		img, _ := boardimg.Render(boardimg.Brown, p.Board, boardimg.Options{Square: 135, WhiteAtBottom: true, LastMove: last})
		scr := image.NewRGBA(image.Rect(0, 0, 1080, 2340))
		for i := 0; i < len(scr.Pix); i += 4 {
			scr.Pix[i], scr.Pix[i+1], scr.Pix[i+2], scr.Pix[i+3] = 38, 36, 34, 255
		}
		draw.Draw(scr, img.Rect.Add(image.Pt(0, 500)), img, image.Point{}, draw.Src)
		return scr
	}
	var mu sync.Mutex
	base := render(pos, chess.NoMove)
	stop := make(chan struct{})
	go func() { // the "ImageReader": a frame every 200 ms
		tk := time.NewTicker(200 * time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tk.C:
				mu.Lock()
				b := base
				mu.Unlock()
				f := androidFrame(b, host)
				s.PushFrame(f.Rect.Dx(), f.Rect.Dy(), f.Stride, f.Pix)
			}
		}
	}()
	defer close(stop)
	s.Toggle()
	wait := func(cond func(st state) bool, what string) state {
		var st state
		deadline := time.Now().Add(60 * time.Second)
		for time.Now().Before(deadline) {
			time.Sleep(150 * time.Millisecond)
			_ = json.Unmarshal([]byte(s.State()), &st)
			if cond(st) {
				return st
			}
		}
		t.Fatalf("timeout waiting for %s: %+v", what, st)
		return st
	}
	st := wait(func(st state) bool { return st.Running && st.Best != "" }, "first best move")
	for round := 0; round < 4; round++ {
		m, err := pos.ParseUCI(st.Best)
		if err != nil {
			t.Fatalf("round %d: illegal best %q in\n%v", round, st.Best, pos.FEN())
		}
		pos, _ = pos.Make(m)
		reply := chess.NoMove
		for _, c := range []string{"d7d6", "g8f6", "f8e7", "a7a6", "h7h6", "c8e6", "e8g8", "b7b6"} {
			if r, err := pos.ParseUCI(c); err == nil {
				reply = r
				break
			}
		}
		if reply == chess.NoMove {
			t.Skip("no scripted reply")
		}
		pos, _ = pos.Make(reply)
		mu.Lock()
		base = render(pos, reply)
		mu.Unlock()
		// wait for a best move that belongs to the new position (legal there)
		st = wait(func(st state) bool {
			if st.Best == "" {
				return false
			}
			_, err := pos.ParseUCI(st.Best)
			return err == nil
		}, "best move after round")
		time.Sleep(2 * time.Second) // arrow is on screen now: it must not disturb the reading
		_ = json.Unmarshal([]byte(s.State()), &st)
		if _, err := pos.ParseUCI(st.Best); err != nil {
			t.Fatalf("round %d: best %q became illegal while the arrow was shown (status %q)", round, st.Best, st.Status)
		}
		t.Logf("round %d ok: best=%s status=%q", round, st.Best, st.Status)
	}
}
