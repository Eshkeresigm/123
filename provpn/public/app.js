'use strict';

const $ = (id) => document.getElementById(id);
const state = {
  profiles: [], subs: [], active: null, selected: null,
  busy: null, // 'connect' | 'disconnect' | null
  privileged: null, toolsMissing: false, offline: false,
};

const ICON = {
  power: '<svg viewBox="0 0 24 24"><path d="M12 3v8"/><path d="M6.500 6.500a8 8 0 1 0 11 0"/></svg>',
  spin: '<svg viewBox="0 0 24 24"><path d="M12 3a9 9 0 1 1-9 9"/></svg>',
  refresh: '<svg viewBox="0 0 24 24"><path d="M20 11a8 8 0 0 0-14.500-4M4 5v3h3M4 13a8 8 0 0 0 14.500 4M20 19v-3h-3"/></svg>',
  trash: '<svg viewBox="0 0 24 24"><path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3"/></svg>',
};

async function api(method, url, body) {
  const res = await fetch(url, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: body ? JSON.stringify(body) : undefined,
  });
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

// ---- the tunnel drawing ----------------------------------------------------
const SVGNS = 'http://www.w3.org/2000/svg';

function svgEl(tag, attrs, parent) {
  const e = document.createElementNS(SVGNS, tag);
  for (const [k, v] of Object.entries(attrs)) e.setAttribute(k, v);
  if (parent) parent.append(e);
  return e;
}

function mix(a, b, t) {
  const p = (h) => [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16));
  const [x, y] = [p(a), p(b)];
  return `#${x.map((v, i) => Math.round(v + (y[i] - v) * t).toString(16).padStart(2, '0')).join('')}`;
}

// Arches recede toward a vanishing point on the horizon; every spring line sits on it.
function buildTunnel() {
  const svg = $('tunnel');
  const cx = 230;
  const vy = 170;
  const N = 8;
  const defs = svgEl('defs', {}, svg);
  const g = svgEl('radialGradient', { id: 'glowGrad' }, defs);
  svgEl('stop', { offset: '0', 'stop-color': '#ffb43a', 'stop-opacity': '.85' }, g);
  svgEl('stop', { offset: '.35', 'stop-color': '#ff9a2a', 'stop-opacity': '.30' }, g);
  svgEl('stop', { offset: '1', 'stop-color': '#ff8a1f', 'stop-opacity': '0' }, g);

  const dims = Array.from({ length: N }, (_, k) => {
    const s = 0.74 ** k;
    return { k, s, hw: 250 * s, floor: vy + 300 * s };
  });

  dims.forEach(({ k, s, hw, floor }) => {
    const t = k / (N - 1);
    const d = `M${cx - hw} ${floor}V${vy}A${hw} ${hw} 0 0 1 ${cx + hw} ${vy}V${floor}Z`;
    const arch = svgEl('path', { d, class: 'arch', 'stroke-width': (2.6 * s + 0.6).toFixed(2) }, svg);
    arch.style.cssText = [
      `--i:${k}`,
      `--off-fill:${mix('#0a1417', '#16313a', t ** 1.1)}`,
      `--off-stroke:${mix('#1c3338', '#3d6066', t)}`,
      `--on-fill:${mix('#0d1819', '#74470f', t ** 1.8)}`,
      `--on-stroke:${mix('#3b2c14', '#ffb43a', t ** 0.7)}`,
    ].join(';');
  });

  dims.slice(0, -1).forEach(({ k, s, floor }, i) => {
    const next = dims[i + 1].floor;
    const dash = svgEl('line', {
      x1: cx, x2: cx, y1: floor - 2, y2: floor - (floor - next) * 0.55,
      class: 'lane', 'stroke-width': (3 * s + 0.6).toFixed(2), 'stroke-linecap': 'round',
    }, svg);
    dash.style.setProperty('--i', k);
  });

  dims.slice(2).forEach(({ k, s, hw }) => {
    const lamp = svgEl('rect', {
      x: (cx - 24 * s).toFixed(2), y: (vy - hw - 1).toFixed(2), width: (48 * s).toFixed(2), height: (5 * s + 1.5).toFixed(2), rx: 2, class: 'lamp',
    }, svg);
    lamp.style.setProperty('--i', k);
  });

  svgEl('circle', { cx, cy: vy - 2, r: 120, fill: 'url(#glowGrad)', class: 'glow' }, svg);
  svgEl('circle', { cx, cy: vy - 2, r: 7, class: 'core' }, svg);
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
  const meta = document.createElement('span'); meta.className = 'meta';
  const b = document.createElement('b'); b.textContent = p.name;
  const s = document.createElement('small');
  if (p.valid) s.textContent = p.endpoint || '';
  else { s.textContent = 'Некорректный конфиг'; s.className = 'bad'; }
  meta.append(b, s);
  const proto = document.createElement('span'); proto.className = 'proto'; proto.textContent = protoName(p);
  const del = iconButton('del', 'Удалить сервер', ICON.trash, () => removeProfile(p.name));
  li.append(radio, meta, proto, del);

  const select = () => { if (!state.active && !state.busy) { state.selected = p.name; render(); } };
  li.addEventListener('click', select);
  li.addEventListener('keydown', (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); select(); } });
  return li;
}

function renderGroups() {
  const locked = !!state.active || !!state.busy;
  const root = $('groups');
  root.textContent = '';
  const manual = state.profiles.filter((p) => !p.sub);
  const blocks = [];
  if (manual.length) blocks.push({ title: state.subs.length ? 'Мои серверы' : '', items: manual });
  state.subs.forEach((sub) => blocks.push({ sub, items: state.profiles.filter((p) => p.sub === sub.id) }));

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
  $('empty').hidden = blocks.length > 0;
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
    sub = (active ? active.name : (sel && sel.name)) || ''; label = title;
  } else if (mode === 'on') {
    title = 'Вы под защитой'; label = 'Отключить';
  } else {
    title = 'Не защищено';
    sub = sel ? `Трафик идёт напрямую. Для защиты будет использован сервер ${sel.name}.` : 'Добавьте сервер или подписку, чтобы включить защиту.';
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
      ? [['В сети', fmtUptime(active.handshake)], ['Сервер', active.name]]
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

  const sig = JSON.stringify([state.profiles, state.subs, state.selected, active && active.name, !!busy]);
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
    state.offline = false;
    if (s.active) state.selected = s.active.name;
    else if (!state.profiles.some((p) => p.name === state.selected)) state.selected = (state.profiles.find((p) => p.valid) || {}).name || null;
  } catch (e) { state.offline = true; }
  render();
}

async function toggle() {
  if (state.busy || state.offline) return;
  if (!state.active && !state.selected) { openAdd(); return; }
  const disconnecting = !!state.active;
  const name = disconnecting ? state.active.name : state.selected;
  state.busy = disconnecting ? 'disconnect' : 'connect';
  render();
  try { await api('POST', disconnecting ? '/api/disconnect' : '/api/connect', { name }); } catch (e) { fail(e); }
  state.busy = null;
  await refresh();
}

async function removeProfile(name) {
  if (!(await ask(`Удалить сервер ${name}?`, 'Конфиг будет удалён с этого компьютера.'))) return;
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
    toast(`Сервер ${name} добавлен`);
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

$('toggle').addEventListener('click', toggle);
buildTunnel();
render();
refresh();
setInterval(() => { if (!state.busy) refresh(); }, 3000);
