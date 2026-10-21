// Test helpers for the relay: DER and CBOR encoders, an in-test X.509 chain
// in App Attest's shape, and a fake fetch. Only the tests import this file;
// the Worker never does. Every key is made here with WebCrypto, so no test
// touches Apple or a real device.

const subtle = globalThis.crypto.subtle;
const enc = new TextEncoder();

export const concat = (...parts) => {
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
  let o = 0;
  for (const p of parts) {
    out.set(p, o);
    o += p.length;
  }
  return out;
};

export const sha256 = async (b) => new Uint8Array(await subtle.digest("SHA-256", b));
export const b64 = (b) => Buffer.from(b).toString("base64");

// ---- DER ----

function derLen(n) {
  if (n < 0x80) return Uint8Array.of(n);
  const bytes = [];
  while (n > 0) {
    bytes.unshift(n & 0xff);
    n >>= 8;
  }
  return Uint8Array.of(0x80 | bytes.length, ...bytes);
}
export const tlv = (tag, ...content) => {
  const c = concat(...content);
  return concat(Uint8Array.of(tag), derLen(c.length), c);
};
export const seq = (...c) => tlv(0x30, ...c);
export const set = (...c) => tlv(0x31, ...c);
export const octets = (b) => tlv(0x04, b);
export const bits = (b) => tlv(0x03, Uint8Array.of(0), b);
export const bool = (v) => tlv(0x01, Uint8Array.of(v ? 0xff : 0));
export const utf8 = (s) => tlv(0x0c, enc.encode(s));
export const ctx = (n, ...c) => tlv(0xa0 | n, ...c);
export function int(b) {
  let i = 0;
  while (i < b.length - 1 && b[i] === 0 && !(b[i + 1] & 0x80)) i++;
  b = b.subarray(i);
  return tlv(0x02, b[0] & 0x80 ? concat(Uint8Array.of(0), b) : b);
}
export function oid(s) {
  const p = s.split(".").map(Number);
  const out = [40 * p[0] + p[1]];
  for (const v of p.slice(2)) {
    const chunk = [v & 0x7f];
    let x = v >>> 7;
    while (x > 0) {
      chunk.unshift(0x80 | (x & 0x7f));
      x >>>= 7;
    }
    out.push(...chunk);
  }
  return tlv(0x06, Uint8Array.from(out));
}
function utcTime(d) {
  const p = (n) => String(n).padStart(2, "0");
  const s = `${p(d.getUTCFullYear() % 100)}${p(d.getUTCMonth() + 1)}${p(d.getUTCDate())}` +
    `${p(d.getUTCHours())}${p(d.getUTCMinutes())}${p(d.getUTCSeconds())}Z`;
  return tlv(0x17, enc.encode(s));
}

// rawToDer turns WebCrypto's r‖s into the DER SEQUENCE {r, s} X.509 and App
// Attest use.
export function rawToDer(raw) {
  const h = raw.length / 2;
  return seq(int(raw.subarray(0, h)), int(raw.subarray(h)));
}

const CURVES = {
  "P-256": "1.2.840.10045.3.1.7",
  "P-384": "1.3.132.0.34",
};
const SIG_OIDS = { "SHA-256": "1.2.840.10045.4.3.2", "SHA-384": "1.2.840.10045.4.3.3" };

export const ecKeys = (curve) =>
  subtle.generateKey({ name: "ECDSA", namedCurve: curve }, true, ["sign", "verify"]);
export const rawPublic = async (pub) => new Uint8Array(await subtle.exportKey("raw", pub));
export async function signDer(priv, curve, hash, data) {
  void curve;
  const raw = new Uint8Array(await subtle.sign({ name: "ECDSA", hash }, priv, data));
  return rawToDer(raw);
}

const nameOf = (cn) => seq(set(seq(oid("2.5.4.3"), utf8(cn))));

