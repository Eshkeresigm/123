package ui

import (
	"testing"
	"time"

	"chessvision/internal/app"
	"chessvision/internal/gfx"
)

type nopPlat struct{ app.Platform }

func BenchmarkRender(b *testing.B) {
	ctl := app.New(nil, b.TempDir(), b.TempDir(), "")
	u := New(ctl)
	w, h := u.PixelSize()
	c := gfx.NewCanvas(w, h)
	u.Render(c)
	time.Sleep(10 * time.Millisecond)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u.Render(c)
	}
}
