//go:build windows

package winapi

import (
	"errors"
	"image"
	"runtime"
	"time"
	"unsafe"
)

const pickClass = "ChessVisionPicker"

type picker struct {
	hwnd             uintptr
	virt             image.Rectangle
	orig, dim, back  *dib
	dragging         bool
	start, cur       image.Point
	done, ok         bool
	result           image.Rectangle
	font             uintptr
	accent, darkBrsh uintptr
}

func (pk *picker) selRect() image.Rectangle {
	return image.Rectangle{Min: pk.start, Max: pk.cur}.Canon()
}

func (pk *picker) finish(ok bool) {
	if pk.done {
		return
	}
	pk.done, pk.ok = true, ok
	call(pDestroyWindow, pk.hwnd)
}

func (pk *picker) proc(hwnd uintptr, m uint32, wp, lp uintptr) uintptr {
	switch m {
	case wmEraseBkgnd:
		return 1
	case wmSetCursor:
		call(pSetCursor, loadCursor(idcCross))
		return 1
	case wmTimer:
		if call(pGetAsyncKeyState, vkEscape)&0x8000 != 0 {
			pk.finish(false)
		}
		return 0
	case wmKeyDown:
		if wp == vkEscape {
			pk.finish(false)
		}
		return 0
	case wmRButtonDown:
		pk.finish(false)
		return 0
	case wmLButtonDown:
		pk.start = image.Pt(int(int16(lp)), int(int16(lp>>16)))
		pk.cur = pk.start
		pk.dragging = true
		call(pSetCapture, hwnd)
		call(pInvalidateRect, hwnd, 0, 0)
		return 0
	case wmMouseMove:
		if pk.dragging {
			pk.cur = image.Pt(int(int16(lp)), int(int16(lp>>16)))
			call(pInvalidateRect, hwnd, 0, 0)
		}
		return 0
	case wmLButtonUp:
		if !pk.dragging {
			return 0
		}
		pk.dragging = false
		call(pReleaseCapture)
		pk.cur = image.Pt(int(int16(lp)), int(int16(lp>>16)))
		r := pk.selRect().Intersect(image.Rect(0, 0, pk.virt.Dx(), pk.virt.Dy()))
		if r.Dx() < 32 || r.Dy() < 32 { // accidental click: keep waiting
			call(pInvalidateRect, hwnd, 0, 0)
			return 0
		}
		pk.result = r.Add(pk.virt.Min)
		pk.finish(true)
		return 0
	case wmPaint:
		var ps paintStruct
		hdc := call(pBeginPaint, hwnd, uintptr(unsafe.Pointer(&ps)))
		pk.paint(hdc)
		call(pEndPaint, hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case wmDestroy:
		call(pPostQuitMessage, 0)
		return 0
	}
	return call(pDefWindowProcW, hwnd, uintptr(m), wp, lp)
}

func fillRect(dc uintptr, brush uintptr, x, y, w, h int) {
	rc := rect{int32(x), int32(y), int32(x + w), int32(y + h)}
	call(pFillRect, dc, uintptr(unsafe.Pointer(&rc)), brush)
}

func (pk *picker) label(dc uintptr, text string, cx, top int) {
	s := utf16(text)
	call(pSelectObject, dc, pk.font)
	var sz size
	call(pGetTextExtentPoint32W, dc, uintptr(unsafe.Pointer(&s[0])), uintptr(len(s)), uintptr(unsafe.Pointer(&sz)))
	w, h := int(sz.cx)+28, int(sz.cy)+14
	x := cx - w/2
	fillRect(dc, pk.accent, x-1, top-1, w+2, h+2)
	fillRect(dc, pk.darkBrsh, x, top, w, h)
	call(pSetBkMode, dc, transparentBk)
	call(pSetTextColor, dc, 0x00FFFFFF)
	call(pTextOutW, dc, uintptr(x+14), uintptr(top+7), uintptr(unsafe.Pointer(&s[0])), uintptr(len(s)))
}

func (pk *picker) paint(hdc uintptr) {
	w, h := pk.virt.Dx(), pk.virt.Dy()
	dc := pk.back.dc
	call(pBitBlt, dc, 0, 0, uintptr(w), uintptr(h), pk.dim.dc, 0, 0, srcCopy)
	if pk.dragging {
		r := pk.selRect().Intersect(image.Rect(0, 0, w, h))
		if !r.Empty() {
			call(pBitBlt, dc, uintptr(r.Min.X), uintptr(r.Min.Y), uintptr(r.Dx()), uintptr(r.Dy()), pk.orig.dc, uintptr(r.Min.X), uintptr(r.Min.Y), srcCopy)
			const t = 2
			fillRect(dc, pk.accent, r.Min.X-t, r.Min.Y-t, r.Dx()+2*t, t)
			fillRect(dc, pk.accent, r.Min.X-t, r.Max.Y, r.Dx()+2*t, t)
			fillRect(dc, pk.accent, r.Min.X-t, r.Min.Y, t, r.Dy())
			fillRect(dc, pk.accent, r.Max.X, r.Min.Y, t, r.Dy())
			ly := r.Max.Y + 12
			if ly+40 > h {
				ly = r.Min.Y - 44
			}
			pk.label(dc, itoa(r.Dx())+" × "+itoa(r.Dy()), (r.Min.X+r.Max.X)/2, ly)
		}
	}
	// hint on the primary monitor (virtual origin offset)
	px := int(int32(call(pGetSystemMetrics, smCXScreen)))/2 - pk.virt.Min.X
	pk.label(dc, "Выделите доску мышью  ·  Esc или правая кнопка — отмена", px, 28-pk.virt.Min.Y)
	call(pBitBlt, hdc, 0, 0, uintptr(w), uintptr(h), dc, 0, 0, srcCopy)
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}

func utf16(s string) []uint16 {
	r := []rune(s)
	out := make([]uint16, 0, len(r)+1)
	for _, c := range r {
		if c >= 0x10000 {
			c -= 0x10000
			out = append(out, uint16(0xD800+(c>>10)), uint16(0xDC00+(c&0x3FF)))
		} else {
			out = append(out, uint16(c))
		}
	}
	return out
}

var pickRegistered bool

// PickRegion lets the user drag a rectangle over a frozen, dimmed screenshot.
func (p *Platform) PickRegion() (image.Rectangle, bool, error) {
	type res struct {
		r   image.Rectangle
		ok  bool
		err error
	}
	ch := make(chan res, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		r, ok, err := p.pick()
		ch <- res{r, ok, err}
	}()
	x := <-ch
	return x.r, x.ok, x.err
}

func (p *Platform) pick() (image.Rectangle, bool, error) {
	// hide our own window so it is not part of the frozen picture
	mh := p.mainHwnd.Load()
	hidden := false
	if mh != 0 && call(pIsWindowVisible, mh) != 0 {
		call(pShowWindow, mh, swHide)
		hidden = true
	}
	p.HideOverlay()
	if hidden {
		defer func() {
			// give the controller time to take its own capture first
			time.AfterFunc(400*time.Millisecond, func() { call(pShowWindow, mh, swShowNoActive) })
		}()
	}
	time.Sleep(220 * time.Millisecond)

	virt := virtualScreen()
	shot, err := captureScreen(virt)
	if err != nil {
		return image.Rectangle{}, false, err
	}
	pk := &picker{virt: virt}
	pk.orig = newDIB(0, virt.Dx(), virt.Dy())
	pk.dim = newDIB(0, virt.Dx(), virt.Dy())
	pk.back = newDIB(0, virt.Dx(), virt.Dy())
	if pk.orig == nil || pk.dim == nil || pk.back == nil {
		pk.orig.free()
		pk.dim.free()
		pk.back.free()
		return image.Rectangle{}, false, errors.New("не хватает памяти для выбора области")
	}
	defer pk.orig.free()
	defer pk.dim.free()
	defer pk.back.free()
	pk.orig.setRGBA(shot, true)
	for i := 0; i+3 < len(pk.orig.pix); i += 4 {
		pk.dim.pix[i] = byte(int(pk.orig.pix[i]) * 38 / 100)
		pk.dim.pix[i+1] = byte(int(pk.orig.pix[i+1]) * 34 / 100)
		pk.dim.pix[i+2] = byte(int(pk.orig.pix[i+2]) * 32 / 100)
		pk.dim.pix[i+3] = 255
	}
	pk.accent = call(pCreateSolidBrush, 0x00FFC319) // COLORREF 0x00BBGGRR = RGB(25,195,255)
	pk.darkBrsh = call(pCreateSolidBrush, 0x00361F14)
	defer call(pDeleteObject, pk.accent)
	defer call(pDeleteObject, pk.darkBrsh)
	pk.font = call(pGetStockObject, 17) // DEFAULT_GUI_FONT

	if !pickRegistered {
		if err := registerClass(pickClass, csHRedraw|csVRedraw, loadCursor(idcCross), 0); err != nil {
			return image.Rectangle{}, false, err
		}
		pickRegistered = true
	}
	hw, err := createWindow(wsExTopmost|wsExToolWindow, pickClass, "ChessVision", wsPopup,
		virt.Min.X, virt.Min.Y, virt.Dx(), virt.Dy())
	if err != nil {
		return image.Rectangle{}, false, err
	}
	pk.hwnd = hw
	handlers.Store(hw, pk)
	defer handlers.Delete(hw)
	call(pShowWindow, hw, swShow)
	call(pSetForegroundWindow, hw)
	call(pSetFocus, hw)
	call(pSetTimer, hw, 1, 40, 0)
	pump()
	if pk.ok {
		return pk.result, true, nil
	}
	return image.Rectangle{}, false, nil
}
