/* Chart cache: instant start from the last saved charts, refreshed once per local day (and at most every 6 hours).
 * Preview URLs are never persisted: they are signed and expire. */
(function (g) {
  'use strict';
  const KEY = 'pluginsound.cache.v2';
  const MAX_AGE = 6 * 3600 * 1000;
  const pad = n => String(n).padStart(2, '0');
  const dayKey = ms => { const d = new Date(ms); return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate()); };
  const isStale = (entry, now) => {
    now = now == null ? Date.now() : now;
    return !entry || !entry.at || dayKey(entry.at) !== dayKey(now) || now - entry.at > MAX_AGE;
  };
  const slim = t => ({ id: t.id, src: t.src, title: t.title, artist: t.artist, cover: t.cover, duration: t.duration | 0, preview: null, url: t.url || '' });

  function create(storage) {
    const mem = new Map();
    try {
      const raw = storage.getItem(KEY);
      if (raw) {
        for (const [id, e] of Object.entries(JSON.parse(raw))) {
          const tracks = e && Array.isArray(e.tracks) ? e.tracks.filter(t => t && t.id && t.title) : [];
          if (tracks.length && typeof e.at === 'number') mem.set(id, { at: e.at, tracks });
        }
      }
    } catch (err) { /* corrupt cache: start empty */ }
    const persist = () => {
      try {
        const o = {};
        mem.forEach((e, id) => { o[id] = { at: e.at, tracks: e.tracks.map(slim) }; });
        storage.setItem(KEY, JSON.stringify(o));
      } catch (err) { /* storage full or unavailable: the in-memory copy still works */ }
    };
    return {
      get: id => mem.get(id) || null,
      set(id, tracks, at) { mem.set(id, { at: at == null ? Date.now() : at, tracks }); persist(); },
      clear() { mem.clear(); persist(); },
      ids: () => Array.from(mem.keys())
    };
  }

  g.PluginSoundCache = { create, isStale, dayKey, MAX_AGE, KEY };
})(typeof globalThis !== 'undefined' ? globalThis : window);
