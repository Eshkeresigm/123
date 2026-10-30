'use strict';

const $ = (id) => document.getElementById(id);
const state = {
  profiles: [], subs: [], active: null, selected: null,
  busy: null, // 'connect' | 'disconnect' | null
  privileged: null, toolsMissing: false, offline: false,
  failures: 0, seenJob: 0, filter: '',
};

const labelOf = (name) => {
  const p = state.profiles.find((x) => x.name === name);
  return (p && (p.label || p.name)) || name;
};

const ICON = {
  power: '<svg viewBox="0 0 24 24"><path d="M12 3v8"/><path d="M6.500 6.500a8 8 0 1 0 11 0"/></svg>',
  spin: '<svg viewBox="0 0 24 24"><path d="M12 3a9 9 0 1 1-9 9"/></svg>',
  refresh: '<svg viewBox="0 0 24 24"><path d="M20 11a8 8 0 0 0-14.500-4M4 5v3h3M4 13a8 8 0 0 0 14.500 4M20 19v-3h-3"/></svg>',
  trash: '<svg viewBox="0 0 24 24"><path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3"/></svg>',
};

async function api(method, url, body) {
  const opts = {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: body ? JSON.stringify(body) : undefined,
  };
  let res;
  try { res = await fetch(url, opts); } catch (e) {
    // Chromium aborts requests when the network changes (a tunnel coming up). Reads are safe to repeat.
    if (method !== 'GET') throw e;
    await new Promise((r) => setTimeout(r, 400));
    res = await fetch(url, opts);
  }
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw Object.assign(new Error(data.error || res.statusText), { details: data.details });
  return data;
}

// ---- formatting ------------------------------------------------------------
function fmtBytes(n) {
  const u = ['Б', 'КБ', 'МБ', 'ГБ', 'ТБ'];
  let i = 0;
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
  return `${n.toFixed(i ? 1 : 0).replace('.', ',')} ${u[i]}`;
}

function fmtUptime(since) {
  const s = Math.max(0, Math.floor(Date.now() / 1000 - since));
  if (s < 60) return `${s} с`;
  if (s < 3600) return `${Math.floor(s / 60)} мин`;
  return `${Math.floor(s / 3600)} ч ${Math.floor((s % 3600) / 60)} мин`;
}

function fmtAgo(unix) {
  if (!unix) return 'нет';
  const s = Math.max(0, Math.floor(Date.now() / 1000 - unix));
  if (s < 60) return `${s} с назад`;
  if (s < 3600) return `${Math.floor(s / 60)} мин назад`;
  return `${Math.floor(s / 3600)} ч назад`;
}

function plural(n, forms) {
  const a = Math.abs(n) % 100;
  const b = a % 10;
  if (a > 10 && a < 20) return forms[2];
  if (b > 1 && b < 5) return forms[1];
  return b === 1 ? forms[0] : forms[2];
}

const protoName = (p) => (p.kind === 'vless' ? 'VLESS' : p.kind === 'trojan' ? 'Trojan' : 'WireGuard');

// ---- the globe ---------------------------------------------------------------
const MODE_RGB = { off: [150, 130, 255], on: [45, 245, 180], busy: [255, 193, 90], offline: [255, 111, 127] };
const cur = [...MODE_RGB.off];

