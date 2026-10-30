package gfx

import (
	"image/png"
	"os"
	"testing"

	"chessvision/assets"
)

func TestRenderSample(t *testing.T) {
	ui, err := LoadFont(assets.UIRegular)
	if err != nil {
		t.Fatal(err)
	}
	bold, err := LoadFont(assets.UIBold)
	if err != nil {
		t.Fatal(err)
	}
	sym, err := LoadFont(assets.Symbols)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range "Привет, мир! ёЁйЙ" {
		if !ui.Has(r) {
			t.Errorf("UI font lacks %q", r)
		}
	}
	for _, r := range "♔♕♖♗♘♙♚♛♜♝♞♟" {
		if !sym.Has(r) {
			t.Errorf("symbol font lacks %q", r)
		}
	}
	c := NewCanvas(640, 360)
	c.Clear(Hex(0x12151c))
	c.Shadow(30, 30, 280, 140, 18, 24, 0, 8, RGBA(0, 0, 0, 160))
	c.FillRound(30, 30, 280, 140, 18, Linear(30, 30, 310, 170, Stop{0, Hex(0x7c5cff)}, Stop{1, Hex(0x19c3ff)}))
	c.StrokeRound(30, 30, 280, 140, 18, 1.5, Solid(RGBA(255, 255, 255, 60)))
	c.DrawText(bold, 28, 50, 80, "Правильный ход", Hex(0xffffff))
	c.DrawText(ui, 16, 50, 110, "Привет, мир! ёЁйЙ  g1 → f3  +0.35", RGBA(255, 255, 255, 220))
	c.Line(60, 140, 280, 150, 6, Solid(Hex(0xffd166)))
	c.FillContour(Arrow(340, 260, 600, 90, 18, 44), Solid(Hex(0x3ddc97)))
	for i, r := range []rune("♔♕♖♗♘♙♚♛♜♝♞♟") {
		x := 30 + float64(i)*48
		m := sym.GlyphPath(r, 44, x, 250)
		c.Fill(m, Solid(Hex(0xffffff)))
	}
	c.FillCircle(500, 280, 30, Radial(500, 280, 30, Stop{0, Hex(0xff5c7a)}, Stop{1, Hex(0x7c5cff)}))
	c.StrokeCircle(580, 290, 24, 4, Solid(Hex(0xffb84d)))
	f, _ := os.Create(os.Getenv("GFX_OUT"))
	if f != nil {
		defer f.Close()
		png.Encode(f, c.Img)
	}
}
