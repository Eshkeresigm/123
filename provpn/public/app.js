'use strict';

const $ = (id) => document.getElementById(id);
const state = { profiles: [], subs: [], active: null, selected: null, busy: false, privileged: null };
const ICON_LINK = '<svg viewBox="0 0 24 24"><path d="M10 14a4 4 0 0 0 5.700 0l3-3a4 4 0 0 0-5.700-5.700l-1 1M14 10a4 4 0 0 0-5.700 0l-3 3a4 4 0 0 0 5.700 5.700l1-1"/></svg>';
const ICON_REFRESH = '<svg viewBox="0 0 24 24"><path d="M20 11a8 8 0 0 0-14.500-4M4 5v3h3M4 13a8 8 0 0 0 14.500 4M20 19v-3h-3"/></svg>';

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
  $('empty').hidden = profiles.length > 0 || state.subs.length > 0;

  const subsEl = $('subs');
  subsEl.textContent = '';
  state.subs.forEach((sub) => {
    const li = document.createElement('li');
    li.className = 'item sub';
    const ico = document.createElement('div'); ico.className = 'ico'; ico.innerHTML = ICON_LINK;
    const meta = document.createElement('div'); meta.className = 'meta';
    const b = document.createElement('b'); b.textContent = sub.host;
    const s = document.createElement('small');
    const un = Object.entries(sub.unsupported || {}).map(([k, n]) => `${k} ${n}`).join(', ');
    s.textContent = `${sub.count} серв. · ${new Date(sub.updatedAt).toLocaleString('ru-RU')}` + (un ? ` · не поддерживается: ${un}` : '');
    meta.append(b, s);
    const ref = document.createElement('button'); ref.className = 'del'; ref.title = 'Обновить'; ref.innerHTML = ICON_REFRESH;
    ref.addEventListener('click', (e) => { e.stopPropagation(); refreshSub(sub.id); });
    const del = document.createElement('button'); del.className = 'del'; del.title = 'Удалить подписку'; del.innerHTML = ICON_TRASH;
    del.addEventListener('click', (e) => { e.stopPropagation(); removeSub(sub.id); });
    li.append(ico, meta, ref, del);
    subsEl.append(li);
  });

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
    state.subs = s.subscriptions || [];
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

// ---- subscriptions ---------------------------------------------------------
let noteTimer = null;
function note(msg) {
  const el = $('note');
  el.textContent = msg;
  el.hidden = false;
  clearTimeout(noteTimer);
  noteTimer = setTimeout(() => { el.hidden = true; }, 9000);
}

async function refreshSub(id) {
  try {
    const r = await api('POST', `/api/subscriptions/${id}/refresh`);
    note(`Подписка обновлена: ${r.imported} серверов`);
  } catch (e) { fail(e); }
  refresh();
}

async function removeSub(id) {
  if (!confirm('Удалить подписку и все её серверы?')) return;
  try { await api('DELETE', `/api/subscriptions/${id}`); } catch (e) { fail(e); }
  refresh();
}

const dlgSub = $('dlgSub');
$('subBtn').addEventListener('click', () => { $('sErr').hidden = true; dlgSub.showModal(); $('sUrl').focus(); });
$('sCancel').addEventListener('click', () => dlgSub.close());
$('formSub').addEventListener('submit', async (e) => {
  e.preventDefault();
  const go = $('sGo');
  go.disabled = true; go.textContent = 'Загрузка…'; $('sErr').hidden = true;
  try {
    const r = await api('POST', '/api/subscriptions', { url: $('sUrl').value.trim() });
    dlgSub.close();
    $('sUrl').value = '';
    const un = Object.entries(r.unsupported || {}).map(([k, n]) => `${k}: ${n}`).join(', ');
    note(`Добавлено серверов: ${r.imported}` + (r.skipped.length ? `, пропущено: ${r.skipped.length}` : '') + (un ? `. Не поддерживается: ${un}` : ''));
    await refresh();
  } catch (err) {
    const box = $('sErr');
    box.hidden = false;
    box.textContent = err.message + (err.details && err.details.length ? '\n' + err.details.join('\n') : '');
  } finally { go.disabled = false; go.textContent = 'Загрузить'; }
});

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
