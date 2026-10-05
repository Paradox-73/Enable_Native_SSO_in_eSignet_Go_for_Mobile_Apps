// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip
//
// Helper library of the "Native SSO" folder. build.py stores this file in the collection variable
// `ns_lib`; each request script loads it with
//   const L = new Function('CryptoJS', 'pm', pm.collectionVariables.get('ns_lib'))(CryptoJS, pm);
// It needs only CryptoJS (SHA-256, HMAC-SHA-256) and BigInt, both present in the Postman app and in
// newman, because newman has no crypto.subtle. ES256 (ECDSA over P-256) is done here in BigInt.
// Keys are made at run time and kept only in variables; nothing is stored in the collection file.
'use strict';

const P = BigInt('0xffffffff00000001000000000000000000000000ffffffffffffffffffffffff');
const N = BigInt('0xffffffff00000000ffffffffffffffffbce6faada7179e84f3b9cac2fc632551');
const A = P - 3n;
const GX = BigInt('0x6b17d1f2e12c4247f8bce6e563a440f277037d812deb33a0f4a13945d898c296');
const GY = BigInt('0x4fe342e2fe1a7f9b8ee7eb4a7c0f9e162bce33576b315ececbb6406837bf51f5');

const mod = (a, m) => { const r = a % m; return r >= 0n ? r : r + m; };
function inv(a, m) {
  let r0 = mod(a, m), r1 = m, s0 = 1n, s1 = 0n;
  while (r1 !== 0n) {
    const q = r0 / r1;
    const r2 = r0 - q * r1; r0 = r1; r1 = r2;
    const s2 = s0 - q * s1; s0 = s1; s1 = s2;
  }
  return mod(s0, m);
}
// Jacobian coordinates (X, Y, Z); the point at infinity has Z = 0.
function dbl(p) {
  const X = p[0], Y = p[1], Z = p[2];
  if (Z === 0n || Y === 0n) return [0n, 1n, 0n];
  const YY = mod(Y * Y, P), S = mod(4n * X * YY, P), ZZ = mod(Z * Z, P);
  const M = mod(3n * X * X + A * ZZ * ZZ, P);
  const X3 = mod(M * M - 2n * S, P);
  return [X3, mod(M * (S - X3) - 8n * YY * YY, P), mod(2n * Y * Z, P)];
}
function add(p, q) {
  if (p[2] === 0n) return q;
  if (q[2] === 0n) return p;
  const X1 = p[0], Y1 = p[1], Z1 = p[2], X2 = q[0], Y2 = q[1], Z2 = q[2];
  const Z1Z1 = mod(Z1 * Z1, P), Z2Z2 = mod(Z2 * Z2, P);
  const U1 = mod(X1 * Z2Z2, P), U2 = mod(X2 * Z1Z1, P);
  const S1 = mod(Y1 * Z2 * Z2Z2, P), S2 = mod(Y2 * Z1 * Z1Z1, P);
  if (U1 === U2) return S1 === S2 ? dbl(p) : [0n, 1n, 0n];
  const H = mod(U2 - U1, P), R = mod(S2 - S1, P), HH = mod(H * H, P), HHH = mod(H * HH, P), V = mod(U1 * HH, P);
  const X3 = mod(R * R - HHH - 2n * V, P);
  return [X3, mod(R * (V - X3) - S1 * HHH, P), mod(H * Z1 * Z2, P)];
}
function mul(k, pt) {
  let r = [0n, 1n, 0n], q = pt;
  while (k > 0n) { if (k & 1n) r = add(r, q); q = dbl(q); k >>= 1n; }
  return r;
}
function affine(p) {
  const zi = inv(p[2], P), zi2 = mod(zi * zi, P);
  return [mod(p[0] * zi2, P), mod(p[1] * zi2 * zi, P)];
}

