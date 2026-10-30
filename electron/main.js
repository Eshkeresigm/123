'use strict';
const { app, BrowserWindow, ipcMain, net, shell } = require('electron');
const path = require('path');

// Hosts the renderer may reach through the native fetch bridge (no CORS in the main process).
const ALLOWED_HOSTS = new Set([
  'api.deezer.com',
  'rss.applemarketingtools.com',
  'itunes.apple.com',
  'api.music.yandex.net',
  'accounts.spotify.com',
  'api.spotify.com',
  'api-v2.soundcloud.com'
]);

ipcMain.handle('net:json', async (_event, url, opts) => {
  const u = new URL(String(url));
  if (u.protocol !== 'https:' || !ALLOWED_HOSTS.has(u.hostname)) {
    throw new Error('Host not allowed: ' + u.hostname);
  }
  const o = opts && typeof opts === 'object' ? opts : {};
  const headers = {};
  if (o.headers && typeof o.headers === 'object') {
    for (const [k, v] of Object.entries(o.headers)) headers[String(k)] = String(v);
  }
  const init = { method: o.method === 'POST' ? 'POST' : 'GET', headers };
  if (init.method === 'POST' && typeof o.body === 'string') init.body = o.body;
  const res = await net.fetch(u.toString(), init);
  const text = await res.text();
  if (!res.ok) throw new Error('HTTP ' + res.status);
  try {
    return JSON.parse(text);
  } catch {
    throw new Error('Invalid JSON from ' + u.hostname);
  }
});

function createWindow() {
  const win = new BrowserWindow({
    width: 1280,
    height: 820,
    minWidth: 900,
    minHeight: 640,
    backgroundColor: '#07070c',
    autoHideMenuBar: true,
    title: 'PluginSound',
    icon: path.join(__dirname, '..', 'build', 'icon.png'),
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      sandbox: true,
      nodeIntegration: false
    }
  });
  win.removeMenu();
  win.webContents.setWindowOpenHandler(({ url }) => {
    if (/^https:\/\//i.test(url)) shell.openExternal(url);
    return { action: 'deny' };
  });
  win.webContents.on('will-navigate', (e, url) => {
    if (!url.startsWith('file://')) e.preventDefault();
  });
  win.loadFile(path.join(__dirname, '..', 'www', 'index.html'));
}

app.setAppUserModelId('app.pluginsound.music');

if (!app.requestSingleInstanceLock()) {
  app.quit();
} else {
  app.whenReady().then(createWindow);
  app.on('window-all-closed', () => app.quit());
}
