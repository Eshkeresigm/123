/* Chart sources. Classic script (no ES modules) so it loads from file:// and https://localhost alike.
 * Every source: { id, name, color, home, keys[], fetch(ctx) -> Track[] }
 * Track: { id, src, title, artist, cover, duration(sec), preview(url|null), url }
 * ctx:   { json(url, opts) -> parsed JSON, settings: {...}, kv: { get(k), set(k, v), del(k) } }
 */
(function (g) {
  'use strict';

  const NEEDS = 'NEEDS';
  function need(st, keys, label) {
    const miss = keys.filter(k => !String(st[k] || '').trim());
    if (miss.length) {
      const e = new Error('Для «' + label + '» укажите ключ в настройках');
      e.code = NEEDS;
      throw e;
    }
  }
  const clean = s => String(s || '').replace(/["“”]/g, ' ').trim();

  const COUNTRIES = [
    { en: 'Russia', ru: 'Россия' }, { en: 'USA', ru: 'США' }, { en: 'United Kingdom', ru: 'Великобритания' },
    { en: 'Germany', ru: 'Германия' }, { en: 'France', ru: 'Франция' }, { en: 'Netherlands', ru: 'Нидерланды' },
    { en: 'Ukraine', ru: 'Украина' }, { en: 'Kazakhstan', ru: 'Казахстан' }
  ];
  const countryOf = st => COUNTRIES.find(c => c.en === (st && st.dzCountry)) || COUNTRIES[0];

  /* ---------- Deezer (public API, no key) ---------- */
  function mapDeezer(d) {
    return {
      id: 'dz' + d.id,
      src: 'deezer',
      title: d.title || '',
      artist: (d.artist && d.artist.name) || '',
      cover: (d.album && (d.album.cover_big || d.album.cover_medium)) || '',
      duration: d.duration | 0,
      preview: d.preview || null,
      url: d.link || ''
    };
  }
  function deezerList(r) {
    if (r && r.error) throw new Error('Deezer: ' + (r.error.message || r.error.type || 'error'));
    return ((r && r.data) || []).filter(d => d && d.id && d.title).map(mapDeezer);
  }
  // Official "Top <country>" playlists are looked up by name at runtime instead of hard-coding playlist ids.
  function pickChartPlaylist(r, countryEn) {
    const want = String(countryEn).toLowerCase();
    const best = ((r && r.data) || [])
      .filter(p => p && p.id && /^top\b/i.test(p.title || '') && String(p.title).toLowerCase().includes(want) &&
        p.user && /deezer/i.test(p.user.name || ''))
      .sort((a, b) => (b.nb_tracks || 0) - (a.nb_tracks || 0))[0];
    return best ? best.id : null;
  }
  async function deezerCountryChart(ctx) {
    const c = countryOf(ctx.settings);
    const key = 'dzpl:' + c.en;
    const TTL = 7 * 864e5;
    const lookup = async () => {
      const id = pickChartPlaylist(await ctx.json('https://api.deezer.com/search/playlist?limit=25&q=' + encodeURIComponent('Top ' + c.en)), c.en);
      if (!id) throw new Error('Deezer: не нашёл официальный чарт для страны «' + c.ru + '»');
      ctx.kv.set(key, { id, at: Date.now() });
      return id;
    };
    const hit = ctx.kv.get(key);
    let id = hit && Date.now() - hit.at < TTL ? hit.id : await lookup();
    try {
      return deezerList(await ctx.json('https://api.deezer.com/playlist/' + id + '/tracks?limit=50'));
    } catch (e) {
      if (!(hit && hit.id === id)) throw e;             // id came from the cache: it may be stale, look it up once more
      ctx.kv.del(key);
      id = await lookup();
      return deezerList(await ctx.json('https://api.deezer.com/playlist/' + id + '/tracks?limit=50'));
    }
  }

  /* ---------- Spotify (client credentials; needs the user's own app keys) ---------- */
  let spToken = { value: '', exp: 0 };
  async function spotifyToken(ctx) {
    if (spToken.value && Date.now() < spToken.exp - 30000) return spToken.value;
    const st = ctx.settings;
    const r = await ctx.json('https://accounts.spotify.com/api/token', {
      method: 'POST',
      headers: {
        Authorization: 'Basic ' + g.btoa(st.spotifyId.trim() + ':' + st.spotifySecret.trim()),
        'Content-Type': 'application/x-www-form-urlencoded'
      },
      body: 'grant_type=client_credentials'
    });
    if (!r.access_token) throw new Error('Spotify: токен не получен');
    spToken = { value: r.access_token, exp: Date.now() + (r.expires_in || 3600) * 1000 };
    return spToken.value;
  }

  const SOURCES = [
    {
      id: 'deezer', name: 'Deezer', color: '#a238ff', home: 'deezer.com', keys: [],
      async fetch(ctx) {
        return deezerList(await ctx.json('https://api.deezer.com/chart/0/tracks?limit=50'));
      }
    },
    {
      id: 'deezer-country', name: 'Deezer', color: '#a238ff', home: 'deezer.com', keys: [], country: true,
      async fetch(ctx) { return deezerCountryChart(ctx); }
    },
    {
      id: 'vk', name: 'VK Музыка', color: '#0077ff', home: 'vk.com', keys: ['vkToken'], experimental: true,
      async fetch(ctx) {
        need(ctx.settings, this.keys, this.name);
        const r = await ctx.json('https://api.vk.com/method/audio.getPopular?v=5.131&count=50&access_token=' + encodeURIComponent(ctx.settings.vkToken.trim()));
        if (r && r.error) throw new Error('VK: ' + (r.error.error_msg || r.error.error_code));
        const rows = Array.isArray(r && r.response) ? r.response : ((r && r.response && r.response.items) || []);
        return rows.filter(a => a && a.title).map(a => {
          const th = a.album && a.album.thumb;
          return {
            id: 'vk' + a.owner_id + '_' + a.id,
            src: 'vk',
            title: a.title || '',
            artist: a.artist || '',
            cover: (th && (th.photo_300 || th.photo_270 || th.photo_135)) || '',
            duration: a.duration | 0,
            preview: null,
            url: 'https://vk.com/audio' + a.owner_id + '_' + a.id
          };
        });
      }
    },
    {
      id: 'yandex', name: 'Яндекс Музыка', color: '#ffcc00', home: 'music.yandex.ru', keys: ['yandexToken'], experimental: true,
      async fetch(ctx) {
        need(ctx.settings, this.keys, this.name);
        const r = await ctx.json('https://api.music.yandex.net/landing3/chart', {
          headers: { Authorization: 'OAuth ' + ctx.settings.yandexToken.trim() }
        });
        const res = (r && r.result) || {};
        const rows = (res.chart && res.chart.tracks) || res.tracks || [];
        return rows.map(x => x.track || x).filter(t => t && t.id).map(t => ({
          id: 'ya' + t.id,
          src: 'yandex',
          title: t.title || '',
          artist: (t.artists || []).map(a => a.name).join(', '),
          cover: t.coverUri ? 'https://' + String(t.coverUri).replace('%%', '600x600') : '',
          duration: Math.round((t.durationMs || 0) / 1000),
          preview: null,
          url: t.albums && t.albums[0] ? 'https://music.yandex.ru/album/' + t.albums[0].id + '/track/' + t.id : ''
        }));
      }
    },
    {
      id: 'spotify', name: 'Spotify', color: '#1ed760', home: 'spotify.com', keys: ['spotifyId', 'spotifySecret'], experimental: true,
      async fetch(ctx) {
        need(ctx.settings, this.keys, this.name);
        const tok = await spotifyToken(ctx);
        const r = await ctx.json('https://api.spotify.com/v1/playlists/37i9dQZEVXbMDoHDwVN2tF/tracks?limit=50', {
          headers: { Authorization: 'Bearer ' + tok }
        });
        return ((r && r.items) || []).map(x => x.track).filter(t => t && t.id).map(t => ({
          id: 'sp' + t.id,
          src: 'spotify',
          title: t.name || '',
          artist: (t.artists || []).map(a => a.name).join(', '),
          cover: (t.album && t.album.images && t.album.images[0] && t.album.images[0].url) || '',
          duration: Math.round((t.duration_ms || 0) / 1000),
          preview: null,
          url: (t.external_urls && t.external_urls.spotify) || ''
        }));
      }
    },
    {
      id: 'soundcloud', name: 'SoundCloud', color: '#ff5500', home: 'soundcloud.com', keys: ['scClientId'], experimental: true,
      async fetch(ctx) {
        need(ctx.settings, this.keys, this.name);
        const u = 'https://api-v2.soundcloud.com/charts?kind=top&genre=soundcloud%3Agenres%3Aall-music&limit=50&client_id=' +
          encodeURIComponent(ctx.settings.scClientId.trim());
        const r = await ctx.json(u);
        return ((r && r.collection) || []).map(x => x.track).filter(t => t && t.id).map(t => ({
          id: 'sc' + t.id,
          src: 'soundcloud',
          title: t.title || '',
          artist: (t.user && t.user.username) || '',
          cover: String(t.artwork_url || '').replace('-large.', '-t500x500.'),
          duration: Math.round((t.duration || 0) / 1000),
          preview: null,
          url: t.permalink_url || ''
        }));
      }
    }
  ];

  const byId = id => SOURCES.find(s => s.id === id) || SOURCES[0];
  const label = (s, st) => (s.country ? 'Deezer · ' + countryOf(st).ru : s.name);
  const hasKeys = (s, st) => s.keys.every(k => String((st && st[k]) || '').trim());
  // The icon is the service's own site icon, loaded at runtime (nothing is redrawn or bundled here).
  const iconUrl = s => 'https://www.google.com/s2/favicons?sz=64&domain=' + encodeURIComponent(s.home);

  /* ---------- search + audio preview resolver (Deezer) ---------- */
  async function search(q, ctx) {
    return deezerList(await ctx.json('https://api.deezer.com/search?limit=30&q=' + encodeURIComponent(q)));
  }

  // Deezer preview links are signed and expire, so they are never trusted after a reload; { fresh: true } skips every cache.
  const previewCache = new Map();
  async function resolvePreview(t, ctx, opts) {
    if (t.demo) return null;
    const fresh = !!(opts && opts.fresh);
    if (!fresh && t.preview) return t.preview;
    if (!fresh && previewCache.has(t.id)) return previewCache.get(t.id);
    if (t.src === 'deezer' && /^dz\d+$/.test(t.id)) {
      try {
        const r = await ctx.json('https://api.deezer.com/track/' + t.id.slice(2));
        if (r && r.preview) { previewCache.set(t.id, r.preview); return r.preview; }
      } catch (e) { /* fall through to the name search */ }
    }
    const tries = [
      'artist:"' + clean(t.artist.split(',')[0]) + '" track:"' + clean(t.title) + '"',
      clean(t.artist.split(',')[0]) + ' ' + clean(t.title)
    ];
    for (const q of tries) {
      const r = await ctx.json('https://api.deezer.com/search?limit=1&q=' + encodeURIComponent(q));
      const hit = r && r.data && r.data[0];
      if (hit && hit.preview) {
        previewCache.set(t.id, hit.preview);
        return hit.preview;
      }
    }
    previewCache.set(t.id, null);
    return null;
  }

  /* ---------- offline demo data (fictional, clearly labelled in the UI) ---------- */
  function demo() {
    const names = [
      ['Neon Tides', 'Luma Vale'], ['Glass Orchard', 'Kito Rae'], ['Midnight Cartography', 'The Ardent'],
      ['Sundial', 'Nova Ferrous'], ['Velvet Static', 'Iris Moon'], ['Low Orbit', 'Marlow & Pike'],
      ['Paper Satellites', 'Ode to Echo'], ['Amber Hours', 'Sela Quinn'], ['Hollow Bloom', 'Dune Atlas'],
      ['Chrome Lullaby', 'Vesper Kay'], ['Tidal Wire', 'Juno Park'], ['Afterglow Protocol', 'Lumen Row']
    ];
    return names.map((n, i) => ({
      id: 'demo' + i, src: 'demo', title: n[0], artist: n[1], cover: '',
      duration: 170 + ((i * 37) % 90), preview: null, url: '', demo: true
    }));
  }

  g.PluginSoundSources = { SOURCES, COUNTRIES, byId, label, hasKeys, iconUrl, countryOf, search, resolvePreview, demo, mapDeezer, pickChartPlaylist, NEEDS };
})(typeof globalThis !== 'undefined' ? globalThis : window);