// bytes <-> BigInt, base64url, CryptoJS word arrays
const toBig = (bytes) => BigInt('0x' + (bytes.map((b) => b.toString(16).padStart(2, '0')).join('') || '0'));
function toBytes(n, len) {
  const h = n.toString(16).padStart(len * 2, '0'), out = [];
  for (let i = 0; i < len; i++) out.push(parseInt(h.substr(i * 2, 2), 16));
  return out;
}
const toWords = (bytes) => CryptoJS.lib.WordArray.create(new Uint8Array(bytes));
function fromWords(w) {
  const out = [];
  for (let i = 0; i < w.sigBytes; i++) out.push((w.words[i >>> 2] >>> (24 - (i % 4) * 8)) & 255);
  return out;
}
const utf8 = (s) => Array.from(unescape(encodeURIComponent(s)), (c) => c.charCodeAt(0));
function b64u(bytes) {
  return CryptoJS.enc.Base64.stringify(toWords(bytes)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}
const fromB64u = (s) => fromWords(CryptoJS.enc.Base64.parse(s.replace(/-/g, '+').replace(/_/g, '/')));
const b64uStr = (s) => b64u(utf8(s));
const sha256 = (bytes) => fromWords(CryptoJS.SHA256(toWords(bytes)));
const hmac = (key, bytes) => fromWords(CryptoJS.HmacSHA256(toWords(bytes), toWords(key)));

// Random bytes: crypto.getRandomValues when the runtime has it, else a SHA-256 mix of Math.random,
// time and CryptoJS random words (test keys only).
let counter = 0;
function randomBytes(n) {
  try {
    if (typeof crypto !== 'undefined' && crypto.getRandomValues) return Array.from(crypto.getRandomValues(new Uint8Array(n)));
  } catch (e) { /* fall through */ }
  const out = [];
  while (out.length < n) {
    const seed = utf8([Math.random(), Math.random(), Date.now(), ++counter].join(':'));
    out.push(...sha256(seed.concat(fromWords(CryptoJS.lib.WordArray.random(16)))));
  }
  return out.slice(0, n);
}
function uuid() {
  const b = randomBytes(16);
  b[6] = (b[6] & 15) | 64; b[8] = (b[8] & 63) | 128;
  const h = b.map((x) => x.toString(16).padStart(2, '0')).join('');
  return [h.slice(0, 8), h.slice(8, 12), h.slice(12, 16), h.slice(16, 20), h.slice(20)].join('-');
}

// Deterministic nonce k (RFC 6979, SHA-256) so signing never depends on a weak random source.
function nonceK(d, hash) {
  const x = toBytes(d, 32), h1 = toBytes(mod(toBig(hash), N), 32);
  let V = new Array(32).fill(1), K = new Array(32).fill(0);
  K = hmac(K, V.concat([0], x, h1)); V = hmac(K, V);
  K = hmac(K, V.concat([1], x, h1)); V = hmac(K, V);
  for (;;) {
    V = hmac(K, V);
    const k = toBig(V);
    if (k > 0n && k < N) return k;
    K = hmac(K, V.concat([0])); V = hmac(K, V);
  }
}
// Returns the 64-byte r||s signature (JWS ES256 form) of the message bytes.
function ecdsa(d, msg) {
  const hash = sha256(msg), z = toBig(hash);
  for (;;) {
    const k = nonceK(d, hash);
    const r = mod(affine(mul(k, [GX, GY, 1n]))[0], N);
    if (r === 0n) continue;
    const s = mod(inv(k, N) * (z + r * d), N);
    if (s === 0n) continue;
    return toBytes(r, 32).concat(toBytes(s, 32));
  }
}

// A key is {kty:'EC', crv:'P-256', x, y, d, kid}; publicJwk() drops d.
function newKey() {
  let d;
  do { d = toBig(randomBytes(32)); } while (d <= 0n || d >= N);
  const xy = affine(mul(d, [GX, GY, 1n]));
  const jwk = { kty: 'EC', crv: 'P-256', x: b64u(toBytes(xy[0], 32)), y: b64u(toBytes(xy[1], 32)), d: b64u(toBytes(d, 32)) };
  jwk.kid = thumbprint(jwk);
  return jwk;
}
const publicJwk = (k) => ({ kty: k.kty, crv: k.crv, x: k.x, y: k.y });
// RFC 7638: SHA-256 over the members crv, kty, x, y in that order.
function thumbprint(k) {
  return b64u(sha256(utf8('{"crv":"' + k.crv + '","kty":"' + k.kty + '","x":"' + k.x + '","y":"' + k.y + '"}')));
}
function jws(header, payload, key) {
  const input = b64uStr(JSON.stringify(header)) + '.' + b64uStr(JSON.stringify(payload));
  return input + '.' + b64u(ecdsa(toBig(fromB64u(key.d)), utf8(input)));
}
const now = () => Math.floor(Date.now() / 1000);
// private_key_jwt. The audience is the issuer: this server rejects an endpoint URL as audience.
const clientAssertion = (clientId, key, aud) =>
  jws({ alg: 'ES256', typ: 'JWT', kid: key.kid }, { iss: clientId, sub: clientId, aud: aud, jti: uuid(), iat: now(), exp: now() + 120 }, key);
// DPoP proof (RFC 9449). ath is set for requests that carry an access token.
function dpopProof(key, htm, htu, accessToken, nonce) {
  const p = { jti: uuid(), htm: htm, htu: htu, iat: now() };
  if (accessToken) p.ath = b64u(sha256(utf8(accessToken)));
  if (nonce) p.nonce = nonce;
  return jws({ typ: 'dpop+jwt', alg: 'ES256', jwk: publicJwk(key) }, p, key);
}
function pkce() {
  const verifier = b64u(randomBytes(32));
  return { verifier: verifier, challenge: b64u(sha256(utf8(verifier))) };
}
function decodeJwt(token) {
  const part = (s) => JSON.parse(CryptoJS.enc.Utf8.stringify(CryptoJS.enc.Base64.parse(s.replace(/-/g, '+').replace(/_/g, '/'))));
  const p = String(token).split('.');
  return { header: part(p[0]), payload: part(p[1]) };
}
// Splits a URL into the part before the query and a map of its query parameters (newman has no URL class).
function parseUrl(u) {
  const i = String(u).indexOf('?');
  const base = i < 0 ? String(u) : String(u).slice(0, i);
  const params = {};
  if (i >= 0) {
    String(u).slice(i + 1).split('#')[0].split('&').forEach((kv) => {
      if (!kv) return;
      const j = kv.indexOf('=');
      const k = decodeURIComponent((j < 0 ? kv : kv.slice(0, j)).replace(/\+/g, ' '));
      params[k] = j < 0 ? '' : decodeURIComponent(kv.slice(j + 1).replace(/\+/g, ' '));
    });
  }
  return { base: base, params: params };
}
// Reads a variable (environment first, then collection).
const getVar = (name) => pm.environment.get(name) || pm.collectionVariables.get(name);
const getKey = (name) => {
  const v = getVar(name);
  if (!v) throw new Error('Variable ' + name + ' is empty: run the Setup requests first');
  return JSON.parse(v);
};

return { newKey, publicJwk, thumbprint, jws, clientAssertion, dpopProof, pkce, decodeJwt, parseUrl, b64u, b64uStr, sha256, utf8, uuid, getVar, getKey, now };
