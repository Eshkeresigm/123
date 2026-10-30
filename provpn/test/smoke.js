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

    console.log(`smoke[${WINMODE ? 'win32' : 'posix'}]: all checks passed (stub wg, no real tunnel)`);
  } finally {
    server.close();
    fs.rmSync(tmp, { recursive: true, force: true });
  }
})().catch((e) => { console.error(e); process.exit(1); });
