package app

import (
	"image"
	"image/color"
	"testing"
	"time"

	"chessvision/internal/chess"
)

type coverPlat struct {
	fakePlatform
	on   bool
	rect image.Rectangle
}

func (p *coverPlat) OwnRects() []image.Rectangle {
	if !p.on {
		return nil
	}
	return []image.Rectangle{p.rect}
}

// paint draws our "window" (flat grey) over the screen like the real one.
func (p *coverPlat) paint() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for y := p.rect.Min.Y; y < p.rect.Max.Y; y++ {
		for x := p.rect.Min.X; x < p.rect.Max.X; x++ {
			p.screen.SetRGBA(x, y, color.RGBA{236, 240, 250, 255})
		}
	}
}

// The ChessVision window lies over the h/g files. The squares under it cannot
// be read, but everything else must keep being tracked and nothing may be
// misread because of the window.
func TestWindowOverBoard(t *testing.T) {
	cp := &coverPlat{rect: image.Rect(214+62*6+10, 0, 900, 700)}
	start := chess.Start()
	cp.show(start.Board, chess.NoMove, true)
	c := New(cp, t.TempDir(), t.TempDir(), "")
	c.scanEvery = 40 * time.Millisecond
	c.Start()
	defer c.Close()
	c.SetMoveTime(100)
	c.SelectRegion()
	c.ToggleRun()
	if m := c.Snapshot(); !m.Running || !m.Calibrated {
		t.Fatalf("setup: %q", m.Status)
	}
	// now the window appears over the right-hand files
	cp.on = true
	pos := start
	for _, u := range []string{"e2e4", "e7e5", "d2d4", "b8c6"} {
		mv, _ := pos.ParseUCI(u)
		pos, _ = pos.Make(mv)
		cp.show(pos.Board, mv, true)
		cp.paint()
		waitFor(t, "move "+u, func(m Model) bool { return m.HavePos && m.Pos.Board == pos.Board }, c)
	}
	time.Sleep(300 * time.Millisecond)
	m := c.Snapshot()
	if m.Pos.Board != pos.Board {
		t.Fatalf("position lost under the window: %s", m.Pos.FEN())
	}
}
