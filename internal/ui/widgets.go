package ui

import (
	"math"

	"chessvision/internal/gfx"
)

type btnKind int

const (
	btnSecondary btnKind = iota
	btnGhost
	btnPrimary
)

// card draws a rounded glass panel with an optional title.
func (u *UI) card(p P, x, y, w, h float64, title string) {
	if !u.building {
		return // painted once into the cached backdrop
	}
	p.Shadow(x, y, w, h, 18, 34, 0, 12, gfx.RGBA(50, 66, 140, 52))
	p.Round(x, y, w, h, 18, p.Lin(x, y, x, y+h, gfx.Stop{Pos: 0, Color: colCardHi}, gfx.Stop{Pos: 1, Color: colCard}))
	p.Stroke(x, y, w, h, 18, 1, colLine)
	// faint top highlight
	p.Round(x+1, y+1, w-2, 40, 17, p.Lin(x, y, x, y+40, gfx.Stop{Pos: 0, Color: gfx.RGBA(255, 255, 255, 0)}, gfx.Stop{Pos: 1, Color: gfx.RGBA(255, 255, 255, 0)}))
	if title != "" {
		p.Round(x+20, y+18, 4, 16, 2, p.Lin(0, y+18, 0, y+34, gfx.Stop{Pos: 0, Color: colViolet}, gfx.Stop{Pos: 1, Color: colCyan}))
		p.Text(u.bold, 16, x+32, y+26, title, colText)
	}
}

