//go:build windows

package winapi

import (
	"image"
	"sync/atomic"
	"unsafe"
)

// Platform implements app.Platform on Windows.
type Platform struct {
	ov       overlay
	mainHwnd atomic.Uintptr
	// selfFading is set while the window is fading in or out.
	selfFading atomic.Bool
}

// NewPlatform creates the Windows platform.
func NewPlatform() *Platform { return &Platform{} }

// ScreenBounds is the whole virtual screen (all monitors).
func (p *Platform) ScreenBounds() image.Rectangle { return virtualScreen() }

// OwnRects lists our own visible windows, so the board finder can ignore the
// board drawn in our own interface.
func (p *Platform) OwnRects() []image.Rectangle {
	h := p.mainHwnd.Load()
	if h == 0 || call(pIsWindowVisible, h) == 0 || call(pIsIconic, h) != 0 {
		return nil
	}
	var rc rect
	call(pGetWindowRect, h, uintptr(unsafe.Pointer(&rc)))
	return []image.Rectangle{image.Rect(int(rc.left), int(rc.top), int(rc.right), int(rc.bottom))}
}

// SetSelfVisible makes our own window fully transparent (false) or visible
// again (true). Calibration uses it so that the screenshot shows the real
// board even when our window lies over it, without a taskbar flicker.
func (p *Platform) SetSelfVisible(v bool) {
	h := p.mainHwnd.Load()
	if h == 0 || p.selfFading.Load() {
		return
	}
	ex := call(pGetWindowLongPtrW, h, gwlExStyle)
	if !v {
		call(pSetWindowLongPtrW, h, gwlExStyle, ex|wsExLayered)
		call(pSetLayeredAttrs, h, 0, 0, lwaAlpha)
		return
	}
	call(pSetLayeredAttrs, h, 0, 255, lwaAlpha)
	call(pSetWindowLongPtrW, h, gwlExStyle, ex&^uintptr(wsExLayered))
	call(pSetWindowPos, h, 0, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoZOrder|swpNoActivate|swpFrameChanged)
	call(pInvalidateRect, h, 0, 0)
}

// MoveAway moves our window beside the board when it lies over it. It reports
// whether the window no longer covers the board (false: there is no free room
// on that monitor).
func (p *Platform) MoveAway(board image.Rectangle) bool {
	h := p.mainHwnd.Load()
	if h == 0 || call(pIsIconic, h) != 0 {
		return true
	}
	var rc rect
	call(pGetWindowRect, h, uintptr(unsafe.Pointer(&rc)))
	win := image.Rect(int(rc.left), int(rc.top), int(rc.right), int(rc.bottom))
	if !win.Overlaps(board) {
		return true
	}
	pt := point{int32((board.Min.X + board.Max.X) / 2), int32((board.Min.Y + board.Max.Y) / 2)}
	mon := call(pMonitorFromPoint, uintptr(*(*int64)(unsafe.Pointer(&pt))), monitorNearest)
	wk := monitorWork(mon)
	work := image.Rect(int(wk.left), int(wk.top), int(wk.right), int(wk.bottom))
	ww, wh := win.Dx(), win.Dy()
	const gap = 12
	clamp := func(v, lo, hi int) int {
		if hi < lo {
			return lo
		}
		return min(max(v, lo), hi)
	}
	cands := []image.Point{
		{board.Max.X + gap, clamp(win.Min.Y, work.Min.Y, work.Max.Y-wh)},
		{board.Min.X - gap - ww, clamp(win.Min.Y, work.Min.Y, work.Max.Y-wh)},
		{clamp(win.Min.X, work.Min.X, work.Max.X-ww), board.Max.Y + gap},
		{clamp(win.Min.X, work.Min.X, work.Max.X-ww), board.Min.Y - gap - wh},
	}
	best, bestD := image.Point{}, -1
	for _, c := range cands {
		r := image.Rect(c.X, c.Y, c.X+ww, c.Y+wh)
		if !r.In(work) || r.Overlaps(board) {
			continue
		}
		d := abs(c.X-win.Min.X) + abs(c.Y-win.Min.Y)
		if bestD < 0 || d < bestD {
			best, bestD = c, d
		}
	}
	if bestD < 0 {
		return false
	}
	call(pSetWindowPos, h, 0, uintptr(int32(best.X)), uintptr(int32(best.Y)), 0, 0, swpNoSize|swpNoZOrder|swpNoActivate)
	return true
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
