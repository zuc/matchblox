// The push relay on matchblox.sh/push/*. It stores nothing and logs
// nothing: no console call anywhere in this file, so no payload, token or
// grant reaches the Worker's logs.
//
//   POST /push/grant    an App Attest attestation (first call) or assertion
//                       (later) → one grant per item, and an attest grant
//                       that carries the attested key so later calls need no
//                       storage.
//   POST /push/channel  an assertion → a new broadcast channel and its grant.
//   POST /push/send     an event the host sends (internal/push/relay.go) →
//                       Apple's status, as the HTTP status and in {status}.
//
// A grant is hex(HMAC-SHA256(GRANT_SECRET, kind ":" target)) and opens that
// one target only.
//
// Secrets (RELEASING.md): APNS_KEY (the .p8), APNS_KEY_ID, APNS_TEAM_ID,
// APNS_BUNDLE_ID, GRANT_SECRET. APNS_ENV = "development" sends to Apple's
// sandbox and accepts development App Attest keys; anything else is
// production.

import { AttestError, fromBase64, verifyAssertion, verifyAttestation } from "./attest.js";

const subtle = globalThis.crypto.subtle;
const enc = new TextEncoder();

// Apple's hosts, paths and headers, checked 2026-10-21 against:
// - "Sending notification requests to APNs"
//   (developer.apple.com/documentation/usernotifications/sending-notification-requests-to-apns):
//   api.push.apple.com:443 / api.sandbox.push.apple.com:443, POST
//   /3/device/<token>, authorization "bearer <token>", apns-push-type,
//   apns-priority (10, 5 or 1), apns-topic (the bundle id).
// - "Starting and updating Live Activities with ActivityKit push
//   notifications" (developer.apple.com/documentation/activitykit/…):
//   apns-push-type liveactivity, apns-topic "<bundle>.push-type.liveactivity";
//   on a channel, apns-channel-id instead.
// - "Sending broadcast push notification requests to APNs"
//   (developer.apple.com/documentation/usernotifications/sending-broadcast-push-notification-requests-to-apns):
//   POST /4/broadcasts/apps/<bundle> on api.push.apple.com:443, apns-channel-id,
//   apns-expiration required (0 on a channel that stores no message),
//   apns-priority, apns-push-type liveactivity.
// - "Sending channel management requests to APNs"
//   (developer.apple.com/documentation/usernotifications/sending-channel-management-requests-to-apns):
//   api-manage-broadcast.push.apple.com:2196 (sandbox
//   api-manage-broadcast.sandbox.push.apple.com:2195), POST
//   /1/apps/<bundle>/channels, body {"message-storage-policy": 0|1,
//   "push-type": "LiveActivity"}, 201 with the id in apns-channel-id.
// - "Establishing a token-based connection to APNs"
//   (developer.apple.com/documentation/usernotifications/establishing-a-token-based-connection-to-apns):
//   ES256, header {alg, kid}, claims {iss: team, iat}; refresh no more than
//   once each 20 minutes and no less than once each 60.
export const APPLE = {
  production: { push: "https://api.push.apple.com", manage: "https://api-manage-broadcast.push.apple.com:2196" },
  development: { push: "https://api.sandbox.push.apple.com", manage: "https://api-manage-broadcast.sandbox.push.apple.com:2195" },
  devicePath: (token) => `/3/device/${token}`,
  broadcastPath: (bundle) => `/4/broadcasts/apps/${bundle}`,
  channelsPath: (bundle) => `/1/apps/${bundle}/channels`,
  liveActivityTopic: (bundle) => `${bundle}.push-type.liveactivity`,
};

// The token is made again after 50 minutes: inside Apple's 20–60 minute
// window.
const TOKEN_TTL_MS = 50 * 60 * 1000;
// The longest request body: Apple's broadcast payload is at most 5 KB, an
// alert 4 KB; with the grant and the target this is ample.
const MAX_BODY = 16 * 1024;
// How far the time in client_data may be from now.
const CLIENT_WINDOW_S = 10 * 60;
const MAX_ITEMS = 8;

