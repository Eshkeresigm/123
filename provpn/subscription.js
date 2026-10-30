'use strict';
// Pure subscription parsing: text -> WireGuard configs / VLESS+Trojan outbounds. No I/O, no dependencies.
//
// Understood formats (after optional base64 decoding):
//   * wireguard:// / wg:// / vless:// / trojan:// links, one per line
//   * plain WireGuard .conf text (one or several [Interface] blocks)
//   * sing-box JSON with wireguard / vless / trojan outbounds
// Everything else (vmess, ss, hysteria, ...) is only counted as unsupported.

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

// ---- vless:// and trojan:// -> sing-box outbound ---------------------------
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const isIp = (h) => /^\d{1,3}(\.\d{1,3}){3}$/.test(h) || h.includes(':');

function splitUri(line, scheme) {
  let rest = line.slice(scheme.length + 3);
  const h = rest.indexOf('#');
  const label = h >= 0 ? dec(rest.slice(h + 1)) : '';
  if (h >= 0) rest = rest.slice(0, h);
  const q = rest.indexOf('?');
  const params = new URLSearchParams(q >= 0 ? rest.slice(q + 1) : '');
  const authority = (q >= 0 ? rest.slice(0, q) : rest).replace(/\/+$/, '');
  const at = authority.lastIndexOf('@');
  if (at < 1) throw new Error('credentials missing');
  return { cred: dec(authority.slice(0, at)), hostport: authority.slice(at + 1), params, label };
}

function splitHostPort(hp) {
  let host;
  let port;
  if (hp.startsWith('[')) {
    const e = hp.indexOf(']');
    if (e < 0) throw new Error('bad IPv6 address');
    host = hp.slice(1, e);
    port = hp.slice(e + 2);
  } else {
    const i = hp.lastIndexOf(':');
    if (i < 1) throw new Error('port missing');
    host = hp.slice(0, i);
    port = hp.slice(i + 1);
  }
  port = Number(port);
  if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error('bad port');
  if (!host || /[\s/\\]/.test(host)) throw new Error('bad host');
  return { host, port };
}

function buildTls(params, protocol, server) {
  const get = (...n) => pick(params, ...n);
  const sec = (get('security') || (protocol === 'trojan' ? 'tls' : 'none')).toLowerCase();
  if (sec === 'none') return undefined;
  if (sec !== 'tls' && sec !== 'reality') throw new Error(`security "${sec}" is not supported`);
  const tls = { enabled: true };
  const sni = get('sni', 'peer') || get('host') || (isIp(server) ? '' : server);
  if (sni) tls.server_name = sni;
  if (['1', 'true'].includes(get('allowinsecure', 'insecure').toLowerCase())) tls.insecure = true;
  if (get('alpn')) tls.alpn = get('alpn').split(',').map((s) => s.trim()).filter(Boolean);
  const fp = get('fp');
  if (fp || sec === 'reality') tls.utls = { enabled: true, fingerprint: fp || 'chrome' };
  if (sec === 'reality') tls.reality = { enabled: true, public_key: get('pbk'), short_id: get('sid') };
  return tls;
}

function buildTransport(params) {
  const get = (...n) => pick(params, ...n);
  const type = (get('type') || 'tcp').toLowerCase();
  const host = get('host');
  const p = get('path') || '/';
  switch (type) {
    case 'tcp':
      if ((get('headertype') || 'none').toLowerCase() !== 'none') throw new Error('tcp headerType is not supported');
      return undefined;
    case 'ws': { const t = { type: 'ws', path: p }; if (host) t.headers = { Host: host }; return t; }
    case 'grpc': return { type: 'grpc', service_name: get('servicename', 'service_name') };
    case 'http': case 'h2': { const t = { type: 'http', path: p }; if (host) t.host = host.split(','); return t; }
    case 'httpupgrade': { const t = { type: 'httpupgrade', path: p }; if (host) t.host = host; return t; }
    default: throw new Error(`transport "${type}" is not supported`);
  }
}

