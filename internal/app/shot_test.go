package app

import (
	"image"
	"testing"
)

func TestShots(t *testing.T) {
	for _, tc := range []struct {
		f   string
		own image.Rectangle
	}{
		{"shot3", image.Rect(843, 20, 1471, 780)},
		{"shot4", image.Rect(998, 20, 1622, 780)},
	} {
		rp := loadScreen(t, "../vision/testdata/"+tc.f+".png", tc.own)
		c := New(rp, t.TempDir(), t.TempDir(), "")
		c.scanEvery = 40 * 1e6
		c.SetMoveTime(100)
		c.Start()
		c.ToggleRun()
		m := c.Snapshot()
		t.Logf("%s rect=%v white=%v status=%q\n fen=%s", tc.f, m.Cfg.BoardRect, m.Cfg.WhiteAtBottom, m.Status, m.Pos.FEN())
		c.Close()
	}
}

func TestStaleRegionRecovers(t *testing.T) {
	rp := loadScreen(t, "../vision/testdata/shot3.png", image.Rect(843, 0, 1471, 822))
	// make the arrow ghost irrelevant: only the locate step is of interest
	c := New(rp, t.TempDir(), t.TempDir(), "")
	c.scanEvery = 40 * 1e6
	c.mu.Lock()
	c.m.Cfg.HasRegion = true
	c.m.Cfg.Region = [4]int{-81, 6, 724, 814}
	c.mu.Unlock()
	img, rect, err := c.locateBoard()
	_ = img
	t.Logf("rect=%v err=%v region=%v", rect, err, c.Snapshot().Cfg.Region)
}
