'use strict';
// ProVpn local controller: stores WireGuard profiles and drives wg-quick.
// Binds to 127.0.0.1 only. No third-party dependencies.

const http = require('node:http');
const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const { execFile } = require('node:child_process');

// ---- configuration (single place) ------------------------------------------
// PROVPN_PLATFORM exists so the Windows code path can be tested with stubs.
const WIN = (process.env.PROVPN_PLATFORM || process.platform) === 'win32';
const WG_DIR = process.env.PROVPN_WG_DIR || 'C:\\Program Files\\WireGuard';
const DEFAULT_PROFILES = WIN
  ? path.join(process.env.APPDATA || path.join(os.homedir(), 'AppData', 'Roaming'), 'ProVpn', 'profiles')
  : path.join(__dirname, 'profiles');

const CONFIG = {
  host: '127.0.0.1',
  port: Number(process.env.PROVPN_PORT || 7878),
  profilesDir: path.resolve(process.env.PROVPN_PROFILES || DEFAULT_PROFILES),
  publicDir: path.join(__dirname, 'public'), // also resolves inside a pkg snapshot
  // Windows: wireguard.exe manages tunnel services; Linux/macOS: wg-quick.
  wgQuick: process.env.PROVPN_WG_QUICK || (WIN ? path.join(WG_DIR, 'wireguard.exe') : 'wg-quick'),
  wg: process.env.PROVPN_WG || (WIN ? path.join(WG_DIR, 'wg.exe') : 'wg'),
  maxBody: 64 * 1024,
  cmdTimeoutMs: 30000,
};

const NAME_RE = /^[A-Za-z0-9_-]{1,15}$/; // wg-quick: interface name <= 15 chars
const KEY_RE = /^[A-Za-z0-9+/]{43}=$/;
const FORBIDDEN_KEYS = new Set(['preup', 'postup', 'predown', 'postdown']); // run shell commands

