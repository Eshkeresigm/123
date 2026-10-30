@echo off
rem Builds dist\ProVpn.exe (desktop app). Needs Windows, Node.js 20+ and internet.
setlocal
cd /d "%~dp0"

where node >nul 2>nul || (echo Node.js not found. Install from https://nodejs.org & exit /b 1)

echo [1/4] Installing dependencies...
call npm install --no-audit --no-fund || (echo npm install failed. & exit /b 1)

echo [2/4] Checking code...
node --check main.js || exit /b 1
node --check server.js || exit /b 1
node --check subscription.js || exit /b 1
node --check core.js || exit /b 1
node --check public\app.js || exit /b 1

echo [3/4] Fetching sing-box core...
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\fetch-core.ps1 || (echo Core download failed. & exit /b 1)

echo [4/4] Building exe...
call npm run build:win || (echo Build failed. & exit /b 1)

if not exist dist\ProVpn.exe (echo dist\ProVpn.exe was not created. & exit /b 1)
echo.
echo Done: %~dp0dist\ProVpn.exe
echo Requires WireGuard for Windows: https://www.wireguard.com/install/
endlocal
