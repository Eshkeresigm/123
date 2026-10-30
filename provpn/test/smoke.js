'use strict';
// Smoke test: real HTTP server + stub wg/wg-quick binaries.
// Verifies validation, security checks and the connect/disconnect control flow.
// It does NOT create a real tunnel.

const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const assert = require('node:assert/strict');

if (process.argv.includes('--win')) process.env.PROVPN_PLATFORM = 'win32';
const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'provpn-'));
const stateFile = path.join(tmp, 'up');

const stubQuick = path.join(tmp, 'wg-quick');
const stubWg = path.join(tmp, 'wg');
const callLog = path.join(tmp, 'calls');
const WINMODE = process.env.PROVPN_PLATFORM === 'win32';
fs.writeFileSync(stubQuick, `#!/bin/sh\necho "$1" >> "${callLog}"\ncase "$1" in up|/installtunnelservice) basename "$2" .conf > "${stateFile}";; *) rm -f "${stateFile}";; esac\n`, { mode: 0o755 });
fs.writeFileSync(stubWg, `#!/bin/sh\n[ -f "${stateFile}" ] && [ "$2" = "$(cat "${stateFile}")" ] || exit 1\nprintf 'priv\\tpub\\t51820\\toff\\npeer\\t(none)\\t1.2.3.4:51820\\t0.0.0.0/0\\t%s\\t100\\t200\\t0\\n' "$(date +%s)"\n`, { mode: 0o755 });

process.env.PROVPN_PORT = '17878';
process.env.PROVPN_PROFILES = path.join(tmp, 'profiles');
process.env.PROVPN_WG_QUICK = stubQuick;
process.env.PROVPN_WG = stubWg;

// Fake sing-box cores: one that "starts" and idles, one that dies at once.
const U = '11111111-2222-3333-4444-555555555555';
const okCore = path.join(tmp, 'core-ok');
const badCore = path.join(tmp, 'core-bad');
fs.writeFileSync(okCore, '#!/bin/sh\necho "INFO sing-box started (0.01s)"\nexec sleep 600\n', { mode: 0o755 });
fs.writeFileSync(badCore, `#!/bin/sh\necho "FATAL decode config: bad credential ${U}"\nexit 1\n`, { mode: 0o755 });
process.env.PROVPN_SINGBOX = okCore;

const { server, parseConf } = require('../server.js');
const K = 'A'.repeat(43) + '=';
const GOOD = `[Interface]\nPrivateKey = ${K}\nAddress = 10.0.0.2/32\nDNS = 1.1.1.1\n\n[Peer]\nPublicKey = ${K}\nEndpoint = 1.2.3.4:51820\nAllowedIPs = 0.0.0.0/0\n`;
const base = 'http://127.0.0.1:17878';
const H = { 'Content-Type': 'application/json', Origin: base };

async function call(method, url, body, headers = H) {
  const r = await fetch(base + url, { method, headers, body: body && JSON.stringify(body) });
  return { status: r.status, json: await r.json().catch(() => ({})) };
}

