'use strict';
// ProVpn desktop shell (Electron): runs the local controller in-process and
// shows the UI in its own window. Administrator rights come from the exe manifest.

const path = require('node:path');
const { app, BrowserWindow, Menu, dialog, shell } = require('electron');

process.env.PROVPN_NO_BROWSER = '1';
const fs = require('node:fs');
const { server, CONFIG, detectElevation } = require('./server.js');

// Software rendering fallback for machines/VMs where the GPU process cannot start
// (Chromium then aborts at launch). Enable with --disable-gpu, PROVPN_NO_GPU=1,
// or an empty file named "disable-gpu" next to the profiles folder.
const noGpuFile = path.join(path.dirname(CONFIG.profilesDir), 'disable-gpu');
if (process.argv.includes('--disable-gpu') || process.env.PROVPN_NO_GPU || fs.existsSync(noGpuFile)) {
  app.disableHardwareAcceleration();
  app.commandLine.appendSwitch('disable-gpu');
}

const ORIGIN = `http://localhost:${CONFIG.port}`;
let win = null;

if (!app.requestSingleInstanceLock()) {
  app.quit();
} else {
  app.on('second-instance', () => {
    if (win) { if (win.isMinimized()) win.restore(); win.focus(); }
  });

  app.whenReady().then(async () => {
    Menu.setApplicationMenu(null);
    await detectElevation();

    server.once('error', (e) => {
      dialog.showErrorBox('ProVpn',
        e.code === 'EADDRINUSE'
          ? `Порт ${CONFIG.port} занят другой программой. Закройте её или задайте PROVPN_PORT.`
          : `Ошибка сервера: ${e.message}`);
      app.exit(1);
    });
    server.listen(CONFIG.port, CONFIG.host, createWindow);
  });
}

function createWindow() {
  win = new BrowserWindow({
    width: 480,
    height: 860,
    minWidth: 380,
    minHeight: 640,
    title: 'ProVpn',
    backgroundColor: '#070b18',
    autoHideMenuBar: true,
    icon: path.join(__dirname, 'build', 'icon.png'),
    webPreferences: { contextIsolation: true, sandbox: true, nodeIntegration: false },
  });

  // The UI must stay on our own origin; anything else opens in the system browser.
  win.webContents.setWindowOpenHandler(({ url }) => {
    if (/^https:\/\//.test(url)) shell.openExternal(url);
    return { action: 'deny' };
  });
  win.webContents.on('will-navigate', (e, url) => {
    if (!url.startsWith(ORIGIN)) e.preventDefault();
  });
  win.webContents.on('did-fail-load', (_e, code, desc) => {
    if (code !== -3) dialog.showErrorBox('ProVpn', `Не удалось загрузить интерфейс: ${desc}`);
  });
  win.on('closed', () => { win = null; });
  win.loadURL(ORIGIN);
}

app.on('window-all-closed', () => { server.close(); app.quit(); });