// Each kind, its push type, and the shape of its target.
const KINDS = {
  device: { pushType: "alert", target: /^[0-9a-fA-F]{16,400}$/ },
  start: { pushType: "liveactivity", target: /^[0-9a-fA-F]{16,400}$/ },
  channel: { pushType: "liveactivity", target: /^[A-Za-z0-9+/]{4,200}={0,2}$/ },
};

const hex = (b) => [...new Uint8Array(b)].map((x) => x.toString(16).padStart(2, "0")).join("");
const fromHex = (s) => Uint8Array.from(s.match(/../g), (x) => parseInt(x, 16));
const b64url = (b) => btoa(String.fromCharCode(...new Uint8Array(b))).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
const fromB64url = (s) => fromBase64(s.replace(/-/g, "+").replace(/_/g, "/") + "=".repeat((4 - (s.length % 4)) % 4));
const sha256 = async (b) => new Uint8Array(await subtle.digest("SHA-256", b));

const hmacKey = (secret) =>
  subtle.importKey("raw", enc.encode(secret), { name: "HMAC", hash: "SHA-256" }, false, ["sign", "verify"]);

// grantFor answers the grant for one target: hex(HMAC(secret, kind ":" value)).
export async function grantFor(secret, kind, value) {
  return hex(await subtle.sign("HMAC", await hmacKey(secret), enc.encode(`${kind}:${value}`)));
}

// checkGrant says whether event.grant opens event.kind:event.target. The
// compare is HMAC verify, so it takes the same time for any wrong grant.
export async function checkGrant(secret, event) {
  const { kind, target, grant } = event ?? {};
  if (typeof kind !== "string" || !/^[a-z]+$/.test(kind)) return false;
  if (typeof target !== "string" || target === "") return false;
  if (typeof grant !== "string" || !/^[0-9a-f]{64}$/.test(grant)) return false;
  return subtle.verify("HMAC", await hmacKey(secret), fromHex(grant), enc.encode(`${kind}:${target}`));
}

// attestGrantFor carries an attested key: "<key, base64url>.<hex HMAC>".
// The HMAC's message has a newline, which no kind has, so it can never be a
// push grant.
export async function attestGrantFor(secret, publicKey) {
  const k = b64url(publicKey);
  return `${k}.${hex(await subtle.sign("HMAC", await hmacKey(secret), enc.encode(`attest\n${k}`)))}`;
}

async function openAttestGrant(secret, grant) {
  const m = typeof grant === "string" && /^([A-Za-z0-9_-]{80,100})\.([0-9a-f]{64})$/.exec(grant);
  if (!m) return null;
  const ok = await subtle.verify("HMAC", await hmacKey(secret), fromHex(m[2]), enc.encode(`attest\n${m[1]}`));
  return ok ? fromB64url(m[1]) : null;
}

// ---- the APNs token

let cached = null;

// resetTokenCache drops the kept token (tests).
export function resetTokenCache() {
  cached = null;
}

async function apnsToken(env, now) {
  const t = now();
  const id = `${env.APNS_TEAM_ID}/${env.APNS_KEY_ID}`;
  if (cached && cached.id === id && t - cached.at < TOKEN_TTL_MS && t >= cached.at) return cached.token;
  const der = fromBase64(String(env.APNS_KEY).replace(/-----[^-]+-----|\s/g, ""));
  const key = await subtle.importKey("pkcs8", der, { name: "ECDSA", namedCurve: "P-256" }, false, ["sign"]);
  const head = b64url(enc.encode(JSON.stringify({ alg: "ES256", kid: env.APNS_KEY_ID })));
  const claims = b64url(enc.encode(JSON.stringify({ iss: env.APNS_TEAM_ID, iat: Math.floor(t / 1000) })));
  const sig = await subtle.sign({ name: "ECDSA", hash: "SHA-256" }, key, enc.encode(`${head}.${claims}`));
  const token = `${head}.${claims}.${b64url(sig)}`;
  cached = { id, at: t, token };
  return token;
}

