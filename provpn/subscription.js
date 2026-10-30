'use strict';
// Pure subscription parsing: text -> WireGuard configs. No I/O, no dependencies.
//
// Understood formats (after optional base64 decoding):
//   * wireguard:// / wg:// links, one per line
//   * plain WireGuard .conf text (one or several [Interface] blocks)
//   * sing-box JSON with type "wireguard" outbounds/endpoints
// Everything else (vless, vmess, trojan, ss, ...) is only counted as unsupported.

const MAX_PROFILES = 200;

function clean(v) {
  const s = String(v == null ? '' : v).trim();
  if (/[\r\n]/.test(s)) throw new Error('control characters in value');
  return s;
}

function hostPort(host, port) {
  const h = clean(host);
  const p = clean(port);
  if (!h || !p) throw new Error('endpoint host/port missing');
  return h.includes(':') && !h.startsWith('[') ? `[${h}]:${p}` : `${h}:${p}`;
}

function buildConf(o) {
  const lines = ['[Interface]', `PrivateKey = ${clean(o.privateKey)}`, `Address = ${clean(o.address)}`];
  if (o.dns) lines.push(`DNS = ${clean(o.dns)}`);
  if (o.mtu) lines.push(`MTU = ${clean(o.mtu)}`);
  lines.push('', '[Peer]', `PublicKey = ${clean(o.publicKey)}`);
  if (o.psk) lines.push(`PresharedKey = ${clean(o.psk)}`);
  lines.push(`Endpoint = ${o.endpoint}`, `AllowedIPs = ${clean(o.allowedIps || '0.0.0.0/0, ::/0')}`);
  if (o.keepalive) lines.push(`PersistentKeepalive = ${clean(o.keepalive)}`);
  return lines.join('\n');
}

function pick(params, ...names) {
  for (const [k, v] of params) if (names.includes(k.toLowerCase())) return v;
  return '';
}

const dec = (s) => { try { return decodeURIComponent(s); } catch { return s; } };

// wireguard://PRIVATEKEY@host:port?publickey=...&address=...&dns=...#name
function wgUriToConf(line) {
  const m = /^(?:wireguard|wg):\/\/([^@]+)@([^?#]+)(?:\?([^#]*))?(?:#(.*))?$/i.exec(line.trim());
  if (!m) throw new Error('unrecognized wireguard:// link');
  const params = new URLSearchParams(m[3] || '');
  const fixKey = (s) => s.replace(/ /g, '+'); // '+' in an unencoded query becomes a space
  const hp = m[2].replace(/\/+$/, '');
  const i = hp.lastIndexOf(':');
  if (i < 1) throw new Error('endpoint port missing');
  return {
    label: dec(m[4] || '') || 'wg',
    conf: buildConf({
      privateKey: fixKey(dec(m[1])),
      address: pick(params, 'address', 'ip', 'local_address'),
      dns: pick(params, 'dns'),
      mtu: pick(params, 'mtu'),
      publicKey: fixKey(pick(params, 'publickey', 'public_key', 'peer_public_key', 'pbk')),
      psk: fixKey(pick(params, 'presharedkey', 'psk', 'pre_shared_key')),
      endpoint: hostPort(hp.slice(0, i), hp.slice(i + 1)),
      allowedIps: pick(params, 'allowedips', 'allowed_ips'),
      keepalive: pick(params, 'keepalive', 'persistentkeepalive'),
    }),
  };
}

const SB_SKIP = new Set(['direct', 'block', 'dns', 'selector', 'urltest', 'fallback', 'loadbalance', 'relay']);

function fromSingBox(text) {
  const doc = JSON.parse(text);
  const profiles = [];
  const unsupported = {};
  for (const item of [...(doc.endpoints || []), ...(doc.outbounds || [])]) {
    if (!item || !item.type) continue;
    if (item.type !== 'wireguard') {
      if (!SB_SKIP.has(item.type)) unsupported[item.type] = (unsupported[item.type] || 0) + 1;
      continue;
    }
    const peers = item.peers || [{
      address: item.server, port: item.server_port, public_key: item.peer_public_key,
      pre_shared_key: item.pre_shared_key, allowed_ips: item.allowed_ips,
    }];
    const p = peers[0] || {};
    profiles.push({
      label: item.tag || 'wg',
      conf: buildConf({
        privateKey: item.private_key,
        address: [].concat(item.address || item.local_address || []).join(', '),
        mtu: item.mtu,
        publicKey: p.public_key,
        psk: p.pre_shared_key,
        endpoint: hostPort(p.address, p.port),
        allowedIps: p.allowed_ips ? [].concat(p.allowed_ips).join(', ') : '',
      }),
    });
  }
  return { profiles, unsupported };
}

// Subscriptions are usually base64 text; plain text and JSON pass through.
function normalize(text) {
  const t = String(text).replace(/^﻿/, '').trim();
  if (/\[Interface\]/i.test(t) || /:\/\//.test(t) || t.startsWith('{')) return t;
  const b64 = t.replace(/\s+/g, '').replace(/-/g, '+').replace(/_/g, '/');
  if (b64 && /^[A-Za-z0-9+/]+=*$/.test(b64)) {
    const d = Buffer.from(b64, 'base64').toString('utf8').trim();
    if (/\[Interface\]/i.test(d) || /:\/\//.test(d) || d.startsWith('{')) return d;
  }
  return t;
}

function parseSubscription(text) {
  const t = normalize(text);
  const profiles = [];
  const unsupported = {};
  const skipped = [];

  if (t.startsWith('{')) {
    try {
      const r = fromSingBox(t);
      profiles.push(...r.profiles);
      Object.assign(unsupported, r.unsupported);
    } catch (e) { skipped.push(`JSON: ${e.message}`); }
  } else if (/^\s*\[Interface\]/im.test(t)) {
    t.split(/\r?\n(?=\[Interface\])/i).forEach((block, n) => {
      if (/\[Interface\]/i.test(block)) profiles.push({ label: `wg${n + 1}`, conf: block.trim() });
    });
  } else {
    for (const raw of t.split(/\r?\n/)) {
      const line = raw.trim();
      if (!line || line.startsWith('#')) continue;
      const scheme = /^([a-z][a-z0-9+.-]*):\/\//i.exec(line);
      if (!scheme) continue;
      const s = scheme[1].toLowerCase();
      if (s === 'wireguard' || s === 'wg') {
        try { profiles.push(wgUriToConf(line)); } catch (e) { skipped.push(`${s}: ${e.message}`); }
      } else {
        unsupported[s] = (unsupported[s] || 0) + 1;
      }
    }
  }
  return { profiles: profiles.slice(0, MAX_PROFILES), unsupported, skipped };
}

module.exports = { parseSubscription, wgUriToConf, normalize, MAX_PROFILES };