// makeCert builds and signs one certificate. ext is a list of
// [oid, critical, valueBytes].
export async function makeCert({ subject, issuer, pub, curve, signer, signerCurve, hash,
  notBefore, notAfter, ext = [], serial = 1 }) {
  const alg = seq(oid(SIG_OIDS[hash]));
  const spki = new Uint8Array(await subtle.exportKey("spki", pub));
  void curve;
  const exts = ext.map(([o, crit, v]) => seq(oid(o), ...(crit ? [bool(true)] : []), octets(v)));
  const tbs = seq(
    ctx(0, int(Uint8Array.of(2))),
    int(Uint8Array.of(serial)),
    alg,
    nameOf(issuer),
    seq(utcTime(notBefore), utcTime(notAfter)),
    nameOf(subject),
    spki,
    ...(exts.length ? [ctx(3, seq(...exts))] : []),
  );
  const sig = await signDer(signer, signerCurve, hash, tbs);
  return seq(tbs, alg, bits(sig));
}

export const BASIC_CA = ["2.5.29.19", true, seq(bool(true))];
export const NONCE_OID = "1.2.840.113635.100.8.2";
export const nonceExt = (nonce) => [NONCE_OID, false, seq(ctx(1, octets(nonce)))];

// testChain makes a root, an intermediate and a leaf in Apple's shape: the
// root and the intermediate on P-384, the leaf on P-256 and signed with
// SHA-256, the nonce in the leaf's 1.2.840.113635.100.8.2 extension.
export async function testChain(nonce, opts = {}) {
  const day = 86400000;
  const now = opts.now ?? Date.now();
  const root = await ecKeys("P-384");
  const inter = await ecKeys("P-384");
  const leaf = opts.leafKeys ?? (await ecKeys("P-256"));
  const rootDer = await makeCert({ subject: "Test Root", issuer: "Test Root", pub: root.publicKey,
    curve: "P-384", signer: root.privateKey, signerCurve: "P-384", hash: "SHA-384",
    notBefore: new Date(now - 10 * day), notAfter: new Date(now + 3650 * day), ext: [BASIC_CA] });
  const interDer = await makeCert({ subject: "Test CA 1", issuer: "Test Root", pub: inter.publicKey,
    curve: "P-384", signer: (opts.interSigner ?? root).privateKey, signerCurve: "P-384", hash: "SHA-384",
    notBefore: new Date(now - 10 * day), notAfter: new Date(now + 3650 * day), ext: [BASIC_CA], serial: 2 });
  const leafDer = await makeCert({ subject: "leaf", issuer: "Test CA 1", pub: leaf.publicKey,
    curve: "P-256", signer: inter.privateKey, signerCurve: "P-384", hash: "SHA-256",
    notBefore: new Date(now - day), notAfter: new Date(now + (opts.leafDays ?? 3) * day),
    ext: [nonceExt(nonce)], serial: 3 });
  return { rootDer, interDer, leafDer, leaf };
}

// ---- CBOR (maps, byte strings, text strings, arrays, ints) ----

function cborHead(major, n) {
  if (n < 24) return Uint8Array.of((major << 5) | n);
  if (n < 0x100) return Uint8Array.of((major << 5) | 24, n);
  if (n < 0x10000) return Uint8Array.of((major << 5) | 25, n >> 8, n & 0xff);
  return Uint8Array.of((major << 5) | 26, n >>> 24, (n >> 16) & 0xff, (n >> 8) & 0xff, n & 0xff);
}
export function cbor(v) {
  if (typeof v === "number") return v >= 0 ? cborHead(0, v) : cborHead(1, -1 - v);
  if (typeof v === "string") {
    const b = enc.encode(v);
    return concat(cborHead(3, b.length), b);
  }
  if (v instanceof Uint8Array) return concat(cborHead(2, v.length), v);
  if (Array.isArray(v)) return concat(cborHead(4, v.length), ...v.map(cbor));
  const entries = v instanceof Map ? [...v.entries()] : Object.entries(v);
  return concat(cborHead(5, entries.length), ...entries.flatMap(([k, x]) => [cbor(k), cbor(x)]));
}

// coseKey is the COSE_Key of an uncompressed P-256 point, as App Attest
// puts it in authData.
export const coseKey = (raw) =>
  cbor(new Map([[1, 2], [3, -7], [-1, 1], [-2, raw.subarray(1, 33)], [-3, raw.subarray(33, 65)]]));

export const PROD_AAGUID = concat(enc.encode("appattest"), new Uint8Array(7));

