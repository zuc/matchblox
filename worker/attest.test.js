import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  APPLE_ROOT_DER, decodeCbor, parseCert, signedBy, verifyAssertion, verifyAttestation, verifyChain,
} from "./attest.js";
import { attestGrantFor, handlePush } from "./push.js";
import {
  b64, cbor, ecKeys, fakeEnv, fakeFetch, makeAssertion, makeAttestation, post, rawPublic, sha256,
  testChain, BUNDLE, TEAM,
} from "./testkit.js";

const enc = new TextEncoder();
const APP_ID = `${TEAM}.${BUNDLE}`;

// Apple's own sample from "Attestation object validation guide"
// (developer.apple.com/documentation/devicecheck/attestation-object-validation-guide):
// team 1234567890, bundle com.example.myapp, the app passed the raw bytes of
// "example_server_challenge" as clientDataHash. Its leaf is valid
// 2026-04-20..2026-04-23, so the test runs at 2026-04-21.
const sample = JSON.parse(readFileSync(new URL("./testdata/apple-attestation.json", import.meta.url), "utf8"));

test("Apple's sample attestation verifies against the embedded root", async () => {
  const out = await verifyAttestation({
    attestation: Buffer.from(sample.attestation, "base64"),
    keyId: Buffer.from(sample.keyId, "base64"),
    clientDataHash: enc.encode(sample.challenge),
    appId: sample.appId,
    now: Date.parse("2026-04-21T12:00:00Z"),
    categories: [1],
  });
  assert.equal(b64(await sha256(out.publicKey)), sample.keyId);
});

test("Apple's sample attestation is refused once its leaf has expired", async () => {
  await assert.rejects(verifyAttestation({
    attestation: Buffer.from(sample.attestation, "base64"),
    keyId: Buffer.from(sample.keyId, "base64"),
    clientDataHash: enc.encode(sample.challenge),
    appId: sample.appId,
    now: Date.parse("2026-10-21T12:00:00Z"),
    categories: [1],
  }), /certificate/);
});

test("Apple's sample attestation is refused for another challenge", async () => {
  await assert.rejects(verifyAttestation({
    attestation: Buffer.from(sample.attestation, "base64"),
    keyId: Buffer.from(sample.keyId, "base64"),
    clientDataHash: enc.encode("another_challenge"),
    appId: sample.appId,
    now: Date.parse("2026-04-21T12:00:00Z"),
    categories: [1],
  }), /nonce/);
});

test("the embedded Apple root parses and signs itself", async () => {
  const root = parseCert(APPLE_ROOT_DER);
  assert.equal(root.notAfter, Date.parse("2045-03-15T00:00:00Z"));
  assert.equal(root.ca, true);
  await signedBy(root, root);
});

test("a chain of only a leaf is refused", async () => {
  const { rootDer, leafDer } = await testChain(new Uint8Array(32));
  await assert.rejects(verifyChain([leafDer], rootDer, Date.now()), /chain/);
});

test("a chain whose intermediate another root signed is refused", async () => {
  const other = await ecKeys("P-384");
  const { rootDer, interDer, leafDer } = await testChain(new Uint8Array(32), { interSigner: other });
  await assert.rejects(verifyChain([leafDer, interDer], rootDer, Date.now()), /signature/);
});

test("the CBOR decoder reads maps, byte strings, text, arrays and ints", () => {
  const v = decodeCbor(cbor({ a: [1, 300, 70000], b: Uint8Array.of(1, 2), c: "x", d: -7 })).value;
  assert.deepEqual(v.get("a"), [1, 300, 70000]);
  assert.deepEqual([...v.get("b")], [1, 2]);
  assert.equal(v.get("c"), "x");
  assert.equal(v.get("d"), -7);
  assert.throws(() => decodeCbor(Uint8Array.of(0xf9, 0, 0)), /cbor/);
});

test("an attestation verifies and gives the key", async () => {
  const cdh = await sha256(enc.encode("client data"));
  const a = await makeAttestation({ appId: APP_ID, clientDataHash: cdh });
  const out = await verifyAttestation({ attestation: a.att, keyId: a.keyId, clientDataHash: cdh, appId: APP_ID, root: a.root });
  assert.deepEqual([...out.publicKey], [...a.raw]);
});

test("an attestation with a wrong rpIdHash is refused", async () => {
  const cdh = await sha256(enc.encode("client data"));
  const a = await makeAttestation({ appId: APP_ID, clientDataHash: cdh, rpIdHash: await sha256(enc.encode(`${TEAM}.com.other.app`)) });
  await assert.rejects(
    verifyAttestation({ attestation: a.att, keyId: a.keyId, clientDataHash: cdh, appId: APP_ID, root: a.root }),
    /rpIdHash/,
  );
});

