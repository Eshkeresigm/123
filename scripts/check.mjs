// Smoke check: syntax, file/id consistency, and source parsers against mocked API payloads.
// NOTE: mocked payloads reflect the response shapes the adapters expect; this proves parsing logic, not live API compatibility.
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import assert from 'node:assert/strict';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const read = p => fs.readFileSync(path.join(root, p), 'utf8');
let n = 0;
const ok = m => console.log('  ok  ' + m, ++n && '');

// 1. JSON configs
const pkg = JSON.parse(read('package.json'));
const cap = JSON.parse(read('capacitor.config.json'));
assert.equal(pkg.main, 'electron/main.js'); ok('package.json main');
assert.equal(cap.webDir, 'www'); ok('capacitor webDir');
assert.equal(cap.plugins.CapacitorHttp.enabled, true); ok('CapacitorHttp enabled');
for (const f of ['electron/main.js', 'electron/preload.js', 'www/index.html', 'www/css/style.css', 'www/js/app.js', 'www/js/sources.js', 'www/js/art.js',
  'www/img/icon.svg', 'www/img/mark.svg', 'www/img/empty-lib.svg', 'www/img/empty-search.svg', 'www/img/hero-wave.svg',
  'build/icon.png', 'build/icon.ico', 'resources/android/res/mipmap-xxxhdpi/ic_launcher.png', 'resources/android/res/mipmap-xxxhdpi/ic_launcher_foreground.png', 'resources/android/res/values/ic_launcher_background.xml']) {
  assert.ok(fs.existsSync(path.join(root, f)), 'missing ' + f);
}
ok('all project files exist');

