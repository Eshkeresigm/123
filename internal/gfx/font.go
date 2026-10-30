package gfx

import (
	"encoding/binary"
	"errors"
	"math"
)

// Font is a parsed TrueType (glyf) font.
type Font struct {
	data       []byte
	unitsPEm   float64
	numGlyphs  int
	locaLong   bool
	loca, glyf []byte
	hmtx       []byte
	numHM      int
	cmap       map[rune]int
	Ascent     float64 // in em units (positive)
	Descent    float64 // in em units (positive)
	LineGap    float64
	id         int
}

var fontCounter int

type tableRec struct{ off, length int }

func be16(b []byte) int  { return int(binary.BigEndian.Uint16(b)) }
func bes16(b []byte) int { return int(int16(binary.BigEndian.Uint16(b))) }
func be32(b []byte) int  { return int(binary.BigEndian.Uint32(b)) }

// LoadFont parses a TrueType font with glyf outlines.
func LoadFont(data []byte) (*Font, error) {
	if len(data) < 12 {
		return nil, errors.New("font too short")
	}
	n := be16(data[4:])
	tabs := map[string]tableRec{}
	for i := 0; i < n; i++ {
		r := data[12+16*i:]
		if len(r) < 16 {
			return nil, errors.New("bad table directory")
		}
		tabs[string(r[0:4])] = tableRec{be32(r[8:]), be32(r[12:])}
	}
	get := func(tag string) ([]byte, error) {
		t, ok := tabs[tag]
		if !ok || t.off+t.length > len(data) {
			return nil, errors.New("missing table " + tag)
		}
		return data[t.off : t.off+t.length], nil
	}
	f := &Font{data: data, cmap: map[rune]int{}}
	fontCounter++
	f.id = fontCounter
	head, err := get("head")
	if err != nil {
		return nil, err
	}
	f.unitsPEm = float64(be16(head[18:]))
	f.locaLong = bes16(head[50:]) != 0
	maxp, err := get("maxp")
	if err != nil {
		return nil, err
	}
	f.numGlyphs = be16(maxp[4:])
	hhea, err := get("hhea")
	if err != nil {
		return nil, err
	}
	f.Ascent = float64(bes16(hhea[4:])) / f.unitsPEm
	f.Descent = -float64(bes16(hhea[6:])) / f.unitsPEm
	f.LineGap = float64(bes16(hhea[8:])) / f.unitsPEm
	f.numHM = be16(hhea[34:])
	if f.hmtx, err = get("hmtx"); err != nil {
		return nil, err
	}
	if f.loca, err = get("loca"); err != nil {
		return nil, err
	}
	if f.glyf, err = get("glyf"); err != nil {
		return nil, err
	}
	cm, err := get("cmap")
	if err != nil {
		return nil, err
	}
	if err := f.parseCmap(cm); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *Font) parseCmap(cm []byte) error {
	n := be16(cm[2:])
	best := -1
	bestScore := -1
	for i := 0; i < n; i++ {
		rec := cm[4+8*i:]
		plat, enc, off := be16(rec), be16(rec[2:]), be32(rec[4:])
		score := -1
		switch {
		case plat == 3 && enc == 10:
			score = 4
		case plat == 3 && enc == 1:
			score = 3
		case plat == 0:
			score = 2
		}
		if score > bestScore && off < len(cm) {
			best, bestScore = off, score
		}
	}
	if best < 0 {
		return errors.New("no usable cmap")
	}
	t := cm[best:]
	switch be16(t) {
	case 4:
		segX2 := be16(t[6:])
		seg := segX2 / 2
		ends := t[14:]
		starts := t[16+segX2:]
		deltas := t[16+2*segX2:]
		ranges := t[16+3*segX2:]
		for i := 0; i < seg; i++ {
			end, start := be16(ends[2*i:]), be16(starts[2*i:])
			delta, ro := bes16(deltas[2*i:]), be16(ranges[2*i:])
			for c := start; c <= end && c != 0xFFFF; c++ {
				var g int
				if ro == 0 {
					g = (c + delta) & 0xFFFF
				} else {
					idx := 2*i + ro + 2*(c-start)
					if idx+2 > len(ranges) {
						continue
					}
					g = be16(ranges[idx:])
					if g != 0 {
						g = (g + delta) & 0xFFFF
					}
				}
				if g != 0 {
					f.cmap[rune(c)] = g
				}
			}
		}
	case 12:
		ng := be32(t[12:])
		for i := 0; i < ng; i++ {
			g := t[16+12*i:]
			s, e, gid := be32(g), be32(g[4:]), be32(g[8:])
			for c := s; c <= e && c-s < 70000; c++ {
				f.cmap[rune(c)] = gid + (c - s)
			}
		}
	default:
		return errors.New("unsupported cmap format")
	}
	return nil
}

