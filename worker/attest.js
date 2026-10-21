// App Attest, checked in the Worker with WebCrypto and nothing else.
//
// Checked against Apple's "Validating apps that connect to your server"
// (developer.apple.com/documentation/devicecheck/validating-apps-that-connect-to-your-server)
// and its "Attestation object validation guide"
// (developer.apple.com/documentation/devicecheck/attestation-object-validation-guide),
// both read 2026-10-21. An attestation is CBOR {fmt: "apple-appattest",
// attStmt: {x5c: [credCert, caCert], receipt}, authData}; credCert chains to
// the Apple App Attestation Root CA; its extension 1.2.840.113635.100.8.2
// holds SHA-256(authData ‖ clientDataHash); authData starts with
// SHA-256("<team>.<bundle>"), a counter of 0, the aaguid and the credential
// id SHA-256(public key). An assertion is CBOR {signature,
// authenticatorData}, an ECDSA P-256 signature over
// SHA-256(authenticatorData ‖ clientDataHash).
//
// What this does not check: the receipt (Apple's fraud metric, a server call
// this relay does not make), revocation, key usage, name constraints and
// critical extensions it does not know. The relay stores nothing, so an
// assertion's counter is checked to be above 0, not above the last one.

const subtle = globalThis.crypto.subtle;
const enc = new TextEncoder();

export class AttestError extends Error {}
const fail = (msg) => {
  throw new AttestError(msg);
};

// Apple App Attestation Root CA, from
// https://www.apple.com/certificateauthority/Apple_App_Attestation_Root_CA.pem
// (SHA-256 fingerprint 1C:B9:82:3B:A2:8B:A6:AD:2D:33:A0:06:94:1D:E2:AE:
// 4F:51:3E:F1:D4:E8:31:B9:F7:E0:FA:7B:62:42:C9:32, valid to 2045-03-15).
export const APPLE_ROOT_PEM = `-----BEGIN CERTIFICATE-----
MIICITCCAaegAwIBAgIQC/O+DvHN0uD7jG5yH2IXmDAKBggqhkjOPQQDAzBSMSYw
JAYDVQQDDB1BcHBsZSBBcHAgQXR0ZXN0YXRpb24gUm9vdCBDQTETMBEGA1UECgwK
QXBwbGUgSW5jLjETMBEGA1UECAwKQ2FsaWZvcm5pYTAeFw0yMDAzMTgxODMyNTNa
Fw00NTAzMTUwMDAwMDBaMFIxJjAkBgNVBAMMHUFwcGxlIEFwcCBBdHRlc3RhdGlv
biBSb290IENBMRMwEQYDVQQKDApBcHBsZSBJbmMuMRMwEQYDVQQIDApDYWxpZm9y
bmlhMHYwEAYHKoZIzj0CAQYFK4EEACIDYgAERTHhmLW07ATaFQIEVwTtT4dyctdh
NbJhFs/Ii2FdCgAHGbpphY3+d8qjuDngIN3WVhQUBHAoMeQ/cLiP1sOUtgjqK9au
Yen1mMEvRq9Sk3Jm5X8U62H+xTD3FE9TgS41o0IwQDAPBgNVHRMBAf8EBTADAQH/
MB0GA1UdDgQWBBSskRBTM72+aEH/pwyp5frq5eWKoTAOBgNVHQ8BAf8EBAMCAQYw
CgYIKoZIzj0EAwMDaAAwZQIwQgFGnByvsiVbpTKwSga0kP0e8EeDS4+sQmTvb7vn
53O5+FRXgeLhpJ06ysC5PrOyAjEAp5U4xDgEgllF7En3VcE3iexZZtKeYnpqtijV
oyFraWVIyd/dganmrduC1bmTBGwD
-----END CERTIFICATE-----`;

export const fromBase64 = (s) => Uint8Array.from(atob(s), (c) => c.charCodeAt(0));
export const APPLE_ROOT_DER = fromBase64(APPLE_ROOT_PEM.replace(/-----[^-]+-----|\s/g, ""));

