(function () {
  'use strict';
  const Src = globalThis.PluginSoundSources;
  const Art = globalThis.PluginSoundArt;
  const CacheMod = globalThis.PluginSoundCache;
  const $ = (s, r) => (r || document).querySelector(s);
  const $$ = (s, r) => Array.from((r || document).querySelectorAll(s));
  const esc = s => String(s == null ? '' : s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  const sleep = ms => new Promise(r => setTimeout(r, ms));

  /* ---------- storage (falls back to memory when localStorage is unavailable) ---------- */
  const storage = (() => {
    try { localStorage.setItem('pluginsound.t', '1'); localStorage.removeItem('pluginsound.t'); return localStorage; }
    catch (e) { const m = {}; return { getItem: k => (k in m ? m[k] : null), setItem: (k, v) => { m[k] = String(v); }, removeItem: k => { delete m[k]; } }; }
  })();
  const store = {
    get(k, d) { try { const v = storage.getItem('pluginsound.' + k); return v == null ? d : JSON.parse(v); } catch (e) { return d; } },
    set(k, v) { try { storage.setItem('pluginsound.' + k, JSON.stringify(v)); } catch (e) { /* ignore */ } },
    del(k) { try { storage.removeItem('pluginsound.' + k); } catch (e) { /* ignore */ } }
  };
  const SETTING_DEFAULTS = { dzCountry: 'Russia', vkToken: '', yandexToken: '', spotifyId: '', spotifySecret: '', scClientId: '' };
  const Cache = CacheMod.create(storage);

  const state = {
    view: 'charts',
    source: Src.byId(store.get('source', 'deezer')).id,
    settings: Object.assign({}, SETTING_DEFAULTS, store.get('settings', {})),
    status: {},                       // sourceId -> { loading, error, promise }
    lists: { chart: [], search: [], lib: [] },
    fav: store.get('fav', []),
    queue: [], idx: -1, listName: 'chart',
    playing: false
  };

  /* ---------- network ---------- */
  async function browserJSON(url, opts) {
    const ctl = new AbortController();
    const timer = setTimeout(() => ctl.abort(), 15000);
    try {
      const r = await fetch(url, Object.assign({}, opts || {}, { signal: ctl.signal }));
      if (!r.ok) throw new Error('HTTP ' + r.status);
      return await r.json();
    } finally { clearTimeout(timer); }
  }
  const ctx = {
    get settings() { return state.settings; },
    kv: { get: k => store.get('kv.' + k, null), set: (k, v) => store.set('kv.' + k, v), del: k => store.del('kv.' + k) },
    json(url, opts) { return window.psNative ? window.psNative.fetchJSON(url, opts) : browserJSON(url, opts); }
  };

  /* ---------- helpers ---------- */
  const fmt = s => { s = Math.max(0, Math.round(s || 0)); return Math.floor(s / 60) + ':' + String(s % 60).padStart(2, '0'); };
  const hueOf = str => Art.hash(str) % 360;
  const artOf = t => Art.uri(t.title + '|' + t.artist);
  function cover(t, cls) {
    // generative SVG art is always underneath; a real cover image (if any and if it loads) fades in above it
    return '<div class="cover ' + (cls || '') + '" style="background-image:url(' + artOf(t) + ')">' +
      (t.cover ? '<img src="' + esc(t.cover) + '" alt="" loading="lazy" decoding="async" referrerpolicy="no-referrer">' : '') + '</div>';
  }
  const srcIcon = s => '<span class="si"><i style="background:' + s.color + '"></i><img src="' + esc(Src.iconUrl(s)) + '" alt="" width="20" height="20" decoding="async" referrerpolicy="no-referrer"></span>';
  function emptyHTML(kind, title, sub) {
    return '<div class="empty"><img src="img/empty-' + kind + '.svg" alt="" width="240" height="180"><b>' + esc(title) + '</b><span>' + esc(sub || '') + '</span></div>';
  }
  const isFav = t => state.fav.some(f => f.id === t.id);
  const sameTrack = (a, b) => !!a && !!b && a.id === b.id;
  function whenText(ms) {
    const d = new Date(ms), hm = String(d.getHours()).padStart(2, '0') + ':' + String(d.getMinutes()).padStart(2, '0');
    if (CacheMod.dayKey(ms) === CacheMod.dayKey(Date.now())) return 'сегодня в ' + hm;
    return String(d.getDate()).padStart(2, '0') + '.' + String(d.getMonth() + 1).padStart(2, '0') + ' в ' + hm;
  }
  let toastTimer = 0;
  function toast(msg) {
    const el = $('#toast'); el.textContent = msg; el.classList.add('on');
    clearTimeout(toastTimer); toastTimer = setTimeout(() => el.classList.remove('on'), 2800);
  }
  // image lifecycle: covers fade in when loaded, broken ones disappear, service icons replace the colour dot
  document.addEventListener('load', e => {
    const t = e.target;
    if (!t || t.tagName !== 'IMG') return;
    if (t.parentElement && t.parentElement.classList.contains('si')) t.parentElement.classList.add('ok');
    else if (t.closest('.cover')) t.classList.add('ld');
  }, true);
  document.addEventListener('error', e => { if (e.target && e.target.tagName === 'IMG') e.target.classList.add('noimg'); }, true);

  /* ---------- charts: cache + daily refresh ---------- */
  const NEEDS_MSG = s => ({ message: 'Для «' + Src.label(s, state.settings) + '» укажите ключ в настройках', code: Src.NEEDS });

  function refresh(id, opts) {
    const s = Src.byId(id);
    const st = state.status[id] || (state.status[id] = {});
    if (st.loading) return st.promise;
    const entry = Cache.get(id);
    if (!(opts && opts.force) && entry && !CacheMod.isStale(entry)) return Promise.resolve();
    if (!Src.hasKeys(s, state.settings)) { st.error = NEEDS_MSG(s); if (state.source === id) renderChart(); return Promise.resolve(); }
    st.loading = true; st.error = null;
    if (state.source === id) renderStatus();
    st.promise = (async () => {
      try {
        const tracks = await s.fetch(ctx);
        if (!tracks.length) throw new Error(Src.label(s, state.settings) + ': источник вернул пустой чарт');
        Cache.set(id, tracks);
      } catch (e) {
        st.error = { message: e && e.message ? e.message : String(e), code: e && e.code };
      } finally {
        st.loading = false;
        if (state.source === id) renderChart();
      }
    })();
    return st.promise;
  }

  let warming = false;
  async function warmup() {                       // keep the other tabs ready so switching is instant and works offline
    if (warming) return;
    warming = true;
    try {
      for (const s of Src.SOURCES) {
        if (s.id === state.source || !Src.hasKeys(s, state.settings)) continue;
        if (!CacheMod.isStale(Cache.get(s.id))) continue;
        await refresh(s.id);
        await sleep(600);
      }
    } finally { warming = false; }
  }
  async function ensureFresh() {
    await refresh(state.source);
    warmup();
  }

  /* ---------- rendering ---------- */
  function rowHTML(t, i, list) {
    const cur = state.listName === list && sameTrack(state.queue[state.idx], t);
    return '<div class="row' + (cur ? ' cur' : '') + (cur && state.playing ? ' playing' : '') + '" data-list="' + list + '" data-i="' + i + '" style="--d:' + Math.min(i, 14) + '">' +
      '<div class="rk' + (i < 3 && list === 'chart' ? ' podium' : '') + '"><span class="n">' + (i + 1) + '</span><i class="eq"><b></b><b></b><b></b></i></div>' + cover(t, 'sm') +
      '<div class="meta"><b>' + esc(t.title) + '</b><span>' + esc(t.artist) + '</span></div>' +
      '<div class="dur">' + (t.duration ? fmt(t.duration) : '') + '</div>' +
      '<button class="ico like' + (isFav(t) ? ' on' : '') + '" data-act="like" aria-label="В избранное"><svg class="ic"><use href="#i-heart"/></svg></button>' +
      '<button class="ico play" data-act="play" aria-label="Играть"><svg class="ic"><use href="#i-' + (cur && state.playing ? 'pause' : 'play') + '"/></svg></button></div>';
  }
  const sig = new Map();                          // container selector -> signature of what is rendered
  function fillList(sel, list, name, emptyHTMLText) {
    state.lists[name] = list;
    const sg = name + '|' + list.map(t => t.id).join(',');
    if (sig.get(sel) === sg && list.length) return;   // identical content: keep the DOM (no flicker, no re-animation)
    sig.set(sel, sg);
    $(sel).innerHTML = list.length ? list.map((t, i) => rowHTML(t, i, name)).join('') : emptyHTMLText;
  }

  function renderPills() {
    $('#pills').innerHTML = Src.SOURCES.map(s =>
      '<button class="pill' + (s.id === state.source ? ' on' : '') + '" data-src="' + s.id + '" style="--c:' + s.color + '">' + srcIcon(s) +
      '<span class="pn">' + esc(Src.label(s, state.settings)) + '</span>' +
      (s.keys.length && !Src.hasKeys(s, state.settings) ? '<small>КЛЮЧ</small>' : '') + '</button>').join('');
  }
  function markPills() { $$('.pill').forEach(p => p.classList.toggle('on', p.dataset.src === state.source)); }

  function renderStatus() {
    const s = Src.byId(state.source), st = state.status[s.id] || {}, entry = Cache.get(s.id);
    const loading = !!st.loading;
    $('#upd').classList.toggle('spin', loading);
    $('#upd-text').textContent = loading ? 'Обновляю чарт…'
      : entry ? 'Обновлено ' + whenText(entry.at) + (st.error && st.error.code !== Src.NEEDS ? ' · не удалось обновить' : '')
        : '';
  }

  let heroSig = '';
  function renderChart() {
    const s = Src.byId(state.source), st = state.status[s.id] || {}, entry = Cache.get(s.id);
    markPills(); renderStatus();
    const banner = $('#banner');
    let tracks, demo = false;
    if (entry) {
      tracks = entry.tracks;
      if (st.error && st.error.code !== Src.NEEDS) {
        banner.hidden = false;
        banner.innerHTML = '<span>' + esc(st.error.message) + ' Показан сохранённый чарт.</span><button class="btn ghost" data-act="refresh">Повторить</button>';
      } else banner.hidden = true;
    } else if (st.loading || !st.error) {
      banner.hidden = true; heroSig = '';
      $('#hero').innerHTML = '<div class="skel" style="width:100%;height:164px"></div>';
      sig.delete('#chart-list');
      $('#chart-list').innerHTML = new Array(8).fill('<div class="skel"></div>').join('');
      return;
    } else {
      tracks = Src.demo(); demo = true;
      banner.hidden = false;
      banner.innerHTML = '<span>' + esc(st.error.message) + (s.experimental && st.error.code !== Src.NEEDS ? ' (источник экспериментальный)' : '') + ' — показаны демо-данные (вымышленные треки без звука).</span>' +
        (st.error.code === Src.NEEDS ? '<button class="btn" data-act="settings">Открыть настройки</button>' : '<button class="btn ghost" data-act="refresh">Повторить</button>');
    }
    const top = tracks[0];
    const hs = s.id + '|' + (top ? top.id : '') + '|' + demo;
    if (top && hs !== heroSig) {
      heroSig = hs;
      $('#hero').innerHTML =
        '<div class="hbg" style="background-image:url(' + (top.cover ? '&quot;' + esc(top.cover) + '&quot;' : artOf(top)) + ')"></div>' +
        '<img class="wave" src="img/hero-wave.svg" alt="">' + cover(top, '') +
        '<div class="info"><div class="eyebrow">' + srcIcon(s) + '№1 · ' + esc(Src.label(s, state.settings)) + (demo ? ' · демо' : '') + '</div>' +
        '<h2>' + esc(top.title) + '</h2><p>' + esc(top.artist) + '</p>' +
        '<button class="btn" data-act="play-top"><svg class="ic"><use href="#i-play"/></svg>Слушать чарт</button></div>';
    }
    fillList('#chart-list', tracks, 'chart', emptyHTML('search', 'Чарт пуст', 'Попробуйте другой источник'));
  }

  function renderLib() { sig.delete('#lib-list'); fillList('#lib-list', state.fav.slice(), 'lib', emptyHTML('lib', 'Пока пусто', 'Нажмите сердечко у трека, и он появится здесь')); }
  function renderSearchIdle() { sig.delete('#search-list'); fillList('#search-list', [], 'search', emptyHTML('search', 'Найдите любой трек', 'Введите название или исполнителя в поле сверху')); }

  const TITLES = {
    charts: ['Чарты', 'Топ-чарты, обновляются каждый день'],
    search: ['Поиск', 'Найдите любой трек или исполнителя'],
    lib: ['Избранное', 'Ваша личная подборка']
  };
  function setView(v) {
    state.view = v;
    $$('.view').forEach(el => el.classList.toggle('on', el.id === 'view-' + v));
    $$('[data-view]').forEach(el => el.classList.toggle('on', el.dataset.view === v));
    $('#title').textContent = TITLES[v][0];
    $('#subtitle').textContent = TITLES[v][1];
    if (v === 'lib') renderLib();
    if (v === 'search') $('#q').focus({ preventScroll: true });
  }

  /* ---------- search ---------- */
  let qTimer = 0, qSeq = 0;
  function onQuery() {
    clearTimeout(qTimer);
    const q = $('#q').value.trim();
    if (!q) { qSeq++; renderSearchIdle(); return; }
    qTimer = setTimeout(async () => {
      const seq = ++qSeq;
      if (state.view !== 'search') setView('search');
      sig.delete('#search-list');
      $('#search-list').innerHTML = new Array(6).fill('<div class="skel"></div>').join('');
      try {
        const res = await Src.search(q, ctx);
        if (seq === qSeq) fillList('#search-list', res, 'search', emptyHTML('search', 'Ничего не найдено', 'Попробуйте другой запрос'));
      } catch (e) {
        if (seq === qSeq) $('#search-list').innerHTML = emptyHTML('search', 'Поиск недоступен', e.message);
      }
    }, 380);
  }

  /* ---------- player ---------- */
  const audio = new Audio();
  audio.preload = 'auto';
  audio.volume = (store.get('vol', 80)) / 100;
  $('#vol').value = Math.round(audio.volume * 100);

  function setRange(el) { el.style.setProperty('--p', (el.value - el.min) / (el.max - el.min) * 100 + '%'); }
  function setPlayIcon() { $('#p-play use').setAttribute('href', state.playing ? '#i-pause' : '#i-play'); }
  function markCurrent() {
    $$('.row').forEach(r => {
      const t = state.lists[r.dataset.list] && state.lists[r.dataset.list][r.dataset.i];
      const cur = !!t && state.listName === r.dataset.list && sameTrack(state.queue[state.idx], t);
      r.classList.toggle('cur', cur);
      r.classList.toggle('playing', cur && state.playing);
      const u = $('.play use', r); if (u) u.setAttribute('href', cur && state.playing ? '#i-pause' : '#i-play');
    });
  }
  function showNow(t) {
    $('#p-title').textContent = t.title;
    $('#p-artist').textContent = t.artist;
    $('#p-cover').outerHTML = cover(t, 'sm').replace('class="cover sm"', 'id="p-cover" class="cover sm"');
    $('#p-like').classList.toggle('on', isFav(t));
    document.documentElement.style.setProperty('--hue', hueOf(t.title + t.artist));
    document.title = t.title + ' · ' + t.artist + ' — PluginSound';
    if ('mediaSession' in navigator && window.MediaMetadata) {
      navigator.mediaSession.metadata = new MediaMetadata({ title: t.title, artist: t.artist, artwork: t.cover ? [{ src: t.cover }] : [] });
    }
  }

  let playToken = 0;
  async function playIndex(listName, i) {
    const list = state.lists[listName];
    const t = list && list[i];
    if (!t) return;
    const token = ++playToken;
    state.queue = list; state.idx = i; state.listName = listName;
    t.retried = false;
    showNow(t); markCurrent();
    let url = null, failed = false;
    try { url = await Src.resolvePreview(t, ctx); } catch (e) { failed = true; toast('Не удалось найти аудио: ' + e.message); }
    if (token !== playToken) return;                // the user moved on while we were resolving
    if (!url) {
      state.playing = false; setPlayIcon(); markCurrent();
      if (t.demo) toast('Демо-трек: подключите источник, чтобы слушать');
      else if (!failed) toast('Для этого трека нет аудио-превью');
      return;
    }
    audio.src = url;
    try { await audio.play(); } catch (e) {
      if (e && e.name === 'AbortError') return;     // a newer track replaced this one; not an error
      state.playing = false; setPlayIcon(); markCurrent(); toast('Воспроизведение заблокировано: ' + e.message);
    }
  }
  function step(d) {
    if (!state.queue.length) return;
    playIndex(state.listName, (state.idx + d + state.queue.length) % state.queue.length);
  }
  function togglePlay() {
    if (!audio.src) { if (state.lists.chart.length) playIndex('chart', 0); return; }
    if (audio.paused) audio.play().catch(() => { }); else audio.pause();
  }
  audio.addEventListener('play', () => { state.playing = true; setPlayIcon(); markCurrent(); if ('mediaSession' in navigator) navigator.mediaSession.playbackState = 'playing'; });
  audio.addEventListener('pause', () => { state.playing = false; setPlayIcon(); markCurrent(); if ('mediaSession' in navigator) navigator.mediaSession.playbackState = 'paused'; });
  audio.addEventListener('ended', () => step(1));
  audio.addEventListener('error', async () => {
    const t = state.queue[state.idx], token = playToken;
    if (t && !t.retried && !t.demo && audio.src) {  // signed preview links expire: fetch a fresh one once and retry
      t.retried = true;
      try {
        const url = await Src.resolvePreview(t, ctx, { fresh: true });
        if (url && token === playToken && url !== audio.src) { audio.src = url; await audio.play(); return; }
      } catch (e) { /* fall through to the error toast */ }
    }
    if (token !== playToken) return;
    state.playing = false; setPlayIcon(); markCurrent(); toast('Не удалось загрузить аудио');
  });
  let seeking = false;
  audio.addEventListener('timeupdate', () => {
    const d = audio.duration || 0;
    if (d && !seeking) { const s = $('#seek'); s.value = Math.round(audio.currentTime / d * 1000); setRange(s); }
    $('#p-time').textContent = fmt(audio.currentTime);
  });
  const seekEl = $('#seek');
  ['pointerdown', 'touchstart'].forEach(ev => seekEl.addEventListener(ev, () => { seeking = true; }, { passive: true }));
  ['pointerup', 'touchend', 'change'].forEach(ev => seekEl.addEventListener(ev, () => { seeking = false; }));
  seekEl.addEventListener('input', e => { setRange(e.target); if (audio.duration) audio.currentTime = e.target.value / 1000 * audio.duration; });
  $('#vol').addEventListener('input', e => { setRange(e.target); audio.volume = e.target.value / 100; store.set('vol', +e.target.value); });
  $('#p-play').addEventListener('click', togglePlay);
  $('#p-next').addEventListener('click', () => step(1));
  $('#p-prev').addEventListener('click', () => step(-1));
  if ('mediaSession' in navigator) {
    const set = (a, f) => { try { navigator.mediaSession.setActionHandler(a, f); } catch (e) { /* unsupported action */ } };
    set('play', togglePlay); set('pause', togglePlay); set('nexttrack', () => step(1)); set('previoustrack', () => step(-1));
  }

  /* ---------- favourites (stored without preview URL: Deezer links expire) ---------- */
  function toggleFav(t) {
    if (!t || t.demo) return toast('Демо-треки нельзя добавить в избранное');
    const i = state.fav.findIndex(f => f.id === t.id);
    if (i >= 0) state.fav.splice(i, 1);
    else state.fav.unshift({ id: t.id, src: t.src, title: t.title, artist: t.artist, cover: t.cover, duration: t.duration, preview: null, url: t.url });
    store.set('fav', state.fav);
    $$('.row').forEach(r => { const x = state.lists[r.dataset.list][r.dataset.i]; if (x) $('.like', r).classList.toggle('on', isFav(x)); });
    const cur = state.queue[state.idx]; if (cur) $('#p-like').classList.toggle('on', isFav(cur));
    if (state.view === 'lib') renderLib();
  }

  /* ---------- settings ---------- */
  const dlg = $('#settings');
  $('[data-key=dzCountry]').innerHTML = Src.COUNTRIES.map(c => '<option value="' + esc(c.en) + '">' + esc(c.ru) + '</option>').join('');
  function openSettings() {
    $$('[data-key]', dlg).forEach(el => { el.value = state.settings[el.dataset.key] || SETTING_DEFAULTS[el.dataset.key] || ''; });
    dlg.showModal();
  }
  $('#settings-form').addEventListener('submit', e => {
    // Save is the first submit button, so Enter inside a field saves; only the explicit "Закрыть" cancels
    if (e.submitter && e.submitter.value === 'cancel') return;
    $$('[data-key]', dlg).forEach(el => { state.settings[el.dataset.key] = el.value.trim(); });
    store.set('settings', state.settings);
    state.status = {};
    renderPills();
    ensureFresh();
    renderChart();
  });

  /* ---------- events ---------- */
  document.addEventListener('click', e => {
    const view = e.target.closest('[data-view]');
    if (view) return setView(view.dataset.view);
    const pill = e.target.closest('[data-src]');
    if (pill) {
      state.source = pill.dataset.src; store.set('source', state.source);
      heroSig = ''; renderChart(); refresh(state.source);
      pill.scrollIntoView({ block: 'nearest', inline: 'center', behavior: 'smooth' });
      return;
    }
    const act = e.target.closest('[data-act]');
    const row = e.target.closest('.row');
    if (act && !row) {
      switch (act.dataset.act) {
        case 'settings': return openSettings();
        case 'refresh': return refresh(state.source, { force: true });
        case 'play-top': return playIndex('chart', 0);
        case 'like-now': return toggleFav(state.queue[state.idx]);
      }
      return;
    }
    if (row) {
      const list = row.dataset.list, i = +row.dataset.i, t = state.lists[list][i];
      if (act && act.dataset.act === 'like') return toggleFav(t);
      if (state.listName === list && sameTrack(state.queue[state.idx], t) && audio.src) return togglePlay();
      playIndex(list, i);
    }
  });
  $('#q').addEventListener('input', onQuery);
  $('#q').addEventListener('keydown', e => { if (e.key === 'Enter') { clearTimeout(qTimer); qTimer = 0; onQuery(); } });
  document.addEventListener('keydown', e => {
    if (e.target.closest('input, select, textarea, button, dialog')) return;   // Space on a focused button must only click it
    if (e.code === 'Space') { e.preventDefault(); togglePlay(); }
    else if (e.code === 'ArrowRight' && e.shiftKey) step(1);
    else if (e.code === 'ArrowLeft' && e.shiftKey) step(-1);
  });
  document.addEventListener('visibilitychange', () => {
    document.documentElement.classList.toggle('idle', document.hidden);   // pause background animation when hidden
    if (!document.hidden) ensureFresh();                                   // a new day may have started while we were away
  });
  window.addEventListener('online', ensureFresh);
  setInterval(ensureFresh, 10 * 60 * 1000);                                // roll over to the new day while the app stays open

  /* ---------- boot ---------- */
  setRange($('#seek')); setRange($('#vol'));
  renderPills();
  renderSearchIdle();
  setView('charts');
  renderChart();                                  // instant: the last saved chart, if any
  setTimeout(ensureFresh, 0);                     // then revalidate in the background
})();