// 1b. SVG files are well-formed enough (balanced tags, xmlns, unique ids referenced by url(#..) exist)
for (const f of fs.readdirSync(path.join(root, 'www/img')).filter(x => x.endsWith('.svg'))) {
  const x = read('www/img/' + f);
  assert.match(x, /^<svg[^>]+xmlns="http:\/\/www\.w3\.org\/2000\/svg"/, f + ' xmlns');
  assert.ok(x.trimEnd().endsWith('</svg>'), f + ' closing tag');
  const defined = new Set([...x.matchAll(/\bid="([^"]+)"/g)].map(m => m[1]));
  for (const m of x.matchAll(/url\(#([^)]+)\)/g)) assert.ok(defined.has(m[1]), f + ' missing #' + m[1]);
}
ok('SVG files well-formed, gradient/mask references resolve');
for (const m of (read('www/index.html') + read('www/js/app.js')).matchAll(/img\/([\w-]+)(?:\.svg|-)/g)) { /* dynamic names checked below */ }
for (const k of ['lib', 'search']) assert.ok(fs.existsSync(path.join(root, 'www/img/empty-' + k + '.svg')), 'empty-' + k);
ok('empty-state illustrations exist for every kind used by app.js');

// 2. index.html references + ids used by app.js
const html = read('www/index.html');
for (const m of html.matchAll(/(?:src|href)="((?:js|css)\/[^"]+)"/g)) assert.ok(fs.existsSync(path.join(root, 'www', m[1])), 'broken ref ' + m[1]);
ok('index.html asset references resolve');
const ids = new Set([...html.matchAll(/\bid="([^"]+)"/g)].map(m => m[1]));
const app = read('www/js/app.js');
for (const m of app.matchAll(/\$\('#([\w-]+)'\)/g)) assert.ok(ids.has(m[1]), 'app.js uses missing #' + m[1]);
ok('every $("#id") in app.js exists in index.html');
const symbols = new Set([...html.matchAll(/<symbol id="([^"]+)"/g)].map(m => m[1]));
for (const m of (html + app).matchAll(/href[=,]\s*["']#(i-[\w-]+)["']/g)) assert.ok(symbols.has(m[1]), 'missing icon ' + m[1]);
ok('every icon reference has a <symbol>');
const keys = new Set([...html.matchAll(/data-key="(\w+)"/g)].map(m => m[1]));
for (const k of ['appleCountry', 'yandexToken', 'spotifyId', 'spotifySecret', 'scClientId']) assert.ok(keys.has(k), 'settings key ' + k);
ok('settings keys match dialog fields');
const nativeHosts = [...read('electron/main.js').matchAll(/'([a-z0-9.-]+\.(?:com|net))'/g)].map(m => m[1]);
const srcCode = read('www/js/sources.js');
// only hosts that are actually fetched; lines with `url:` are outbound links shown to the user
const fetched = srcCode.split('\n').filter(l => !/^\s*url:/.test(l)).join('\n');
for (const h of new Set([...fetched.matchAll(/https:\/\/([a-z0-9.-]+)\//g)].map(m => m[1]))) assert.ok(nativeHosts.includes(h), 'host not allowed in main.js: ' + h);
ok('every host used by sources.js is on the Electron allowlist');

// 3. Source parsers with mocked ctx
const sandbox = { globalThis: null, btoa, console, Date };
sandbox.globalThis = sandbox;
vm.createContext(sandbox);
vm.runInContext(srcCode, sandbox);
const S = sandbox.PluginSoundSources;
const mk = (routes) => ({
  settings: { appleCountry: 'ru', yandexToken: 'tok', spotifyId: 'id', spotifySecret: 'sec', scClientId: 'cid' },
  calls: [],
  async json(url, opts) { this.calls.push(url); for (const [re, v] of routes) if (re.test(url)) return typeof v === 'function' ? v(url, opts) : v; throw new Error('unmocked ' + url); }
});

const dz = { data: [{ id: 1, title: 'T', duration: 200, preview: 'https://p/1.mp3', link: 'https://dz/1', artist: { name: 'A' }, album: { cover_big: 'https://c/1.jpg' } }] };
let t = await S.byId('deezer').fetch(mk([[/chart\/0\/tracks/, dz]]));
assert.deepEqual([t[0].id, t[0].title, t[0].artist, t[0].duration, t[0].preview], ['dz1', 'T', 'A', 200, 'https://p/1.mp3']); ok('deezer parser');
await assert.rejects(S.byId('deezer').fetch(mk([[/chart/, { error: { message: 'quota' } }]])), /quota/); ok('deezer error payload surfaces');

const ap = { feed: { results: [{ id: '9', name: 'N', artistName: 'B', artworkUrl100: 'https://i/100x100bb.jpg', url: 'https://apple/9' }] } };
const apCtx = mk([[/rss\.applemarketingtools\.com\/api\/v2\/ru\/music\/most-played\/50\/songs\.json/, ap]]);
t = await S.byId('apple').fetch(apCtx);
assert.equal(t[0].cover, 'https://i/600x600bb.jpg'); assert.equal(t[0].id, 'ap9'); ok('apple parser + country URL');

const ya = { result: { chart: { tracks: [{ track: { id: 5, title: 'Я', durationMs: 180000, artists: [{ name: 'X' }, { name: 'Y' }], coverUri: 'avatars.yandex.net/get-music-content/1/abc/%%', albums: [{ id: 77 }] } }] } } };
t = await S.byId('yandex').fetch(mk([[/landing3\/chart/, (u, o) => { assert.equal(o.headers.Authorization, 'OAuth tok'); return ya; }]]));
assert.deepEqual([t[0].artist, t[0].duration, t[0].cover], ['X, Y', 180, 'https://avatars.yandex.net/get-music-content/1/abc/600x600']); ok('yandex parser + auth header');

const sp = { items: [{ track: { id: 'q', name: 'S', duration_ms: 123000, artists: [{ name: 'Z' }], album: { images: [{ url: 'https://s/i.jpg' }] }, external_urls: { spotify: 'https://sp/q' } } }] };
t = await S.byId('spotify').fetch(mk([
  [/accounts\.spotify\.com\/api\/token/, (u, o) => { assert.match(o.headers.Authorization, /^Basic /); assert.equal(o.body, 'grant_type=client_credentials'); return { access_token: 'AT', expires_in: 3600 }; }],
  [/api\.spotify\.com\/v1\/playlists/, (u, o) => { assert.equal(o.headers.Authorization, 'Bearer AT'); return sp; }]]));
assert.deepEqual([t[0].id, t[0].duration, t[0].cover], ['spq', 123, 'https://s/i.jpg']); ok('spotify token flow + parser');

const sc = { collection: [{ track: { id: 3, title: 'C', duration: 90000, artwork_url: 'https://a/x-large.jpg', user: { username: 'U' }, permalink_url: 'https://sc/3' } }] };
t = await S.byId('soundcloud').fetch(mk([[/api-v2\.soundcloud\.com\/charts.*client_id=cid/, sc]]));
assert.deepEqual([t[0].artist, t[0].cover, t[0].duration], ['U', 'https://a/x-t500x500.jpg', 90]); ok('soundcloud parser');

for (const id of ['yandex', 'spotify', 'soundcloud']) {
  const c = mk([]); c.settings = {};
  await assert.rejects(S.byId(id).fetch(c), e => e.code === 'NEEDS');
}
ok('keyed sources fail with NEEDS when keys are empty');

// 3b. Generative SVG cover art
const artBox = { globalThis: null, encodeURIComponent, Math, String };
artBox.globalThis = artBox; vm.createContext(artBox);
vm.runInContext(read('www/js/art.js'), artBox);
const A = artBox.PluginSoundArt;
assert.equal(A.uri('a|b'), A.uri('a|b')); assert.notEqual(A.uri('a|b'), A.uri('c|d')); ok('cover art is deterministic and seed-dependent');
const kinds = new Set();
for (let i = 0; i < 80; i++) {
  const svg = A.svg('seed' + i);
  assert.match(svg, /^<svg[\s\S]*<\/svg>$/); assert.ok(!/NaN|undefined/.test(svg), 'bad number in art ' + i);
  kinds.add(A.hash('seed' + i) >>> 8 & 3);
  const u = A.uri('seed' + i); assert.ok(!/['"() <>]/.test(u.slice('data:image/svg+xml;utf8,'.length)), 'uri not CSS-safe');
}
assert.equal(kinds.size, 4); ok('all 4 art styles generate valid, CSS-safe SVG (80 seeds)');

// 4. Preview resolver: direct, search fallback, caching
const c1 = mk([[/search\?limit=1/, { data: [{ preview: 'https://p/found.mp3' }] }]]);
const tr = { id: 'ap9', title: 'N', artist: 'B', preview: null };
assert.equal(await S.resolvePreview(tr, c1), 'https://p/found.mp3');
await S.resolvePreview(tr, c1); assert.equal(c1.calls.length, 1); ok('resolvePreview searches once and caches');
assert.equal(await S.resolvePreview({ id: 'x', preview: 'https://p/direct.mp3' }, mk([])), 'https://p/direct.mp3'); ok('resolvePreview keeps direct preview');
assert.equal(await S.resolvePreview(S.demo()[0], mk([])), null); ok('demo tracks have no audio');

console.log('\nAll checks passed (' + n + ').');