const NONCE_OID = "1.2.840.113635.100.8.2";
const BASIC_CONSTRAINTS = "2.5.29.19";
const EC_PUBLIC_KEY = "1.2.840.10045.2.1";
const CURVES = { "1.2.840.10045.3.1.7": ["P-256", 32], "1.3.132.0.34": ["P-384", 48] };
const SIG_HASH = { "1.2.840.10045.4.3.2": "SHA-256", "1.2.840.10045.4.3.3": "SHA-384" };
const AAGUID = {
  production: concat(enc.encode("appattest"), new Uint8Array(7)),
  development: enc.encode("appattestdevelop"),
};
// Launch categories accepted when authData carries
// apple_validation_category_01: TestFlight and the App Store; a development
// build and ad-hoc too in the development environment.
const CATEGORIES = { production: [2, 4], development: [2, 3, 4, 5] };

export function concat(...parts) {
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
  let o = 0;
  for (const p of parts) {
    out.set(p, o);
    o += p.length;
  }
  return out;
}
const equal = (a, b) => a.length === b.length && a.every((x, i) => x === b[i]);
const sha256 = async (b) => new Uint8Array(await subtle.digest("SHA-256", b));

// ---- CBOR: maps, byte strings, text strings, arrays, unsigned and
// negative ints (COSE keys use negative labels). Anything else is refused.

export function decodeCbor(b, at = 0, depth = 0) {
  if (depth > 8) fail("cbor: too deep");
  if (at >= b.length) fail("cbor: truncated");
  const major = b[at] >> 5;
  const info = b[at] & 0x1f;
  let n;
  let p = at + 1;
  if (info < 24) n = info;
  else if (info >= 24 && info <= 26) {
    const size = 1 << (info - 24);
    if (p + size > b.length) fail("cbor: truncated");
    n = 0;
    for (let i = 0; i < size; i++) n = n * 256 + b[p + i];
    p += size;
  } else fail("cbor: unsupported length");
  switch (major) {
    case 0:
      return { value: n, end: p };
    case 1:
      return { value: -1 - n, end: p };
    case 2:
    case 3: {
      if (p + n > b.length) fail("cbor: truncated");
      const bytes = b.slice(p, p + n);
      return { value: major === 2 ? bytes : new TextDecoder("utf-8", { fatal: true }).decode(bytes), end: p + n };
    }
    case 4: {
      const out = [];
      for (let i = 0; i < n; i++) {
        const r = decodeCbor(b, p, depth + 1);
        out.push(r.value);
        p = r.end;
      }
      return { value: out, end: p };
    }
    case 5: {
      const out = new Map();
      for (let i = 0; i < n; i++) {
        const k = decodeCbor(b, p, depth + 1);
        const v = decodeCbor(b, k.end, depth + 1);
        if (typeof k.value !== "string" && typeof k.value !== "number") fail("cbor: bad map key");
        out.set(k.value, v.value);
        p = v.end;
      }
      return { value: out, end: p };
    }
    default:
      fail("cbor: unsupported type");
  }
}

function decodeWhole(b) {
  const r = decodeCbor(b);
  if (r.end !== b.length) fail("cbor: trailing bytes");
  return r.value;
}

// ---- DER, as much as an X.509 certificate needs.

function tlv(b, at) {
  if (at + 2 > b.length) fail("der: truncated");
  const tag = b[at];
  let len = b[at + 1];
  let p = at + 2;
  if (len & 0x80) {
    const size = len & 0x7f;
    if (size === 0 || size > 3 || p + size > b.length) fail("der: bad length");
    len = 0;
    for (let i = 0; i < size; i++) len = len * 256 + b[p + i];
    p += size;
  }
  if (p + len > b.length) fail("der: truncated");
  return { tag, start: at, body: p, end: p + len };
}
const children = (b, node) => {
  const out = [];
  for (let p = node.body; p < node.end;) {
    const c = tlv(b, p);
    out.push(c);
    p = c.end;
  }
  return out;
};
const bytesOf = (b, node) => b.subarray(node.body, node.end);
const whole = (b, node) => b.subarray(node.start, node.end);

