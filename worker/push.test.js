import { test, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { apnsRequest, attestGrantFor, checkGrant, grantFor, handlePush, resetTokenCache } from "./push.js";
import worker from "./index.js";
import {
  b64, ecKeys, fakeEnv, fakeFetch, fakeLimiter, makeAssertion, post, rawPublic, sha256, BUNDLE, TEAM,
} from "./testkit.js";

// Apple's hosts, paths and headers, named once here as the fixtures expect
// them (checked against developer.apple.com, see push.js).
const APNS = "https://api.push.apple.com";
const DEVICE = "ab".repeat(32);
const START = "cd".repeat(40);
const CHANNEL = "dHN0LXNyY2gtY2hubA==";
const PAYLOAD = { aps: { alert: { title: "secret-session-name", body: "secret-text" } } };

beforeEach(() => resetTokenCache());

async function event(env, kind, target, extra = {}) {
  return { target, kind, grant: await grantFor(env.GRANT_SECRET, kind, target),
    push_type: kind === "device" ? "alert" : "liveactivity", priority: 10, payload: PAYLOAD, ...extra };
}

const b64url = (s) => Buffer.from(s, "base64url");

test("grant for one item does not open another", async () => {
  const s = "test-grant-secret";
  const g = await grantFor(s, "device", DEVICE);
  assert.match(g, /^[0-9a-f]{64}$/);
  assert.equal(await checkGrant(s, { kind: "device", target: DEVICE, grant: g }), true);
  assert.equal(await checkGrant(s, { kind: "device", target: "ef".repeat(32), grant: g }), false);
  assert.equal(await checkGrant(s, { kind: "start", target: DEVICE, grant: g }), false);
  assert.equal(await checkGrant("another-secret", { kind: "device", target: DEVICE, grant: g }), false);
  assert.equal(await checkGrant(s, { kind: "device", target: DEVICE, grant: "" }), false);
  assert.equal(await checkGrant(s, { kind: "device", target: DEVICE, grant: "zz" }), false);
  assert.equal(await checkGrant(s, { kind: "device", target: DEVICE }), false);
  // "device" + ":" + "a:b" and "device:a" + ":" + "b" are the same string, so
  // the colon is not allowed in a kind.
  assert.equal(await checkGrant(s, { kind: "device:a", target: "b", grant: await grantFor(s, "device", "a:b") }), false);
});

test("a send without a grant is refused 403", async () => {
  const { env } = await fakeEnv();
  const fetch = fakeFetch();
  for (const e of [await event(env, "device", DEVICE, { grant: undefined }),
    await event(env, "device", DEVICE, { grant: "00".repeat(32) }),
    { ...(await event(env, "device", DEVICE)), target: "ef".repeat(32) }]) {
    const res = await handlePush(post("/push/send", e), env, { fetch });
    assert.equal(res.status, 403);
  }
  assert.equal(fetch.calls.length, 0);
});

test("a send with a good grant reaches APNs with the right path and headers", async () => {
  const { env, publicKey } = await fakeEnv();
  const fetch = fakeFetch();
  const res = await handlePush(post("/push/send", await event(env, "device", DEVICE)), env, { fetch });
  assert.equal(res.status, 200);
  assert.deepEqual(await res.json(), { status: 200 });
  assert.equal(fetch.calls.length, 1);
  const c = fetch.calls[0];
  assert.equal(c.url, `${APNS}/3/device/${DEVICE}`);
  assert.equal(c.method, "POST");
  assert.equal(c.headers["apns-topic"], BUNDLE);
  assert.equal(c.headers["apns-push-type"], "alert");
  assert.equal(c.headers["apns-priority"], "10");
  assert.deepEqual(JSON.parse(c.body), PAYLOAD);

  // The token: ES256, kid and iss and iat, signed with the APNs key.
  const [h, p, sig] = c.headers.authorization.replace(/^bearer /, "").split(".");
  assert.deepEqual(JSON.parse(b64url(h)), { alg: "ES256", kid: "KEYID12345" });
  const claims = JSON.parse(b64url(p));
  assert.equal(claims.iss, TEAM);
  const age = Date.now() / 1000 - claims.iat;
  assert.ok(age >= 0 && age < 3000, "iat is at most 50 minutes old");
  assert.ok(await crypto.subtle.verify({ name: "ECDSA", hash: "SHA-256" }, publicKey, b64url(sig),
    new TextEncoder().encode(`${h}.${p}`)));

  // A push-to-start goes to the Live Activity topic.
  const r2 = await apnsRequest(env, await event(env, "start", START, { priority: 5 }));
  assert.equal(r2.url, `${APNS}/3/device/${START}`);
  assert.equal(r2.headers["apns-topic"], `${BUNDLE}.push-type.liveactivity`);
  assert.equal(r2.headers["apns-push-type"], "liveactivity");
  assert.equal(r2.headers["apns-priority"], "5");
});

test("a broadcast send uses apns-channel-id", async () => {
  const { env } = await fakeEnv();
  const fetch = fakeFetch();
  const res = await handlePush(post("/push/send", await event(env, "channel", CHANNEL)), env, { fetch });
  assert.equal(res.status, 200);
  const c = fetch.calls[0];
  assert.equal(c.url, `${APNS}/4/broadcasts/apps/${BUNDLE}`);
  assert.equal(c.headers["apns-channel-id"], CHANNEL);
  assert.equal(c.headers["apns-push-type"], "liveactivity");
  assert.equal(c.headers["apns-expiration"], "0");
  assert.equal(c.headers["apns-priority"], "10");
  assert.equal(c.headers["apns-topic"], undefined);
});

test("the sandbox environment sends to Apple's development hosts", async () => {
  const { env } = await fakeEnv({ APNS_ENV: "development" });
  const r = await apnsRequest(env, await event(env, "channel", CHANNEL));
  assert.equal(r.url, `https://api.sandbox.push.apple.com/4/broadcasts/apps/${BUNDLE}`);
});

test("Apple's status comes back as the status, and a 410 on a start stays 410", async () => {
  const { env } = await fakeEnv();
  const fetch = fakeFetch(() => new Response(JSON.stringify({ reason: "Unregistered" }), { status: 410 }));
  const res = await handlePush(post("/push/send", await event(env, "start", START)), env, { fetch });
  assert.equal(res.status, 410);
  assert.deepEqual(await res.json(), { status: 410, reason: "Unregistered" });
});

test("a kind sent with the wrong push type or priority is refused", async () => {
  const { env } = await fakeEnv();
  const fetch = fakeFetch();
  for (const e of [await event(env, "device", DEVICE, { push_type: "liveactivity" }),
    await event(env, "channel", CHANNEL, { push_type: "alert" }),
    await event(env, "device", DEVICE, { priority: 7 }),
    await event(env, "nope", DEVICE)]) {
    const res = await handlePush(post("/push/send", e), env, { fetch });
    assert.equal(res.status, 400);
  }
  assert.equal(fetch.calls.length, 0);
});

test("the token's iat is a 50-minute bucket, so every isolate signs the same claims", async () => {
  const { env } = await fakeEnv();
  const e = await event(env, "device", DEVICE);
  const claims = (auth) => JSON.parse(b64url(auth.replace(/^bearer /, "").split(".")[1]));
  let t = Date.parse("2026-10-21T10:00:00Z");
  const now = () => t;
  let last;
  for (let i = 0; i < 30; i++, t += 7 * 60000) {
    const auth = (await apnsRequest(env, e, now)).headers.authorization;
    const { iat } = claims(auth);
    assert.equal(iat % 3000, 0);
    assert.ok(t / 1000 - iat >= 0 && t / 1000 - iat < 3000, "never older than 50 minutes");
    if (last && last.iat === iat) assert.equal(auth, last.auth, "kept within its bucket");
    if (last && last.iat !== iat) assert.notEqual(auth, last.auth, "made again when the bucket turns");
    last = { iat, auth };
  }
  // Another isolate (no kept token) at the same moment signs the same iat.
  resetTokenCache();
  t -= 7 * 60000;
  assert.equal(claims((await apnsRequest(env, e, now)).headers.authorization).iat, last.iat);
});

test("a relay whose APNs key will not import answers 500, not 400", async () => {
  const { env } = await fakeEnv();
  env.APNS_KEY = "-----BEGIN PRIVATE KEY-----\nbm90IGEga2V5\n-----END PRIVATE KEY-----\n";
  const fetch = fakeFetch();
  const res = await handlePush(post("/push/send", await event(env, "device", DEVICE)), env, { fetch });
  assert.equal(res.status, 500);
  assert.match((await res.json()).error, /not configured/);
  assert.equal(fetch.calls.length, 0);
  const missing = await handlePush(post("/push/send", await event(env, "device", DEVICE)), { ...env, GRANT_SECRET: "" }, { fetch });
  assert.equal(missing.status, 500);
});

test("the rate rule: each target has its own budget on /push/send, and only a grant spends it", async () => {
  const { env } = await fakeEnv();
  env.RL_SEND = fakeLimiter(2);
  const fetch = fakeFetch();
  const send = async (e) => (await handlePush(post("/push/send", e), env, { fetch })).status;
  const d = await event(env, "device", DEVICE);
  assert.deepEqual([await send(d), await send(d), await send(d)], [200, 200, 429]);
  assert.equal(await send(await event(env, "device", "ef".repeat(32))), 200);
  assert.equal(await send(await event(env, "channel", CHANNEL)), 200);
  assert.equal(fetch.calls.length, 4);
  // A send without the grant is refused before the limiter: it cannot use
  // up a target's budget.
  const before = env.RL_SEND.keys.length;
  assert.equal(await send({ ...d, target: "12".repeat(32) }), 403);
  assert.equal(env.RL_SEND.keys.length, before);
  assert.deepEqual(env.RL_SEND.keys.slice(0, 2), [`device:${DEVICE}`, `device:${DEVICE}`]);
});

test("the rate rule: /push/grant and /push/channel are limited per IP", async () => {
  const { env } = await fakeEnv();
  env.RL_GRANT = fakeLimiter(1);
  env.RL_CHANNEL = fakeLimiter(1);
  for (const [path, rl] of [["/push/grant", env.RL_GRANT], ["/push/channel", env.RL_CHANNEL]]) {
    const call = (ip) => handlePush(post(path, {}, { "cf-connecting-ip": ip }), env, { fetch: fakeFetch() });
    assert.notEqual((await call("192.0.2.1")).status, 429);
    assert.equal((await call("192.0.2.1")).status, 429);
    assert.notEqual((await call("192.0.2.2")).status, 429);
    assert.deepEqual(rl.keys, ["ip:192.0.2.1", "ip:192.0.2.1", "ip:192.0.2.2"]);
  }
});

test("a kind named like an Object property is refused as a kind, not by accident", async () => {
  const { env } = await fakeEnv();
  const fetch = fakeFetch();
  // A send's kind is lower case letters, so only "constructor" gets past
  // the grant.
  const res = await handlePush(post("/push/send", await event(env, "constructor", DEVICE)), env, { fetch });
  assert.equal(res.status, 400);
  assert.deepEqual(await res.json(), { error: "bad kind or target" });
  for (const kind of ["constructor", "toString", "__proto__", "hasOwnProperty"]) {
    const g = await handlePush(post("/push/grant", { client_data: { items: [{ kind, value: DEVICE }] } }), env, { fetch });
    assert.equal(g.status, 400);
    assert.deepEqual(await g.json(), { error: "bad item" });
  }
  assert.equal(fetch.calls.length, 0);
});

test("the rate rule: one attested key is one budget, however its key_id is spelled", async () => {
  const { env } = await fakeEnv();
  env.RL_CHANNEL = fakeLimiter(1);
  const k = await ecKeys("P-256");
  const raw = await rawPublic(k.publicKey);
  const keyId = await sha256(raw);
  const attest_grant = await attestGrantFor(env.GRANT_SECRET, raw);
  const cd = JSON.stringify({ items: [], time: Math.floor(Date.now() / 1000) });
  const assertion = await makeAssertion({ priv: k.privateKey, appId: `${TEAM}.${BUNDLE}`, clientData: cd });
  const fetch = fakeFetch(() => new Response(null, { status: 201, headers: { "apns-channel-id": CHANNEL } }));
  const id = b64(keyId);
  for (const [i, spelled] of [id, ` ${id.slice(0, 20)}\n${id.slice(20)}`].entries()) {
    const res = await handlePush(post("/push/channel", { assertion: b64(assertion), attest_grant, key_id: spelled, client_data: cd },
      { "cf-connecting-ip": `192.0.2.${i + 1}` }), env, { fetch });
    assert.equal(res.status, i === 0 ? 200 : 429);
  }
  assert.ok(env.RL_CHANNEL.keys.includes(`key:${Buffer.from(keyId).toString("hex")}`));
  assert.equal(fetch.calls.length, 1);
});

test("a rate binding that fails limits nothing", async () => {
  const { env } = await fakeEnv();
  env.RL_SEND = { limit: async () => { throw new Error("down"); } };
  const res = await handlePush(post("/push/send", await event(env, "device", DEVICE)), env, { fetch: fakeFetch() });
  assert.equal(res.status, 200);
});

test("no payload is logged", async () => {
  const { env } = await fakeEnv();
  const seen = [];
  const saved = {};
  for (const m of ["log", "info", "warn", "error", "debug", "trace"]) {
    saved[m] = console[m];
    console[m] = (...a) => seen.push(a);
  }
  try {
    const ok = fakeFetch();
    await handlePush(post("/push/send", await event(env, "device", DEVICE)), env, { fetch: ok });
    await handlePush(post("/push/send", await event(env, "device", DEVICE, { grant: "00" })), env, { fetch: ok });
    await handlePush(post("/push/send", "{not json"), env, { fetch: ok });
    const bad = fakeFetch(() => new Response(JSON.stringify({ reason: "BadDeviceToken" }), { status: 400 }));
    await handlePush(post("/push/send", await event(env, "device", DEVICE)), env, { fetch: bad });
    const down = async () => { throw new Error("network down secret-text"); };
    const res = await handlePush(post("/push/send", await event(env, "device", DEVICE)), env, { fetch: down });
    assert.equal(res.status, 502);
    assert.doesNotMatch(await res.text(), /secret/);
  } finally {
    Object.assign(console, saved);
  }
  assert.deepEqual(seen, []);
});

test("the Worker routes /push/ on matchblox.sh to the relay, and only POST", async () => {
  const { env } = await fakeEnv();
  env.ASSETS = { fetch: async () => new Response("asset") };
  const res = await worker.fetch(post("/push/send", { kind: "device", target: DEVICE }), env);
  assert.equal(res.status, 403);
  const get = await worker.fetch(new Request("https://matchblox.sh/push/send"), env);
  assert.equal(get.status, 405);
  const none = await worker.fetch(post("/push/other", {}), env);
  assert.equal(none.status, 404);
  const site = await worker.fetch(new Request("https://matchblox.com/push/send", { method: "POST" }), env);
  assert.equal(await site.text(), "asset");
});

test("a body over the limit is refused", async () => {
  const { env } = await fakeEnv();
  const fetch = fakeFetch();
  const big = await event(env, "device", DEVICE, { payload: { aps: { alert: "x".repeat(20000) } } });
  const res = await handlePush(post("/push/send", big), env, { fetch });
  assert.equal(res.status, 413);
  assert.equal(fetch.calls.length, 0);
});
