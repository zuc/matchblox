import { test, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { apnsRequest, checkGrant, grantFor, handlePush, resetTokenCache } from "./push.js";
import worker from "./index.js";
import { fakeEnv, fakeFetch, post, BUNDLE, TEAM } from "./testkit.js";

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
  assert.ok(Math.abs(claims.iat - Date.now() / 1000) < 5);
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

test("the token is kept for 50 minutes and then made again", async () => {
  const { env } = await fakeEnv();
  let t = Date.parse("2026-10-21T10:00:00Z");
  const now = () => t;
  const e = await event(env, "device", DEVICE);
  const a = (await apnsRequest(env, e, now)).headers.authorization;
  t += 49 * 60000;
  assert.equal((await apnsRequest(env, e, now)).headers.authorization, a);
  t += 2 * 60000;
  assert.notEqual((await apnsRequest(env, e, now)).headers.authorization, a);
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
