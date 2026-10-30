@echo off
rem Builds dist\ProVpn.exe. Run on Windows with Node.js 20+ installed and internet access.
setlocal
cd /d "%~dp0"

where node >nul 2>nul || (echo Node.js not found. Install from https://nodejs.org & exit /b 1)

echo [1/2] Checking code...
node --check server.js || exit /b 1
node --check public\app.js || exit /b 1

echo [2/2] Building exe (first run downloads the Node base binary)...
call npm run build:win || (echo Build failed. & exit /b 1)

if not exist dist\ProVpn.exe (echo dist\ProVpn.exe was not created. & exit /b 1)
echo.
echo Done: %~dp0dist\ProVpn.exe
echo Requires WireGuard for Windows: https://www.wireguard.com/install/
endlocal
