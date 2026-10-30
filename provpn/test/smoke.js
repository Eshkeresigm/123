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
      'vless://uuid@example.com:443?security=tls#x',
      `wireguard://${K}@1.2.3.4:51820?publickey=${KP}&address=10.9.0.2/32&dns=1.1.1.1%0APostUp%20%3D%20calc#evil`,
    ]));
    assert.equal(p.profiles.length, 2, 'two wireguard links parsed');
    assert.deepEqual(p.unsupported, { vless: 1 });
    assert.equal(p.skipped.length, 1, 'newline-injection link skipped');
    assert.equal(parseConf(p.profiles[0].conf).ok, true);
    assert.ok(p.profiles[0].conf.includes(`PublicKey = ${KP}`), "'+' in key survives query parsing");
    assert.match(p.profiles[1].conf, /Endpoint = \[2001:db8::1\]:51820/);

    p = parseSubscription(JSON.stringify({ outbounds: [
      { type: 'wireguard', tag: 'sb1', server: '5.6.7.8', server_port: 51820, local_address: ['10.1.0.2/32'], private_key: K, peer_public_key: K },
      { type: 'vless', tag: 'v' }, { type: 'direct', tag: 'd' },
    ] }));
    assert.equal(p.profiles.length, 1);
    assert.deepEqual(p.unsupported, { vless: 1 });
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

      subBody = 'vless://u@e.com:443#a\nvmess://abc\n';
      r = await call('POST', '/api/subscriptions', { url: subUrl });
      assert.equal(r.status, 422);
      assert.match(r.json.error, /vless: 1, vmess: 1/);

      assert.equal((await call('POST', '/api/subscriptions', { url: 'ftp://x/y' })).status, 400);
      assert.equal((await call('POST', '/api/subscriptions', { url: 'nonsense' })).status, 400);
      assert.equal((await call('POST', '/api/subscriptions', { url: 'http://127.0.0.1:17990/missing' })).status, 502);

      assert.equal((await call('DELETE', `/api/subscriptions/${subId}`)).status, 200);
      s = (await call('GET', '/api/state')).json;
      assert.equal(s.subscriptions.length, 0);
      assert.equal(s.profiles.length, 0, 'subscription profiles removed');
    } finally { mock.close(); }

    console.log(`smoke[${WINMODE ? 'win32' : 'posix'}]: all checks passed (stub wg, no real tunnel)`);
  } finally {
    server.close();
    fs.rmSync(tmp, { recursive: true, force: true });
  }
})().catch((e) => { console.error(e); process.exit(1); });
