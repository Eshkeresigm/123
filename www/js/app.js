(function () {
  'use strict';
  const Src = globalThis.PluginSoundSources;
  const $ = (s, r) => (r || document).querySelector(s);
  const $$ = (s, r) => Array.from((r || document).querySelectorAll(s));
  const esc = s => String(s == null ? '' : s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  const store = {
    get(k, d) { try { const v = localStorage.getItem('pluginsound.' + k); return v == null ? d : JSON.parse(v); } catch (e) { return d; } },
    set(k, v) { try { localStorage.setItem('pluginsound.' + k, JSON.stringify(v)); } catch (e) { /* storage unavailable */ } }
  };
  const SETTING_DEFAULTS = { appleCountry: 'ru', yandexToken: '', spotifyId: '', spotifySecret: '', scClientId: '' };

  const state = {
    view: 'charts',
    source: store.get('source', 'deezer'),
    settings: Object.assign({}, SETTING_DEFAULTS, store.get('settings', {})),
    cache: {},          // sourceId -> { at, tracks, error, demo }
    lists: { chart: [], search: [], lib: [] },
    fav: store.get('fav', []),
    queue: [], idx: -1, listName: 'chart',
    playing: false
  };
  const TTL = 10 * 60 * 1000;

  /* ---------- network ---------- */
  async function browserJSON(url, opts) {
    const r = await fetch(url, opts || {});
    if (!r.ok) throw new Error('HTTP ' + r.status);
    return r.json();
  }
  const ctx = {
    get settings() { return state.settings; },
    json(url, opts) { return window.psNative ? window.psNative.fetchJSON(url, opts) : browserJSON(url, opts); }
  };

  /* ---------- helpers ---------- */
  const fmt = s => { s = Math.max(0, Math.round(s || 0)); return Math.floor(s / 60) + ':' + String(s % 60).padStart(2, '0'); };
  function hueOf(str) { let h = 0; for (const c of String(str)) h = (h * 31 + c.charCodeAt(0)) >>> 0; return h % 360; }
  const artOf = t => globalThis.PluginSoundArt.uri(t.title + '|' + t.artist);
  function cover(t, cls) {
    // generative SVG art is always underneath; a real cover image (if any and if it loads) covers it
    return '<div class="cover ' + (cls || '') + '" style="background-image:url(' + artOf(t) + ')">' +
      (t.cover ? '<img src="' + esc(t.cover) + '" alt="" loading="lazy" referrerpolicy="no-referrer">' : '') + '</div>';
  }
  function emptyHTML(kind, title, sub) {
    return '<div class="empty"><img src="img/empty-' + kind + '.svg" alt="" width="240" height="180"><b>' + esc(title) + '</b><span>' + esc(sub || '') + '</span></div>';
  }
  const isFav = t => state.fav.some(f => f.id === t.id);
  let toastTimer = 0;
  function toast(msg) {
    const el = $('#toast'); el.textContent = msg; el.classList.add('on');
    clearTimeout(toastTimer); toastTimer = setTimeout(() => el.classList.remove('on'), 2600);
  }
  document.addEventListener('error', e => { if (e.target && e.target.tagName === 'IMG') e.target.classList.add('noimg'); }, true);

  /* ---------- rendering ---------- */
  function rowHTML(t, i, list) {
    const cur = state.listName === list && state.queue[state.idx] && state.queue[state.idx].id === t.id;
    return '<div class="row' + (cur ? ' cur' : '') + '" data-list="' + list + '" data-i="' + i + '" style="--d:' + Math.min(i, 20) + '">' +
      '<div class="rk' + (i < 3 && list === 'chart' ? ' podium' : '') + '"><span class="n">' + (i + 1) + '</span><i class="eq"><b></b><b></b><b></b></i></div>' + cover(t, 'sm') +
      '<div class="meta"><b>' + esc(t.title) + '</b><span>' + esc(t.artist) + '</span></div>' +
      '<div class="dur">' + (t.duration ? fmt(t.duration) : '') + '</div>' +
      '<button class="ico like' + (isFav(t) ? ' on' : '') + '" data-act="like" aria-label="В избранное"><svg class="ic"><use href="#i-heart"/></svg></button>' +
      '<button class="ico play" data-act="play" aria-label="Играть"><svg class="ic"><use href="#i-play"/></svg></button></div>';
  }
  function fillList(sel, list, name, emptyText) {
    state.lists[name] = list;
    $(sel).innerHTML = list.length ? list.map((t, i) => rowHTML(t, i, name)).join('') : emptyText;
  }

  function renderPills() {
    $('#pills').innerHTML = Src.SOURCES.map(s =>
      '<button class="pill' + (s.id === state.source ? ' on' : '') + '" data-src="' + s.id + '" style="--c:' + s.color + '"><i></i>' + esc(s.name) +
      (s.keys.length ? '<small>КЛЮЧ</small>' : '') + '</button>').join('');
  }

  function renderChart() {
    const s = Src.byId(state.source);
    const c = state.cache[s.id];
    renderPills();
    const banner = $('#banner');
    if (!c) {
      banner.hidden = true;
      $('#hero').innerHTML = '<div class="skel" style="width:100%;height:164px"></div>';
      $('#chart-list').innerHTML = new Array(8).fill('<div class="skel"></div>').join('');
      return;
    }
    const tracks = c.tracks;
    state.lists.chart = tracks;
    if (c.error) {
      banner.hidden = false;
      banner.innerHTML = '<span>' + esc(c.error.message) + ' — показаны демо-данные (вымышленные треки без звука).</span>' +
        (c.error.code === Src.NEEDS ? '<button class="btn" data-act="settings">Открыть настройки</button>' : '<button class="btn ghost" data-act="retry">Повторить</button>');
    } else banner.hidden = true;

    const top = tracks[0];
    if (top) {
      $('#hero').innerHTML =
        '<div class="hbg" style="background-image:url(' + (top.cover ? '&quot;' + esc(top.cover) + '&quot;' : artOf(top)) + ')"></div>' +
        '<img class="wave" src="img/hero-wave.svg" alt="">' +
        cover(top, '') +
        '<div class="info"><div class="eyebrow" style="--dot:' + s.color + '"><i></i>№1 · ' + esc(s.name) + (c.error ? ' · демо' : '') + '</div>' +
        '<h2>' + esc(top.title) + '</h2><p>' + esc(top.artist) + '</p>' +
        '<button class="btn" data-act="play-top"><svg class="ic"><use href="#i-play"/></svg>Слушать чарт</button></div>';
    }
    fillList('#chart-list', tracks, 'chart', emptyHTML('search', 'Чарт пуст', 'Попробуйте другой источник'));
  }

  async function loadChart(id, force) {
    const s = Src.byId(id);
    const c = state.cache[id];
    if (!force && c && !c.error && Date.now() - c.at < TTL) return renderChart();
    delete state.cache[id];
    renderChart();
    let entry;
    try {
      const tracks = await s.fetch(ctx);
      if (!tracks.length) throw new Error(s.name + ': источник вернул пустой чарт');
      entry = { at: Date.now(), tracks };
    } catch (e) {
      const code = e && e.code, raw = e && e.message ? e.message : String(e);
      entry = { at: Date.now(), tracks: Src.demo(), error: { message: code === Src.NEEDS ? raw : 'Не удалось загрузить «' + s.name + '»: ' + raw, code } };
    }
    state.cache[id] = entry;
    if (state.source === id) renderChart();
  }

  function renderLib() { fillList('#lib-list', state.fav.slice(), 'lib', emptyHTML('lib', 'Пока пусто', 'Нажмите сердечко у трека, и он появится здесь')); }
  function renderSearchIdle() { fillList('#search-list', [], 'search', emptyHTML('search', 'Найдите любой трек', 'Введите название или исполнителя в поле сверху')); }

  const TITLES = {
    charts: ['Чарты', 'Что слушает мир прямо сейчас'],
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
    if (v === 'search') $('#q').focus();
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
      $('#search-list').innerHTML = new Array(6).fill('<div class="skel"></div>').join('');
      try {
        const res = await Src.search(q, ctx);
        if (seq === qSeq) fillList('#search-list', res, 'search', emptyHTML('search', 'Ничего не найдено', 'Попробуйте другой запрос'));
      } catch (e) {
        if (seq === qSeq) $('#search-list').innerHTML = emptyHTML('search', 'Поиск недоступен', e.message);
      }
    }, 420);
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
      const cur = t && state.listName === r.dataset.list && state.queue[state.idx] && state.queue[state.idx].id === t.id;
      r.classList.toggle('cur', !!cur);
      r.classList.toggle('playing', !!cur && state.playing);
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

  async function playIndex(listName, i) {
    const list = state.lists[listName];
    const t = list && list[i];
    if (!t) return;
    state.queue = list; state.idx = i; state.listName = listName;
    showNow(t); markCurrent();
    let url = null, failed = false;
    try { url = await Src.resolvePreview(t, ctx); } catch (e) { failed = true; toast('Не удалось найти аудио: ' + e.message); }
    if (state.queue[state.idx] !== t) return;           // user moved on while we were resolving
    if (!url) {
      state.playing = false; setPlayIcon(); markCurrent();
      if (t.demo) toast('Демо-трек: подключите источник, чтобы слушать');
      else if (!failed) toast('Для этого трека нет аудио-превью');
      return;
    }
    audio.src = url;
    try { await audio.play(); } catch (e) {
      if (e && e.name === 'AbortError') return;       // a newer track replaced this one; not an error
      state.playing = false; setPlayIcon(); markCurrent(); toast('Воспроизведение заблокировано: ' + e.message);
    }
  }

  function step(d) {
    if (!state.queue.length) return;
    const n = (state.idx + d + state.queue.length) % state.queue.length;
    playIndex(state.listName, n);
  }
  function togglePlay() {
    if (!audio.src) { if (state.lists.chart.length) playIndex('chart', 0); return; }
    if (audio.paused) audio.play().catch(() => { }); else audio.pause();
  }
  audio.addEventListener('play', () => { state.playing = true; setPlayIcon(); markCurrent(); if ('mediaSession' in navigator) navigator.mediaSession.playbackState = 'playing'; });
  audio.addEventListener('pause', () => { state.playing = false; setPlayIcon(); markCurrent(); if ('mediaSession' in navigator) navigator.mediaSession.playbackState = 'paused'; });
  audio.addEventListener('ended', () => step(1));
  audio.addEventListener('error', () => { state.playing = false; setPlayIcon(); toast('Не удалось загрузить аудио'); });
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
  function openSettings() {
    $$('[data-key]', dlg).forEach(el => { el.value = state.settings[el.dataset.key] || ''; });
    dlg.showModal();
  }
  $('#settings-form').addEventListener('submit', e => {
    // Save is the first submit button, so Enter inside a field saves; only the explicit "Закрыть" cancels
    if (e.submitter && e.submitter.value === 'cancel') return;
    $$('[data-key]', dlg).forEach(el => { state.settings[el.dataset.key] = el.value.trim(); });
    store.set('settings', state.settings);
    state.cache = {};
    loadChart(state.source, true);
  });

  /* ---------- events ---------- */
  document.addEventListener('click', e => {
    const view = e.target.closest('[data-view]');
    if (view) return setView(view.dataset.view);
    const pill = e.target.closest('[data-src]');
    if (pill) { state.source = pill.dataset.src; store.set('source', state.source); return loadChart(state.source, false); }
    const act = e.target.closest('[data-act]');
    const row = e.target.closest('.row');
    if (act && !row) {
      switch (act.dataset.act) {
        case 'settings': return openSettings();
        case 'retry': return loadChart(state.source, true);
        case 'play-top': return playIndex('chart', 0);
        case 'like-now': return toggleFav(state.queue[state.idx]);
      }
      return;
    }
    if (row) {
      const list = row.dataset.list, i = +row.dataset.i, t = state.lists[list][i];
      if (act && act.dataset.act === 'like') return toggleFav(t);
      const cur = state.listName === list && state.queue[state.idx] === t;
      if (cur && audio.src) return togglePlay();
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

  /* ---------- boot ---------- */
  setRange($('#seek')); setRange($('#vol'));
  renderSearchIdle();
  setView('charts');
  loadChart(state.source, false);
})();