test("an attestation under a root that is not Apple's is refused", async () => {
  const cdh = await sha256(enc.encode("client data"));
  const a = await makeAttestation({ appId: APP_ID, clientDataHash: cdh });
  await assert.rejects(
    verifyAttestation({ attestation: a.att, keyId: a.keyId, clientDataHash: cdh, appId: APP_ID }),
    /signature|chain|issuer/,
  );
});

test("an attestation with another key id, counter or environment is refused", async () => {
  const cdh = await sha256(enc.encode("client data"));
  const a = await makeAttestation({ appId: APP_ID, clientDataHash: cdh });
  await assert.rejects(verifyAttestation({ attestation: a.att, keyId: new Uint8Array(32), clientDataHash: cdh,
    appId: APP_ID, root: a.root }), /key id/);
  const c = await makeAttestation({ appId: APP_ID, clientDataHash: cdh, counter: 1 });
  await assert.rejects(verifyAttestation({ attestation: c.att, keyId: c.keyId, clientDataHash: cdh,
    appId: APP_ID, root: c.root }), /counter/);
  await assert.rejects(verifyAttestation({ attestation: a.att, keyId: a.keyId, clientDataHash: cdh,
    appId: APP_ID, root: a.root, env: "development" }), /aaguid/);
});

test("an attestation of a launch category not allowed is refused", async () => {
  const cdh = await sha256(enc.encode("client data"));
  const a = await makeAttestation({ appId: APP_ID, clientDataHash: cdh,
    ext: { apple_validation_category_01: Uint8Array.of(1, 0, 0, 0) } });
  await assert.rejects(verifyAttestation({ attestation: a.att, keyId: a.keyId, clientDataHash: cdh,
    appId: APP_ID, root: a.root }), /category/);
  const ok = await makeAttestation({ appId: APP_ID, clientDataHash: cdh,
    ext: { apple_validation_category_01: Uint8Array.of(4, 0, 0, 0) } });
  await verifyAttestation({ attestation: ok.att, keyId: ok.keyId, clientDataHash: cdh, appId: APP_ID, root: ok.root });
});

test("an assertion verifies over its client data and no other", async () => {
  const k = await ecKeys("P-256");
  const raw = await rawPublic(k.publicKey);
  const assertion = await makeAssertion({ priv: k.privateKey, appId: APP_ID, clientData: "hello" });
  const out = await verifyAssertion({ assertion, publicKey: raw, clientDataHash: await sha256(enc.encode("hello")), appId: APP_ID });
  assert.equal(out.counter, 1);
  await assert.rejects(verifyAssertion({ assertion, publicKey: raw, clientDataHash: await sha256(enc.encode("hellp")),
    appId: APP_ID }), /signature/);
  const zero = await makeAssertion({ priv: k.privateKey, appId: APP_ID, clientData: "hello", counter: 0 });
  await assert.rejects(verifyAssertion({ assertion: zero, publicKey: raw, clientDataHash: await sha256(enc.encode("hello")),
    appId: APP_ID }), /counter/);
});

const now = () => Date.now();
const clientData = (items, time = Math.floor(Date.now() / 1000)) => JSON.stringify({ items, time });

test("an assertion verifies against the attest grant", async () => {
  const { env } = await fakeEnv();
  const k = await ecKeys("P-256");
  const raw = await rawPublic(k.publicKey);
  const attest_grant = await attestGrantFor(env.GRANT_SECRET, raw);
  const cd = clientData([{ kind: "device", value: "ab".repeat(32) }]);
  const assertion = await makeAssertion({ priv: k.privateKey, appId: APP_ID, clientData: cd });
  const body = { assertion: b64(assertion), attest_grant, key_id: b64(await sha256(raw)), client_data: cd };
  const res = await handlePush(post("/push/grant", body), env, { fetch: fakeFetch(), now });
  assert.equal(res.status, 200);
  const out = await res.json();
  assert.match(out.grants["device:" + "ab".repeat(32)], /^[0-9a-f]{64}$/);

  // The same assertion under another key's grant opens nothing.
  const other = await ecKeys("P-256");
  const otherGrant = await attestGrantFor(env.GRANT_SECRET, await rawPublic(other.publicKey));
  const bad = await handlePush(post("/push/grant", { ...body, attest_grant: otherGrant }), env, { fetch: fakeFetch(), now });
  assert.equal(bad.status, 403);

  // A grant made with another secret is refused.
  const forged = await attestGrantFor("not-the-secret", raw);
  const f = await handlePush(post("/push/grant", { ...body, attest_grant: forged }), env, { fetch: fakeFetch(), now });
  assert.equal(f.status, 403);
});