// authData builds App Attest authenticator data: rpIdHash, flags, counter,
// and with a credential the aaguid, the credential id and its COSE key.
export function authData({ rpIdHash, counter = 0, aaguid, credId, cose, ext }) {
  const c = Uint8Array.of(counter >>> 24, (counter >> 16) & 0xff, (counter >> 8) & 0xff, counter & 0xff);
  const flags = Uint8Array.of((credId ? 0x40 : 0) | (ext ? 0x80 : 0));
  const parts = [rpIdHash, flags, c];
  if (credId) parts.push(aaguid, Uint8Array.of(credId.length >> 8, credId.length & 0xff), credId, cose);
  if (ext) parts.push(cbor(ext));
  return concat(...parts);
}

// makeAttestation builds a whole attestation object for keyPair over
// clientDataHash, with a chain under a fresh test root.
export async function makeAttestation({ appId, clientDataHash, rpIdHash, counter = 0, aaguid = PROD_AAGUID, ext,
  leafKeys, now } = {}) {
  leafKeys = leafKeys ?? (await ecKeys("P-256"));
  const raw = await rawPublic(leafKeys.publicKey);
  const credId = await sha256(raw);
  const ad = authData({ rpIdHash: rpIdHash ?? (await sha256(enc.encode(appId))), counter, aaguid, credId,
    cose: coseKey(raw), ext });
  const nonce = await sha256(concat(ad, clientDataHash));
  const chain = await testChain(nonce, { leafKeys, now });
  const att = cbor({ fmt: "apple-appattest", attStmt: { x5c: [chain.leafDer, chain.interDer], receipt: new Uint8Array(4) },
    authData: ad });
  return { att, keyId: credId, raw, root: chain.rootDer, leafKeys };
}

// makeAssertion signs an assertion over clientData with the attested key.
export async function makeAssertion({ priv, appId, clientData, counter = 1, rpIdHash }) {
  const ad = authData({ rpIdHash: rpIdHash ?? (await sha256(enc.encode(appId))), counter });
  const nonce = await sha256(concat(ad, await sha256(enc.encode(clientData))));
  const signature = await signDer(priv, "P-256", "SHA-256", nonce);
  return cbor({ signature, authenticatorData: ad });
}

// ---- fakes ----

// fakeFetch records every call and answers with respond(req) or 200.
export function fakeFetch(respond) {
  const calls = [];
  const f = async (input, init = {}) => {
    const req = new Request(input, init);
    const call = { url: req.url, method: req.method, headers: Object.fromEntries(req.headers), body: await req.text() };
    calls.push(call);
    return respond ? respond(call) : new Response(null, { status: 200 });
  };
  f.calls = calls;
  return f;
}

// apnsKey makes a P-256 key in the .p8 form (PKCS#8 PEM) Apple hands out.
export async function apnsKey() {
  const k = await ecKeys("P-256");
  const pkcs8 = Buffer.from(await subtle.exportKey("pkcs8", k.privateKey)).toString("base64");
  const pem = `-----BEGIN PRIVATE KEY-----\n${pkcs8.match(/.{1,64}/g).join("\n")}\n-----END PRIVATE KEY-----\n`;
  return { pem, publicKey: k.publicKey };
}

export const TEAM = "ABCDE12345";
export const BUNDLE = "com.blox.matchblox";

export async function fakeEnv(extra = {}) {
  const key = await apnsKey();
  return {
    env: { APNS_KEY: key.pem, APNS_KEY_ID: "KEYID12345", APNS_TEAM_ID: TEAM, APNS_BUNDLE_ID: BUNDLE,
      GRANT_SECRET: "test-grant-secret", ...extra },
    publicKey: key.publicKey,
  };
}

export const post = (path, body, headers = {}) =>
  new Request("https://matchblox.sh" + path, { method: "POST", headers: { "Content-Type": "application/json", ...headers },
    body: typeof body === "string" ? body : JSON.stringify(body) });

// fakeLimiter is a Workers Rate Limiting binding that lets each key through
// n times, and records the keys it was asked about.
export function fakeLimiter(n) {
  const counts = new Map();
  return {
    keys: [],
    async limit({ key }) {
      this.keys.push(key);
      counts.set(key, (counts.get(key) ?? 0) + 1);
      return { success: counts.get(key) <= n };
    },
  };
}
