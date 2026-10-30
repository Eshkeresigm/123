/* Generative SVG cover art: deterministic from a seed string, returned as a CSS-safe data URI. */
(function (g) {
  'use strict';

  function hash(str) {
    let h = 2166136261 >>> 0;
    for (const c of String(str)) { h ^= c.charCodeAt(0); h = Math.imul(h, 16777619) >>> 0; }
    return h >>> 0;
  }
  function rng(seed) {                       // mulberry32
    let a = seed >>> 0;
    return () => { a = (a + 0x6D2B79F5) >>> 0; let t = a; t = Math.imul(t ^ (t >>> 15), t | 1); t ^= t + Math.imul(t ^ (t >>> 7), t | 61); return ((t ^ (t >>> 14)) >>> 0) / 4294967296; };
  }
  const hsl = (h, s, l, a) => 'hsl(' + Math.round(h % 360) + ',' + s + '%,' + l + '%' + (a == null ? '' : ',' + a) + ')';
  const f = n => Math.round(n * 10) / 10;

  function svg(seed) {
    const hs = hash(seed), r = rng(hs);
    const h1 = hs % 360, h2 = h1 + 40 + r() * 70;
    const kind = (hs >>> 8) % 4;
    let body = '';
    if (kind === 0) {                                   // concentric rings
      const cx = 120 + r() * 160, cy = 120 + r() * 160;
      for (let i = 0; i < 7; i++) body += '<circle cx="' + f(cx) + '" cy="' + f(cy) + '" r="' + (30 + i * 34) + '" fill="none" stroke="' + hsl(h2, 90, 78, f(0.5 - i * 0.055)) + '" stroke-width="' + (6 + i * 2) + '"/>';
    } else if (kind === 1) {                            // flowing waves
      for (let i = 0; i < 6; i++) {
        const y = 60 + i * 56, a = 20 + r() * 40, ph = r() * 60;
        body += '<path d="M-20 ' + y + ' C ' + f(80 + ph) + ' ' + f(y - a) + ', ' + f(160 - ph) + ' ' + f(y + a) + ', 220 ' + y + ' S ' + f(360 + ph) + ' ' + f(y - a) + ', 420 ' + f(y + 10) + '" fill="none" stroke="' + hsl(h2 + i * 8, 92, 76, f(0.55 - i * 0.05)) + '" stroke-width="' + (10 + i * 3) + '" stroke-linecap="round"/>';
      }
    } else if (kind === 2) {                            // sunburst
      const cx = 200, cy = 200 + (r() - 0.5) * 120, n = 14 + Math.floor(r() * 8);
      for (let i = 0; i < n; i++) {
        const a0 = (i / n) * Math.PI * 2, a1 = a0 + (Math.PI * 2 / n) * 0.45;
        body += '<path d="M' + cx + ' ' + f(cy) + 'L' + f(cx + Math.cos(a0) * 420) + ' ' + f(cy + Math.sin(a0) * 420) + 'L' + f(cx + Math.cos(a1) * 420) + ' ' + f(cy + Math.sin(a1) * 420) + 'Z" fill="' + hsl(h2, 90, 80, 0.2) + '"/>';
      }
      body += '<circle cx="' + cx + '" cy="' + f(cy) + '" r="' + f(46 + r() * 30) + '" fill="' + hsl(h2 + 20, 95, 72, 0.9) + '"/>';
    } else {                                            // bubbles
      for (let i = 0; i < 9; i++) body += '<circle cx="' + f(r() * 400) + '" cy="' + f(r() * 400) + '" r="' + f(24 + r() * 80) + '" fill="' + hsl(h1 + r() * 90, 90, 40 + r() * 40, f(0.28 + r() * 0.3)) + '"/>';
    }
    return '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 400" preserveAspectRatio="xMidYMid slice">' +
      '<defs><linearGradient id="a" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="' + hsl(h1, 85, 56) + '"/><stop offset="1" stop-color="' + hsl(h2, 85, 30) + '"/></linearGradient></defs>' +
      '<rect width="400" height="400" fill="url(#a)"/>' + body +
      '<rect width="400" height="400" fill="none" stroke="rgba(255,255,255,.08)" stroke-width="8"/></svg>';
  }

  // Data URI safe for an unquoted CSS url(...) inside an HTML attribute.
  function uri(seed) {
    return 'data:image/svg+xml;utf8,' + encodeURIComponent(svg(seed)).replace(/'/g, '%27').replace(/\(/g, '%28').replace(/\)/g, '%29');
  }

  g.PluginSoundArt = { svg, uri, hash };
})(typeof globalThis !== 'undefined' ? globalThis : window);
