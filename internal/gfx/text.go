package gfx

import (
	"image"
	"math"
	"sync"
)

type glyphKey struct {
	font  int
	gid   int
	size  int // size*16
	phase int // sub-pixel phase 0..3
}

var (
	glyphMu    sync.Mutex
	glyphCache = map[glyphKey]*Mask{}
)

// glyphMask returns a coverage mask positioned relative to the glyph origin
// (baseline-left); the mask rectangle's Min is the offset from the origin.
func (f *Font) glyphMask(gid int, size float64, phase int) *Mask {
	k := glyphKey{f.id, gid, int(size*16 + 0.5), phase}
	glyphMu.Lock()
	if m, ok := glyphCache[k]; ok {
		glyphMu.Unlock()
		return m
	}
	glyphMu.Unlock()
	p := f.outline(gid, size, float64(phase)/4)
	m := p.Rasterize(image.Rect(-4096, -4096, 4096, 4096))
	glyphMu.Lock()
	glyphCache[k] = m // nil is cached too (blank glyphs)
	glyphMu.Unlock()
	return m
}

// TextWidth measures a string in pixels.
func (f *Font) TextWidth(s string, size float64) float64 {
	var w float64
	for _, r := range s {
		w += f.Advance(f.GlyphIndex(r)) * size
	}
	return w
}

// LineHeight returns a comfortable line height.
func (f *Font) LineHeight(size float64) float64 {
	return (f.Ascent + f.Descent + f.LineGap) * size
}

// Fit truncates s with an ellipsis so that it fits maxW pixels.
func (f *Font) Fit(s string, size, maxW float64) string {
	if f.TextWidth(s, size) <= maxW {
		return s
	}
	rs := []rune(s)
	for len(rs) > 0 {
		rs = rs[:len(rs)-1]
		t := string(rs) + "…"
		if f.TextWidth(t, size) <= maxW {
			return t
		}
	}
	return ""
}

// Wrap splits s into lines no wider than maxW (greedy, by spaces).
func (f *Font) Wrap(s string, size, maxW float64) []string {
	var lines []string
	var cur string
	word := ""
	flushWord := func() {
		if word == "" {
			return
		}
		try := word
		if cur != "" {
			try = cur + " " + word
		}
		if f.TextWidth(try, size) <= maxW || cur == "" {
			cur = try
		} else {
			lines = append(lines, cur)
			cur = word
		}
		word = ""
	}
	for _, r := range s {
		switch r {
		case ' ':
			flushWord()
		case '\n':
			flushWord()
			lines = append(lines, cur)
			cur = ""
		default:
			word += string(r)
		}
	}
	flushWord()
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// DrawText draws s with its baseline at y, starting at x.
func (c *Canvas) DrawText(f *Font, size float64, x, y float64, s string, col Color) {
	pen := x
	by := int(math.Round(y))
	for _, r := range s {
		gid := f.GlyphIndex(r)
		ix := math.Floor(pen)
		phase := int((pen - ix) * 4)
		if m := f.glyphMask(gid, size, phase); m != nil {
			c.FillMaskAt(m, int(ix), by, Solid(col))
		}
		pen += f.Advance(gid) * size
	}
}

// DrawTextRight right-aligns the text at x.
func (c *Canvas) DrawTextRight(f *Font, size float64, x, y float64, s string, col Color) {
	c.DrawText(f, size, x-f.TextWidth(s, size), y, s, col)
}

// DrawTextCenter centres the text horizontally on x.
func (c *Canvas) DrawTextCenter(f *Font, size float64, x, y float64, s string, col Color) {
	c.DrawText(f, size, x-f.TextWidth(s, size)/2, y, s, col)
}

// GlyphPath returns the path of rune r in pixel space with its origin at (x,y)
// (baseline-left). Used to draw large glyphs such as chess pieces.
func (f *Font) GlyphPath(r rune, size, x, y float64) *Path {
	p := f.outline(f.GlyphIndex(r), size, 0)
	for _, c := range p.C {
		for i := range c {
			c[i].X += x
			c[i].Y += y
		}
	}
	return p
}
