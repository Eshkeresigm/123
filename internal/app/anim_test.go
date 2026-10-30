package app

import (
	"image"
	"sync"
	"testing"
	"time"

	"chessvision/internal/chess"
)

type recPlat struct {
	fakePlatform
	rmu    sync.Mutex
	frames []*image.RGBA
	hidden int
}

func (r *recPlat) ShowOverlay(rc image.Rectangle, img *image.RGBA) {
	r.rmu.Lock()
	r.frames = append(r.frames, img)
	r.rmu.Unlock()
}
func (r *recPlat) HideOverlay() { r.rmu.Lock(); r.hidden++; r.rmu.Unlock() }

func alphaSum(img *image.RGBA) (s int) {
	for i := 3; i < len(img.Pix); i += 4 {
		s += int(img.Pix[i])
	}
	return
}

func TestOverlayAnimationSmooth(t *testing.T) {
	rp := &recPlat{}
	c := New(rp, t.TempDir(), t.TempDir(), "")
	defer c.Close()
	mv := chess.Move(0)
	pos := chess.Start()
	mv, _ = pos.ParseUCI("e2e4")
	r := image.Rect(10, 10, 410, 410)

	c.mu.Lock()
	c.ovGen, c.ovAnim = 1, true
	c.mu.Unlock()
	start := time.Now()
	c.runOverlayAnim(1, r, true, chess.NoMove, 0, mv)
	el := time.Since(start)

	rp.rmu.Lock()
	frames := rp.frames
	rp.rmu.Unlock()
	if len(frames) < 8 {
		t.Fatalf("only %d frames in %v - not smooth", len(frames), el)
	}
	prev := -1
	for i, f := range frames {
		s := alphaSum(f)
		if s < prev-prev/50 { // monotone growth (tiny tolerance)
			t.Fatalf("frame %d shrank: %d < %d", i, s, prev)
		}
		prev = s
	}
	if alphaSum(frames[0]) >= alphaSum(frames[len(frames)-1])/2 {
		t.Error("first frame should be much fainter than the last")
	}
	want := renderOverlay(400, 400, true, mv)
	last := frames[len(frames)-1]
	if string(last.Pix) != string(want.Pix) {
		t.Error("final frame differs from the resting overlay used for un-blending")
	}
	c.mu.Lock()
	if c.ovAnim || c.overlayImg != last || c.ovShown != mv || c.ovLevel != 1 {
		t.Errorf("state after anim: anim=%v shown=%v level=%v", c.ovAnim, c.ovShown, c.ovLevel)
	}
	c.mu.Unlock()
	t.Logf("%d frames in %v", len(frames), el)

	// fade out and hide
	rp.rmu.Lock()
	rp.frames = nil
	rp.rmu.Unlock()
	c.mu.Lock()
	c.ovGen, c.ovAnim = 2, true
	c.mu.Unlock()
	c.runOverlayAnim(2, r, true, mv, 1, chess.NoMove)
	rp.rmu.Lock()
	n, hid := len(rp.frames), rp.hidden
	rp.rmu.Unlock()
	c.mu.Lock()
	if n < 4 || hid != 1 || c.ovAnim || c.overlayImg != nil {
		t.Errorf("fade-out: frames=%d hidden=%d anim=%v", n, hid, c.ovAnim)
	}
	c.mu.Unlock()
}

func TestOverlayAnimationSuperseded(t *testing.T) {
	rp := &recPlat{}
	c := New(rp, t.TempDir(), t.TempDir(), "")
	defer c.Close()
	pos := chess.Start()
	a, _ := pos.ParseUCI("e2e4")
	b, _ := pos.ParseUCI("d2d4")
	r := image.Rect(0, 0, 320, 320)
	c.mu.Lock()
	c.ovGen, c.ovAnim = 1, true
	c.mu.Unlock()
	done := make(chan struct{})
	go func() { c.runOverlayAnim(1, r, true, chess.NoMove, 0, a); close(done) }()
	time.Sleep(80 * time.Millisecond)
	c.mu.Lock()
	c.ovGen = 2 // a newer animation takes over
	c.mu.Unlock()
	<-done
	c.mu.Lock()
	if !c.ovAnim {
		t.Error("a superseded animation must leave the flag to its successor")
	}
	c.mu.Unlock()
	c.runOverlayAnim(2, r, true, c.ovShown, c.ovLevel, b)
	c.mu.Lock()
	if c.ovAnim || c.ovShown != b {
		t.Errorf("successor did not finish: anim=%v shown=%v", c.ovAnim, c.ovShown)
	}
	c.mu.Unlock()
}

func BenchmarkOverlayFrame(b *testing.B) {
	pos := chess.Start()
	mv, _ := pos.ParseUCI("g1f3")
	for i := 0; i < b.N; i++ {
		renderOverlayT(800, 800, true, mv, 0.6, 0.8)
	}
}