const TOP_KEYS = ['type', 'server', 'server_port', 'uuid', 'flow', 'password', 'packet_encoding', 'tls', 'transport'];
const TLS_KEYS = ['enabled', 'server_name', 'insecure', 'alpn', 'utls', 'reality'];
const TRANSPORT_KEYS = ['type', 'path', 'host', 'headers', 'service_name', 'max_early_data', 'early_data_header_name'];
const only = (obj, keys) => Object.fromEntries(Object.entries(obj || {}).filter(([k, v]) => keys.includes(k) && v !== undefined));

// Whitelist + validate. Throws on anything unusable. Returns a clean outbound.
function validateOutbound(raw) {
  const o = only(raw, TOP_KEYS);
  if (o.tls) o.tls = only(o.tls, TLS_KEYS);
  if (o.transport) o.transport = only(o.transport, TRANSPORT_KEYS);
  if (o.type !== 'vless' && o.type !== 'trojan') throw new Error(`protocol "${o.type}" is not supported`);

  clean(o.server);
  if (!o.server || /[\s/\\]/.test(o.server)) throw new Error('bad server');
  if (!Number.isInteger(o.server_port) || o.server_port < 1 || o.server_port > 65535) throw new Error('bad port');
  if (o.type === 'vless') {
    if (!UUID_RE.test(o.uuid || '')) throw new Error('bad UUID');
    if (o.flow && o.flow !== 'xtls-rprx-vision') throw new Error(`flow "${o.flow}" is not supported`);
    delete o.password;
  } else {
    if (!o.password) throw new Error('password missing');
    clean(o.password);
    delete o.uuid;
    delete o.flow;
  }
  if (o.tls && o.tls.reality) {
    const r = o.tls.reality;
    if (!/^[A-Za-z0-9_-]{43}$/.test(r.public_key || '')) throw new Error('bad Reality public key');
    if (!/^([0-9a-f]{2}){0,8}$/i.test(r.short_id || '')) throw new Error('bad Reality short id');
  }
  if (o.transport && o.transport.type === 'grpc' && o.transport.service_name === undefined) o.transport.service_name = '';
  return o;
}

function proxyUriToProfile(line) {
  const scheme = /^(vless|trojan):\/\//i.exec(line.trim());
  if (!scheme) throw new Error('unrecognized link');
  const protocol = scheme[1].toLowerCase();
  const { cred, hostport, params, label } = splitUri(line.trim(), scheme[1]);
  const { host, port } = splitHostPort(hostport);
  const enc = pick(params, 'encryption');
  if (protocol === 'vless' && enc && enc.toLowerCase() !== 'none') throw new Error(`encryption "${enc}" is not supported`);

  const outbound = {
    type: protocol,
    server: host,
    server_port: port,
    ...(protocol === 'vless' ? { uuid: cred, flow: pick(params, 'flow') || undefined } : { password: cred }),
    tls: buildTls(params, protocol, host),
    transport: buildTransport(params),
  };
  return { label: label || host, protocol, outbound: validateOutbound(outbound) };
}

const SB_SKIP = new Set(['direct', 'block', 'dns', 'selector', 'urltest', 'fallback', 'loadbalance', 'relay']);

function fromSingBox(text) {
  const doc = JSON.parse(text);
  const profiles = [];
  const unsupported = {};
  const skipped = [];
  for (const item of [...(doc.endpoints || []), ...(doc.outbounds || [])]) {
    if (!item || !item.type) continue;
    if (item.type === 'vless' || item.type === 'trojan') {
      try {
        profiles.push({ label: item.tag || item.type, protocol: item.type, outbound: validateOutbound(item) });
      } catch (e) { skipped.push(`${item.tag || item.type}: ${e.message}`); }
      continue;
    }
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
  return { profiles, unsupported, skipped };
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
      skipped.push(...r.skipped);
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
      } else if (s === 'vless' || s === 'trojan') {
        try { profiles.push(proxyUriToProfile(line)); } catch (e) { skipped.push(`${s}: ${e.message}`); }
      } else {
        unsupported[s] = (unsupported[s] || 0) + 1;
      }
    }
  }
  return { profiles: profiles.slice(0, MAX_PROFILES), unsupported, skipped };
}

module.exports = { parseSubscription, wgUriToConf, proxyUriToProfile, validateOutbound, normalize, MAX_PROFILES };