// button draws a clickable button and registers its action.
func (u *UI) button(p P, id string, x, y, w, h float64, label string, icon func(P, float64, float64, float64, gfx.Color), kind btnKind, enabled bool, act func()) {
	hot := enabled && u.isHot(id)
	down := enabled && u.isActive(id)
	hv := u.ease(id+"/h", b2f(hot), 14)
	dy := 0.0
	if down {
		dy = 1
	}
	yy := y + dy
	r := math.Min(14, h/2)
	var txt gfx.Color
	switch kind {
	case btnPrimary:
		glow := 0.30 + 0.25*hv
		if !enabled {
			glow = 0
		}
		p.Shadow(x+4, y+4, w-8, h-4, r, 30, 0, 10, colViolet.Alpha(glow))
		top, bot := gfx.Mix(colViolet, gfx.Hex(0xFFFFFF), 0.10*hv), gfx.Mix(colCyan, gfx.Hex(0xFFFFFF), 0.12*hv)
		if !enabled {
			top, bot = gfx.Mix(top, colCard, 0.7), gfx.Mix(bot, colCard, 0.7)
		}
		p.Round(x, yy, w, h, r, p.Lin(x, yy, x+w, yy+h, gfx.Stop{Pos: 0, Color: top}, gfx.Stop{Pos: 1, Color: bot}))
		p.Round(x+1, yy+1, w-2, h/2, r-1, p.Lin(0, yy, 0, yy+h/2, gfx.Stop{Pos: 0, Color: gfx.RGBA(255, 255, 255, 46)}, gfx.Stop{Pos: 1, Color: gfx.RGBA(255, 255, 255, 0)}))
		txt = gfx.Hex(0xFFFFFF)
		if !enabled {
			txt = gfx.RGBA(255, 255, 255, 120)
		}
	case btnGhost:
		a := 0.0 + 0.07*hv
		p.RoundC(x, yy, w, h, r, glass(a*1.0))
		p.Stroke(x, yy, w, h, r, 1, glass(0.10+0.12*hv))
		txt = gfx.Mix(colMuted, colText, hv)
		if !enabled {
			txt = colFaint
		}
	default:
		p.Shadow(x+2, yy+2, w-4, h-2, r, 12, 0, 4, glass(0.07+0.07*hv))
		p.RoundC(x, yy, w, h, r, gfx.Mix(colWhite, gfx.Hex(0xEEF1FB), hv))
		p.Stroke(x, yy, w, h, r, 1, glass(0.14+0.14*hv))
		txt = colText
		if !enabled {
			txt = colFaint
		}
	}
	size := 14.0
	f := u.reg
	if kind == btnPrimary {
		size, f = 17, u.bold
	}
	tw := p.W(f, size, label)
	iw := 0.0
	if icon != nil {
		iw = size*1.35 + 8
	}
	start := x + (w-(tw+iw))/2
	if icon != nil {
		icon(p, start+size*0.62, yy+h/2, size*1.15, txt)
	}
	p.Text(f, size, start+iw, yy+h/2, label, txt)
	if enabled {
		u.hit(id, x, y, w, h)
		u.actions[id] = act
	}
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// segmented draws a pill selector; enabled may be nil.
func (u *UI) segmented(p P, id string, x, y, w, h float64, labels []string, sel int, enabled []bool, onSelect func(i int)) {
	n := float64(len(labels))
	p.RoundC(x, y, w, h, h/2, gfx.Hex(0xE8ECF7))
	p.Stroke(x, y, w, h, h/2, 1, colLine)
	segW := (w - 6) / n
	pos := u.ease(id+"/pos", float64(sel), 18)
	px := x + 3 + segW*pos
	p.Shadow(px, y+3, segW, h-6, (h-6)/2, 16, 0, 3, colViolet.Alpha(0.35))
	p.Round(px, y+3, segW, h-6, (h-6)/2, p.Lin(px, y, px+segW, y, gfx.Stop{Pos: 0, Color: colViolet}, gfx.Stop{Pos: 1, Color: gfx.Mix(colViolet, colCyan, 0.75)}))
	p.Round(px+1, y+4, segW-2, (h-6)/2, (h-6)/2, p.Lin(0, y+3, 0, y+3+(h-6)/2, gfx.Stop{Pos: 0, Color: gfx.RGBA(255, 255, 255, 40)}, gfx.Stop{Pos: 1, Color: gfx.RGBA(255, 255, 255, 0)}))
	for i, l := range labels {
		sx := x + 3 + segW*float64(i)
		sid := id + "/" + string(rune('a'+i))
		ok := enabled == nil || enabled[i]
		hv := u.ease(sid+"/h", b2f(ok && u.isHot(sid)), 14)
		col := gfx.Mix(colMuted, colText, hv)
		if i == sel {
			col = gfx.Hex(0xFFFFFF)
		}
		if !ok {
			col = colFaint
		}
		p.TextC(u.reg, 13.5, sx+segW/2, y+h/2, p.Fit(u.reg, 13.5, segW-8, l), col)
		if ok {
			idx := i
			u.hit(sid, sx, y, segW, h)
			u.actions[sid] = func() { onSelect(idx) }
		}
	}
}

// toggle draws a switch with a label.
func (u *UI) toggle(p P, id string, x, y, w float64, label string, on bool, onChange func(bool)) {
	hv := u.ease(id+"/h", b2f(u.isHot(id)), 14)
	t := u.ease(id+"/on", b2f(on), 16)
	tw, th := 38.0, 22.0
	trackCol := gfx.Mix(glass(0.20), colViolet, t)
	if t > 0.01 {
		p.Shadow(x+2, y+2, tw-4, th-4, th/2, 14, 0, 3, colViolet.Alpha(0.35*t))
	}
	p.RoundC(x, y, tw, th, th/2, trackCol)
	if t > 0.01 {
		p.Round(x, y, tw, th, th/2, p.Lin(x, y, x+tw, y, gfx.Stop{Pos: 0, Color: colViolet.Alpha(t)}, gfx.Stop{Pos: 1, Color: colCyan.Alpha(t)}))
	}
	kx := x + th/2 + (tw-th)*t
	p.CircleC(kx, y+th/2+1, th/2-3, glass(0.2))
	p.CircleC(kx, y+th/2, th/2-3, gfx.Hex(0xFFFFFF))
	p.Text(u.reg, 14, x+tw+12, y+th/2, p.Fit(u.reg, 14, w-tw-12, label), gfx.Mix(colMuted, colText, math.Max(hv, t*0.8)))
	u.hit(id, x, y-4, w, th+8)
	u.actions[id] = func() { onChange(!on) }
}
