package app

import (
	"testing"
	"time"

	"chessvision/internal/chess"
)

func setupFlip(t *testing.T, white bool) (*Controller, *fakePlatform, chess.Position) {
	fp := &fakePlatform{}
	start := chess.Start()
	fp.show(start.Board, chess.NoMove, white)
	c := New(fp, t.TempDir(), t.TempDir(), "")
	c.scanEvery = 40 * time.Millisecond
	c.Start()
	t.Cleanup(c.Close)
	c.SetMoveTime(100)
	c.SelectRegion()
	c.ToggleRun()
	m := c.Snapshot()
	if !m.Running || m.Cfg.WhiteAtBottom != white {
		t.Fatalf("setup: running=%v white=%v %q", m.Running, m.Cfg.WhiteAtBottom, m.Status)
	}
	return c, fp, start
}

// The site starts a new game and now shows the board from Black's side.
func TestAutoSideNewGameFlipped(t *testing.T) {
	c, fp, start := setupFlip(t, true)
	fp.show(start.Board, chess.NoMove, false)
	m := waitFor(t, "orientation flip", func(m Model) bool { return !m.Cfg.WhiteAtBottom }, c)
	m = waitFor(t, "start position kept", func(m Model) bool { return m.HavePos && m.Pos.Board == start.Board }, c)
	if m.Pos.FEN() != start.FEN() {
		t.Fatalf("position %s", m.Pos.FEN())
	}
}

// Mid-game the board is turned around: position and history must survive.
func TestAutoSideMidGameFlipped(t *testing.T) {
	c, fp, start := setupFlip(t, true)
	pos := start
	for _, u := range []string{"e2e4", "e7e5", "g1f3"} {
		mv, err := pos.ParseUCI(u)
		if err != nil {
			t.Fatal(err)
		}
		pos, _ = pos.Make(mv)
		fp.show(pos.Board, mv, true)
		waitFor(t, "move "+u, func(m Model) bool { return m.HavePos && m.Pos.Board == pos.Board }, c)
	}
	fp.show(pos.Board, chess.NoMove, false)
	m := waitFor(t, "orientation flip", func(m Model) bool { return !m.Cfg.WhiteAtBottom }, c)
	time.Sleep(500 * time.Millisecond)
	m = c.Snapshot()
	if m.Pos.Board != pos.Board || len(m.History) < 2 {
		t.Fatalf("position/history lost after flip: %s hist=%d", m.Pos.FEN(), len(m.History))
	}
}