const apple = (env) => (env.APNS_ENV === "development" ? APPLE.development : APPLE.production);

// apnsRequest builds Apple's request for one checked event.
export async function apnsRequest(env, event, now = Date.now) {
  const bundle = env.APNS_BUNDLE_ID;
  const headers = {
    authorization: `bearer ${await apnsToken(env, now)}`,
    "apns-push-type": event.push_type,
    "apns-priority": String(event.priority),
  };
  let url;
  if (event.kind === "channel") {
    url = apple(env).push + APPLE.broadcastPath(bundle);
    headers["apns-channel-id"] = event.target;
    headers["apns-expiration"] = "0";
  } else {
    url = apple(env).push + APPLE.devicePath(event.target);
    headers["apns-topic"] = event.kind === "start" ? APPLE.liveActivityTopic(bundle) : bundle;
  }
  return { url, method: "POST", headers, body: JSON.stringify(event.payload) };
}

// ---- the routes

const json = (status, body) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json", "Cache-Control": "no-store" } });

class HttpError extends Error {
  constructor(status, msg) {
    super(msg);
    this.status = status;
  }
}
const refuse = (status, msg) => {
  throw new HttpError(status, msg);
};

async function readJSON(request) {
  if (Number(request.headers.get("content-length") || 0) > MAX_BODY) refuse(413, "body too large");
  const text = await request.text();
  if (text.length > MAX_BODY) refuse(413, "body too large");
  try {
    return JSON.parse(text);
  } catch {
    refuse(400, "body is not JSON");
  }
}

function configured(env) {
  for (const k of ["APNS_KEY", "APNS_KEY_ID", "APNS_TEAM_ID", "APNS_BUNDLE_ID", "GRANT_SECRET"]) {
    if (!env[k]) refuse(500, "relay not configured");
  }
}

// appleStatus answers Apple's status, and its reason when it is a plain word.
async function appleStatus(res) {
  const out = { status: res.status };
  try {
    const r = JSON.parse(await res.text()).reason;
    if (typeof r === "string" && /^[A-Za-z]{1,64}$/.test(r)) out.reason = r;
  } catch {
    // An empty or other body: the status alone.
  }
  return out;
}

async function send(request, env, deps) {
  const e = await readJSON(request);
  if (!(await checkGrant(env.GRANT_SECRET, e))) refuse(403, "no grant for this target");
  const k = KINDS[e.kind];
  if (!k || !k.target.test(e.target)) refuse(400, "bad kind or target");
  if (e.push_type !== k.pushType) refuse(400, "bad push type for this kind");
  if (e.priority !== 10 && e.priority !== 5) refuse(400, "priority is 10 or 5");
  if (typeof e.payload !== "object" || e.payload === null || Array.isArray(e.payload)) refuse(400, "payload is a JSON object");
  const r = await apnsRequest(env, e, deps.now);
  let res;
  try {
    res = await deps.fetch(r.url, { method: r.method, headers: r.headers, body: r.body });
  } catch {
    refuse(502, "apns unreachable");
  }
  const out = await appleStatus(res);
  return json(out.status, out);
}

