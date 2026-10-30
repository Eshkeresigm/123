// Package cvmobile is the gomobile entry point of ChessVision: it wraps the
// desktop controller (board detection, piece recognition, move tracking and
// engine) behind a small API that the Android app drives. Frames come from
// MediaProjection; the arrow is drawn by the host.
package cvmobile

import (
	"encoding/json"
	"errors"
	"image"
	"image/draw"
	"sync"
	"time"

	"chessvision/internal/app"
)

// Host is implemented by the Android side.
type Host interface {
	// SetSelfVisible hides/shows our own floating windows so that a capture
	// shows the real board.
	SetSelfVisible(visible bool)
	// ShowOverlay draws a premultiplied RGBA picture (w*h*4 bytes) at the
	// given frame-pixel position; HideOverlay removes it.
	ShowOverlay(x, y, w, h int, rgba []byte)
	HideOverlay()
}

type framePlat struct {
	host Host

	mu         sync.Mutex
	img        *image.RGBA
	ts         time.Time // arrival time of img
	freshAfter time.Time // the next Capture wants a frame that arrived after this
	panel      image.Rectangle
}

func (p *framePlat) frame() (*image.RGBA, time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.img, p.ts
}

// Capture returns a copy of r from the newest frame.
func (p *framePlat) Capture(r image.Rectangle) (*image.RGBA, error) {
	p.mu.Lock()
	need := p.freshAfter
	p.freshAfter = time.Time{}
	p.mu.Unlock()
	deadline := time.Now().Add(900 * time.Millisecond)
	for {
		img, ts := p.frame()
		if img != nil && (need.IsZero() || ts.After(need) || time.Now().After(deadline)) {
			r = r.Intersect(img.Rect)
			if r.Empty() {
				return nil, errors.New("область вне кадра экрана")
			}
			out := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
			draw.Draw(out, out.Rect, img, r.Min, draw.Src)
			return out, nil
		}
		if time.Now().After(deadline) {
			return nil, errors.New("кадр экрана ещё не получен")
		}
		time.Sleep(15 * time.Millisecond)
	}
}

func (p *framePlat) PickRegion() (image.Rectangle, bool, error) {
	if img, _ := p.frame(); img != nil {
		return img.Rect, true, nil
	}
	return image.Rectangle{}, false, errors.New("кадр экрана ещё не получен")
}
func (p *framePlat) GetClipboard() (string, error) {
	return "", errors.New("нет буфера обмена")
}
func (p *framePlat) SetClipboard(string) error { return nil }

func (p *framePlat) ShowOverlay(r image.Rectangle, img *image.RGBA) {
	if img == nil || r.Empty() {
		return
	}
	w, h := img.Rect.Dx(), img.Rect.Dy()
	buf := make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		copy(buf[y*w*4:(y+1)*w*4], img.Pix[img.PixOffset(img.Rect.Min.X, img.Rect.Min.Y+y):])
	}
	p.host.ShowOverlay(r.Min.X, r.Min.Y, w, h, buf)
}
func (p *framePlat) HideOverlay() { p.host.HideOverlay() }

func (p *framePlat) ScreenBounds() image.Rectangle {
	if img, _ := p.frame(); img != nil {
		return img.Rect
	}
	return image.Rectangle{}
}

func (p *framePlat) OwnRects() []image.Rectangle {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.panel.Empty() {
		return nil
	}
	return []image.Rectangle{p.panel}
}

func (p *framePlat) SetSelfVisible(v bool) {
	if !v {
		// the screen only changes after the host removed our windows; ask for a
		// frame that shows that state
		p.mu.Lock()
		p.freshAfter = time.Now().Add(60 * time.Millisecond)
		p.mu.Unlock()
	}
	p.host.SetSelfVisible(v)
}

// Session is one analysis session.
type Session struct {
	plat *framePlat
	ctl  *app.Controller
}

// NewSession creates a session. Nothing is persisted: the board and pieces are
// learned again each time, which keeps it robust against changing screens.
func NewSession(host Host) *Session {
	p := &framePlat{host: host}
	c := app.New(p, "", "", "")
	c.SetMoveTime(1500)
	c.SetOverlay(true)
	c.Start()
	return &Session{plat: p, ctl: c}
}

// PushFrame hands over the newest screen frame: RGBA bytes, stride bytes per row.
func (s *Session) PushFrame(w, h, stride int, pix []byte) {
	if w < 16 || h < 16 || stride < w*4 || len(pix) < stride*(h-1)+w*4 {
		return
	}
	img := &image.RGBA{Pix: pix, Stride: stride, Rect: image.Rect(0, 0, w, h)}
	s.plat.mu.Lock()
	s.plat.img = img
	s.plat.ts = time.Now()
	s.plat.mu.Unlock()
}

// SetPanelRect tells where our floating panel lies (frame pixels); it is
// blanked in captures. An empty rectangle clears it.
func (s *Session) SetPanelRect(x, y, w, h int) {
	s.plat.mu.Lock()
	s.plat.panel = image.Rect(x, y, x+w, y+h)
	s.plat.mu.Unlock()
}

// Toggle starts or pauses the analysis (the first start finds the board).
func (s *Session) Toggle() { go s.ctl.ToggleRun() }

// Recalibrate finds the board and learns the pieces again.
func (s *Session) Recalibrate() { go s.ctl.Calibrate() }

// Reset forgets the current game.
func (s *Session) Reset() { s.ctl.ResetGame() }

// SetBoth chooses hints for both sides instead of only the user's.
func (s *Session) SetBoth(both bool) { s.ctl.SetHintBoth(both) }

// SetMoveTime sets thinking time in milliseconds.
func (s *Session) SetMoveTime(ms int) { s.ctl.SetMoveTime(ms) }

type state struct {
	Running    bool    `json:"running"`
	Calibrated bool    `json:"calibrated"`
	Busy       bool    `json:"busy"`
	Status     string  `json:"status"`
	Kind       int     `json:"kind"`
	HasBoard   bool    `json:"hasBoard"`
	Board      [4]int  `json:"board"`
	White      bool    `json:"white"`
	Best       string  `json:"best"`
	BestSAN    string  `json:"bestSan"`
	Eval       float64 `json:"eval"`
	Depth      int     `json:"depth"`
	Mate       int     `json:"mate"`
	IsMate     bool    `json:"isMate"`
	Result     string  `json:"result"`
	Analyzing  bool    `json:"analyzing"`
	Scans      int     `json:"scans"`
}

// State returns the current state as JSON.
func (s *Session) State() string {
	m := s.ctl.Snapshot()
	st := state{Running: m.Running, Calibrated: m.Calibrated, Status: m.Status, Kind: int(m.StatusKind),
		HasBoard: m.Cfg.HasBoard, Board: m.Cfg.BoardRect, White: m.Cfg.WhiteAtBottom, Result: m.Result,
		Analyzing: m.Analyzing, Scans: m.Scans, Eval: m.EvalWhite}
	if m.Best != 0 && m.HavePos {
		st.Best, st.BestSAN = m.Best.UCI(), m.BestSAN
	}
	if m.HaveInfo {
		st.Depth, st.IsMate, st.Mate = m.Info.Depth, m.Info.IsMate, m.Info.Mate
	}
	b, _ := json.Marshal(st)
	return string(b)
}

// Close stops the session.
func (s *Session) Close() { s.ctl.Close() }
