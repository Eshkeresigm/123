/* Chart sources. Classic script (no ES modules) so it loads from file:// and https://localhost alike.
 * Every source: { id, name, color, keys[], fetch(ctx) -> Track[] }
 * Track: { id, src, title, artist, cover, duration(sec), preview(url|null), url }
 * ctx:   { json(url, opts) -> parsed JSON, settings: {...} }
 */
(function (g) {
  'use strict';

  const NEEDS = 'NEEDS';
  function need(st, keys, label) {
    const miss = keys.filter(k => !String(st[k] || '').trim());
    if (miss.length) {
      const e = new Error('Для «' + label + '» укажите ключи в настройках');
      e.code = NEEDS;
      throw e;
    }
  }
  const clean = s => String(s || '').replace(/["“”]/g, ' ').trim();

  /* ---------- Deezer (public, no key) ---------- */
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
    return ((r && r.data) || []).map(mapDeezer);
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
      id: 'deezer', name: 'Deezer', color: '#a238ff', keys: [], live: true,
      blurb: 'Глобальный топ · 30-сек превью',
      async fetch(ctx) {
        return deezerList(await ctx.json('https://api.deezer.com/chart/0/tracks?limit=50'));
      }
    },
    {
      id: 'apple', name: 'Apple Music', color: '#fa2d48', keys: [], live: true,
      blurb: 'Самые прослушиваемые · по стране',
      async fetch(ctx) {
        const cc = /^[a-z]{2}$/.test(ctx.settings.appleCountry || '') ? ctx.settings.appleCountry : 'ru';
        const r = await ctx.json('https://rss.applemarketingtools.com/api/v2/' + cc + '/music/most-played/50/songs.json');
        const rows = (r && r.feed && r.feed.results) || [];
        return rows.map(x => ({
          id: 'ap' + x.id,
          src: 'apple',
          title: x.name || '',
          artist: x.artistName || '',
          cover: String(x.artworkUrl100 || '').replace('100x100', '600x600'),
          duration: 0,
          preview: null,
          url: x.url || ''
        }));
      }
    },
    {
      id: 'yandex', name: 'Яндекс Музыка', color: '#ffcc00', keys: ['yandexToken'], live: false,
      blurb: 'Чарт · нужен OAuth-токен',
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
      id: 'spotify', name: 'Spotify', color: '#1ed760', keys: ['spotifyId', 'spotifySecret'], live: false,
      blurb: 'Top 50 Global · нужны ключи приложения',
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
      id: 'soundcloud', name: 'SoundCloud', color: '#ff5500', keys: ['scClientId'], live: false,
      blurb: 'Top · нужен client_id',
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

  /* ---------- search + audio preview resolver (Deezer) ---------- */
  async function search(q, ctx) {
    return deezerList(await ctx.json('https://api.deezer.com/search?limit=30&q=' + encodeURIComponent(q)));
  }

  const previewCache = new Map();
  async function resolvePreview(t, ctx) {
    if (t.demo) return null;
    if (t.preview) return t.preview;
    if (previewCache.has(t.id)) return previewCache.get(t.id);
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

  g.PluginSoundSources = { SOURCES, byId, search, resolvePreview, demo, mapDeezer, NEEDS };
})(typeof globalThis !== 'undefined' ? globalThis : window);