(async () => {
  // validation unit checks
  assert.equal(parseConf(GOOD).ok, true);
  assert.equal(parseConf(GOOD + 'PostUp = rm -rf /\n').ok, false, 'PostUp must be rejected');
  assert.equal(parseConf('[Interface]\nAddress = 1.1.1.1/32\n').ok, false);

  await new Promise((r) => server.listen(17878, '127.0.0.1', r));
  try {
    // static UI is served
    const html = await (await fetch(base + '/')).text();
    assert.match(html, /ProVpn/);
    assert.equal((await fetch(base + '/..%2fserver.js')).status, 404, 'path traversal blocked');

    // security: bad origin / bad host
    assert.equal((await call('POST', '/api/connect', { name: 'x' }, { 'Content-Type': 'application/json', Origin: 'http://evil.example' })).status, 403);
    const badHostStatus = await new Promise((resolve, reject) => {
      require('node:http').get({ host: '127.0.0.1', port: 17878, path: '/api/state', headers: { Host: 'evil.example' } },
        (res) => { res.resume(); resolve(res.statusCode); }).on('error', reject);
    });
    assert.equal(badHostStatus, 403, 'DNS-rebinding host rejected');

    // profile lifecycle
    assert.equal((await call('POST', '/api/profiles', { name: '../x', conf: GOOD })).status, 400);
    assert.equal((await call('POST', '/api/profiles', { name: 'demo', conf: 'junk' })).status, 400);
    assert.equal((await call('POST', '/api/profiles', { name: 'demo', conf: GOOD })).status, 200);
    let s = (await call('GET', '/api/state')).json;
    assert.equal(s.profiles[0].name, 'demo');
    assert.equal(s.profiles[0].endpoint, '1.2.3.4:51820');
    assert.equal(s.active, null);
    assert.equal(fs.statSync(path.join(process.env.PROVPN_PROFILES, 'demo.conf')).mode & 0o077, 0, 'profile is 0600');

    // connect / state / disconnect
    assert.equal((await call('POST', '/api/connect', { name: 'demo' })).status, 200);
    s = (await call('GET', '/api/state')).json;
    assert.equal(s.active.name, 'demo');
    assert.equal(s.active.rx, 100);
    assert.equal((await call('POST', '/api/connect', { name: 'demo' })).status, 409);
    assert.equal((await call('DELETE', '/api/profiles/demo')).status, 409);
    assert.equal((await call('POST', '/api/disconnect', { name: 'demo' })).status, 200);
    assert.equal((await call('GET', '/api/state')).json.active, null);
    assert.equal((await call('DELETE', '/api/profiles/demo')).status, 200);

    const calls = fs.readFileSync(callLog, 'utf8').trim().split('\n');
    assert.deepEqual(calls, WINMODE ? ['/installtunnelservice', '/uninstalltunnelservice'] : ['up', 'down'],
      'backend called with platform-correct verbs');

    // ---- subscription parsing ----
    const { parseSubscription } = require('../subscription.js');
    const KP = 'C'.repeat(20) + '+' + 'C'.repeat(22) + '='; // public key containing '+'
    const link = (name, hp) => `wireguard://${K}@${hp}?publickey=${KP}&address=10.9.0.2/32&dns=1.1.1.1#${encodeURIComponent(name)}`;
    const b64 = (arr) => Buffer.from([].concat(arr).join('\n')).toString('base64');

    let p = parseSubscription(b64([
      link('Test One', '1.2.3.4:51820'),
      link('V6 Node', '[2001:db8::1]:51820'),
      'vmess://abc#x',
      `wireguard://${K}@1.2.3.4:51820?publickey=${KP}&address=10.9.0.2/32&dns=1.1.1.1%0APostUp%20%3D%20calc#evil`,
    ]));
    assert.equal(p.profiles.length, 2, 'two wireguard links parsed');
    assert.deepEqual(p.unsupported, { vmess: 1 });
    assert.equal(p.skipped.length, 1, 'newline-injection link skipped');
    assert.equal(parseConf(p.profiles[0].conf).ok, true);
    assert.ok(p.profiles[0].conf.includes(`PublicKey = ${KP}`), "'+' in key survives query parsing");
    assert.match(p.profiles[1].conf, /Endpoint = \[2001:db8::1\]:51820/);

    p = parseSubscription(JSON.stringify({ outbounds: [
      { type: 'wireguard', tag: 'sb1', server: '5.6.7.8', server_port: 51820, local_address: ['10.1.0.2/32'], private_key: K, peer_public_key: K },
      { type: 'vmess', tag: 'v' }, { type: 'direct', tag: 'd' },
    ] }));
    assert.equal(p.profiles.length, 1);
    assert.deepEqual(p.unsupported, { vmess: 1 });
    assert.equal(parseConf(p.profiles[0].conf).ok, true);
    assert.equal(parseSubscription(GOOD + '\n' + GOOD).profiles.length, 2, 'plain .conf blocks');

    // ---- subscription API (local mock server) ----
    let subBody = b64([link('One', '1.2.3.4:51820'), link('Two', '5.6.7.8:51820')]);
    const mock = require('node:http').createServer((q, r) => {
      if (q.url.startsWith('/s')) r.end(subBody); else { r.statusCode = 404; r.end(); }
    });
    await new Promise((r) => mock.listen(17990, '127.0.0.1', r));
    try {
      const subUrl = 'http://127.0.0.1:17990/s?token=SECRET123';
      let r = await call('POST', '/api/subscriptions', { url: subUrl });
      assert.equal(r.status, 200);
      assert.equal(r.json.imported, 2);
      s = (await call('GET', '/api/state')).json;
      assert.equal(s.subscriptions.length, 1);
      assert.equal(s.subscriptions[0].count, 2);
      assert.ok(!JSON.stringify(s).includes('SECRET123'), 'subscription URL secret is never returned');
      const subId = s.subscriptions[0].id;
      assert.equal(s.profiles.filter((x) => x.sub === subId).length, 2);
      assert.equal(fs.statSync(path.join(process.env.PROVPN_PROFILES, '_subscriptions.json')).mode & 0o077, 0, 'subs file is 0600');

      const first = s.profiles[0].name;
      assert.equal((await call('POST', '/api/connect', { name: first })).status, 200, 'connect to imported profile');
      subBody = b64(link('Only', '9.9.9.9:51820'));
      assert.equal((await call('POST', `/api/subscriptions/${subId}/refresh`)).status, 409, 'refresh blocked while connected');
      assert.equal((await call('POST', '/api/disconnect', { name: first })).status, 200);
      r = await call('POST', `/api/subscriptions/${subId}/refresh`);
      assert.equal(r.status, 200);
      assert.equal(r.json.imported, 1);
      s = (await call('GET', '/api/state')).json;
      assert.equal(s.profiles.length, 1, 'old profiles replaced on refresh');
      assert.equal(s.profiles[0].endpoint, '9.9.9.9:51820');

      // mixed subscription: WireGuard + VLESS + Trojan (+ one unsupported protocol)
      subBody = b64([link('W', '1.1.1.1:51820'), `vless://${U}@1.2.3.4:443?security=none#V`, `trojan://pw@5.6.7.8:443?security=none#T`, 'vmess://zzz']);
      r = await call('POST', '/api/subscriptions', { url: subUrl + '&x=2' });
      assert.equal(r.status, 200);
      assert.equal(r.json.imported, 3);
      assert.deepEqual(r.json.unsupported, { vmess: 1 });
      const sub2 = r.json.id;
      s = (await call('GET', '/api/state')).json;
      assert.deepEqual(s.profiles.filter((x) => x.sub === sub2).map((x) => x.kind).sort(), ['trojan', 'vless', 'wireguard']);
      const vlessName = s.profiles.find((x) => x.sub === sub2 && x.kind === 'vless').name;
      assert.equal((await call('POST', '/api/connect', { name: vlessName })).status, 200, 'connect to VLESS profile from subscription');
      assert.equal((await call('POST', `/api/subscriptions/${sub2}/refresh`)).status, 409, 'refresh blocked while proxy connected');
      assert.equal((await call('DELETE', `/api/subscriptions/${sub2}`)).status, 409, 'delete blocked while proxy connected');
      assert.equal((await call('POST', '/api/disconnect', { name: vlessName })).status, 200);
      assert.equal((await call('DELETE', `/api/subscriptions/${sub2}`)).status, 200);

      subBody = 'vmess://a\nss://b\nhysteria2://c\n';
      r = await call('POST', '/api/subscriptions', { url: subUrl });
      assert.equal(r.status, 422);
      assert.match(r.json.error, /vmess: 1, ss: 1, hysteria2: 1/);

      assert.equal((await call('POST', '/api/subscriptions', { url: 'ftp://x/y' })).status, 400);
      assert.equal((await call('POST', '/api/subscriptions', { url: 'nonsense' })).status, 400);
      assert.equal((await call('POST', '/api/subscriptions', { url: 'http://127.0.0.1:17990/missing' })).status, 502);

      assert.equal((await call('DELETE', `/api/subscriptions/${subId}`)).status, 200);
      s = (await call('GET', '/api/state')).json;
      assert.equal(s.subscriptions.length, 0);
      assert.equal(s.profiles.length, 0, 'subscription profiles removed');
    } finally { mock.close(); }

    // ---- VLESS / Trojan: parsing ----
    const { proxyUriToProfile } = require('../subscription.js');
    const { buildRuntimeConfig, createCore } = require('../core.js');

    const reality = proxyUriToProfile(`vless://${U}@example.com:443?security=reality&sni=www.microsoft.com&fp=chrome&pbk=${'A'.repeat(43)}&sid=ab12&flow=xtls-rprx-vision&type=tcp#R`);
    assert.equal(reality.protocol, 'vless');
    assert.equal(reality.outbound.flow, 'xtls-rprx-vision');
    assert.equal(reality.outbound.tls.reality.short_id, 'ab12');
    assert.equal(reality.outbound.tls.utls.fingerprint, 'chrome');

    const ws = proxyUriToProfile('trojan://pa%40ss@[2001:db8::1]:8443?sni=t.example.com&type=ws&host=cdn.example.com&path=%2Fws#T');
    assert.equal(ws.outbound.password, 'pa@ss');
    assert.equal(ws.outbound.server, '2001:db8::1');
    assert.deepEqual(ws.outbound.transport, { type: 'ws', path: '/ws', headers: { Host: 'cdn.example.com' } });
    assert.equal(ws.outbound.tls.server_name, 't.example.com');

    assert.equal(proxyUriToProfile('trojan://pw@h.example.com:443#x').outbound.tls.enabled, true, 'trojan defaults to TLS');
    assert.equal(proxyUriToProfile(`vless://${U}@h.example.com:443#x`).outbound.tls, undefined, 'vless defaults to no TLS');
    assert.throws(() => proxyUriToProfile('vless://not-a-uuid@e.com:443'), /UUID/);
    assert.throws(() => proxyUriToProfile(`vless://${U}@e.com:443?type=xhttp`), /not supported/);
    assert.throws(() => proxyUriToProfile(`vless://${U}@e.com:99999`), /port/);
    assert.throws(() => proxyUriToProfile(`vless://${U}@e.com:443?security=reality&pbk=short`), /Reality/);
    assert.throws(() => proxyUriToProfile(`vless://${U}@e.com:443?flow=xtls-rprx-direct`), /flow/);
    assert.throws(() => proxyUriToProfile('trojan://pw%0Aevil@e.com:443'), /control/);

    // ---- runtime config ----
    const cfg = buildRuntimeConfig(ws.outbound);
    assert.equal(cfg.inbounds[0].type, 'tun');
    assert.equal(cfg.inbounds[0].strict_route, true);
    assert.equal(cfg.outbounds[0].tag, 'proxy');
    assert.equal(cfg.route.final, 'proxy');
    assert.deepEqual(cfg.dns.rules, [], 'IP server needs no bootstrap DNS rule');
    assert.deepEqual(buildRuntimeConfig(reality.outbound).dns.rules, [{ domain: ['example.com'], server: 'local' }]);

    // ---- core process manager (fake sing-box) ----
    const runDir = path.join(tmp, 'run-unit');
    const c1 = createCore({ binary: okCore, runDir, startTimeoutMs: 3000 });
    await c1.start('t1', reality.outbound);
    assert.equal(c1.status().name, 't1');
    const cfgFile = path.join(runDir, 't1.json');
    assert.equal(fs.statSync(cfgFile).mode & 0o077, 0, 'runtime config is 0600');
    assert.equal(JSON.parse(fs.readFileSync(cfgFile, 'utf8')).outbounds[0].uuid, U);
    await assert.rejects(() => c1.start('t1b', reality.outbound), /уже запущено/);
    await c1.stop();
    assert.equal(c1.status(), null);
    assert.ok(!fs.existsSync(cfgFile), 'runtime config removed on stop');

    const c2 = createCore({ binary: badCore, runDir, startTimeoutMs: 3000 });
    await assert.rejects(() => c2.start('t2', reality.outbound), (e) => {
      assert.match(e.message, /завершилось/);
      assert.ok(e.details[0].includes('***') && !e.details[0].includes(U), 'secrets redacted from core output');
      return true;
    });
    assert.ok(!fs.existsSync(path.join(runDir, 't2.json')), 'runtime config removed after failed start');

    const c3 = createCore({ binary: path.join(tmp, 'no-such-core'), runDir, startTimeoutMs: 3000 });
    assert.equal(c3.missing(), true);
    await assert.rejects(() => c3.start('t3', reality.outbound), /не удалось запустить ядро/);

    // ---- VLESS profile via API ----
    const vl = `vless://${U}@127.0.0.1:443?security=none#x`;
    assert.equal((await call('POST', '/api/profiles', { name: 'vl1', conf: vl })).status, 200);
    s = (await call('GET', '/api/state')).json;
    const vp = s.profiles.find((x) => x.name === 'vl1');
    assert.equal(vp.kind, 'vless');
    assert.equal(vp.endpoint, '127.0.0.1:443');
    assert.equal(fs.statSync(path.join(process.env.PROVPN_PROFILES, 'vl1.proxy.json')).mode & 0o077, 0, 'proxy profile is 0600');
    assert.equal((await call('POST', '/api/profiles', { name: 'vl1', conf: GOOD })).status, 409, 'name clash across kinds');
    assert.equal((await call('POST', '/api/profiles', { name: 'bad1', conf: 'vless://nope@h:1' })).status, 400);

    assert.equal((await call('POST', '/api/connect', { name: 'vl1' })).status, 200);
    s = (await call('GET', '/api/state')).json;
    assert.equal(s.active.name, 'vl1');
    assert.equal(s.active.kind, 'proxy');
    assert.equal((await call('POST', '/api/connect', { name: 'vl1' })).status, 409);
    assert.equal((await call('DELETE', '/api/profiles/vl1')).status, 409);
    assert.equal((await call('POST', '/api/disconnect', { name: 'vl1' })).status, 200);
    assert.equal((await call('GET', '/api/state')).json.active, null);
    assert.equal((await call('DELETE', '/api/profiles/vl1')).status, 200);

    console.log(`smoke[${WINMODE ? 'win32' : 'posix'}]: all checks passed (stub wg, stub sing-box, no real tunnel)`);
  } finally {
    server.close();
    fs.rmSync(tmp, { recursive: true, force: true });
  }
})().catch((e) => { console.error(e); process.exit(1); });
