//go:build windows

package winapi

import (
	"math"
	"time"
	"unsafe"

	"chessvision/internal/app"
	"chessvision/internal/gfx"
	"chessvision/internal/ui"
)

const mainClass = "ChessVisionMain"

const (
	wmChanged  = wmApp + 1
	timerAnim  = 1
	timerFade  = 2
	fadeInMs   = 360
	fadeOutMs  = 150
	slidePx    = 18
	hotkeyRun  = 1
	hotkeyCal  = 2
	animPeriod = 16
)

type mainWin struct {
	hwnd     uintptr
	u        *ui.UI
	ctl      *app.Controller
	plat     *Platform
	canvas   *gfx.Canvas
	bb       *dib
	tracking bool
	timer    bool
	hand     uintptr
	arrow    uintptr
	icons    []uintptr

	// open/close fade
	fadeStart time.Time
	fadeOut   bool
	fading    bool
	baseX     int
	baseY     int
	layered   bool
}

func makeIcon(size int) uintptr {
	img := ui.RenderIcon(size)
	d := newDIB(0, size, size)
	if d == nil {
		return 0
	}
	defer d.free()
	d.setRGBA(img, false)
	mask := call(pCreateBitmap, uintptr(size), uintptr(size), 1, 1, 0) // all zero: colour bitmap alpha decides
	defer call(pDeleteObject, mask)
	ii := iconInfo{fIcon: 1, hbmMask: mask, hbmColor: d.bmp}
	return call(pCreateIconIndirect, uintptr(unsafe.Pointer(&ii)))
}

func monitorWork(mon uintptr) rect {
	mi := monitorInfo{cbSize: uint32(unsafe.Sizeof(monitorInfo{}))}
	call(pGetMonitorInfoW, mon, uintptr(unsafe.Pointer(&mi)))
	return mi.work
}

// fitScale returns the UI scale for a monitor: DPI scale, reduced when the
// window would not fit on a small screen.
func fitScale(dpi int, mon uintptr) float64 {
	s := float64(dpi) / 96
	if s < 0.5 {
		s = 1
	}
	if mon != 0 {
		wk := monitorWork(mon)
		ww, wh := float64(wk.right-wk.left), float64(wk.bottom-wk.top)
		if ww > 0 && wh > 0 {
			s = math.Min(s, math.Min(ww*0.985/ui.LogicalW, wh*0.985/ui.LogicalH))
		}
	}
	if s < 0.5 {
		s = 0.5
	}
	return s
}

func windowDPI(hwnd uintptr) int {
	if pGetDpiForWindow.Find() == nil {
		if d := int(call(pGetDpiForWindow, hwnd)); d > 0 {
			return d
		}
	}
	dc := call(pGetDC, 0)
	defer call(pReleaseDC, 0, dc)
	if d := int(call(pGetDeviceCaps, dc, logPixelsX)); d > 0 {
		return d
	}
	return 96
}