// GlyphIndex maps a rune to a glyph (0 = .notdef).
func (f *Font) GlyphIndex(r rune) int { return f.cmap[r] }

// Has reports whether the font covers r.
func (f *Font) Has(r rune) bool { return f.cmap[r] != 0 }

// Advance returns the advance of a glyph in em units.
func (f *Font) Advance(g int) float64 {
	if g >= f.numHM {
		g = f.numHM - 1
	}
	return float64(be16(f.hmtx[4*g:])) / f.unitsPEm
}

type glyphPoint struct {
	x, y float64
	on   bool
}

func (f *Font) glyphData(g int) []byte {
	if g < 0 || g >= f.numGlyphs {
		return nil
	}
	var a, b int
	if f.locaLong {
		a, b = be32(f.loca[4*g:]), be32(f.loca[4*g+4:])
	} else {
		a, b = 2*be16(f.loca[2*g:]), 2*be16(f.loca[2*g+2:])
	}
	if b <= a || b > len(f.glyf) {
		return nil
	}
	return f.glyf[a:b]
}

// contours returns raw TrueType contours in font units (y up).
func (f *Font) contours(g, depth int) [][]glyphPoint {
	d := f.glyphData(g)
	if len(d) < 10 || depth > 6 {
		return nil
	}
	nc := bes16(d)
	if nc >= 0 {
		return f.simpleGlyph(d, nc)
	}
	// composite
	var out [][]glyphPoint
	p := d[10:]
	for {
		if len(p) < 4 {
			break
		}
		flags, gi := be16(p), be16(p[2:])
		p = p[4:]
		var dx, dy float64
		if flags&1 != 0 {
			if len(p) < 4 {
				break
			}
			if flags&2 != 0 {
				dx, dy = float64(bes16(p)), float64(bes16(p[2:]))
			}
			p = p[4:]
		} else {
			if len(p) < 2 {
				break
			}
			if flags&2 != 0 {
				dx, dy = float64(int8(p[0])), float64(int8(p[1]))
			}
			p = p[2:]
		}
		a, b, c, dd := 1.0, 0.0, 0.0, 1.0
		f2 := func(b []byte) float64 { return float64(bes16(b)) / 16384 }
		switch {
		case flags&8 != 0:
			if len(p) < 2 {
				return out
			}
			a = f2(p)
			dd = a
			p = p[2:]
		case flags&0x40 != 0:
			if len(p) < 4 {
				return out
			}
			a, dd = f2(p), f2(p[2:])
			p = p[4:]
		case flags&0x80 != 0:
			if len(p) < 8 {
				return out
			}
			a, b, c, dd = f2(p), f2(p[2:]), f2(p[4:]), f2(p[6:])
			p = p[8:]
		}
		for _, cn := range f.contours(gi, depth+1) {
			nc := make([]glyphPoint, len(cn))
			for i, q := range cn {
				nc[i] = glyphPoint{a*q.x + c*q.y + dx, b*q.x + dd*q.y + dy, q.on}
			}
			out = append(out, nc)
		}
		if flags&0x20 == 0 {
			break
		}
	}
	return out
}

