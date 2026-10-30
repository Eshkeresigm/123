'use strict';
// Proxy core manager: turns a vless/trojan outbound into a system-wide VPN tunnel
// (sing-box TUN mode) and supervises the sing-box process. No dependencies.
//
// Needs administrator rights (TUN + routes) and the sing-box binary.

const fs = require('node:fs');
const path = require('node:path');
const { spawn } = require('node:child_process');

const isIp = (h) => /^\d{1,3}(\.\d{1,3}){3}$/.test(h) || h.includes(':');

// Runtime config for the pinned sing-box 1.11 series. Everything except the
// outbound is fixed: full-tunnel TUN, strict routing (no leaks), DNS via the proxy.
function buildRuntimeConfig(outbound) {
  const dnsRules = isIp(outbound.server) ? [] : [{ domain: [outbound.server], server: 'local' }];
  return {
    log: { level: 'info', timestamp: false },
    dns: {
      servers: [
        { tag: 'remote', address: 'https://1.1.1.1/dns-query', detour: 'proxy' },
        { tag: 'local', address: 'local' },
      ],
      rules: dnsRules,
      final: 'remote',
      strategy: 'ipv4_only',
    },
    inbounds: [{
      type: 'tun', tag: 'tun-in', address: ['172.19.0.1/30'],
      auto_route: true, strict_route: true, stack: 'system',
    }],
    outbounds: [{ ...outbound, tag: 'proxy' }, { type: 'direct', tag: 'direct' }],
    route: {
      auto_detect_interface: true,
      final: 'proxy',
      rules: [
        { action: 'sniff' },
        { protocol: 'dns', action: 'hijack-dns' },
        { ip_is_private: true, outbound: 'direct' },
      ],
    },
  };
}

const alive = (e) => e && e.child.exitCode === null && e.child.signalCode === null;

function createCore({ binary, runDir, startTimeoutMs = 12000, buildConfig = buildRuntimeConfig }) {
  let cur = null;

  const redact = (text, secrets) => secrets.reduce((t, s) => t.split(s).join('***'), text);

  // Is the binary present? Only checkable when an absolute path is configured.
  function missing() {
    return path.isAbsolute(binary) && !fs.existsSync(binary);
  }

  function status() {
    return alive(cur) ? { name: cur.name, since: cur.since } : null;
  }

  async function start(name, outbound) {
    if (alive(cur)) throw new Error('ядро уже запущено');
    fs.mkdirSync(runDir, { recursive: true, mode: 0o700 });
    const cfgFile = path.join(runDir, `${name}.json`);
    fs.writeFileSync(cfgFile, JSON.stringify(buildConfig(outbound)), { mode: 0o600 });
    const secrets = [outbound.uuid, outbound.password].filter(Boolean);
    const cleanup = () => fs.rmSync(cfgFile, { force: true });

    const child = spawn(binary, ['run', '-c', cfgFile], { windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'] });
    const entry = { name, child, since: Date.now(), tail: '' };
    const onData = (b) => { entry.tail = (entry.tail + b.toString()).slice(-4000); };
    child.stdout.on('data', onData);
    child.stderr.on('data', onData);

    const result = await new Promise((resolve) => {
      let done = false;
      const finish = (v) => { if (!done) { done = true; clearTimeout(timer); clearInterval(poll); resolve(v); } };
      const timer = setTimeout(() => finish('timeout'), startTimeoutMs);
      const poll = setInterval(() => { if (/sing-box started/i.test(entry.tail)) finish('started'); }, 100);
      child.once('exit', () => finish('exit'));
      child.once('error', (e) => { entry.spawnError = e; finish('error'); });
    });

    if (result !== 'started') {
      if (alive(entry)) child.kill();
      cleanup();
      const why = {
        error: `не удалось запустить ядро (${entry.spawnError && (entry.spawnError.code || entry.spawnError.message)})`,
        timeout: 'ядро не запустилось за отведённое время',
        exit: 'ядро завершилось сразу после запуска',
      }[result];
      const err = new Error(why);
      err.details = [redact(entry.tail.trim().split('\n').slice(-6).join('\n'), secrets)].filter(Boolean);
      throw err;
    }

    cur = entry;
    child.once('exit', () => { if (cur === entry) cur = null; cleanup(); });
    return { name, since: entry.since };
  }

  function stop() {
    const entry = cur;
    if (!alive(entry)) { cur = null; return Promise.resolve(); }
    return new Promise((resolve) => {
      const t = setTimeout(() => { try { entry.child.kill('SIGKILL'); } catch { /* already gone */ } resolve(); }, 5000);
      entry.child.once('exit', () => { clearTimeout(t); resolve(); });
      entry.child.kill();
    });
  }

  return { start, stop, status, missing };
}

module.exports = { buildRuntimeConfig, createCore };
