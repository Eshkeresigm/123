'use strict';
// ProVpn local controller: stores WireGuard profiles and drives wg-quick.
// Binds to 127.0.0.1 only. No third-party dependencies.

const http = require('node:http');
const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const crypto = require('node:crypto');
const { execFile } = require('node:child_process');
const { parseSubscription, proxyUriToProfile } = require('./subscription.js');
const { createCore } = require('./core.js');

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
  fetchTimeoutMs: 15000,
  maxSubBytes: 2 * 1024 * 1024,
  // sing-box core for VLESS/Trojan: bundled in <app>/resources/core in the packaged app.
  singbox: process.env.PROVPN_SINGBOX || (process.resourcesPath
    ? path.join(process.resourcesPath, 'core', WIN ? 'sing-box.exe' : 'sing-box')
    : WIN ? path.join(__dirname, 'core', 'sing-box.exe') : 'sing-box'),
  coreStartTimeoutMs: 12000,
};
CONFIG.runDir = path.join(path.dirname(CONFIG.profilesDir), 'run'); // runtime configs hold credentials (0600)

CONFIG.logFile = path.join(path.dirname(CONFIG.profilesDir), 'provpn.log');

// Diagnostics file (never contains credentials: core output is redacted before it gets here).
function log(...parts) {
  try {
    fs.mkdirSync(path.dirname(CONFIG.logFile), { recursive: true });
    try { if (fs.statSync(CONFIG.logFile).size > 512 * 1024) fs.renameSync(CONFIG.logFile, `${CONFIG.logFile}.old`); } catch { /* no log yet */ }
    fs.appendFileSync(CONFIG.logFile, `${new Date().toISOString()} ${parts.join(' ')}\n`, { mode: 0o600 });
  } catch { /* logging must never break the app */ }
}

// Connect/disconnect run as background jobs: the UI polls /api/state for the result.
// A long-lived HTTP request would be aborted by Chromium when the TUN adapter appears
// (network change), which looked like a crash even though the tunnel was up.
let job = null;
let jobSeq = 0;
const JOB_TTL_MS = 30000;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const core = createCore({
  binary: CONFIG.singbox, runDir: CONFIG.runDir, startTimeoutMs: CONFIG.coreStartTimeoutMs,
  onExit: ({ name, code, tail }) => {
    log('core exited unexpectedly:', name, `code=${code}`, tail.trim().split('\n').slice(-6).join(' | '));
    if (job && job.state === 'running') return;
    job = {
      id: ++jobSeq, type: 'drop', name, state: 'error', finishedAt: Date.now(),
      error: `Соединение оборвалось: ядро sing-box остановилось (код ${code})`,
      details: tail.trim().split('\n').slice(-4).filter(Boolean),
    };
  },
});

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
// WireGuard profile: <name>.conf. VLESS/Trojan profile: <name>.proxy.json
// ({protocol, label, outbound}); both hold secrets, so both are written 0600.
function profilePath(name) { return path.join(CONFIG.profilesDir, `${name}.conf`); }
function proxyPath(name) { return path.join(CONFIG.profilesDir, `${name}.proxy.json`); }

function profileKind(name) {
  if (!NAME_RE.test(name || '')) return null;
  if (fs.existsSync(profilePath(name))) return 'wireguard';
  if (fs.existsSync(proxyPath(name))) return 'proxy';
  return null;
}

function readProxy(name) { return JSON.parse(fs.readFileSync(proxyPath(name), 'utf8')); }

function writeProxy(name, rec) {
  fs.mkdirSync(CONFIG.profilesDir, { recursive: true, mode: 0o700 });
  fs.writeFileSync(proxyPath(name), JSON.stringify(rec, null, 2), { mode: 0o600 });
}

function removeProfileFiles(name) {
  fs.rmSync(profilePath(name), { force: true });
  fs.rmSync(proxyPath(name), { force: true });
}