test("an assertion out of its time window is refused", async () => {
  const { env } = await fakeEnv();
  const k = await ecKeys("P-256");
  const raw = await rawPublic(k.publicKey);
  const attest_grant = await attestGrantFor(env.GRANT_SECRET, raw);
  const cd = clientData([{ kind: "device", value: "ab".repeat(32) }], Math.floor(Date.now() / 1000) - 3600);
  const assertion = await makeAssertion({ priv: k.privateKey, appId: APP_ID, clientData: cd });
  const res = await handlePush(post("/push/grant", { assertion: b64(assertion), attest_grant,
    key_id: b64(await sha256(raw)), client_data: cd }), env, { fetch: fakeFetch(), now });
  assert.equal(res.status, 403);
});

test("a first grant call with an attestation answers an attest grant that later calls use", async () => {
  const { env } = await fakeEnv();
  const cd = clientData([{ kind: "start", value: "cd".repeat(40) }, { kind: "channel", value: "dHN0LXNyY2gtY2hubA==" }]);
  const a = await makeAttestation({ appId: APP_ID, clientDataHash: await sha256(enc.encode(cd)) });
  const res = await handlePush(post("/push/grant", { attestation: b64(a.att), key_id: b64(a.keyId), client_data: cd }),
    env, { fetch: fakeFetch(), now, root: a.root });
  assert.equal(res.status, 200);
  const out = await res.json();
  assert.equal(Object.keys(out.grants).length, 2);
  assert.ok(out.grants["channel:dHN0LXNyY2gtY2hubA=="]);

  const cd2 = clientData([{ kind: "device", value: "ef".repeat(32) }]);
  const assertion = await makeAssertion({ priv: a.leafKeys.privateKey, appId: APP_ID, clientData: cd2 });
  const res2 = await handlePush(post("/push/grant", { assertion: b64(assertion), attest_grant: out.attest_grant,
    key_id: b64(a.keyId), client_data: cd2 }), env, { fetch: fakeFetch(), now });
  assert.equal(res2.status, 200);
});

test("a grant call with an unknown kind or a bad token is refused", async () => {
  const { env } = await fakeEnv();
  const k = await ecKeys("P-256");
  const raw = await rawPublic(k.publicKey);
  const attest_grant = await attestGrantFor(env.GRANT_SECRET, raw);
  for (const item of [{ kind: "attest", value: "ab" }, { kind: "device", value: "../4/broadcasts" }]) {
    const cd = clientData([item]);
    const assertion = await makeAssertion({ priv: k.privateKey, appId: APP_ID, clientData: cd });
    const res = await handlePush(post("/push/grant", { assertion: b64(assertion), attest_grant,
      key_id: b64(await sha256(raw)), client_data: cd }), env, { fetch: fakeFetch(), now });
    assert.equal(res.status, 400);
  }
});

test("a channel is made with Apple's channel management API", async () => {
  const { env } = await fakeEnv();
  const k = await ecKeys("P-256");
  const raw = await rawPublic(k.publicKey);
  const attest_grant = await attestGrantFor(env.GRANT_SECRET, raw);
  const cd = clientData([]);
  const assertion = await makeAssertion({ priv: k.privateKey, appId: APP_ID, clientData: cd });
  const fetch = fakeFetch(() => new Response(null, { status: 201, headers: { "apns-channel-id": "dHN0LXNyY2gtY2hubA==" } }));
  const res = await handlePush(post("/push/channel", { assertion: b64(assertion), attest_grant,
    key_id: b64(await sha256(raw)), client_data: cd }), env, { fetch, now });
  assert.equal(res.status, 200);
  const out = await res.json();
  assert.equal(out.channel, "dHN0LXNyY2gtY2hubA==");
  assert.match(out.grant, /^[0-9a-f]{64}$/);
  assert.equal(fetch.calls.length, 1);
  const c = fetch.calls[0];
  assert.equal(c.url, `https://api-manage-broadcast.push.apple.com:2196/1/apps/${BUNDLE}/channels`);
  assert.equal(c.method, "POST");
  assert.match(c.headers.authorization, /^bearer [\w-]+\.[\w-]+\.[\w-]+$/);
  assert.deepEqual(JSON.parse(c.body), { "message-storage-policy": 0, "push-type": "LiveActivity" });

  // Without a valid assertion nothing reaches Apple.
  const fetch2 = fakeFetch();
  const bad = await handlePush(post("/push/channel", { assertion: b64(assertion), attest_grant,
    key_id: b64(await sha256(raw)), client_data: clientData([{ kind: "device", value: "ab" }]) }), env, { fetch: fetch2, now });
  assert.equal(bad.status, 403);
  assert.equal(fetch2.calls.length, 0);
});