// Run creates the main window and blocks in its message loop.
func Run(ctl *app.Controller, u *ui.UI, plat *Platform) error {
	w := &mainWin{u: u, ctl: ctl, plat: plat}
	w.arrow, w.hand = loadCursor(idcArrow), loadCursor(idcHand)
	big, small := makeIcon(48), makeIcon(24)
	w.icons = []uintptr{big, small}
	if err := registerClass(mainClass, csHRedraw|csVRedraw, w.arrow, big); err != nil {
		return err
	}
	cfg := ctl.Snapshot().Cfg
	hw, err := createWindow(0, mainClass, "ChessVision", wsPopup|wsThickFrame|wsMinimizeBox, 0, 0, ui.LogicalW, ui.LogicalH)
	if err != nil {
		return err
	}
	w.hwnd = hw
	handlers.Store(hw, w)
	plat.mainHwnd.Store(hw)
	if big != 0 {
		call(pSendMessageW, hw, wmSetIcon, iconBig, big)
	}
	if small != 0 {
		call(pSendMessageW, hw, wmSetIcon, iconSmall, small)
	}

	// rounded corners (Windows 11) and the system shadow
	pref := int32(2)
	call(pDwmSetWindowAttribute, hw, 33, uintptr(unsafe.Pointer(&pref)), 4)
	mg := margins{1, 1, 1, 1}
	call(pDwmExtendFrameIntoClnt, hw, uintptr(unsafe.Pointer(&mg)))

	// size and position
	x, y := 0, 0
	placed := false
	if cfg.HasWinPos {
		pt := point{int32(cfg.WinX + 60), int32(cfg.WinY + 20)}
		if call(pMonitorFromPoint, uintptr(*(*int64)(unsafe.Pointer(&pt))), monitorNull) != 0 {
			x, y, placed = cfg.WinX, cfg.WinY, true
		}
	}
	mon := call(pMonitorFromWindow, hw, monitorNearest)
	if placed {
		pt := point{int32(x + 60), int32(y + 20)}
		mon = call(pMonitorFromPoint, uintptr(*(*int64)(unsafe.Pointer(&pt))), monitorNearest)
	}
	u.SetScale(fitScale(windowDPI(hw), mon))
	pw, ph := u.PixelSize()
	if !placed {
		wk := monitorWork(mon)
		x = int(wk.left) + (int(wk.right-wk.left)-pw)/2
		y = int(wk.top) + (int(wk.bottom-wk.top)-ph)/2
	}
	top := hwndNoTopmost
	if cfg.AlwaysOnTop {
		top = hwndTopmost
	}
	call(pSetWindowPos, hw, top, uintptr(int32(x)), uintptr(int32(y)), uintptr(pw), uintptr(ph), swpFrameChanged|swpNoActivate)

	u.OnClose = func() { call(pPostMessageW, hw, wmClose, 0, 0) }
	u.OnMinimize = func() { call(pShowWindow, hw, swMinimize) }
	u.OnPin = func(on bool) {
		ctl.SetAlwaysOnTop(on)
		w.setTopmost(on)
	}

	call(pRegisterHotKey, hw, hotkeyRun, 0, vkF8)
	call(pRegisterHotKey, hw, hotkeyCal, 0, vkF9)
	w.baseX, w.baseY = x, y
	w.beginFade(false)
	call(pShowWindow, hw, swShow)
	call(pSetForegroundWindow, hw)

	go func() {
		for range ctl.Changed() {
			call(pPostMessageW, hw, wmChanged, 0, 0)
		}
	}()
	pump()
	call(pUnregisterHotKey, hw, hotkeyRun)
	call(pUnregisterHotKey, hw, hotkeyCal)
	handlers.Delete(hw)
	w.bb.free()
	return nil
}