function listProfiles() {
  if (!fs.existsSync(CONFIG.profilesDir)) return [];
  const out = [];
  for (const f of fs.readdirSync(CONFIG.profilesDir)) {
    if (f.endsWith('.proxy.json')) {
      const name = f.slice(0, -'.proxy.json'.length);
      if (!NAME_RE.test(name)) continue;
      try {
        const r = readProxy(name);
        out.push({ name, kind: r.protocol, valid: true, label: r.label, endpoint: `${r.outbound.server}:${r.outbound.server_port}` });
      } catch { out.push({ name, kind: 'proxy', valid: false }); }
    } else if (f.endsWith('.conf')) {
      const name = f.slice(0, -5);
      if (!NAME_RE.test(name)) continue;
      const parsed = parseConf(fs.readFileSync(profilePath(name), 'utf8'));
      out.push({ name, kind: 'wireguard', valid: parsed.ok, ...(parsed.summary || {}) });
    }
  }
  return out.sort((a, b) => a.name.localeCompare(b.name));
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

async function isActive(name) {
  const st = core.status();
  if (st && st.name === name) return true;
  if (profileKind(name) === 'proxy') return false;
  return !!(await interfaceStatus(name));
}

// kind 'proxy': traffic counters are not collected, `handshake` carries the start time.
// One `wg show interfaces` call tells which tunnels exist; asking per profile would spawn
// hundreds of processes every poll for a big subscription.
async function activeStatus(profiles = listProfiles()) {
  const st = core.status();
  if (st) return { name: st.name, kind: 'proxy', handshake: Math.floor(st.since / 1000), rx: 0, tx: 0 };
  const names = new Set(profiles.filter((p) => p.kind === 'wireguard').map((p) => p.name));
  if (!names.size || toolsMissing()) return null;
  const r = await run(CONFIG.wg, ['show', 'interfaces']);
  const candidates = r.ok ? r.stdout.split(/\s+/).filter((n) => names.has(n)) : [...names].slice(0, 20);
  for (const n of candidates) {
    const w = await interfaceStatus(n);
    if (w) return { ...w, kind: 'wireguard' };
  }
  return null;
}

// ---- subscriptions ---------------------------------------------------------
// A subscription is a URL that returns WireGuard servers. Its URL is a secret
// (usually contains a token): stored 0600, never returned by the API.
function httpErr(status, message, details) { return Object.assign(new Error(message), { status, details }); }

const subsFile = () => path.join(CONFIG.profilesDir, '_subscriptions.json');

function readSubs() {
  try { return JSON.parse(fs.readFileSync(subsFile(), 'utf8')); } catch { return []; }
}

function writeSubs(list) {
  fs.mkdirSync(CONFIG.profilesDir, { recursive: true, mode: 0o700 });
  fs.writeFileSync(subsFile(), JSON.stringify(list, null, 2), { mode: 0o600 });
}

async function fetchText(raw) {
  let u;
  try { u = new URL(raw); } catch { throw httpErr(400, 'Некорректный URL'); }
  if (u.protocol !== 'https:' && u.protocol !== 'http:') throw httpErr(400, 'Поддерживаются только http:// и https:// ссылки');

  let res;
  try {
    res = await fetch(u, {
      redirect: 'follow',
      signal: AbortSignal.timeout(CONFIG.fetchTimeoutMs),
      headers: { 'User-Agent': 'ProVpn/0.2', Accept: '*/*' },
    });
  } catch (e) {
    throw httpErr(502, `Не удалось загрузить подписку: ${(e.cause && e.cause.code) || e.name}`);
  }
  if (!res.ok) throw httpErr(502, `Сервер подписки ответил кодом ${res.status}`);

  const chunks = [];
  let total = 0;
  for await (const chunk of res.body) {
    total += chunk.length;
    if (total > CONFIG.maxSubBytes) throw httpErr(413, 'Подписка слишком большая (лимит 2 МБ)');
    chunks.push(chunk);
  }
  return Buffer.concat(chunks).toString('utf8');
}

// Display names come from untrusted subscriptions: drop control and bidi-override characters.
function cleanLabel(raw) {
  const t = String(raw || '').replace(/[\u0000-\u001f\u007f\u200e\u200f\u202a-\u202e\u2066-\u2069]/g, '').replace(/\s+/g, ' ').trim();
  return Array.from(t).slice(0, 60).join('') || 'Сервер';
}

function unsupportedText(map) {
  return Object.entries(map).map(([k, n]) => `${k}: ${n}`).join(', ');
}

async function importSubscription(rawUrl, existingId) {
  const subs = readSubs();
  const old = existingId ? subs.find((s) => s.id === existingId) : null;
  if (existingId && !old) throw httpErr(404, 'Такой подписки нет');

  const parsed = parseSubscription(await fetchText(rawUrl));
  const valid = [];
  const skipped = [...parsed.skipped];
  for (const p of parsed.profiles) {
    if (p.outbound) { valid.push(p); continue; } // VLESS/Trojan: already validated by the parser
    const r = parseConf(p.conf);
    if (r.ok) valid.push(p); else skipped.push(`${p.label}: ${r.errors[0]}`);
  }
  if (!valid.length) {
    const un = unsupportedText(parsed.unsupported);
    throw httpErr(422,
      un ? `В подписке нет поддерживаемых серверов (WireGuard, VLESS, Trojan). Найдены другие протоколы: ${un}.`
        : 'В подписке не найдено ни одного поддерживаемого сервера (WireGuard, VLESS, Trojan).',
      skipped.slice(0, 5));
  }

  if (old) {
    for (const n of old.profiles) if (await isActive(n)) throw httpErr(409, 'Сначала отключитесь');
    for (const n of old.profiles) removeProfileFiles(n);
  }

  const id = old ? old.id : crypto.randomBytes(2).toString('hex');
  fs.mkdirSync(CONFIG.profilesDir, { recursive: true, mode: 0o700 });
  const names = valid.map((p, i) => {
    const slug = p.label.replace(/[^A-Za-z0-9_]+/g, '').slice(0, 8);
    const name = `${id}-${String(i + 1).padStart(2, '0')}${slug}`.slice(0, 15);
    if (p.outbound) writeProxy(name, { protocol: p.protocol, label: p.label, outbound: p.outbound });
    else fs.writeFileSync(profilePath(name), p.conf.trim() + '\n', { mode: 0o600 });
    return name;
  });

  const labels = {};
  names.forEach((n, i) => { labels[n] = cleanLabel(valid[i].label); });
  const rec = {
    id, url: rawUrl, host: new URL(rawUrl).hostname, updatedAt: Date.now(),
    profiles: names, labels, unsupported: parsed.unsupported,
  };
  writeSubs(old ? subs.map((s) => (s.id === id ? rec : s)) : [...subs, rec]);
  return { id, imported: names.length, skipped, unsupported: parsed.unsupported };
}

async function removeSubscription(id) {
  const subs = readSubs();
  const sub = subs.find((s) => s.id === id);
  if (!sub) throw httpErr(404, 'Такой подписки нет');
  for (const n of sub.profiles) if (await isActive(n)) throw httpErr(409, 'Сначала отключитесь');
  for (const n of sub.profiles) removeProfileFiles(n);
  writeSubs(subs.filter((s) => s.id !== id));
}

// ---- background jobs ------------------------------------------------------
function publicJob() {
  if (!job) return null;
  if (job.state !== 'running' && Date.now() - job.finishedAt > JOB_TTL_MS) return null;
  const { id, type, name, state, error, details } = job;
  return { id, type, name, state, error, details };
}

function startJob(type, name, work) {
  if (job && job.state === 'running') throw httpErr(409, 'Предыдущая операция ещё выполняется');
  const j = { id: ++jobSeq, type, name, state: 'running' };
  job = j;
  log(type, name, 'start');
  Promise.resolve().then(work).then(
    () => { j.state = 'done'; log(type, name, 'ok'); },
    (e) => {
      j.state = 'error'; j.error = e.message; j.details = e.details;
      log(type, name, 'FAILED:', e.message, [].concat(e.details || []).join(' | '));
    },
  ).finally(() => { j.finishedAt = Date.now(); });
  return j;
}

async function doConnect(name, kind) {
  if (kind === 'proxy') {
    try { await core.start(name, readProxy(name).outbound); }
    catch (e) { throw Object.assign(new Error(`Не удалось подключиться: ${e.message}`), { details: e.details }); }
    return;
  }
  const r = await backendUp(name);
  if (!r.ok) {
    throw Object.assign(new Error('Не удалось запустить туннель WireGuard'),
      { details: [(r.stderr || r.stdout || String(r.err && r.err.message)).slice(0, 800)] });
  }
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) {
    if (await interfaceStatus(name)) return;
    await sleep(500);
  }
  await backendDown(name);
  throw new Error('Служба WireGuard запустилась, но туннель не поднялся. Проверьте конфиг сервера.');
}