function oidString(bytes) {
  const out = [Math.floor(bytes[0] / 40), bytes[0] % 40];
  let v = 0;
  for (let i = 1; i < bytes.length; i++) {
    v = v * 128 + (bytes[i] & 0x7f);
    if (!(bytes[i] & 0x80)) {
      out.push(v);
      v = 0;
    }
  }
  return out.join(".");
}

function derTime(b, node) {
  const s = new TextDecoder().decode(bytesOf(b, node));
  let m;
  if (node.tag === 0x17 && (m = /^(\d{2})(\d{2})(\d{2})(\d{2})(\d{2})(\d{2})Z$/.exec(s))) {
    const y = Number(m[1]);
    m[1] = String(y < 50 ? 2000 + y : 1900 + y);
  } else if (node.tag === 0x18 && (m = /^(\d{4})(\d{2})(\d{2})(\d{2})(\d{2})(\d{2})Z$/.exec(s))) {
    // GeneralizedTime
  } else fail("der: bad time");
  return Date.UTC(+m[1], +m[2] - 1, +m[3], +m[4], +m[5], +m[6]);
}

// parseCert reads one DER certificate: the signed part, the algorithm, the
// signature, the names, the validity, the key and the extensions.
export function parseCert(der) {
  const b = der instanceof Uint8Array ? der : new Uint8Array(der);
  const cert = tlv(b, 0);
  if (cert.tag !== 0x30 || cert.end !== b.length) fail("certificate: not a certificate");
  const [tbsN, algN, sigN] = children(b, cert);
  if (!tbsN || !algN || !sigN || sigN.tag !== 0x03) fail("certificate: malformed");
  let f = children(b, tbsN);
  if (f[0]?.tag === 0xa0) f = f.slice(1);
  const [, innerAlgN, issuerN, validityN, subjectN, spkiN, ...rest] = f;
  if (!spkiN || spkiN.tag !== 0x30) fail("certificate: malformed");
  const sigAlg = oidString(bytesOf(b, children(b, algN)[0]));
  if (!equal(whole(b, innerAlgN), whole(b, algN))) fail("certificate: algorithms differ");
  const [nb, na] = children(b, validityN);
  const [keyAlgN, pointN] = children(b, spkiN);
  const [keyTypeN, curveN] = children(b, keyAlgN);
  const extensions = new Map();
  let ca = false;
  const extsN = rest.find((n) => n.tag === 0xa3);
  if (extsN) {
    for (const e of children(b, children(b, extsN)[0])) {
      const parts = children(b, e);
      const id = oidString(bytesOf(b, parts[0]));
      const critical = parts.length === 3 && b[parts[1].body] !== 0;
      const value = bytesOf(b, parts[parts.length - 1]);
      extensions.set(id, { critical, value });
      if (id === BASIC_CONSTRAINTS) {
        const bc = tlv(value, 0);
        const first = children(value, bc)[0];
        ca = !!first && first.tag === 0x01 && value[first.body] !== 0;
      }
    }
  }
  const sigBits = bytesOf(b, sigN);
  const pointBits = bytesOf(b, pointN);
  if (sigBits[0] !== 0 || pointBits[0] !== 0) fail("certificate: bad bit string");
  return {
    tbs: whole(b, tbsN),
    sigAlg,
    signature: sigBits.subarray(1),
    issuer: whole(b, issuerN),
    subject: whole(b, subjectN),
    notBefore: derTime(b, nb),
    notAfter: derTime(b, na),
    spki: whole(b, spkiN),
    keyType: oidString(bytesOf(b, keyTypeN)),
    curve: curveN ? oidString(bytesOf(b, curveN)) : "",
    point: pointBits.subarray(1),
    extensions,
    ca,
  };
}