func (f *Font) simpleGlyph(d []byte, nc int) [][]glyphPoint {
	if nc == 0 {
		return nil
	}
	p := d[10:]
	if len(p) < 2*nc+2 {
		return nil
	}
	ends := make([]int, nc)
	for i := range ends {
		ends[i] = be16(p[2*i:])
	}
	npts := ends[nc-1] + 1
	il := be16(p[2*nc:])
	p = p[2*nc+2+il:]
	flags := make([]byte, npts)
	for i := 0; i < npts; {
		if len(p) == 0 {
			return nil
		}
		fl := p[0]
		p = p[1:]
		flags[i] = fl
		i++
		if fl&8 != 0 {
			if len(p) == 0 {
				return nil
			}
			rep := int(p[0])
			p = p[1:]
			for ; rep > 0 && i < npts; rep-- {
				flags[i] = fl
				i++
			}
		}
	}
	xs := make([]float64, npts)
	ys := make([]float64, npts)
	v := 0
	for i := 0; i < npts; i++ {
		fl := flags[i]
		switch {
		case fl&2 != 0:
			if len(p) == 0 {
				return nil
			}
			dx := int(p[0])
			p = p[1:]
			if fl&0x10 == 0 {
				dx = -dx
			}
			v += dx
		case fl&0x10 == 0:
			if len(p) < 2 {
				return nil
			}
			v += bes16(p)
			p = p[2:]
		}
		xs[i] = float64(v)
	}
	v = 0
	for i := 0; i < npts; i++ {
		fl := flags[i]
		switch {
		case fl&4 != 0:
			if len(p) == 0 {
				return nil
			}
			dy := int(p[0])
			p = p[1:]
			if fl&0x20 == 0 {
				dy = -dy
			}
			v += dy
		case fl&0x20 == 0:
			if len(p) < 2 {
				return nil
			}
			v += bes16(p)
			p = p[2:]
		}
		ys[i] = float64(v)
	}
	out := make([][]glyphPoint, 0, nc)
	start := 0
	for _, e := range ends {
		c := make([]glyphPoint, 0, e-start+1)
		for i := start; i <= e && i < npts; i++ {
			c = append(c, glyphPoint{xs[i], ys[i], flags[i]&1 != 0})
		}
		out = append(out, c)
		start = e + 1
	}
	return out
}

// outline converts a glyph to flattened contours in pixel units, y down,
// origin at the glyph's baseline start.
func (f *Font) outline(g int, size, subX float64) *Path {
	scale := size / f.unitsPEm
	var path Path
	for _, c := range f.contours(g, 0) {
		if len(c) < 2 {
			continue
		}
		// rotate so we start on an on-curve point (or synthesise one)
		var pts []glyphPoint
		first := -1
		for i, q := range c {
			if q.on {
				first = i
				break
			}
		}
		if first >= 0 {
			pts = append(pts, c[first:]...)
			pts = append(pts, c[:first]...)
		} else {
			mid := glyphPoint{(c[0].x + c[len(c)-1].x) / 2, (c[0].y + c[len(c)-1].y) / 2, true}
			pts = append(pts, mid)
			pts = append(pts, c...)
		}
		toPx := func(q glyphPoint) Pt { return Pt{q.x*scale + subX, -q.y * scale} }
		var poly []Pt
		cur := toPx(pts[0])
		poly = append(poly, cur)
		i := 1
		for i <= len(pts) {
			q := pts[i%len(pts)]
			if q.on {
				cur = toPx(q)
				poly = append(poly, cur)
				i++
				continue
			}
			// off-curve control point
			ctrl := toPx(q)
			next := pts[(i+1)%len(pts)]
			var end Pt
			if next.on {
				end = toPx(next)
				i += 2
			} else {
				end = toPx(glyphPoint{(q.x + next.x) / 2, (q.y + next.y) / 2, true})
				i++
			}
			dist := math.Hypot(ctrl.X-cur.X, ctrl.Y-cur.Y) + math.Hypot(end.X-ctrl.X, end.Y-ctrl.Y)
			steps := int(math.Sqrt(dist)*0.9) + 2
			if steps > 24 {
				steps = 24
			}
			for s := 1; s <= steps; s++ {
				t := float64(s) / float64(steps)
				mt := 1 - t
				poly = append(poly, Pt{mt*mt*cur.X + 2*mt*t*ctrl.X + t*t*end.X, mt*mt*cur.Y + 2*mt*t*ctrl.Y + t*t*end.Y})
			}
			cur = end
		}
		path.Add(poly)
	}
	return &path
}