function buildTunnel() {
  const cv = $('globe');
  const ctx = cv.getContext('2d');
  const N = 950;
  const pts = Array.from({ length: N }, (_, i) => {
    const y = 1 - (i + 0.5) * 2 / N;
    const r = Math.sqrt(1 - y * y);
    const t = Math.PI * (3 - Math.sqrt(5)) * i;
    return [Math.cos(t) * r, y, Math.sin(t) * r];
  });
  const still = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  let ang = 0.6;
  let last = 0;
  const tilt = 0.38;
  const ct = Math.cos(tilt);
  const st = Math.sin(tilt);

  function frame(now) {
    const dt = last ? Math.min(0.05, (now - last) / 1000) : 0;
    last = now;
    const mode = document.body.dataset.mode;
    const target = MODE_RGB[mode] || MODE_RGB.off;
    for (let i = 0; i < 3; i++) cur[i] += (target[i] - cur[i]) * Math.min(1, dt * 3);
    ang += dt * (mode === 'busy' ? 1.1 : mode === 'on' ? 0.28 : 0.14);

    const size = cv.clientWidth;
    const dpr = Math.min(2, window.devicePixelRatio || 1);
    if (cv.width !== Math.round(size * dpr)) { cv.width = cv.height = Math.round(size * dpr); }
    const R = size / 3;
    const c = size / 2;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, size, size);
    const col = `${cur[0] | 0},${cur[1] | 0},${cur[2] | 0}`;

    const halo = ctx.createRadialGradient(c, c, R * 0.75, c, c, R * 1.5);
    halo.addColorStop(0, `rgba(${col},.30)`);
    halo.addColorStop(1, `rgba(${col},0)`);
    ctx.fillStyle = halo;
    ctx.fillRect(0, 0, size, size);
    const body = ctx.createRadialGradient(c - R * 0.3, c - R * 0.35, R * 0.1, c, c, R);
    body.addColorStop(0, `rgba(${col},.16)`);
    body.addColorStop(1, 'rgba(10,10,30,.55)');
    ctx.fillStyle = body;
    ctx.beginPath(); ctx.arc(c, c, R, 0, 7); ctx.fill();

    const ca = Math.cos(ang);
    const sa = Math.sin(ang);
    for (const [x, y, z] of pts) {
      const x1 = x * ca + z * sa;
      const z1 = -x * sa + z * ca;
      const y2 = y * ct - z1 * st;
      const z2 = y * st + z1 * ct;
      const front = z2 > 0;
      const a = front ? 0.35 + 0.65 * z2 : 0.08 * (1 + z2);
      ctx.fillStyle = `rgba(${col},${a.toFixed(3)})`;
      const rr = front ? 1.1 + z2 * 0.9 : 0.8;
      ctx.beginPath(); ctx.arc(c + x1 * R, c - y2 * R, rr, 0, 7); ctx.fill();
    }
    if (!still) requestAnimationFrame(frame);
  }
  requestAnimationFrame(frame);
  if (still) { addEventListener('resize', () => requestAnimationFrame(frame)); setInterval(() => requestAnimationFrame(frame), 1000); }
}

// ---- rendering -------------------------------------------------------------
let lastSig = '';

function setFigures(items) {
  const dl = $('figures');
  dl.textContent = '';
  items.forEach(([k, v]) => {
    const box = document.createElement('div'); box.className = 'fig';
    const dt = document.createElement('dt'); dt.textContent = k;
    const dd = document.createElement('dd'); dd.textContent = v;
    box.append(dt, dd);
    dl.append(box);
  });
}

function setKnob(name) {
  const knob = $('knob');
  if (knob.dataset.icon === name) return;
  knob.dataset.icon = name;
  knob.innerHTML = ICON[name];
}

function iconButton(cls, title, icon, onClick) {
  const b = document.createElement('button');
  b.type = 'button'; b.className = `icon-btn ${cls}`; b.title = title; b.setAttribute('aria-label', title); b.innerHTML = icon;
  b.addEventListener('click', (e) => { e.stopPropagation(); onClick(); });
  return b;
}

function profileRow(p, locked) {
  const li = document.createElement('li');
  const live = !!state.active && state.active.name === p.name;
  li.className = `row${p.name === state.selected ? ' sel' : ''}${live ? ' live' : ''}${locked ? ' locked' : ''}`;
  li.tabIndex = 0;
  li.setAttribute('role', 'radio');
  li.setAttribute('aria-checked', String(p.name === state.selected));

  const radio = document.createElement('span'); radio.className = 'radio';
  const label = p.label || p.name;
  li.title = label;
  const av = document.createElement('span'); av.className = 'avatar';
  av.textContent = (/[\p{L}\p{N}]/u.exec(label) || ['•'])[0];
  let h = 0; for (const ch of p.name) h = (h * 31 + ch.charCodeAt(0)) % 360;
  av.style.setProperty('--h', h);
  const meta = document.createElement('span'); meta.className = 'meta';
  const b = document.createElement('b'); b.textContent = label;
  const s = document.createElement('small');
  if (p.valid) s.textContent = p.endpoint || '';
  else { s.textContent = 'Некорректный конфиг'; s.className = 'bad'; }
  meta.append(b, s);
  const proto = document.createElement('span'); proto.className = 'proto'; proto.textContent = protoName(p);
  const del = iconButton('del', 'Удалить сервер', ICON.trash, () => removeProfile(p.name));
  li.append(radio, av, meta, proto, del);

  const select = () => { if (!state.active && !state.busy) { state.selected = p.name; render(); } };
  li.addEventListener('click', select);
  li.addEventListener('keydown', (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); select(); } });
  return li;
}