// derToRaw turns an ECDSA DER SEQUENCE {r, s} into WebCrypto's r‖s.
export function derToRaw(sig, size) {
  const s = tlv(sig, 0);
  if (s.tag !== 0x30 || s.end !== sig.length) fail("signature: malformed");
  const ints = children(sig, s);
  if (ints.length !== 2 || ints.some((i) => i.tag !== 0x02)) fail("signature: malformed");
  const out = new Uint8Array(2 * size);
  ints.forEach((i, k) => {
    let v = bytesOf(sig, i);
    while (v.length > 1 && v[0] === 0) v = v.subarray(1);
    if (v.length > size) fail("signature: malformed");
    out.set(v, (k + 1) * size - v.length);
  });
  return out;
}

const curveOf = (cert) => {
  if (cert.keyType !== EC_PUBLIC_KEY || !CURVES[cert.curve]) fail("certificate: key is not P-256 or P-384");
  return CURVES[cert.curve];
};

// signedBy checks that parent's key signed child.
export async function signedBy(child, parent) {
  const hash = SIG_HASH[child.sigAlg];
  if (!hash) fail("signature: algorithm not allowed");
  const [namedCurve, size] = curveOf(parent);
  const key = await subtle.importKey("spki", parent.spki, { name: "ECDSA", namedCurve }, false, ["verify"]);
  const ok = await subtle.verify({ name: "ECDSA", hash }, key, derToRaw(child.signature, size), child.tbs);
  if (!ok) fail("signature: certificate not signed by its issuer");
}

const inDate = (c, now) => {
  if (now < c.notBefore || now > c.notAfter) fail("certificate expired or not yet valid");
};

// verifyChain checks [leaf, intermediate] up to root at the time now and
// answers the parsed leaf.
export async function verifyChain(x5c, rootDer, now) {
  if (!Array.isArray(x5c) || x5c.length !== 2 || !x5c.every((c) => c instanceof Uint8Array)) {
    fail("chain: x5c must be the leaf and one intermediate");
  }
  const root = parseCert(rootDer);
  const [leaf, inter] = x5c.map(parseCert);
  if (!equal(inter.issuer, root.subject) || !equal(leaf.issuer, inter.subject)) fail("chain: issuer does not match");
  if (!inter.ca || !root.ca) fail("chain: issuer is not a CA");
  if (leaf.ca) fail("chain: leaf is a CA");
  for (const c of [root, inter, leaf]) inDate(c, now);
  await signedBy(inter, root);
  await signedBy(leaf, inter);
  return leaf;
}

function readAuthData(ad, withCredential) {
  if (ad.length < 37) fail("authData: too short");
  const out = {
    rpIdHash: ad.subarray(0, 32),
    flags: ad[32],
    counter: ((ad[33] << 24) >>> 0) + (ad[34] << 16) + (ad[35] << 8) + ad[36],
  };
  let p = 37;
  if (withCredential) {
    if (ad.length < p + 18) fail("authData: no credential");
    out.aaguid = ad.subarray(p, p + 16);
    const n = (ad[p + 16] << 8) | ad[p + 17];
    p += 18;
    if (ad.length < p + n) fail("authData: truncated");
    out.credentialId = ad.subarray(p, p + n);
    p += n;
    const cose = decodeCbor(ad, p);
    out.cose = cose.value;
    p = cose.end;
  }
  // Apple's own sample carries extensions without the ED flag, so whatever
  // follows is read as the extensions map.
  if (p < ad.length) {
    const ext = decodeCbor(ad, p);
    if (ext.end !== ad.length || !(ext.value instanceof Map)) fail("authData: bad extensions");
    out.extensions = ext.value;
  }
  return out;
}

function checkCategory(auth, categories) {
  const v = auth.extensions?.get("apple_validation_category_01");
  if (v === undefined) return;
  const n = v instanceof Uint8Array && v.length === 4 ? v[0] + (v[1] << 8) + (v[2] << 16) + v[3] * 2 ** 24
    : typeof v === "number" ? v : fail("category: malformed");
  if (!categories.includes(n)) fail(`category: launch category ${n} is not allowed`);
}

const envOf = (env) => (env === "development" ? "development" : "production");