// ---- WireGuard config validation -------------------------------------------
function parseConf(text) {
  const errors = [];
  const data = { interface: {}, peers: [] };
  let section = null;
  let peer = null;

  text.split(/\r?\n/).forEach((raw, i) => {
    const line = raw.replace(/[#;].*$/, '').trim();
    if (!line) return;
    const sec = /^\[(\w+)\]$/.exec(line);
    if (sec) {
      section = sec[1].toLowerCase();
      if (section === 'peer') { peer = {}; data.peers.push(peer); }
      else if (section !== 'interface') errors.push(`line ${i + 1}: unknown section [${sec[1]}]`);
      return;
    }
    const kv = /^([A-Za-z]+)\s*=\s*(.+)$/.exec(line);
    if (!kv || !section) { errors.push(`line ${i + 1}: cannot parse`); return; }
    const key = kv[1].toLowerCase();
    if (FORBIDDEN_KEYS.has(key)) {
      errors.push(`line ${i + 1}: ${kv[1]} is not allowed (runs shell commands)`);
      return;
    }
    (section === 'interface' ? data.interface : peer)[key] = kv[2].trim();
  });

  const i = data.interface;
  if (!i.privatekey || !KEY_RE.test(i.privatekey)) errors.push('Interface.PrivateKey missing or malformed');
  if (!i.address) errors.push('Interface.Address missing');
  if (data.peers.length === 0) errors.push('no [Peer] section');
  data.peers.forEach((p, n) => {
    if (!p.publickey || !KEY_RE.test(p.publickey)) errors.push(`Peer ${n + 1}: PublicKey missing or malformed`);
    if (!p.endpoint) errors.push(`Peer ${n + 1}: Endpoint missing`);
    if (!p.allowedips) errors.push(`Peer ${n + 1}: AllowedIPs missing`);
  });

  return {
    ok: errors.length === 0,
    errors,
    summary: errors.length ? null : {
      address: i.address,
      dns: i.dns || '',
      endpoint: data.peers[0].endpoint,
    },
  };
}

// ---- profile storage -------------------------------------------------------
function profilePath(name) { return path.join(CONFIG.profilesDir, `${name}.conf`); }

function listProfiles() {
  if (!fs.existsSync(CONFIG.profilesDir)) return [];
  return fs.readdirSync(CONFIG.profilesDir)
    .filter((f) => f.endsWith('.conf') && NAME_RE.test(f.slice(0, -5)))
    .map((f) => {
      const name = f.slice(0, -5);
      const parsed = parseConf(fs.readFileSync(profilePath(name), 'utf8'));
      return { name, valid: parsed.ok, ...(parsed.summary || {}) };
    })
    .sort((a, b) => a.name.localeCompare(b.name));
}

// ---- process helpers -------------------------------------------------------
function run(cmd, args) {
  return new Promise((resolve) => {
    execFile(cmd, args, { timeout: CONFIG.cmdTimeoutMs, windowsHide: true }, (err, stdout, stderr) => {
      resolve({ ok: !err, code: err && err.code, stdout: String(stdout), stderr: String(stderr), err });
    });
  });
}

// Is the WireGuard toolset present? Only checkable by path on Windows.
function toolsMissing() {
  if (!WIN && !path.isAbsolute(CONFIG.wgQuick)) return false;
  return !(fs.existsSync(CONFIG.wgQuick) && fs.existsSync(CONFIG.wg));
}

function backendUp(name) {
  return WIN
    ? run(CONFIG.wgQuick, ['/installtunnelservice', profilePath(name)])
    : run(CONFIG.wgQuick, ['up', profilePath(name)]);
}

function backendDown(name) {
  return WIN
    ? run(CONFIG.wgQuick, ['/uninstalltunnelservice', name])
    : run(CONFIG.wgQuick, ['down', profilePath(name)]);
}

async function interfaceStatus(name) {
  const r = await run(CONFIG.wg, ['show', name, 'dump']);
  if (!r.ok) return null;
  const lines = r.stdout.trim().split('\n');
  const peer = (lines[1] || '').split('\t');
  const hs = Number(peer[4] || 0);
  return {
    name,
    handshake: hs > 0 ? hs : null, // unix seconds
    rx: Number(peer[5] || 0),
    tx: Number(peer[6] || 0),
  };
}

async function activeStatus() {
  for (const p of listProfiles()) {
    const st = await interfaceStatus(p.name);
    if (st) return st;
  }
  return null;
}

// ---- HTTP plumbing ---------------------------------------------------------
const MIME = {
  '.html': 'text/html; charset=utf-8', '.css': 'text/css; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8', '.svg': 'image/svg+xml',
};

function send(res, status, body, type = 'application/json; charset=utf-8') {
  res.writeHead(status, {
    'Content-Type': type,
    'X-Content-Type-Options': 'nosniff',
    'Cache-Control': 'no-store',
    'Content-Security-Policy': "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:",
  });
  res.end(typeof body === 'string' || Buffer.isBuffer(body) ? body : JSON.stringify(body));
}

function readJson(req) {
  return new Promise((resolve, reject) => {
    let size = 0;
    const chunks = [];
    req.on('data', (c) => {
      size += c.length;
      if (size > CONFIG.maxBody) { reject(Object.assign(new Error('body too large'), { status: 413 })); req.destroy(); return; }
      chunks.push(c);
    });
    req.on('end', () => {
      try { resolve(JSON.parse(Buffer.concat(chunks).toString('utf8') || '{}')); }
      catch { reject(Object.assign(new Error('invalid JSON'), { status: 400 })); }
    });
  });
}

function hostAllowed(req) {
  const allowed = new Set([`127.0.0.1:${CONFIG.port}`, `localhost:${CONFIG.port}`]);
  return allowed.has(req.headers.host);
}

function originAllowed(req) {
  const o = req.headers.origin;
  return o === `http://127.0.0.1:${CONFIG.port}` || o === `http://localhost:${CONFIG.port}`;
}

function serveStatic(req, res) {
  const rel = req.url.split('?')[0] === '/' ? '/index.html' : decodeURIComponent(req.url.split('?')[0]);
  const file = path.normalize(path.join(CONFIG.publicDir, rel));
  if (!file.startsWith(CONFIG.publicDir + path.sep) || !fs.existsSync(file) || !fs.statSync(file).isFile()) {
    return send(res, 404, { error: 'not found' });
  }
  send(res, 200, fs.readFileSync(file), MIME[path.extname(file)] || 'application/octet-stream');
}

async function handleApi(req, res) {
  const url = new URL(req.url, 'http://x');
  const isWrite = req.method !== 'GET';
  if (isWrite && (!originAllowed(req) || !String(req.headers['content-type'] || '').startsWith('application/json'))) {
    return send(res, 403, { error: 'forbidden origin or content-type' });
  }

  if (req.method === 'GET' && url.pathname === '/api/state') {
    return send(res, 200, {
      profiles: listProfiles(),
      active: await activeStatus(),
      privileged: elevated,
      toolsMissing: toolsMissing(),
      platform: WIN ? 'win32' : process.platform,
    });
  }

  if (req.method === 'POST' && url.pathname === '/api/profiles') {
    const { name, conf } = await readJson(req);
    if (!NAME_RE.test(name || '')) return send(res, 400, { error: 'name: 1-15 chars A-Z a-z 0-9 _ -' });
    if (typeof conf !== 'string') return send(res, 400, { error: 'conf must be a string' });
    const parsed = parseConf(conf);
    if (!parsed.ok) return send(res, 400, { error: 'invalid config', details: parsed.errors });
    fs.mkdirSync(CONFIG.profilesDir, { recursive: true, mode: 0o700 });
    fs.writeFileSync(profilePath(name), conf.trim() + '\n', { mode: 0o600 });
    return send(res, 200, { ok: true, name });
  }

  const del = /^\/api\/profiles\/([^/]+)$/.exec(url.pathname);
  if (req.method === 'DELETE' && del) {
    const name = decodeURIComponent(del[1]);
    if (!NAME_RE.test(name) || !fs.existsSync(profilePath(name))) return send(res, 404, { error: 'no such profile' });
    if (await interfaceStatus(name)) return send(res, 409, { error: 'disconnect first' });
    fs.unlinkSync(profilePath(name));
    return send(res, 200, { ok: true });
  }

  if (req.method === 'POST' && (url.pathname === '/api/connect' || url.pathname === '/api/disconnect')) {
    const { name } = await readJson(req);
    if (!NAME_RE.test(name || '') || !fs.existsSync(profilePath(name))) return send(res, 404, { error: 'no such profile' });
    const up = url.pathname === '/api/connect';
    if (toolsMissing()) return send(res, 424, { error: 'WireGuard не установлен (wireguard.com/install)' });
    if (elevated === false) return send(res, 403, { error: 'Нужны права администратора' });
    if (up) {
      const cur = await activeStatus();
      if (cur) return send(res, 409, { error: `already connected: ${cur.name}` });
    }
    const r = await (up ? backendUp(name) : backendDown(name));
    if (!r.ok) return send(res, 500, { error: `${up ? 'connect' : 'disconnect'} failed`, details: (r.stderr || r.stdout || String(r.err.message)).slice(0, 800) });
    return send(res, 200, { ok: true });
  }

  return send(res, 404, { error: 'not found' });
}

const server = http.createServer(async (req, res) => {
  try {
    if (!hostAllowed(req)) return send(res, 403, { error: 'bad host' });
    if (req.url.startsWith('/api/')) return await handleApi(req, res);
    if (req.method !== 'GET') return send(res, 405, { error: 'method not allowed' });
    return serveStatic(req, res);
  } catch (e) {
    send(res, e.status || 500, { error: e.message });
  }
});

// ---- startup: elevation, browser, error handling ---------------------------
let elevated = null; // true/false once detected, null = unknown (not enforced)

function detectElevation() {
  if (!WIN) {
    elevated = typeof process.getuid === 'function' ? process.getuid() === 0 : null;
    return Promise.resolve(elevated);
  }
  // fltmc succeeds only in an elevated session
  return new Promise((resolve) => {
    execFile('fltmc', [], { windowsHide: true }, (err) => { elevated = !err; resolve(elevated); });
  });
}

function relaunchElevated() {
  return new Promise((resolve) => {
    const q = (s) => s.replace(/'/g, "''");
    let ps = `Start-Process -FilePath '${q(process.execPath)}' -Verb RunAs`;
    if (!process.pkg) ps += ` -ArgumentList '"${q(__filename)}"'`;
    execFile('powershell', ['-NoProfile', '-WindowStyle', 'Hidden', '-Command', ps], { windowsHide: true },
      (err) => resolve(!err));
  });
}

function openBrowser(url) {
  if (process.env.PROVPN_NO_BROWSER) return;
  const [cmd, args] = WIN ? ['cmd', ['/c', 'start', '', url]]
    : process.platform === 'darwin' ? ['open', [url]] : ['xdg-open', [url]];
  execFile(cmd, args, { windowsHide: true }, () => {});
}

function pauseAndExit(code) {
  if (!WIN || !process.pkg) process.exit(code);
  console.log('\nНажмите Enter для выхода...');
  process.stdin.resume();
  process.stdin.once('data', () => process.exit(code));
}

async function main() {
  await detectElevation();
  if (WIN && elevated === false && !process.env.PROVPN_NO_ELEVATE) {
    console.log('Запрашиваю права администратора...');
    if (await relaunchElevated()) return process.exit(0);
    console.log('Не удалось получить права администратора. Подключение работать не будет.');
  }

  const url = `http://localhost:${CONFIG.port}`;
  server.once('error', (e) => {
    if (e.code === 'EADDRINUSE') { console.log(`ProVpn уже запущен: ${url}`); openBrowser(url); return setTimeout(() => process.exit(0), 500); }
    console.error('Ошибка сервера:', e.message);
    pauseAndExit(1);
  });
  server.listen(CONFIG.port, CONFIG.host, () => {
    console.log(`ProVpn: ${url}  (закройте это окно, чтобы выйти; активное подключение останется)`);
    if (toolsMissing()) console.log('ВНИМАНИЕ: WireGuard не найден. Установите: https://www.wireguard.com/install/');
    if (elevated === false) console.log('ВНИМАНИЕ: нет прав администратора, подключение не сработает.');
    openBrowser(url);
  });
}

process.on('uncaughtException', (e) => console.error('uncaughtException:', e));
process.on('unhandledRejection', (e) => console.error('unhandledRejection:', e));

if (require.main === module) main();

module.exports = { server, parseConf, CONFIG };
