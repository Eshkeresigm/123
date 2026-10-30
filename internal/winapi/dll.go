//go:build windows

// Package winapi is the thin Win32 layer of ChessVision: main window, screen
// capture, region picker, click-through overlay and clipboard. It uses only
// the standard library (syscall) and implements app.Platform.
package winapi

import (
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	dwmapi   = syscall.NewLazyDLL("dwmapi.dll")
)

var (
	pRegisterClassExW       = user32.NewProc("RegisterClassExW")
	pCreateWindowExW        = user32.NewProc("CreateWindowExW")
	pDestroyWindow          = user32.NewProc("DestroyWindow")
	pDefWindowProcW         = user32.NewProc("DefWindowProcW")
	pGetMessageW            = user32.NewProc("GetMessageW")
	pTranslateMessage       = user32.NewProc("TranslateMessage")
	pDispatchMessageW       = user32.NewProc("DispatchMessageW")
	pPostMessageW           = user32.NewProc("PostMessageW")
	pPostQuitMessage        = user32.NewProc("PostQuitMessage")
	pShowWindow             = user32.NewProc("ShowWindow")
	pSetLayeredAttrs        = user32.NewProc("SetLayeredWindowAttributes")
	pGetWindowLongPtrW      = user32.NewProc("GetWindowLongPtrW")
	pSetWindowLongPtrW      = user32.NewProc("SetWindowLongPtrW")
	pSetWindowPos           = user32.NewProc("SetWindowPos")
	pGetWindowRect          = user32.NewProc("GetWindowRect")
	pInvalidateRect         = user32.NewProc("InvalidateRect")
	pBeginPaint             = user32.NewProc("BeginPaint")
	pEndPaint               = user32.NewProc("EndPaint")
	pGetDC                  = user32.NewProc("GetDC")
	pReleaseDC              = user32.NewProc("ReleaseDC")
	pSetTimer               = user32.NewProc("SetTimer")
	pKillTimer              = user32.NewProc("KillTimer")
	pSetCapture             = user32.NewProc("SetCapture")
	pReleaseCapture         = user32.NewProc("ReleaseCapture")
	pTrackMouseEvent        = user32.NewProc("TrackMouseEvent")
	pLoadCursorW            = user32.NewProc("LoadCursorW")
	pSetCursor              = user32.NewProc("SetCursor")
	pRegisterHotKey         = user32.NewProc("RegisterHotKey")
	pUnregisterHotKey       = user32.NewProc("UnregisterHotKey")
	pGetSystemMetrics       = user32.NewProc("GetSystemMetrics")
	pMonitorFromPoint       = user32.NewProc("MonitorFromPoint")
	pMonitorFromWindow      = user32.NewProc("MonitorFromWindow")
	pGetMonitorInfoW        = user32.NewProc("GetMonitorInfoW")
	pGetDpiForWindow        = user32.NewProc("GetDpiForWindow")
	pSetProcessDpiAwareCtx  = user32.NewProc("SetProcessDpiAwarenessContext")
	pSetProcessDPIAware     = user32.NewProc("SetProcessDPIAware")
	pFindWindowW            = user32.NewProc("FindWindowW")
	pSetForegroundWindow    = user32.NewProc("SetForegroundWindow")
	pSetFocus               = user32.NewProc("SetFocus")
	pIsWindow               = user32.NewProc("IsWindow")
	pIsWindowVisible        = user32.NewProc("IsWindowVisible")
	pIsIconic               = user32.NewProc("IsIconic")
	pUpdateLayeredWindow    = user32.NewProc("UpdateLayeredWindow")
	pOpenClipboard          = user32.NewProc("OpenClipboard")
	pCloseClipboard         = user32.NewProc("CloseClipboard")
	pEmptyClipboard         = user32.NewProc("EmptyClipboard")
	pGetClipboardData       = user32.NewProc("GetClipboardData")
	pSetClipboardData       = user32.NewProc("SetClipboardData")
	pGetAsyncKeyState       = user32.NewProc("GetAsyncKeyState")
	pMessageBoxW            = user32.NewProc("MessageBoxW")
	pFillRect               = user32.NewProc("FillRect")
	pCreateIconIndirect     = user32.NewProc("CreateIconIndirect")
	pSendMessageW           = user32.NewProc("SendMessageW")
	pGetCursorPos           = user32.NewProc("GetCursorPos")
	pCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	pDeleteDC               = gdi32.NewProc("DeleteDC")
	pCreateDIBSection       = gdi32.NewProc("CreateDIBSection")
	pCreateBitmap           = gdi32.NewProc("CreateBitmap")
	pSelectObject           = gdi32.NewProc("SelectObject")
	pDeleteObject           = gdi32.NewProc("DeleteObject")
	pBitBlt                 = gdi32.NewProc("BitBlt")
	pGdiFlush               = gdi32.NewProc("GdiFlush")
	pCreateSolidBrush       = gdi32.NewProc("CreateSolidBrush")
	pSetBkMode              = gdi32.NewProc("SetBkMode")
	pSetTextColor           = gdi32.NewProc("SetTextColor")
	pTextOutW               = gdi32.NewProc("TextOutW")
	pGetTextExtentPoint32W  = gdi32.NewProc("GetTextExtentPoint32W")
	pGetStockObject         = gdi32.NewProc("GetStockObject")
	pGetDeviceCaps          = gdi32.NewProc("GetDeviceCaps")
	pGetModuleHandleW       = kernel32.NewProc("GetModuleHandleW")
	pGlobalAlloc            = kernel32.NewProc("GlobalAlloc")
	pGlobalLock             = kernel32.NewProc("GlobalLock")
	pGlobalUnlock           = kernel32.NewProc("GlobalUnlock")
	pGlobalFree             = kernel32.NewProc("GlobalFree")
	pCreateMutexW           = kernel32.NewProc("CreateMutexW")
	pGetLastError           = kernel32.NewProc("GetLastError")
	pDwmSetWindowAttribute  = dwmapi.NewProc("DwmSetWindowAttribute")
	pDwmExtendFrameIntoClnt = dwmapi.NewProc("DwmExtendFrameIntoClientArea")
)