function renderGroups() {
  const locked = !!state.active || !!state.busy;
  const root = $('groups');
  root.textContent = '';
  const q = state.filter.trim().toLowerCase();
  const shown = q ? state.profiles.filter((p) => `${p.label || p.name} ${p.endpoint || ''} ${protoName(p)}`.toLowerCase().includes(q)) : state.profiles;
  const manual = shown.filter((p) => !p.sub);
  const blocks = [];
  if (manual.length) blocks.push({ title: state.subs.length ? 'Мои серверы' : '', items: manual });
  state.subs.forEach((sub) => { const items = shown.filter((p) => p.sub === sub.id); if (items.length || !q) blocks.push({ sub, items }); });

  blocks.forEach((blk) => {
    const group = document.createElement('div'); group.className = 'group';
    if (blk.title || blk.sub) {
      const head = document.createElement('div'); head.className = 'ghead';
      const text = document.createElement('div'); text.className = 'gtext';
      const strong = document.createElement('strong');
      const small = document.createElement('small');
      if (blk.sub) {
        const sub = blk.sub;
        strong.textContent = sub.host;
        const when = new Date(sub.updatedAt).toLocaleString('ru-RU', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
        const un = Object.entries(sub.unsupported || {}).map(([k, n]) => `${k} ${n}`).join(', ');
        small.textContent = `${sub.count} ${plural(sub.count, ['сервер', 'сервера', 'серверов'])}, обновлено ${when}` + (un ? `. Пропущено: ${un}` : '');
        text.append(strong, small);
        head.append(text, iconButton('', 'Обновить подписку', ICON.refresh, () => refreshSub(sub.id)), iconButton('del', 'Удалить подписку', ICON.trash, () => removeSub(sub.id)));
      } else {
        strong.textContent = blk.title;
        text.append(strong);
        head.append(text);
      }
      group.append(head);
    }
    const ul = document.createElement('ul'); ul.className = 'rows'; ul.setAttribute('role', 'radiogroup');
    blk.items.forEach((p) => ul.append(profileRow(p, locked)));
    group.append(ul);
    root.append(group);
  });
  $('empty').hidden = state.profiles.length > 0;
  if (state.profiles.length && !blocks.length) {
    const n = document.createElement('p'); n.className = 'nomatch'; n.textContent = 'Ничего не найдено'; root.append(n);
  }
  const f = $('filter');
  f.hidden = state.profiles.length <= 8;
}

function render() {
  const { profiles, active, busy, offline } = state;
  const mode = offline ? 'offline' : busy ? 'busy' : active ? 'on' : 'off';
  document.body.dataset.mode = mode;
  const sel = profiles.find((p) => p.name === state.selected);
  const viaProxy = !!active && active.kind === 'proxy';

  let title; let sub = ''; let label;
  if (mode === 'offline') {
    title = 'Нет связи'; sub = 'ProVpn не отвечает. Закройте приложение и откройте его снова.'; label = 'Недоступно';
  } else if (mode === 'busy') {
    title = busy === 'connect' ? 'Подключаем…' : 'Отключаем…';
    sub = labelOf(active ? active.name : (sel && sel.name)) || ''; label = title;
  } else if (mode === 'on') {
    title = 'Вы под защитой'; label = 'Отключить';
  } else {
    title = 'Не защищено';
    sub = sel ? `Трафик идёт напрямую. Для защиты будет использован сервер ${labelOf(sel.name)}.` : 'Добавьте сервер или подписку, чтобы включить защиту.';
    label = profiles.length ? 'Подключить' : 'Добавить сервер';
  }

  $('status').textContent = title;
  const subEl = $('statusSub');
  subEl.textContent = sub;
  subEl.hidden = mode === 'on';

  const fig = $('figures');
  fig.hidden = mode !== 'on';
  if (mode === 'on') {
    setFigures(viaProxy
      ? [['В сети', fmtUptime(active.handshake)], ['Сервер', labelOf(active.name)]]
      : [['Получено', fmtBytes(active.rx)], ['Отправлено', fmtBytes(active.tx)], ['Рукопожатие', fmtAgo(active.handshake)]]);
  }

  const toggleEl = $('toggle');
  toggleEl.setAttribute('aria-checked', String(mode === 'on'));
  toggleEl.setAttribute('aria-busy', String(mode === 'busy'));
  toggleEl.disabled = mode === 'busy' || mode === 'offline';
  $('toggleLabel').textContent = label;
  setKnob(mode === 'busy' ? 'spin' : 'power');

  let msg = '';
  if (state.privileged === false) msg = 'Нет прав администратора, подключение не сработает. Запустите ProVpn от имени администратора.';
  else if (state.toolsMissing && sel && sel.kind === 'wireguard') msg = 'Не найден WireGuard. Установите его с wireguard.com/install, чтобы подключаться к серверам WireGuard.';
  const alertEl = $('alert');
  alertEl.hidden = !msg;
  alertEl.textContent = msg;

  const sig = JSON.stringify([state.profiles, state.subs, state.selected, active && active.name, !!busy, state.filter]);
  if (sig !== lastSig) { lastSig = sig; renderGroups(); }
}

// ---- feedback --------------------------------------------------------------
let toastTimer = null;
function toast(msg, kind = 'ok', ms = 6000) {
  const el = $('toast');
  el.textContent = msg;
  el.className = `toast ${kind}`;
  el.setAttribute('role', kind === 'error' ? 'alert' : 'status');
  el.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { el.hidden = true; }, ms);
}

