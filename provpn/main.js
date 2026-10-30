'use strict';
// ProVpn desktop shell (Electron): runs the local controller in-process and
// shows the UI in its own window. Administrator rights come from the exe manifest.

const path = require('node:path');
const { app, BrowserWindow, Menu, dialog, shell } = require('electron');

process.env.PROVPN_NO_BROWSER = '1';
const fs = require('node:fs');
const { server, CONFIG, detectElevation, shutdown, log } = require('./server.js');

// Software rendering fallback for machines/VMs where the GPU process cannot start
// (Chromium then aborts at launch). Enable with --disable-gpu, PROVPN_NO_GPU=1,
// or an empty file named "disable-gpu" next to the profiles folder.
const noGpuFile = path.join(path.dirname(CONFIG.profilesDir), 'disable-gpu');
if (process.argv.includes('--disable-gpu') || process.env.PROVPN_NO_GPU || fs.existsSync(noGpuFile)) {
  app.disableHardwareAcceleration();
  for (const sw of ['disable-gpu', 'disable-gpu-compositing', 'disable-gpu-sandbox', 'in-process-gpu']) {
    app.commandLine.appendSwitch(sw);
  }
}

// 127.0.0.1, not "localhost": no name resolution, so a VPN's DNS/network change cannot affect the UI.
const ORIGIN = `http://127.0.0.1:${CONFIG.port}`;
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
    width: 460,
    height: 820,
    minWidth: 380,
    minHeight: 600,
    title: 'ProVpn',
    backgroundColor: '#070816',
    titleBarStyle: 'hidden',
    titleBarOverlay: { color: '#070816', symbolColor: '#f1f2ff', height: 44 },
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
  // A tunnel coming up changes the network under Chromium; a reload (not an error box) is the cure.
  let loadRetries = 0;
  win.webContents.on('did-finish-load', () => { loadRetries = 0; });
  win.webContents.on('did-fail-load', (_e, code, desc, _url, isMainFrame) => {
    if (code === -3 || isMainFrame === false) return;
    log('did-fail-load', code, desc);
    if (loadRetries++ < 6) { setTimeout(() => { if (win && !win.isDestroyed()) win.loadURL(ORIGIN); }, 700); return; }
    dialog.showErrorBox('ProVpn', `Не удалось загрузить интерфейс: ${desc}`);
  });
  const reloads = [];
  win.webContents.on('render-process-gone', (_e, d) => {
    log('render-process-gone', d.reason, d.exitCode);
    const now = Date.now();
    while (reloads.length && now - reloads[0] > 60000) reloads.shift();
    if (d.reason === 'clean-exit' || !win || win.isDestroyed()) return;
    if (reloads.push(now) <= 3) win.webContents.reload();
    else dialog.showErrorBox('ProVpn', `Окно интерфейса аварийно завершилось (${d.reason}). Подробности: ${CONFIG.logFile}. Создайте пустой файл disable-gpu рядом с папкой profiles и запустите снова.`);
  });
  win.on('unresponsive', () => log('window unresponsive'));
  win.on('closed', () => { win = null; });
  win.loadURL(ORIGIN);
}

// Closing the window ends a VLESS/Trojan tunnel (the core is our child process).
// A WireGuard tunnel is a Windows service and stays up.
let quitting = false;
app.on('before-quit', (e) => {
  if (quitting) return;
  quitting = true;
  e.preventDefault();
  server.close();
  shutdown().finally(() => app.quit());
});
app.on('child-process-gone', (_e, d) => log('child-process-gone', d.type, d.reason, d.exitCode));
app.on('window-all-closed', () => app.quit());