// verifyAttestation checks an App Attest attestation object and answers the
// attested public key (65 bytes, uncompressed P-256 point).
//   attestation    the attestation object (bytes)
//   keyId          the key id the app sent (bytes, SHA-256 of the key)
//   clientDataHash the bytes the app passed to attestKey
//   appId          "<team>.<bundle>"
//   env            "production" (default) or "development"
//   root, now      for tests: another root, another clock (ms)
export async function verifyAttestation({ attestation, keyId, clientDataHash, appId, env, root, now, categories }) {
  const e = envOf(env);
  const obj = decodeWhole(attestation);
  if (!(obj instanceof Map) || obj.get("fmt") !== "apple-appattest") fail("attestation: fmt is not apple-appattest");
  const stmt = obj.get("attStmt");
  const ad = obj.get("authData");
  if (!(stmt instanceof Map) || !(ad instanceof Uint8Array) || !(stmt.get("receipt") instanceof Uint8Array)) {
    fail("attestation: malformed");
  }
  const leaf = await verifyChain(stmt.get("x5c"), root ?? APPLE_ROOT_DER, now ?? Date.now());

  const nonce = await sha256(concat(ad, clientDataHash));
  const ext = leaf.extensions.get(NONCE_OID) ?? fail("nonce: no nonce extension");
  const seqN = tlv(ext.value, 0);
  const tagged = children(ext.value, seqN).find((n) => n.tag === 0xa1) ?? fail("nonce: malformed");
  const oct = children(ext.value, tagged)[0];
  if (!oct || oct.tag !== 0x04 || !equal(bytesOf(ext.value, oct), nonce)) fail("nonce: does not match");

  curveOf(leaf);
  if (leaf.curve !== "1.2.840.10045.3.1.7" || leaf.point.length !== 65 || leaf.point[0] !== 4) {
    fail("attestation: key is not P-256");
  }
  if (!equal(await sha256(leaf.point), keyId)) fail("key id: does not match the certificate's key");

  const auth = readAuthData(ad, true);
  if (!equal(auth.rpIdHash, await sha256(enc.encode(appId)))) fail("rpIdHash: not this app");
  if (auth.counter !== 0) fail("counter: not 0");
  if (!equal(auth.aaguid, AAGUID[e])) fail("aaguid: not the " + e + " environment");
  if (!equal(auth.credentialId, keyId)) fail("credentialId: does not match the key id");
  const x = auth.cose instanceof Map ? auth.cose.get(-2) : undefined;
  const y = auth.cose instanceof Map ? auth.cose.get(-3) : undefined;
  if (!(x instanceof Uint8Array) || !(y instanceof Uint8Array) || !equal(concat(Uint8Array.of(4), x, y), leaf.point)) {
    fail("attestation: authData key does not match the certificate");
  }
  checkCategory(auth, categories ?? CATEGORIES[e]);
  return { publicKey: leaf.point };
}

// verifyAssertion checks an App Attest assertion against an attested public
// key (65-byte point) and answers its counter.
export async function verifyAssertion({ assertion, publicKey, clientDataHash, appId, env, categories }) {
  const obj = decodeWhole(assertion);
  const sig = obj instanceof Map ? obj.get("signature") : undefined;
  const ad = obj instanceof Map ? obj.get("authenticatorData") : undefined;
  if (!(sig instanceof Uint8Array) || !(ad instanceof Uint8Array)) fail("assertion: malformed");
  const nonce = await sha256(concat(ad, clientDataHash));
  const key = await subtle.importKey("raw", publicKey, { name: "ECDSA", namedCurve: "P-256" }, false, ["verify"]);
  if (!(await subtle.verify({ name: "ECDSA", hash: "SHA-256" }, key, derToRaw(sig, 32), nonce))) {
    fail("signature: assertion does not verify");
  }
  const auth = readAuthData(ad, false);
  if (!equal(auth.rpIdHash, await sha256(enc.encode(appId)))) fail("rpIdHash: not this app");
  if (auth.counter < 1) fail("counter: not above 0");
  checkCategory(auth, categories ?? CATEGORIES[envOf(env)]);
  return { counter: auth.counter };
}