function fail(e) {
  const extra = e.details ? `\n${[].concat(e.details).join('\n')}` : '';
  toast(e.message + extra, 'error', 14000);
}

function ask(title, text, okLabel = 'Удалить') {
  return new Promise((resolve) => {
    const d = $('dlgConfirm');
    $('cTitle').textContent = title;
    $('cText').textContent = text;
    $('cOk').textContent = okLabel;
    d.returnValue = '';
    d.addEventListener('close', () => resolve(d.returnValue === 'ok'), { once: true });
    d.showModal();
  });
}

// ---- state sync and actions ------------------------------------------------
async function refresh() {
  try {
    const s = await api('GET', '/api/state');
    state.profiles = s.profiles;
    state.active = s.active;
    state.privileged = s.privileged;
    state.subs = s.subscriptions || [];
    state.toolsMissing = s.toolsMissing;
    state.failures = 0;
    state.offline = false;
    // The server is the source of truth for "busy": connect/disconnect run as jobs there.
    state.busy = s.job && s.job.state === 'running' ? s.job.type : null;
    if (s.job && s.job.state !== 'running' && s.job.id !== state.seenJob) {
      state.seenJob = s.job.id;
      if (s.job.state === 'error') fail({ message: s.job.error, details: s.job.details });
    }
    if (s.active) state.selected = s.active.name;
    else if (!state.profiles.some((p) => p.name === state.selected)) state.selected = (state.profiles.find((p) => p.valid) || {}).name || null;
  } catch (e) {
    // One failed poll is normal while the network is switching; only a lasting failure is "offline".
    if (++state.failures >= 3) state.offline = true;
  }
  render();
}

async function toggle() {
  if (state.busy || state.offline) return;
  if (!state.active && !state.selected) { openAdd(); return; }
  const disconnecting = !!state.active;
  const name = disconnecting ? state.active.name : state.selected;
  state.busy = disconnecting ? 'disconnect' : 'connect';
  render();
  try {
    await api('POST', disconnecting ? '/api/disconnect' : '/api/connect', { name });
  } catch (e) {
    // A network error here is ambiguous (the tunnel may already be coming up): the poll will tell.
    if (e instanceof TypeError) { schedule(300); return; }
    state.busy = null;
    fail(e);
  }
  await refresh();
  schedule(300);
}

async function removeProfile(name) {
  if (!(await ask(`Удалить сервер ${labelOf(name)}?`, 'Конфиг будет удалён с этого компьютера.'))) return;
  try { await api('DELETE', `/api/profiles/${encodeURIComponent(name)}`); } catch (e) { fail(e); }
  refresh();
}

async function refreshSub(id) {
  try {
    const r = await api('POST', `/api/subscriptions/${id}/refresh`);
    toast(`Подписка обновлена: ${r.imported} ${plural(r.imported, ['сервер', 'сервера', 'серверов'])}`);
  } catch (e) { fail(e); }
  refresh();
}

