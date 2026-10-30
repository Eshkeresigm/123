//go:build windows

package winapi

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// handler receives the messages of one window.
type handler interface {
	proc(hwnd uintptr, m uint32, wp, lp uintptr) uintptr
}

var (
	handlers sync.Map // hwnd -> handler
	procOnce sync.Once
	procCB   uintptr
	// CrashLog, when set, receives panics raised inside window procedures.
	CrashLog func(v any)
)

func wndProc(hwnd, m, wp, lp uintptr) (ret uintptr) {
	defer func() {
		if r := recover(); r != nil {
			if CrashLog != nil {
				CrashLog(r)
			}
			ret = call(pDefWindowProcW, hwnd, m, wp, lp)
		}
	}()
	if h, ok := handlers.Load(hwnd); ok {
		return h.(handler).proc(hwnd, uint32(m), wp, lp)
	}
	return call(pDefWindowProcW, hwnd, m, wp, lp)
}

func callback() uintptr {
	procOnce.Do(func() { procCB = syscall.NewCallback(wndProc) })
	return procCB
}

func hInstance() uintptr { return call(pGetModuleHandleW, 0) }

func registerClass(name string, style uint32, cursor, icon uintptr) error {
	wc := wndClassEx{
		cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		style:         style,
		lpfnWndProc:   callback(),
		hInstance:     hInstance(),
		hIcon:         icon,
		hIconSm:       icon,
		hCursor:       cursor,
		lpszClassName: u16(name),
	}
	if call(pRegisterClassExW, uintptr(unsafe.Pointer(&wc))) == 0 {
		return fmt.Errorf("RegisterClassEx(%s): %w", name, syscall.GetLastError())
	}
	return nil
}

func createWindow(exStyle uint32, class, title string, style uint32, x, y, w, h int) (uintptr, error) {
	hw := call(pCreateWindowExW, uintptr(exStyle), uintptr(unsafe.Pointer(u16(class))), uintptr(unsafe.Pointer(u16(title))),
		uintptr(style), uintptr(int32(x)), uintptr(int32(y)), uintptr(int32(w)), uintptr(int32(h)), 0, 0, hInstance(), 0)
	if hw == 0 {
		return 0, fmt.Errorf("CreateWindowEx(%s): %w", class, syscall.GetLastError())
	}
	return hw, nil
}

func loadCursor(id uintptr) uintptr { return call(pLoadCursorW, 0, id) }

// pump runs a message loop until WM_QUIT.
func pump() {
	var m msg
	for {
		r := int32(call(pGetMessageW, uintptr(unsafe.Pointer(&m)), 0, 0, 0))
		if r <= 0 {
			return
		}
		call(pTranslateMessage, uintptr(unsafe.Pointer(&m)))
		call(pDispatchMessageW, uintptr(unsafe.Pointer(&m)))
	}
}

// EnableDPIAwareness must run before any window is created.
func EnableDPIAwareness() {
	if pSetProcessDpiAwareCtx.Find() == nil {
		if call(pSetProcessDpiAwareCtx, dpiCtxPerMon2) != 0 {
			return
		}
	}
	if pSetProcessDPIAware.Find() == nil {
		call(pSetProcessDPIAware)
	}
}

// AcquireSingleInstance returns false when another copy is already running
// (and brings that copy to the front).
func AcquireSingleInstance() bool {
	call(pCreateMutexW, 0, 1, uintptr(unsafe.Pointer(u16("Local\\ChessVision.Singleton.v1"))))
	if call(pGetLastError) == errAlreadyExist {
		if hw := call(pFindWindowW, uintptr(unsafe.Pointer(u16(mainClass))), 0); hw != 0 {
			call(pShowWindow, hw, swRestore)
			call(pSetForegroundWindow, hw)
		}
		return false
	}
	return true
}

// WriteCrash appends a crash report next to the settings.
func WriteCrash(dir string, v any, stack []byte) {
	_ = os.MkdirAll(dir, 0o755)
	f, err := os.OpenFile(filepath.Join(dir, "crash.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "=== %s ===\n%v\n%s\n", time.Now().Format(time.RFC3339), v, stack)
}
