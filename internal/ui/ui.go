package ui

import (
	"image"
	"math"
	"time"

	"chessvision/assets"
	"chessvision/internal/app"
	"chessvision/internal/gfx"
)

// Logical window size.
const (
	LogicalW = 1120
	LogicalH = 760
	titleH   = 56
)

type region struct {
	id         string
	x, y, w, h float64
}

// UI is the whole interface.
type UI struct {
	Ctl *app.Controller
	S   float64

	reg, bold, sym *gfx.Font

	mx, my   float64
	mdown    bool
	hot      string
	active   string
	regs     []region
	prevRegs []region
	actions  map[string]func()
	drags    map[string]func(x float64)
	prevDrag map[string]func(x float64)

	anim      map[string]float64
	animating bool
	last      time.Time
	dt        float64
	t0        time.Time

	sprites   map[spriteKey]*image.RGBA
	m         app.Model
	evalShare float64
	arrow     arrowAnim

	chrome      *image.RGBA // cached backdrop
	chromeScale float64
	building    bool

	// hooks supplied by the window layer
	OnClose    func()
	OnMinimize func()
	OnPin      func(on bool)
}

// New creates the interface bound to a controller.
func New(ctl *app.Controller) *UI {
	u := &UI{Ctl: ctl, S: 1, actions: map[string]func(){}, drags: map[string]func(float64){},
		anim: map[string]float64{}, sprites: map[spriteKey]*image.RGBA{}, t0: time.Now(), evalShare: 0.5}
	var err error
	if u.reg, err = gfx.LoadFont(assets.UIRegular); err != nil {
		panic(err)
	}
	if u.bold, err = gfx.LoadFont(assets.UIBold); err != nil {
		panic(err)
	}
	if u.sym, err = gfx.LoadFont(assets.Symbols); err != nil {
		panic(err)
	}
	return u
}

// SetScale sets the device scale (1.0 = 96 dpi).
func (u *UI) SetScale(s float64) {
	if s < 0.5 {
		s = 0.5
	}
	if s != u.S {
		u.S = s
		u.sprites = map[spriteKey]*image.RGBA{}
	}
}

// PixelSize returns the window client size in device pixels.
func (u *UI) PixelSize() (int, int) {
	return int(math.Round(LogicalW * u.S)), int(math.Round(LogicalH * u.S))
}

// Animating reports whether another frame is needed even without input.
func (u *UI) Animating() bool { return u.animating }

func (u *UI) toLogical(px, py int) (float64, float64) {
	return float64(px) / u.S, float64(py) / u.S
}

func (u *UI) pick(x, y float64) string {
	for i := len(u.prevRegs) - 1; i >= 0; i-- {
		r := u.prevRegs[i]
		if x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h {
			return r.id
		}
	}
	return ""
}

// MouseMove feeds the pointer position in device pixels.
func (u *UI) MouseMove(px, py int) {
	u.mx, u.my = u.toLogical(px, py)
	u.hot = u.pick(u.mx, u.my)
	if u.mdown && u.active != "" {
		if d, ok := u.prevDrag[u.active]; ok {
			d(u.mx)
		}
	}
}

// MouseLeave clears hover state.
func (u *UI) MouseLeave() {
	if !u.mdown {
		u.hot = ""
	}
}

// MouseDown handles a left press.
func (u *UI) MouseDown(px, py int) {
	u.MouseMove(px, py)
	u.mdown = true
	u.active = u.hot
	if d, ok := u.prevDrag[u.active]; ok && u.active != "" {
		d(u.mx)
	}
}

// MouseUp handles a left release and fires the click.
func (u *UI) MouseUp(px, py int) {
	u.MouseMove(px, py)
	was := u.active
	u.mdown = false
	u.active = ""
	if was != "" && was == u.hot {
		if f, ok := u.actions[was]; ok {
			f()
		}
	}
}

// OverClickable reports whether the pointer is over something clickable.
func (u *UI) OverClickable() bool {
	if u.hot == "" {
		return false
	}
	_, a := u.actions[u.hot]
	_, d := u.prevDrag[u.hot]
	return a || d
}

// InDragZone reports whether a point (device pixels) is empty title-bar space
// that can be used to move the window.
func (u *UI) InDragZone(px, py int) bool {
	x, y := u.toLogical(px, py)
	if y < 0 || y >= titleH {
		return false
	}
	return u.pick(x, y) == ""
}

func (u *UI) hit(id string, x, y, w, h float64) {
	u.regs = append(u.regs, region{id, x, y, w, h})
}

func (u *UI) isHot(id string) bool    { return u.hot == id }
func (u *UI) isActive(id string) bool { return u.active == id && u.mdown }

// ease moves a stored value toward target and flags animation while it moves.
func (u *UI) ease(id string, target, speed float64) float64 {
	cur, ok := u.anim[id]
	if !ok {
		u.anim[id] = target
		return target
	}
	k := 1 - math.Exp(-u.dt*speed)
	cur += (target - cur) * k
	if math.Abs(target-cur) < 0.003 {
		cur = target
	} else {
		u.animating = true
	}
	u.anim[id] = cur
	return cur
}

func (u *UI) now() float64 { return time.Since(u.t0).Seconds() }

// Render draws one frame into c (which must match PixelSize).
func (u *UI) Render(c *gfx.Canvas) {
	now := time.Now()
	if u.last.IsZero() {
		u.dt = 0.016
	} else {
		u.dt = math.Min(now.Sub(u.last).Seconds(), 0.1)
	}
	u.last = now
	u.animating = false
	u.m = u.Ctl.Snapshot()
	u.stepArrow(u.m.Best)
	u.regs = u.regs[:0]
	u.actions = map[string]func(){}
	u.drags = map[string]func(float64){}
	p := P{C: c, S: u.S}
	u.draw(p)
	u.prevRegs = append(u.prevRegs[:0], u.regs...)
	u.prevDrag = u.drags
	if u.m.Analyzing || u.m.DL.Active || (u.m.Running && !u.m.HavePos) {
		u.animating = true
	}
}

func imagePt(x, y float64) image.Point { return image.Pt(int(math.Round(x)), int(math.Round(y))) }

// Recover resets transient drawing state after a panic inside a frame, so the
// next frame starts clean (clip stack, backdrop cache, animation flags).
func (u *UI) Recover() {
	u.building = false
	u.chrome = nil
	u.animating = false
}
