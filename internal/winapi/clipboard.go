//go:build windows

package winapi

import (
	"errors"
	"syscall"
	"time"
	"unsafe"
)

func openClipboard() error {
	for i := 0; i < 20; i++ {
		if call(pOpenClipboard, 0) != 0 {
			return nil
		}
		time.Sleep(15 * time.Millisecond)
	}
	return errors.New("буфер обмена занят другим приложением")
}

// GetClipboard returns the clipboard text.
func (p *Platform) GetClipboard() (string, error) {
	if err := openClipboard(); err != nil {
		return "", err
	}
	defer call(pCloseClipboard)
	h := call(pGetClipboardData, cfUnicodeText)
	if h == 0 {
		return "", nil
	}
	a := call(pGlobalLock, h)
	if a == 0 {
		return "", errors.New("GlobalLock не удался")
	}
	defer call(pGlobalUnlock, h)
	base := ptr(a)
	var buf []uint16
	for i := 0; i < 1<<20; i++ {
		c := *(*uint16)(unsafe.Add(base, i*2))
		if c == 0 {
			break
		}
		buf = append(buf, c)
	}
	return syscall.UTF16ToString(buf), nil
}

// SetClipboard replaces the clipboard text.
func (p *Platform) SetClipboard(s string) error {
	u, err := syscall.UTF16FromString(s)
	if err != nil {
		return err
	}
	if err := openClipboard(); err != nil {
		return err
	}
	defer call(pCloseClipboard)
	h := call(pGlobalAlloc, gmemMoveable, uintptr(len(u)*2))
	if h == 0 {
		return errors.New("GlobalAlloc не удался")
	}
	a := call(pGlobalLock, h)
	if a == 0 {
		call(pGlobalFree, h)
		return errors.New("GlobalLock не удался")
	}
	dst := unsafe.Slice((*uint16)(ptr(a)), len(u))
	copy(dst, u)
	call(pGlobalUnlock, h)
	call(pEmptyClipboard)
	if call(pSetClipboardData, cfUnicodeText, h) == 0 {
		call(pGlobalFree, h)
		return errors.New("SetClipboardData не удался")
	}
	return nil // the clipboard owns h now
}