async function doDisconnect(name, kind) {
  if (kind === 'proxy') { await core.stop(); return; }
  const r = await backendDown(name);
  if (!r.ok) {
    throw Object.assign(new Error('Не удалось отключить туннель WireGuard'),
      { details: [(r.stderr || r.stdout || String(r.err && r.err.message)).slice(0, 800)] });
  }
}

// ---- HTTP plumbing ---------------------------------------------------------
const MIME = {
  '.html': 'text/html; charset=utf-8', '.css': 'text/css; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8', '.svg': 'image/svg+xml',
  '.woff': 'font/woff', '.woff2': 'font/woff2', '.png': 'image/png',
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
    const subs = readSubs();
    const owner = new Map();
    for (const sub of subs) for (const n of sub.profiles) owner.set(n, sub);
    const profiles = listProfiles();
    return send(res, 200, {
      profiles: profiles.map((p) => {
        const sub = owner.get(p.name);
        const label = sub ? cleanLabel((sub.labels && sub.labels[p.name]) || p.label || p.name) : p.name;
        const { label: _raw, ...rest } = p;
        return sub ? { ...rest, label, sub: sub.id } : { ...rest, label };
      }),
      subscriptions: subs.map((s) => ({
        id: s.id, host: s.host, updatedAt: s.updatedAt, count: s.profiles.length, unsupported: s.unsupported,
      })),
      active: await activeStatus(profiles),
      job: publicJob(),
      privileged: elevated,
      toolsMissing: toolsMissing(),
      platform: WIN ? 'win32' : process.platform,
    });
  }

  if (req.method === 'POST' && url.pathname === '/api/profiles') {
    const { name, conf } = await readJson(req);
    if (!NAME_RE.test(name || '')) return send(res, 400, { error: 'name: 1-15 chars A-Z a-z 0-9 _ -' });
    if (typeof conf !== 'string') return send(res, 400, { error: 'conf must be a string' });
    const existing = profileKind(name);
    if (/^\s*(vless|trojan):\/\//i.test(conf)) {
      let prof;
      try { prof = proxyUriToProfile(conf.trim().split(/\r?\n/)[0]); }
      catch (e) { return send(res, 400, { error: 'invalid link', details: [e.message] }); }
      if (existing === 'wireguard') return send(res, 409, { error: 'name is used by a WireGuard profile' });
      writeProxy(name, { protocol: prof.protocol, label: prof.label, outbound: prof.outbound });
      return send(res, 200, { ok: true, name });
    }
    const parsed = parseConf(conf);
    if (!parsed.ok) return send(res, 400, { error: 'invalid config', details: parsed.errors });
    if (existing === 'proxy') return send(res, 409, { error: 'name is used by a VLESS/Trojan profile' });
    fs.mkdirSync(CONFIG.profilesDir, { recursive: true, mode: 0o700 });
    fs.writeFileSync(profilePath(name), conf.trim() + '\n', { mode: 0o600 });
    return send(res, 200, { ok: true, name });
  }

  const del = /^\/api\/profiles\/([^/]+)$/.exec(url.pathname);
  if (req.method === 'DELETE' && del) {
    const name = decodeURIComponent(del[1]);
    if (!profileKind(name)) return send(res, 404, { error: 'no such profile' });
    if (await isActive(name)) return send(res, 409, { error: 'disconnect first' });
    removeProfileFiles(name);
    return send(res, 200, { ok: true });
  }

  if (req.method === 'POST' && (url.pathname === '/api/connect' || url.pathname === '/api/disconnect')) {
    const { name } = await readJson(req);
    const kind = profileKind(name);
    if (!kind) return send(res, 404, { error: 'no such profile' });
    const up = url.pathname === '/api/connect';
    if (elevated === false) return send(res, 403, { error: 'Нужны права администратора' });
    if (up) {
      const cur = await activeStatus();
      if (cur) return send(res, 409, { error: `already connected: ${cur.name}` });
    }
    if (kind === 'proxy' && up && core.missing()) return send(res, 424, { error: 'Не найдено ядро sing-box. Переустановите ProVpn.' });
    if (kind === 'wireguard' && toolsMissing()) return send(res, 424, { error: 'WireGuard не установлен (wireguard.com/install)' });
    const j = startJob(up ? 'connect' : 'disconnect', name, () => (up ? doConnect(name, kind) : doDisconnect(name, kind)));
    return send(res, 202, { ok: true, job: j.id });
  }

  if (req.method === 'POST' && url.pathname === '/api/subscriptions') {
    const { url: subUrl } = await readJson(req);
    if (typeof subUrl !== 'string' || subUrl.length > 4096) return send(res, 400, { error: 'Укажите URL подписки' });
    return send(res, 200, await importSubscription(subUrl.trim()));
  }

  const subRoute = /^\/api\/subscriptions\/([0-9a-f]{4})(\/refresh)?$/.exec(url.pathname);
  if (subRoute && req.method === 'POST' && subRoute[2]) {
    const sub = readSubs().find((s) => s.id === subRoute[1]);
    if (!sub) return send(res, 404, { error: 'Такой подписки нет' });
    return send(res, 200, await importSubscription(sub.url, sub.id));
  }
  if (subRoute && req.method === 'DELETE' && !subRoute[2]) {
    await removeSubscription(subRoute[1]);
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
    send(res, e.status || 500, { error: e.message, details: e.details });
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

process.on('uncaughtException', (e) => { console.error('uncaughtException:', e); log('uncaughtException', e && e.stack || e); });
process.on('unhandledRejection', (e) => { console.error('unhandledRejection:', e); log('unhandledRejection', e && e.stack || e); });
for (const sig of ['SIGINT', 'SIGTERM']) {
  process.on(sig, async () => { await core.stop(); process.exit(0); }); // never leave an orphaned tunnel
}

if (require.main === module) main();

module.exports = { server, parseConf, CONFIG, detectElevation, log, shutdown: () => core.stop() };
