//go:build windows

package winapi

import (
	"image"
	"runtime"
	"sync"
	"unsafe"
)

const overlayClass = "ChessVisionOverlay"

type ovCmd struct {
	rect image.Rectangle
	img  *image.RGBA
	hide bool
}

// overlay is a click-through, always-on-top, per-pixel-alpha window living on
// its own OS thread.
type overlay struct {
	once  sync.Once
	ready chan struct{}
	mu    sync.Mutex
	pend  *ovCmd
	hwnd  uintptr
	d     *dib
	shown bool
	fail  bool
}

func (o *overlay) start() {
	o.once.Do(func() {
		o.ready = make(chan struct{})
		go func() {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			if err := registerClass(overlayClass, 0, loadCursor(idcArrow), 0); err != nil {
				o.fail = true
				close(o.ready)
				return
			}
			hw, err := createWindow(wsExLayered|wsExTransparent|wsExTopmost|wsExToolWindow|wsExNoActivate,
				overlayClass, "", wsPopup, 0, 0, 8, 8)
			if err != nil {
				o.fail = true
				close(o.ready)
				return
			}
			o.hwnd = hw
			handlers.Store(hw, o)
			close(o.ready)
			pump()
		}()
	})
	<-o.ready
}

func (o *overlay) post(c ovCmd) {
	o.start()
	if o.fail {
		return
	}
	o.mu.Lock()
	o.pend = &c
	o.mu.Unlock()
	call(pPostMessageW, o.hwnd, wmApp, 0, 0)
}

func (o *overlay) proc(hwnd uintptr, m uint32, wp, lp uintptr) uintptr {
	if m == wmApp {
		o.mu.Lock()
		c := o.pend
		o.pend = nil
		o.mu.Unlock()
		if c != nil {
			o.apply(hwnd, c)
		}
		return 0
	}
	return call(pDefWindowProcW, hwnd, uintptr(m), wp, lp)
}

func (o *overlay) apply(hwnd uintptr, c *ovCmd) {
	if c.hide || c.img == nil {
		if o.shown {
			call(pShowWindow, hwnd, swHide)
			o.shown = false
		}
		return
	}
	w, h := c.img.Bounds().Dx(), c.img.Bounds().Dy()
	if w <= 0 || h <= 0 {
		return
	}
	if o.d == nil || o.d.w != w || o.d.h != h {
		o.d.free()
		o.d = newDIB(0, w, h)
		if o.d == nil {
			return
		}
	}
	o.d.setRGBA(c.img, false) // premultiplied BGRA, as UpdateLayeredWindow expects
	pos := point{int32(c.rect.Min.X), int32(c.rect.Min.Y)}
	sz := size{int32(w), int32(h)}
	src := point{}
	bf := blendFunction{op: acSrcOver, alpha: 255, format: acSrcAlpha}
	call(pUpdateLayeredWindow, hwnd, 0, uintptr(unsafe.Pointer(&pos)), uintptr(unsafe.Pointer(&sz)),
		o.d.dc, uintptr(unsafe.Pointer(&src)), 0, uintptr(unsafe.Pointer(&bf)), ulwAlpha)
	if !o.shown {
		call(pSetWindowPos, hwnd, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
		call(pShowWindow, hwnd, swShowNoActive)
		o.shown = true
	}
}

// ShowOverlay displays img exactly over r (screen pixels).
func (p *Platform) ShowOverlay(r image.Rectangle, img *image.RGBA) {
	p.ov.post(ovCmd{rect: r, img: img})
}

// HideOverlay hides the arrow window.
func (p *Platform) HideOverlay() {
	if p.ov.ready == nil { // never shown
		return
	}
	p.ov.post(ovCmd{hide: true})
}
