//go:build windows

// ChessVision: reads a chess board from the screen and suggests the best move.
package main

import (
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"

	"chessvision/internal/app"
	"chessvision/internal/ui"
	"chessvision/internal/winapi"
)

func dirs() (cfg, cache, exe string) {
	exePath, _ := os.Executable()
	exe = filepath.Dir(exePath)
	base := os.Getenv("APPDATA")
	if base == "" {
		base = exe
	}
	cfg = filepath.Join(base, "ChessVision")
	cbase := os.Getenv("LOCALAPPDATA")
	if cbase == "" {
		cbase = base
	}
	cache = filepath.Join(cbase, "ChessVision")
	return
}

func main() {
	runtime.LockOSThread() // the window and its message loop live on the main thread
	cfgDir, cacheDir, exeDir := dirs()
	_ = os.MkdirAll(cfgDir, 0o755)
	_ = os.MkdirAll(cacheDir, 0o755)

	defer func() {
		if r := recover(); r != nil {
			winapi.WriteCrash(cfgDir, r, debug.Stack())
			winapi.MessageBox("ChessVision", "Неожиданная ошибка. Подробности: "+filepath.Join(cfgDir, "crash.log"))
			os.Exit(1)
		}
	}()
	winapi.CrashLog = func(v any) { winapi.WriteCrash(cfgDir, v, debug.Stack()) }

	winapi.EnableDPIAwareness()
	if !winapi.AcquireSingleInstance() {
		return
	}
	plat := winapi.NewPlatform()
	ctl := app.New(plat, cfgDir, cacheDir, exeDir)
	ctl.Start()
	u := ui.New(ctl)
	err := winapi.Run(ctl, u, plat)
	plat.HideOverlay()
	ctl.Close()
	if err != nil {
		winapi.WriteCrash(cfgDir, err, nil)
		winapi.MessageBox("ChessVision", "Не удалось создать окно: "+err.Error())
		os.Exit(1)
	}
}
