'use strict';
// End-to-end check against a REAL sing-box binary.
//   1. `sing-box check` accepts the runtime configs we generate (TUN mode).
//   2. vless:// and trojan:// links parsed by ProVpn work through a real sing-box
//      server/client pair on loopback (plain TCP and WebSocket transports).
// TUN routing itself is NOT exercised here (it would reroute the CI runner's network).
// Usage: node test/e2e-core.js <path-to-sing-box>

const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const http = require('node:http');
const { spawn, execFileSync } = require('node:child_process');
const { proxyUriToProfile } = require('../subscription.js');
const { buildRuntimeConfig } = require('../core.js');

const bin = process.argv[2] || process.env.PROVPN_SINGBOX;
if (!bin || !fs.existsSync(bin)) { console.log('e2e-core: skipped (no sing-box binary)'); process.exit(0); }

const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'provpn-e2e-'));
const U = '11111111-2222-3333-4444-555555555555';
const results = [];
const procs = [];

function spawnCore(name, cfg) {
  const file = path.join(tmp, `${name}.json`);
  fs.writeFileSync(file, JSON.stringify(cfg));
  const child = spawn(bin, ['run', '-c', file], { windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'] });
  procs.push(child);
  let tail = '';
  const on = (b) => { tail = (tail + b).slice(-1500); };
  child.stdout.on('data', on);
  child.stderr.on('data', on);
  return new Promise((resolve, reject) => {
    const t = setTimeout(() => reject(new Error(`${name}: not started in 15s: ${tail}`)), 15000);
    const poll = setInterval(() => {
      if (/sing-box started/i.test(tail)) { clearTimeout(t); clearInterval(poll); resolve(child); }
    }, 100);
    child.once('exit', (c) => { clearTimeout(t); clearInterval(poll); reject(new Error(`${name}: exited ${c}: ${tail}`)); });
  });
}

function check(name, cfg) {
  const file = path.join(tmp, `check-${name}.json`);
  fs.writeFileSync(file, JSON.stringify(cfg));
  try {
    execFileSync(bin, ['check', '-c', file], { stdio: 'pipe', windowsHide: true });
    results.push(`check ${name}: ok`);
  } catch (e) {
    throw new Error(`check ${name} FAILED: ${String(e.stderr || e.stdout || e.message).trim().slice(0, 600)}`);
  }
}

function viaProxy(port, target) {
  return new Promise((resolve, reject) => {
    const req = http.request({ host: '127.0.0.1', port, path: target, headers: { Host: new URL(target).host }, timeout: 8000 }, (res) => {
      let body = '';
      res.on('data', (d) => { body += d; });
      res.on('end', () => resolve({ status: res.statusCode, body }));
    });
    req.on('timeout', () => req.destroy(new Error('request timeout')));
    req.on('error', reject);
    req.end();
  });
}

async function roundTrip(name, link, serverInbound) {
  const { outbound } = proxyUriToProfile(link);
  const server = await spawnCore(`${name}-srv`, {
    log: { level: 'info' },
    inbounds: [{ tag: 'in', listen: '127.0.0.1', listen_port: 10443, ...serverInbound }],
    outbounds: [{ type: 'direct', tag: 'direct' }],
  });
  const client = await spawnCore(`${name}-cli`, {
    log: { level: 'info' },
    inbounds: [{ type: 'mixed', tag: 'mixed-in', listen: '127.0.0.1', listen_port: 19080 }],
    outbounds: [{ ...outbound, tag: 'proxy' }],
    route: { final: 'proxy' },
  });
  try {
    const r = await viaProxy(19080, 'http://127.0.0.1:18080/ping');
    if (r.status !== 200 || r.body !== 'pong') throw new Error(`${name}: unexpected response ${r.status} ${r.body}`);
    results.push(`roundtrip ${name}: ok`);
  } finally {
    client.kill();
    server.kill();
    await new Promise((res) => setTimeout(res, 500));
  }
}

(async () => {
  const target = http.createServer((q, r) => r.end('pong'));
  await new Promise((r) => target.listen(18080, '127.0.0.1', r));

  try {
    results.push(`core: ${execFileSync(bin, ['version'], { windowsHide: true }).toString().split('\n')[0]}`);

    // 1. runtime configs accepted by the real core
    const links = {
      reality: `vless://${U}@example.com:443?security=reality&sni=www.microsoft.com&fp=chrome&pbk=${'A'.repeat(43)}&sid=ab12&flow=xtls-rprx-vision&type=tcp#R`,
      'vless-ws-tls': `vless://${U}@example.com:443?security=tls&sni=example.com&type=ws&host=example.com&path=%2Fws&alpn=h2,http/1.1&fp=chrome#W`,
      'vless-grpc': `vless://${U}@example.com:443?security=tls&type=grpc&serviceName=svc#G`,
      'trojan-tls': 'trojan://secret@example.com:443?sni=example.com&alpn=h2#T',
      'trojan-ws': 'trojan://secret@1.2.3.4:443?security=tls&sni=a.example.com&type=ws&path=%2Ft#TW',
    };
    for (const [n, l] of Object.entries(links)) check(n, buildRuntimeConfig(proxyUriToProfile(l).outbound));

    // 2. real traffic through real sing-box server/client
    await roundTrip('vless-tcp', `vless://${U}@127.0.0.1:10443?security=none&type=tcp#a`,
      { type: 'vless', users: [{ uuid: U }] });
    await roundTrip('trojan-tcp', 'trojan://secret@127.0.0.1:10443?security=none#b',
      { type: 'trojan', users: [{ password: 'secret' }] });
    await roundTrip('vless-ws', `vless://${U}@127.0.0.1:10443?security=none&type=ws&path=%2Fp#c`,
      { type: 'vless', users: [{ uuid: U }], transport: { type: 'ws', path: '/p' } });

    const msg = `E2E OK | ${results.join(' | ')}`;
    console.log(process.env.GITHUB_ACTIONS ? `::notice::${msg}` : msg);
  } catch (e) {
    const msg = `E2E FAILED | ${results.join(' | ')} | ${e.message}`.replace(/\r?\n/g, '%0A');
    console.log(process.env.GITHUB_ACTIONS ? `::error::${msg}` : msg);
    process.exitCode = 1;
  } finally {
    procs.forEach((p) => { try { p.kill(); } catch { /* gone */ } });
    target.close();
    fs.rmSync(tmp, { recursive: true, force: true });
  }
})();