// Window messages and constants used by the package.
const (
	wmDestroy       = 0x0002
	wmClose         = 0x0010
	wmPaint         = 0x000F
	wmEraseBkgnd    = 0x0014
	wmSetCursor     = 0x0020
	wmNCCalcSize    = 0x0083
	wmNCHitTest     = 0x0084
	wmKeyDown       = 0x0100
	wmTimer         = 0x0113
	wmHotkey        = 0x0312
	wmMouseMove     = 0x0200
	wmLButtonDown   = 0x0201
	wmLButtonUp     = 0x0202
	wmRButtonDown   = 0x0204
	wmMouseLeave    = 0x02A3
	wmExitSizeMove  = 0x0232
	wmDpiChanged    = 0x02E0
	wmApp           = 0x8000
	htClient        = 1
	htCaption       = 2
	swHide          = 0
	swShowNoActive  = 4
	swMinimize      = 6
	swRestore       = 9
	swShow          = 5
	wsPopup         = 0x80000000
	wsThickFrame    = 0x00040000
	wsMinimizeBox   = 0x00020000
	wsExTopmost     = 0x00000008
	wsExToolWindow  = 0x00000080
	wsExTransparent = 0x00000020
	wsExLayered     = 0x00080000
	wsExNoActivate  = 0x08000000
	swpNoSize       = 0x0001
	swpNoMove       = 0x0002
	swpNoZOrder     = 0x0004
	swpNoActivate   = 0x0010
	swpFrameChanged = 0x0020
	srcCopy         = 0x00CC0020
	captureBlt      = 0x40000000
	biRGB           = 0
	dibRGBColors    = 0
	csHRedraw       = 0x0002
	csVRedraw       = 0x0001
	idcArrow        = 32512
	idcCross        = 32515
	idcHand         = 32649
	smXVirtual      = 76
	smYVirtual      = 77
	smCXVirtual     = 78
	smCYVirtual     = 79
	smCXScreen      = 0
	smCYScreen      = 1
	vkEscape        = 0x1B
	vkF8            = 0x77
	vkF9            = 0x78
	cfUnicodeText   = 13
	gmemMoveable    = 0x0002
	tmeLeave        = 0x00000002
	ulwAlpha        = 0x00000002
	acSrcOver       = 0
	acSrcAlpha      = 1
	transparentBk   = 1
	monitorNearest  = 2
	monitorNull     = 0
	logPixelsX      = 88
	wmSetIcon       = 0x0080
	iconSmall       = 0
	iconBig         = 1
	mbIconError     = 0x10
	errAlreadyExist = 183
)

var (
	hwndTopmost   = ^uintptr(0) // (HWND)-1
	hwndNoTopmost = ^uintptr(1) // (HWND)-2
	dpiCtxPerMon2 = ^uintptr(3) // (DPI_AWARENESS_CONTEXT)-4
)

type point struct{ x, y int32 }
type rect struct{ left, top, right, bottom int32 }
type size struct{ cx, cy int32 }
type margins struct{ l, r, t, b int32 }

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
	priv    uint32
}

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type bitmapInfoHeader struct {
	size          uint32
	width, height int32
	planes        uint16
	bitCount      uint16
	compression   uint32
	sizeImage     uint32
	xppm, yppm    int32
	clrUsed       uint32
	clrImportant  uint32
}

type blendFunction struct{ op, flags, alpha, format byte }

type paintStruct struct {
	hdc      uintptr
	erase    int32
	rcPaint  rect
	restore  int32
	incUpd   int32
	reserved [32]byte
}

type trackMouseEvent struct {
	cbSize    uint32
	flags     uint32
	hwndTrack uintptr
	hover     uint32
}

type iconInfo struct {
	fIcon    int32
	xHot     uint32
	yHot     uint32
	hbmMask  uintptr
	hbmColor uintptr
}

type monitorInfo struct {
	cbSize  uint32
	monitor rect
	work    rect
	flags   uint32
}

// ptr converts an address returned by the OS to an unsafe.Pointer without
// tripping go vet's uintptr→Pointer check.
func ptr(u uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&u)) }

func u16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func call(p *syscall.LazyProc, a ...uintptr) uintptr {
	r, _, _ := p.Call(a...)
	return r
}

func hiword(v uintptr) int { return int(uint16(v >> 16)) }
func loword(v uintptr) int { return int(uint16(v)) }

// MessageBox shows a simple error dialog.
func MessageBox(title, text string) {
	call(pMessageBoxW, 0, uintptr(unsafe.Pointer(u16(text))), uintptr(unsafe.Pointer(u16(title))), mbIconError)
}
