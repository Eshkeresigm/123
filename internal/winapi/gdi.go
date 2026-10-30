//go:build windows

package winapi

import (
	"image"
	"unsafe"
)

// dib is a 32-bit top-down DIB section selected into a memory DC.
type dib struct {
	dc, bmp, old uintptr
	pix          []byte
	w, h         int
}

func newDIB(ref uintptr, w, h int) *dib {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	dc := call(pCreateCompatibleDC, ref)
	bi := bitmapInfoHeader{size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), width: int32(w), height: int32(-h), planes: 1, bitCount: 32, compression: biRGB}
	var bits unsafe.Pointer
	bmp := call(pCreateDIBSection, dc, uintptr(unsafe.Pointer(&bi)), dibRGBColors, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == nil {
		call(pDeleteDC, dc)
		return nil
	}
	old := call(pSelectObject, dc, bmp)
	return &dib{dc: dc, bmp: bmp, old: old, pix: unsafe.Slice((*byte)(bits), w*h*4), w: w, h: h}
}

func (d *dib) free() {
	if d == nil {
		return
	}
	call(pSelectObject, d.dc, d.old)
	call(pDeleteObject, d.bmp)
	call(pDeleteDC, d.dc)
	d.pix = nil
}

// setRGBA copies an RGBA image (same size) into the DIB as BGRA. When opaque
// is true alpha is forced to 255, otherwise premultiplied alpha is kept.
func (d *dib) setRGBA(img *image.RGBA, opaque bool) {
	src := img.Pix
	n := len(d.pix)
	if len(src) < n {
		n = len(src)
	}
	dst := d.pix
	if opaque {
		for i := 0; i+3 < n; i += 4 {
			dst[i], dst[i+1], dst[i+2], dst[i+3] = src[i+2], src[i+1], src[i], 255
		}
		return
	}
	for i := 0; i+3 < n; i += 4 {
		dst[i], dst[i+1], dst[i+2], dst[i+3] = src[i+2], src[i+1], src[i], src[i+3]
	}
}

// toRGBA returns the DIB content as an opaque RGBA image.
func (d *dib) toRGBA() *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, d.w, d.h))
	src, dst := d.pix, out.Pix
	for i := 0; i+3 < len(src); i += 4 {
		dst[i], dst[i+1], dst[i+2], dst[i+3] = src[i+2], src[i+1], src[i], 255
	}
	return out
}

func virtualScreen() image.Rectangle {
	x := int(int32(call(pGetSystemMetrics, smXVirtual)))
	y := int(int32(call(pGetSystemMetrics, smYVirtual)))
	w := int(int32(call(pGetSystemMetrics, smCXVirtual)))
	h := int(int32(call(pGetSystemMetrics, smCYVirtual)))
	if w <= 0 || h <= 0 {
		w = int(int32(call(pGetSystemMetrics, smCXScreen)))
		h = int(int32(call(pGetSystemMetrics, smCYScreen)))
		x, y = 0, 0
	}
	return image.Rect(x, y, x+w, y+h)
}