async function removeSub(id) {
  if (!(await ask('Удалить подписку?', 'Подписка и все её серверы будут удалены с этого компьютера.'))) return;
  try { await api('DELETE', `/api/subscriptions/${id}`); } catch (e) { fail(e); }
  refresh();
}

// ---- dialogs ---------------------------------------------------------------
const dlg = $('dlg');
const dlgSub = $('dlgSub');
let nameTouched = false;

function openAdd() {
  nameTouched = false;
  $('fErr').hidden = true;
  $('fName').value = '';
  $('fConf').value = '';
  dlg.showModal();
  $('fConf').focus();
}

function openSub() {
  $('sErr').hidden = true;
  dlgSub.showModal();
  $('sUrl').focus();
}

function suggestName(text) {
  const first = text.trim().split(/\r?\n/)[0] || '';
  if (!/^(vless|trojan|wireguard|wg):\/\//i.test(first)) return '';
  const slug = (s) => s.replace(/[^A-Za-z0-9_-]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 15);
  const frag = /#(.+)$/.exec(first);
  let decoded = '';
  if (frag) { try { decoded = decodeURIComponent(frag[1]); } catch { decoded = frag[1]; } }
  const host = /@\[?([^\]:/?#]+)/.exec(first);
  return slug(decoded) || slug(host ? host[1] : '');
}

$('addBtn').addEventListener('click', openAdd);
$('emptyAdd').addEventListener('click', openAdd);
$('subBtn').addEventListener('click', openSub);
$('emptySub').addEventListener('click', openSub);
$('fCancel').addEventListener('click', () => dlg.close());
$('sCancel').addEventListener('click', () => dlgSub.close());
$('fPick').addEventListener('click', () => $('fFile').click());
$('fName').addEventListener('input', () => { nameTouched = true; });
$('fConf').addEventListener('input', () => {
  if (!nameTouched) $('fName').value = suggestName($('fConf').value);
});
$('fFile').addEventListener('change', async (e) => {
  const f = e.target.files[0];
  if (!f) return;
  $('fConf').value = await f.text();
  if (!$('fName').value) $('fName').value = f.name.replace(/\.conf$/i, '').replace(/[^A-Za-z0-9_-]/g, '').slice(0, 15);
});

$('form').addEventListener('submit', async (e) => {
  e.preventDefault();
  const name = $('fName').value.trim();
  try {
    await api('POST', '/api/profiles', { name, conf: $('fConf').value });
    state.selected = name;
    dlg.close();
    toast(`Сервер ${labelOf(name)} добавлен`);
    refresh();
  } catch (err) {
    const box = $('fErr');
    box.hidden = false;
    box.textContent = err.message + (err.details ? `\n${[].concat(err.details).join('\n')}` : '');
  }
});

$('formSub').addEventListener('submit', async (e) => {
  e.preventDefault();
  const go = $('sGo');
  go.disabled = true; go.textContent = 'Загружаем…'; $('sErr').hidden = true;
  try {
    const r = await api('POST', '/api/subscriptions', { url: $('sUrl').value.trim() });
    dlgSub.close();
    $('sUrl').value = '';
    const un = Object.entries(r.unsupported || {}).map(([k, n]) => `${k} ${n}`).join(', ');
    toast(`Добавлено: ${r.imported} ${plural(r.imported, ['сервер', 'сервера', 'серверов'])}`
      + (r.skipped.length ? `\nНе удалось прочитать: ${r.skipped.length}` : '')
      + (un ? `\nПропущены другие протоколы: ${un}` : ''), 'ok', 9000);
    await refresh();
  } catch (err) {
    const box = $('sErr');
    box.hidden = false;
    box.textContent = err.message + (err.details && err.details.length ? `\n${[].concat(err.details).join('\n')}` : '');
  } finally { go.disabled = false; go.textContent = 'Загрузить'; }
});

$('filter').addEventListener('input', (e) => { state.filter = e.target.value; render(); });
$('toggle').addEventListener('click', toggle);
buildTunnel();
render();
refresh();
let pollTimer = null;
function schedule(ms) {
  clearTimeout(pollTimer);
  pollTimer = setTimeout(async () => {
    await refresh();
    schedule(state.busy ? 600 : state.failures ? 1000 : 3000);
  }, ms);
}
schedule(3000);
