//go:build windows

package winapi

import (
	"errors"
	"image"
)

// Capture grabs a rectangle of the virtual screen (physical pixels).
// CAPTUREBLT makes layered windows (our own arrow overlay) part of the
// picture; the controller removes it again ("unblend").
func (p *Platform) Capture(r image.Rectangle) (*image.RGBA, error) {
	return captureScreen(r)
}

func captureScreen(r image.Rectangle) (*image.RGBA, error) {
	r = r.Canon()
	if r.Dx() <= 0 || r.Dy() <= 0 {
		return nil, errors.New("пустая область захвата")
	}
	screen := call(pGetDC, 0)
	if screen == 0 {
		return nil, errors.New("GetDC не удался")
	}
	defer call(pReleaseDC, 0, screen)
	d := newDIB(screen, r.Dx(), r.Dy())
	if d == nil {
		return nil, errors.New("не удалось создать буфер кадра")
	}
	defer d.free()
	if call(pBitBlt, d.dc, 0, 0, uintptr(r.Dx()), uintptr(r.Dy()), screen, uintptr(int32(r.Min.X)), uintptr(int32(r.Min.Y)), srcCopy|captureBlt) == 0 {
		return nil, errors.New("BitBlt не удался (экран заблокирован или защищён?)")
	}
	call(pGdiFlush)
	return d.toRGBA(), nil
}