// authenticate checks the App Attest part of a grant or channel call and
// answers the attested key, its attest grant and the client data.
async function authenticate(body, env, deps) {
  const raw = typeof body.client_data === "string" ? body.client_data : JSON.stringify(body.client_data ?? null);
  let cd;
  try {
    cd = JSON.parse(raw);
  } catch {
    refuse(400, "client_data is not JSON");
  }
  if (typeof cd !== "object" || cd === null) refuse(400, "client_data is an object");
  if (typeof cd.time !== "number" || Math.abs(cd.time - deps.now() / 1000) > CLIENT_WINDOW_S) {
    refuse(403, "client_data time is out of its window");
  }
  if (typeof body.key_id !== "string") refuse(400, "key_id missing");
  const keyId = fromBase64(body.key_id);
  if (keyId.length !== 32) refuse(400, "bad key_id");
  const clientDataHash = await sha256(enc.encode(raw));
  const appId = `${env.APNS_TEAM_ID}.${env.APNS_BUNDLE_ID}`;
  const attestEnv = env.APNS_ENV === "development" ? "development" : "production";
  try {
    if (typeof body.attestation === "string") {
      const { publicKey } = await verifyAttestation({ attestation: fromBase64(body.attestation), keyId, clientDataHash,
        appId, env: attestEnv, root: deps.root, now: deps.now() });
      return { cd, attestGrant: await attestGrantFor(env.GRANT_SECRET, publicKey) };
    }
    if (typeof body.assertion === "string") {
      const publicKey = await openAttestGrant(env.GRANT_SECRET, body.attest_grant);
      if (!publicKey || publicKey.length !== 65) refuse(403, "bad attest grant");
      const id = await sha256(publicKey);
      if (id.length !== keyId.length || !id.every((x, i) => x === keyId[i])) refuse(403, "key_id is not the attested key");
      await verifyAssertion({ assertion: fromBase64(body.assertion), publicKey, clientDataHash, appId, env: attestEnv });
      return { cd, attestGrant: body.attest_grant };
    }
  } catch (err) {
    if (err instanceof AttestError) refuse(403, "app attest: " + err.message);
    throw err;
  }
  refuse(400, "an attestation or an assertion is needed");
}

async function grant(request, env, deps) {
  const body = await readJSON(request);
  const items = typeof body.client_data === "string" ? safeItems(body.client_data) : body.client_data?.items;
  if (!Array.isArray(items) || items.length > MAX_ITEMS) refuse(400, `client_data.items is a list of at most ${MAX_ITEMS}`);
  for (const it of items) {
    const k = KINDS[it?.kind];
    if (!k || typeof it.value !== "string" || !k.target.test(it.value)) refuse(400, "bad item");
  }
  const { attestGrant } = await authenticate(body, env, deps);
  const grants = {};
  for (const it of items) grants[`${it.kind}:${it.value}`] = await grantFor(env.GRANT_SECRET, it.kind, it.value);
  return json(200, { attest_grant: attestGrant, grants });
}

function safeItems(s) {
  try {
    return JSON.parse(s)?.items;
  } catch {
    return undefined;
  }
}

async function channel(request, env, deps) {
  const body = await readJSON(request);
  await authenticate(body, env, deps);
  const bundle = env.APNS_BUNDLE_ID;
  let res;
  try {
    res = await deps.fetch(apple(env).manage + APPLE.channelsPath(bundle), {
      method: "POST",
      headers: { authorization: `bearer ${await apnsToken(env, deps.now)}`, "content-type": "application/json" },
      body: JSON.stringify({ "message-storage-policy": 0, "push-type": "LiveActivity" }),
    });
  } catch {
    refuse(502, "apns unreachable");
  }
  const id = res.headers.get("apns-channel-id");
  if (res.status !== 201 || !id || !KINDS.channel.target.test(id)) {
    const out = await appleStatus(res);
    return json(res.status === 201 ? 502 : out.status, out);
  }
  return json(200, { channel: id, grant: await grantFor(env.GRANT_SECRET, "channel", id) });
}

const ROUTES = { "/push/grant": grant, "/push/channel": channel, "/push/send": send };

// handlePush answers one request under /push/. deps is for tests: fetch,
// now (ms) and the App Attest root.
export async function handlePush(request, env, deps = {}) {
  const d = { fetch: deps.fetch ?? ((...a) => fetch(...a)), now: deps.now ?? Date.now, root: deps.root };
  const route = ROUTES[new URL(request.url).pathname];
  if (!route) return json(404, { error: "not found" });
  if (request.method !== "POST") {
    return new Response(JSON.stringify({ error: "POST only" }), { status: 405, headers: { Allow: "POST", "Content-Type": "application/json" } });
  }
  try {
    configured(env);
    return await route(request, env, d);
  } catch (err) {
    if (err instanceof HttpError) return json(err.status, { error: err.message });
    // Any other failure (bad base64 and the like) is the caller's input; the
    // error itself is not echoed, so nothing of the body comes back.
    return json(400, { error: "bad request" });
  }
}
