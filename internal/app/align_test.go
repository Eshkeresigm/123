package app

import (
	"testing"
	"time"

	"chessvision/internal/chess"
)

// The stored board rectangle ends up one square off (the checker pattern
// repeats). The program must notice and repair it without any user action.
func TestBoardRectOneSquareOffIsRepaired(t *testing.T) {
	for _, white := range []bool{true, false} {
		for _, dx := range []int{-1, 1} {
			c, _, start := setupFlip(t, white)
			c.mu.Lock()
			good := c.m.Cfg.BoardRect
			sq := (good[2] - good[0]) / 8
			c.m.Cfg.BoardRect = [4]int{good[0] + dx*sq, good[1], good[2] + dx*sq, good[3]}
			c.m.Cfg.Region = c.m.Cfg.BoardRect
			c.mu.Unlock()
			deadline := time.Now().Add(20 * time.Second)
			ok := false
			for time.Now().Before(deadline) {
				m := c.Snapshot()
				r := m.Cfg.BoardRect
				if abs2(r[0]-good[0]) <= 3 && abs2(r[2]-good[2]) <= 3 && m.HavePos && m.Pos.Board == start.Board && m.Calibrated {
					ok = true
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			if !ok {
				m := c.Snapshot()
				t.Errorf("white=%v dx=%d: not repaired: rect=%v (want %v) status=%q pos=%s", white, dx, m.Cfg.BoardRect, good, m.Status, m.Pos.FEN())
			}
			_ = chess.Start
		}
	}
}
