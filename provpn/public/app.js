'use strict';

const $ = (id) => document.getElementById(id);
const state = { profiles: [], active: null, selected: null, busy: false, privileged: null };

const ICON_GLOBE = '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/><ellipse cx="12" cy="12" rx="4" ry="9"/><path d="M3 12h18"/></svg>';
const ICON_TRASH = '<svg viewBox="0 0 24 24"><path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3"/></svg>';

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

function fmtBytes(n) {
  const u = ['Б', 'КБ', 'МБ', 'ГБ', 'ТБ'];
  let i = 0;
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
  return `${n.toFixed(i ? 1 : 0)} ${u[i]}`;
}

function fmtAgo(unix) {
  if (!unix) return '—';
  const s = Math.max(0, Math.floor(Date.now() / 1000 - unix));
  if (s < 60) return `${s} с назад`;
  if (s < 3600) return `${Math.floor(s / 60)} мин назад`;
  return `${Math.floor(s / 3600)} ч назад`;
}

function render() {
  const { profiles, active, busy } = state;
  const mode = busy ? 'busy' : active ? 'on' : 'off';
  document.body.className = mode;
  $('pillText').textContent = busy ? 'Подождите…' : active ? 'Защищён' : 'Не защищён';
  $('power').disabled = busy;

  const sel = profiles.find((p) => p.name === state.selected);
  if (busy) { $('title').textContent = 'Соединяем…'; $('sub').textContent = state.selected || ''; }
  else if (active) { $('title').textContent = 'Вы подключены'; $('sub').textContent = `Профиль: ${active.name}`; }
  else { $('title').textContent = 'Нажмите для подключения'; $('sub').textContent = sel ? `Профиль: ${sel.name}` : 'Выберите профиль ниже'; }

  $('rx').textContent = fmtBytes(active ? active.rx : 0);
  $('tx').textContent = fmtBytes(active ? active.tx : 0);
  $('hs').textContent = active ? fmtAgo(active.handshake) : '—';

  const list = $('list');
  list.textContent = '';
  profiles.forEach((p) => {
    const li = document.createElement('li');
    li.className = 'item' + (p.name === state.selected ? ' sel' : '');
    const ico = document.createElement('div'); ico.className = 'ico'; ico.innerHTML = ICON_GLOBE;
    const meta = document.createElement('div'); meta.className = 'meta';
    const b = document.createElement('b'); b.textContent = p.name;
    const s = document.createElement('small');
    if (p.valid) s.textContent = p.endpoint; else { s.textContent = 'Некорректный конфиг'; s.className = 'bad'; }
    meta.append(b, s);
    const del = document.createElement('button'); del.className = 'del'; del.title = 'Удалить'; del.innerHTML = ICON_TRASH;
    del.addEventListener('click', (e) => { e.stopPropagation(); removeProfile(p.name); });
    li.append(ico, meta, del);
    li.addEventListener('click', () => { if (!state.active && !state.busy) { state.selected = p.name; render(); } });
    list.append(li);
  });
  $('empty').hidden = profiles.length > 0;

  const warn = $('warn');
  let msg = '';
  if (state.offline) msg = 'Нет связи с ProVpn. Запустите приложение заново.';
  else if (state.toolsMissing) msg = 'WireGuard не найден. Установите его: wireguard.com/install';
  else if (state.privileged === false) msg = 'Нет прав администратора: подключение не сработает. Запустите ProVpn от имени администратора.';
  warn.hidden = !msg;
  warn.textContent = msg;
  if (state.offline) $('pillText').textContent = 'Нет связи';
}

function fail(e) {
  const extra = e.details ? '\n' + [].concat(e.details).join('\n') : '';
  alert(e.message + extra);
}

async function refresh() {
  try {
    const s = await api('GET', '/api/state');
    state.profiles = s.profiles;
    state.active = s.active;
    state.privileged = s.privileged;
    state.toolsMissing = s.toolsMissing;
    state.offline = false;
    if (s.active) state.selected = s.active.name;
    else if (!state.profiles.some((p) => p.name === state.selected)) state.selected = (state.profiles.find((p) => p.valid) || {}).name || null;
    render();
  } catch (e) { state.offline = true; render(); }
}

async function toggle() {
  if (state.busy) return;
  if (!state.active && !state.selected) { $('addBtn').click(); return; }
  state.busy = true; render();
  try {
    if (state.active) await api('POST', '/api/disconnect', { name: state.active.name });
    else await api('POST', '/api/connect', { name: state.selected });
  } catch (e) { fail(e); }
  state.busy = false;
  await refresh();
}

async function removeProfile(name) {
  if (!confirm(`Удалить профиль ${name}?`)) return;
  try { await api('DELETE', `/api/profiles/${encodeURIComponent(name)}`); } catch (e) { fail(e); }
  refresh();
}

// ---- dialog ----------------------------------------------------------------
const dlg = $('dlg');
$('addBtn').addEventListener('click', () => { $('fErr').hidden = true; dlg.showModal(); });
$('fCancel').addEventListener('click', () => dlg.close());
$('fPick').addEventListener('click', () => $('fFile').click());
$('fFile').addEventListener('change', async (e) => {
  const f = e.target.files[0];
  if (!f) return;
  $('fConf').value = await f.text();
  if (!$('fName').value) $('fName').value = f.name.replace(/\.conf$/i, '').replace(/[^A-Za-z0-9_-]/g, '').slice(0, 15);
});
$('form').addEventListener('submit', async (e) => {
  e.preventDefault();
  try {
    await api('POST', '/api/profiles', { name: $('fName').value.trim(), conf: $('fConf').value });
    state.selected = $('fName').value.trim();
    $('fName').value = ''; $('fConf').value = '';
    dlg.close();
    refresh();
  } catch (err) {
    const box = $('fErr');
    box.hidden = false;
    box.textContent = err.message + (err.details ? '\n' + err.details.join('\n') : '');
  }
});

$('power').addEventListener('click', toggle);
refresh();
setInterval(() => { if (!state.busy) refresh(); }, 3000);