func (w *mainWin) setTopmost(on bool) {
	z := hwndNoTopmost
	if on {
		z = hwndTopmost
	}
	call(pSetWindowPos, w.hwnd, z, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
}

func (w *mainWin) invalidate() { call(pInvalidateRect, w.hwnd, 0, 0) }

func (w *mainWin) savePos() {
	var rc rect
	call(pGetWindowRect, w.hwnd, uintptr(unsafe.Pointer(&rc)))
	if call(pIsIconic, w.hwnd) == 0 && rc.left > -30000 {
		w.ctl.SaveWindowPos(int(rc.left), int(rc.top))
	}
}

func (w *mainWin) paint(hdc uintptr) {
	pw, ph := w.u.PixelSize()
	if w.canvas == nil || w.canvas.Bounds().Dx() != pw || w.canvas.Bounds().Dy() != ph {
		w.canvas = gfx.NewCanvas(pw, ph)
		w.bb.free()
		w.bb = newDIB(hdc, pw, ph)
	}
	if w.bb == nil {
		return
	}
	w.render()
	w.bb.setRGBA(w.canvas.Img, true)
	call(pBitBlt, hdc, 0, 0, uintptr(pw), uintptr(ph), w.bb.dc, 0, 0, srcCopy)
	if w.u.Animating() && !w.timer {
		call(pSetTimer, w.hwnd, timerAnim, animPeriod, 0)
		w.timer = true
	} else if !w.u.Animating() && w.timer {
		call(pKillTimer, w.hwnd, timerAnim)
		w.timer = false
	}
}

func (w *mainWin) proc(hwnd uintptr, m uint32, wp, lp uintptr) uintptr {
	switch m {
	case wmNCCalcSize:
		if wp != 0 {
			return 0 // client area = whole window (borderless)
		}
	case wmEraseBkgnd:
		return 1
	case wmNCHitTest:
		var rc rect
		call(pGetWindowRect, hwnd, uintptr(unsafe.Pointer(&rc)))
		x, y := int(int16(lp))-int(rc.left), int(int16(lp>>16))-int(rc.top)
		if w.u.InDragZone(x, y) {
			return htCaption
		}
		return htClient
	case wmSetCursor:
		if loword(lp) == htClient {
			if w.u.OverClickable() {
				call(pSetCursor, w.hand)
			} else {
				call(pSetCursor, w.arrow)
			}
			return 1
		}
	case wmPaint:
		var ps paintStruct
		hdc := call(pBeginPaint, hwnd, uintptr(unsafe.Pointer(&ps)))
		w.paint(hdc)
		call(pEndPaint, hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case wmTimer:
		if wp == timerFade {
			w.stepFade()
			return 0
		}
		w.invalidate()
		return 0
	case wmChanged:
		w.invalidate()
		return 0
	case wmMouseMove:
		if !w.tracking {
			te := trackMouseEvent{cbSize: uint32(unsafe.Sizeof(trackMouseEvent{})), flags: tmeLeave, hwndTrack: hwnd}
			call(pTrackMouseEvent, uintptr(unsafe.Pointer(&te)))
			w.tracking = true
		}
		w.u.MouseMove(int(int16(lp)), int(int16(lp>>16)))
		w.invalidate()
		return 0
	case wmMouseLeave:
		w.tracking = false
		w.u.MouseLeave()
		w.invalidate()
		return 0
	case wmLButtonDown:
		call(pSetCapture, hwnd)
		w.u.MouseDown(int(int16(lp)), int(int16(lp>>16)))
		w.invalidate()
		return 0
	case wmLButtonUp:
		call(pReleaseCapture)
		w.u.MouseUp(int(int16(lp)), int(int16(lp>>16)))
		w.invalidate()
		return 0
	case wmHotkey:
		switch wp {
		case hotkeyRun:
			go w.ctl.ToggleRun()
		case hotkeyCal:
			go w.ctl.Calibrate()
		}
		return 0
	case wmExitSizeMove:
		w.savePos()
		return 0
	case wmDpiChanged:
		rc := (*rect)(ptr(lp))
		pt := point{(rc.left + rc.right) / 2, (rc.top + rc.bottom) / 2}
		mon := call(pMonitorFromPoint, uintptr(*(*int64)(unsafe.Pointer(&pt))), monitorNearest)
		w.u.SetScale(fitScale(hiword(wp), mon))
		pw, ph := w.u.PixelSize()
		call(pSetWindowPos, hwnd, 0, uintptr(int32(rc.left)), uintptr(int32(rc.top)), uintptr(pw), uintptr(ph), swpNoZOrder|swpNoActivate)
		w.invalidate()
		return 0
	case wmClose:
		if w.fadeOut {
			return 0
		}
		w.savePos()
		if w.layered || call(pIsIconic, hwnd) != 0 {
			call(pDestroyWindow, hwnd)
			return 0
		}
		w.beginFade(true)
		return 0
	case wmDestroy:
		call(pPostQuitMessage, 0)
		return 0
	}
	return call(pDefWindowProcW, hwnd, uintptr(m), wp, lp)
}

const (
	gwlExStyle = ^uintptr(19) // -20
	lwaAlpha   = 2
)

func easeOutCubic(t float64) float64 { t = 1 - t; return 1 - t*t*t }

// beginFade starts the smooth open (out=false) or close (out=true) animation.
// Without layered-window support the window simply appears/closes at once.
func (w *mainWin) beginFade(out bool) {
	if !out {
		ex := call(pGetWindowLongPtrW, w.hwnd, gwlExStyle)
		call(pSetWindowLongPtrW, w.hwnd, gwlExStyle, ex|wsExLayered)
		if call(pSetLayeredAttrs, w.hwnd, 0, 0, lwaAlpha) == 0 {
			call(pSetWindowLongPtrW, w.hwnd, gwlExStyle, ex)
			return
		}
		w.layered = true
		var rc rect
		call(pGetWindowRect, w.hwnd, uintptr(unsafe.Pointer(&rc)))
		w.baseX, w.baseY = int(rc.left), int(rc.top)
		call(pSetWindowPos, w.hwnd, 0, uintptr(int32(w.baseX)), uintptr(int32(w.baseY+slidePx)), 0, 0, swpNoSize|swpNoZOrder|swpNoActivate)
	} else {
		var rc rect
		call(pGetWindowRect, w.hwnd, uintptr(unsafe.Pointer(&rc)))
		w.baseX, w.baseY = int(rc.left), int(rc.top)
		ex := call(pGetWindowLongPtrW, w.hwnd, gwlExStyle)
		call(pSetWindowLongPtrW, w.hwnd, gwlExStyle, ex|wsExLayered)
		call(pSetLayeredAttrs, w.hwnd, 0, 255, lwaAlpha)
		w.layered = true
	}
	w.fadeOut = out
	w.fading = true
	w.plat.selfFading.Store(true)
	w.fadeStart = time.Now()
	call(pSetTimer, w.hwnd, timerFade, animPeriod, 0)
}

func (w *mainWin) stepFade() {
	if !w.fading {
		call(pKillTimer, w.hwnd, timerFade)
		w.plat.selfFading.Store(false)
		return
	}
	dur := float64(fadeInMs)
	if w.fadeOut {
		dur = fadeOutMs
	}
	t := time.Since(w.fadeStart).Seconds() * 1000 / dur
	if t >= 1 {
		w.fading = false
		w.plat.selfFading.Store(false)
		call(pKillTimer, w.hwnd, timerFade)
		if w.fadeOut {
			call(pDestroyWindow, w.hwnd)
			return
		}
		// back to a normal window (best rendering and shadow)
		call(pSetLayeredAttrs, w.hwnd, 0, 255, lwaAlpha)
		ex := call(pGetWindowLongPtrW, w.hwnd, gwlExStyle)
		call(pSetWindowLongPtrW, w.hwnd, gwlExStyle, ex&^uintptr(wsExLayered))
		call(pSetWindowPos, w.hwnd, 0, uintptr(int32(w.baseX)), uintptr(int32(w.baseY)), 0, 0,
			swpNoSize|swpNoZOrder|swpNoActivate|swpFrameChanged)
		w.layered = false
		w.invalidate()
		return
	}
	var a, off float64
	if w.fadeOut {
		a = 1 - t
		off = -6 * t
	} else {
		e := easeOutCubic(t)
		a = e
		off = float64(slidePx) * (1 - e)
	}
	call(pSetLayeredAttrs, w.hwnd, 0, uintptr(a*255+0.5), lwaAlpha)
	call(pSetWindowPos, w.hwnd, 0, uintptr(int32(w.baseX)), uintptr(int32(float64(w.baseY)+off)), 0, 0,
		swpNoSize|swpNoZOrder|swpNoActivate)
}

// render draws one frame. A failure inside the interface code must never leave
// a half-painted or frozen window: it is logged and the frame is still shown.
func (w *mainWin) render() {
	defer func() {
		if r := recover(); r != nil {
			if CrashLog != nil {
				CrashLog(r)
			}
			w.canvas.ResetClip()
			w.u.Recover()
		}
	}()
	w.u.Render(w.canvas)
}
