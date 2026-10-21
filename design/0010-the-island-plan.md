# The island: implementation plan (#89, #90)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship design 0010 as one project: the buzz and the Live Activity with the app closed, through a stateless relay on the matchblox.sh Worker, with a Sessions face and a Usage face, a setting (Follow, Show, Buzz), and a tap that opens the session.

**Architecture:** The host's service gains `internal/push`. It numbers sessions and accounts, builds one content state with both faces from each `state.Doc`, seals buzzes with HPKE to the phone's key, and posts events to the relay with a timeout. Phones register through a new phone-only gate command over ssh, in the foreground, with Face ID. The relay (worker/push.js) checks App Attest grants, makes broadcast channels and signs APNs pushes; it stores nothing. The iOS app gains a Widget extension (the Live Activity, both faces), a Notification Service Extension (opens the buzz), an Island settings screen, and the open link.

**Tech Stack:** Go 1.26 (`crypto/hpke`, `crypto/hmac`), Bubble Tea v2 console, Cloudflare Worker (plain JS, WebCrypto, tested with `node --test`, no npm dependencies), Swift 5 / SwiftUI, ActivityKit (iOS 18 broadcast channels, push-to-start), UserNotifications (service extension), CryptoKit (HPKE), DeviceCheck (App Attest), XcodeGen.

**Spec:** `design/0010-the-island.md` (approved 2026-10-21). It builds on `design/0006-anywhere.md` §6.4 and §6.6, and on the approved Sessions mockup `design/0006-live-match.html`. Executors read both designs.

## Global Constraints

- No key opens a host without Face ID or the passcode. Nothing connects to a host from the background. Registration and the open path run only while the app is open, after `unlockKey()` (ios/Matchblox/ConsoleView.swift:282).
- No session name, account name, provider, path or text leaves the host in clear. A content state holds only numbers, states, progress, counts, times, percents, window short names and `host` (8 characters of the host key fingerprint).
- The relay stores nothing and logs no payload.
- One island at a time: the widget renders nothing for a content state whose `host` is not the followed host.
- Minimum iOS: **18.0** (ios/project.yml `deploymentTarget`, `IPHONEOS_DEPLOYMENT_TARGET`, and MatchbloxKit `Package.swift` `.iOS("18.0")`).
- Defaults: Show `Sessions`; Follow = the host opened last; Buzz on for each host.
- Rates, verbatim: buzz at most one event each 30 s for each host; a new wait, done or out at once, high priority; progress in steps of 5 %, at most each 3 minutes; usage when a window's whole percent changes by 2 or more, or a window resets, at most each 5 minutes, low priority; the stoker on or off at once. Content state under 2 KB. Up to 4 accounts; the expanded island shows 2. A wait breaks through the Usage face for 60 s.
- Bars: `warn` from 60 %, `neg` from 90 % (DESIGN.md §6 Dock). Glyphs and colours only from DESIGN.md; `Tokens.swift` mirrors `internal/app/theme.go` `Tokens(true)`.
- Copy, verbatim:
  - Push failure on the Machine tab: `push: <why>`. Island renewal: `island: open matchblox on the phone to renew`.
  - Ended session on open: `That session ended.`
  - Unknown numbers: `session 12`, `account 3`.
  - Unreachable host in the setting: `work-1 is not reachable: it changes at the next connect`.
  - Setting labels: `Follow`, `Show`, `Sessions` · `Usage` · `Auto (Usage while the stoker runs)`, `Buzz`.
  - Door title: `Keep matchblox running`.
- Fixtures in `testdata/push/` are read by Go and Swift tests; `.github/workflows/ios.yml` path filters add `testdata/push/**`.
- `internal/app` imports no `push` (internal/arch_test.go). New state reaches the console through `state.Doc` fields.
- Coordination: design 0009 (worktree-feat-box-opens) also changes `internal/setup/devices.go` and the pair flow. Task 6 starts after 0009's pull request merges, or rebases onto it; do not edit pairing.
- Commit types: feat, fix, chore, test, docs, refactor, ci, build. `go test -race ./...` passes before each Go commit; `swift test --package-path ios/MatchbloxKit` before each Swift commit; `node --test worker/` before each Worker commit.
- Live checks only on a private tmux socket and a private state dir. Never the owner's running `matchblox serve`. Never push to real devices from tests: the relay base URL is a config value, and tests use a fake.

## Review Focus

1. **A followed host goes offline while Follow changes.** The old host keeps pushing. The widget must show nothing for it, and the app must end that activity at its next run. Owner: Task 12 (`testAStateOfAHostNotFollowedShowsNothing`) and Task 13 (`testStrayActivitiesEndAtLaunch` as a manual check line in ios/README).
2. **The relay is down or slow.** The service tick must not wait on it. A send that takes longer than its timeout is dropped and shows once as `push: <why>`. Owner: Task 4 (`TestARelayThatHangsDoesNotStallPublish`).
3. **A phone paired before this release.** Its line has `gate phone` without a fingerprint. The service upgrades the line, and `push register` works without pairing again. Owner: Task 6 (`TestOldPhoneLinesGetTheirFingerprint`).
4. **Many accounts and long times.** With 4 accounts × 3 windows, the waits and the forecast, the content state stays under 2 KB; a fifth account is dropped. Owner: Task 3 (`TestTheContentStateStaysUnder2KB`).
5. **A session ends and a new one starts in the same pane.** The new session gets a new number; the old number is never reused, so a stale name never shows on the island. Owner: Task 2 (`TestANumberIsNeverUsedAgain`).

---

### Task 0: Mockups of the Usage face, for the owner's approval

**Files:**
- Create: `design/0010-island-usage.html`

**Interfaces:**
- Consumes: `design/0006-live-match.html` (the Sessions face, approved).
- Produces: the approved look of the Usage face (lock screen, compact, minimal, expanded, the wait that breaks through), which Task 14 implements.

- [ ] **Step 1:** Draw the four Usage sizes next to the Sessions ones from 0006-live-match.html, in DESIGN.md tokens: two accounts (`Max` 5h 61 % `warn` · 14:10, wk 38 %; `Plus` 5h 92 % `neg`), the stoker word, `lasts the week` / `out Fri ~20:00`; compact `5h 61%` and `╿ 5h 61%`.
- [ ] **Step 2:** Publish it as an artifact and ask the owner to approve. Do not start Task 14 before the approval. Commit `docs: the island's usage face mockups`.

### Task 1: The payloads and their fixtures

**Files:**
- Create: `internal/push/payload.go`, `internal/push/payload_test.go`, `testdata/push/content-state.json`, `testdata/push/buzz.json`, `testdata/push/register-request.json`, `testdata/push/register-reply.json`
- Modify: `.github/workflows/ios.yml` (path filters `testdata/push/**`, both events)

**Interfaces:**
- Produces (Go, JSON names snake_case):
  - `type ContentState struct { Host string "host"; Burn, Wait int; Top *Top "top,omitempty"; Accounts []AccountUse "accounts"; Stoker bool "stoker" }`
  - `type Top struct { N int "n"; State string "state" /* burns|waits|done|out|quiet */; Pct int "pct" /* 0..100, 5 % steps */; Out time.Time "out,omitzero" }`
  - `type AccountUse struct { N int "n"; Windows []WindowUse "windows"; Lasts bool "lasts"; RunsOut time.Time "runs_out,omitzero" }`
  - `type WindowUse struct { Name string "name" /* 5h|wk|mo */; Pct int "pct"; Resets time.Time "resets,omitzero" }`
  - `type Buzz struct { Host string "host"; N int "n"; Sealed []byte "sealed" /* HPKE enc ‖ ciphertext */ }`
  - `type RegisterRequest struct { DeviceToken string "device_token"; Grants map[string]string "grants"; NotifyKey []byte "notify_key" /* X25519 public */; Buzz bool "buzz"; Island *IslandReg "island,omitempty" }`
  - `type IslandReg struct { StartToken string "start_token"; Channel string "channel" }`
  - `type Registration struct { Fingerprint string "fingerprint"; RegisterRequest; At time.Time "at" }` (here, not in setup: `setup` imports `push`, and `push` never imports `setup`)
  - `func Host8(fingerprint string) string`: the first 8 hex characters of the SHA-256 in `SHA256:<base64>`, lowercase. testdata/push/host8.json holds three fingerprints and their tags.
  - `type RegisterReply struct { OK bool "ok"; Error string "error,omitempty"; Sessions map[int]string "sessions"; Accounts map[int]string "accounts" }`

- [ ] **Step 1: Write the failing test** `TestHost8MatchesItsFixture`, `TestThePayloadsRoundTripTheirFixtures`: for each fixture, `json.Unmarshal` into its type, `json.Marshal` back, and compare with the file after `json.Compact`. `TestTheContentStateFixtureHasNoName`: walk every string value of content-state.json; each matches `^([0-9a-f]{8}|burns|waits|done|out|quiet|5h|wk|mo|\d{4}-.*Z)$`.
- [ ] **Step 2:** Run `go test ./internal/push/ -run Payload`. Expected: FAIL (package missing).
- [ ] **Step 3:** Write the types and the four fixtures (two accounts, one top session that waits, stoker on).
- [ ] **Step 4:** Run the tests. Expected: PASS.
- [ ] **Step 5:** Commit `feat(push): the island's payloads and their shared fixtures`.

### Task 2: Session and account numbers

**Files:**
- Create: `internal/push/numbers.go`, `internal/push/numbers_test.go`

**Interfaces:**
- Produces:
  - `func OpenNumbers(dir string) (*Numbers, error)` (file `numbers.json` in the state dir; atomic write like `stoker.Store.Set`)
  - `func (n *Numbers) Session(sessionID string) int`, `func (n *Numbers) Account(provider, account string) int` (assign on first sight, persist)
  - `func (n *Numbers) SessionOf(num int) (sessionID string, ok bool)` (for the open path, Task 7)
  - `func (n *Numbers) Names(sessions []sample.Session, accounts []limits.Account) (map[int]string, map[int]string)` (for `RegisterReply`)

- [ ] **Step 1: Write the failing tests:** `TestASessionKeepsItsNumber` (same ID → same number, also after `OpenNumbers` again); `TestANumberIsNeverUsedAgain` (ID a → 1, a ends, ID b in the same pane → 2; `Session("a")` again still 1); `TestAccountsAreNumberedOnTheirOwn`.
- [ ] **Step 2:** Run `go test ./internal/push/ -run Number`. Expected: FAIL.
- [ ] **Step 3:** Implement. The file keeps `next` and the maps; numbers start at 1.
- [ ] **Step 4:** Run. Expected: PASS.
- [ ] **Step 5:** Commit `feat(push): sessions and accounts keep one number for life`.

### Task 3: The content state from a state.Doc

**Files:**
- Create: `internal/push/state.go`, `internal/push/state_test.go`

**Interfaces:**
- Consumes: `state.Doc` (internal/state/state.go:22), `queue.Item` and its states (internal/queue/queue.go:16-22), `sample.Session` (`Busy`, `Out`, `Progress *Progress`, `SessionID`), `limits.Account.Uses(now)` and `.Forecast`, `doc.Stoker != nil && doc.Stoker.On`; `Numbers` (Task 2).
- Produces: `func Build(doc state.Doc, hostFP string, n *Numbers, now time.Time) ContentState`

The top session follows 0006 §6.6.1: a session that waits before one that burns; of the waits, the oldest `Since`; of the burns, the one prompted last (`Session.Started` newest). The states: a queue item with `StatePermission` or `StateQuestion` is `waits`, `StateFinished` is `done`; `Out` not zero is `out`; `Busy` is `burns`; none of these in the whole doc is `quiet` (`Top` = nil). `Pct` is `Progress.Pct` rounded down to 5. `Host` is `Host8(hostFP)` (Task 1).

- [ ] **Step 1: Write the failing tests:** `TestAWaitComesBeforeABurn`, `TestTheOldestWaitComesFirst`, `TestProgressGoesInStepsOf5`, `TestQuietHasNoTop`, `TestUsageHasTheWindowsAndTheForecastOfEachAccount`, `TestStokerOnIsInTheState`, `TestTheContentStateStaysUnder2KB` (4 accounts × 3 windows, a fifth dropped, `len(json) < 2048`), `TestBuildMatchesTheFixture` (a doc made to match testdata/push/content-state.json).
- [ ] **Step 2:** Run `go test ./internal/push/ -run 'Wait|Progress|Quiet|Usage|Stoker|2KB|Fixture'`. Expected: FAIL.
- [ ] **Step 3:** Implement `Build`.
- [ ] **Step 4:** Run. Expected: PASS.
- [ ] **Step 5:** Commit `feat(push): one content state with both faces from each state`.

### Task 4: The sender: its rules and the relay client

**Files:**
- Create: `internal/push/sender.go`, `internal/push/relay.go`, `internal/push/sender_test.go`

**Interfaces:**
- Consumes: `Build` (Task 3), `Seal` (Task 5), `Registrations` (Task 6, as the interface below).
- Produces:
  - `type Relay interface { Send(ctx context.Context, e Event) error }`; `type Event struct { Target, Kind /* device|start|channel */, Grant, PushType /* alert|liveactivity */ string; Priority int /* 10 high, 5 low */; Payload []byte }`
  - `func NewHTTPRelay(base string, timeout time.Duration) Relay` (POST `<base>/push/send`, JSON body; default base `https://matchblox.sh`, timeout 5 s)
  - `type Phones interface { List() []Registration }` (Task 1's type; main.go adapts `setup.Registrations`)
  - `func NewSender(r Relay, p Phones, n *Numbers, hostFP string, now func() time.Time) *Sender`
  - `func (s *Sender) OnDoc(doc state.Doc)` (never blocks: hands the doc to one goroutine through a 1-slot channel that keeps the newest)
  - `func (s *Sender) Status() string` (`""` or `push: <why>`, the last failure, cleared by a success)

Rules, from Global Constraints: buzz when the set of waiting session IDs changes, at most once each 30 s per host, to each registration with `Buzz`; activity update to the followed registration (`Island != nil`) at once on a change of `Top.State`, `Top.N`, a wait count rise, or `Stoker`; on a `Pct` change at most each 3 minutes; on usage (a whole percent change ≥ 2 or a reset) at most each 5 minutes, priority 5. Push-to-start (`Kind: start`) when no activity was started for this registration, and 30 minutes before 8 h since the last start.

- [ ] **Step 1: Write the failing tests** with a fake clock and a recording fake Relay: `TestABuzzGoesAtMostEach30s`, `TestANewWaitUpdatesTheIslandAtOnce`, `TestProgressWaits3Minutes`, `TestUsageWaitsFor2PercentOr5Minutes`, `TestTheStokerFlipGoesAtOnce`, `TestTheIslandStartsAgainBefore8Hours`, `TestOnlyTheFollowingPhoneGetsTheIsland`, `TestBuzzOffGetsNoBuzz`, `TestAFailedStartAsksToRenew` (the relay refuses a `start` with 410: `Status()` is `island: open matchblox on the phone to renew`, and channel updates go on), `TestARelayThatHangsDoesNotStallPublish` (a Relay that blocks: `OnDoc` returns in < 10 ms, the event is dropped after the timeout, `Status()` is `push: <why>`).
- [ ] **Step 2:** Run `go test -race ./internal/push/ -run Sender`. Expected: FAIL.
- [ ] **Step 3:** Implement `Sender` and `NewHTTPRelay` (no retry, no queue).
- [ ] **Step 4:** Run. Expected: PASS.
- [ ] **Step 5:** Commit `feat(push): the sender keeps the buzz and the island's rates`.

### Task 5: The sealed buzz (HPKE), with a vector for Swift

**Files:**
- Create: `internal/push/seal.go`, `internal/push/seal_test.go`, `testdata/push/seal-vector.json`

**Interfaces:**
- Produces: `func Seal(notifyKey []byte, plain []byte) ([]byte, error)` with `crypto/hpke` suite DHKEM(X25519, HKDF-SHA256) / HKDF-SHA256 / ChaCha20Poly1305 and info `matchblox buzz v1`; output `enc ‖ ciphertext`. CryptoKit's `HPKE.Ciphersuite.Curve25519_SHA256_ChachaPoly` opens it (Task 12). The plain text is the line `work-1 · api waits: "Run the migration?"` built by `func BuzzLine(hostLabel string, item queue.Item) string` (the queue word: `asks`, `waits`, `done`; `LastLine` in quotes, cut to 120 characters).
- seal-vector.json: a fixed recipient private key, its public key, the plain text, and one sealed output.

- [ ] **Step 1: Write the failing tests:** `TestASealedBuzzOpensWithTheKey` (open with `crypto/hpke` and the private key), `TestTheVectorOpens`, `TestTheBuzzLineSaysWhoWaitsAndWhy`.
- [ ] **Step 2:** Run. Expected: FAIL.
- [ ] **Step 3:** Implement; write the vector once with a test helper behind `-update`.
- [ ] **Step 4:** Run. Expected: PASS.
- [ ] **Step 5:** Commit `feat(push): the buzz is sealed to the phone's key`.

### Task 6: Registration through the gate

**Files:**
- Create: `internal/setup/register.go`, `internal/setup/register_test.go`, `cmd/matchblox/register.go`
- Modify: `internal/setup/devices.go` (`withPhone` :68 writes `gate phone <fingerprint>`; `gateLine` :152 accepts an optional fingerprint; `RemoveDevice` :189 also removes the registration), `cmd/matchblox/gate.go` (:21-28 parse `["phone", fp]`; `gateDecision` :53 maps `matchblox push register` for phone mode), `cmd/matchblox/devices.go` (`listDevices` :31 adds `push` and `island` columns), `cmd/matchblox/main.go` (`newService` :479 runs `setup.UpgradePhoneLines` once)
- Test: `cmd/matchblox/gate_test.go`, `cmd/matchblox/devices_test.go`

**Interfaces:**
- Consumes: `RegisterRequest`, `RegisterReply` (Task 1), `Numbers.Names` (Task 2).
- Produces:
  - `push.Registration` (Task 1) stored as `~/.ssh/.matchblox-push-<fp-hex>` (0600, written under `withKeysLock`, as `claimNote` :117 is). `fp-hex` is the hex of the SHA-256.
  - `func Register(home string, r Registration) error`, `func Registrations(home string) ([]Registration, error)`, `func Unregister(home, fingerprint string) error`
  - `func UpgradePhoneLines(home, exe string) (changed int, err error)` (rewrites `gate phone"` lines to `gate phone <fp>"` through `rewriteKeys`)
  - Gate: the signature becomes `func gateDecision(mode, fp, orig string) (argv []string, err error)`. `gateDecision("phone", "<fp>", "matchblox push register")` → `[]string{"push", "register", "<fp>"}`; with `fp == ""` (a line not upgraded yet) it is refused with `this phone's key line is old; open matchblox serve once, then try again`; a laptop key is refused with `this key starts only the matchblox stream; use your own ssh login for a shell`.
  - `func registerMain(home string, in io.Reader, out io.Writer, fp string, names func() (map[int]string, map[int]string)) error` (one line read, at most 4096 bytes, 30 s timeout, as `pairMain` pair.go:57; reply one JSON line). A request with `Island == nil` clears this phone's island on this host.

- [ ] **Step 1: Write the failing tests:** `TestOldPhoneLinesGetTheirFingerprint`, `TestNewPhoneLinesCarryTheirFingerprint`, `TestGateRunsOnlyItsCommands` gains rows `{"phone", "<fp>", "matchblox push register", {"push","register","<fp>"}}` and `{"phone", "", "matchblox push register", refused}` and the near-misses (`matchblox push register x`, `matchblox push`) refused; `TestRegisterKeepsOneRegistrationPerPhone`, `TestRegisterRepliesTheNames`, `TestRegisterRefusesABadRequest` (too long, bad JSON, missing device token), `TestRemovingADeviceRemovesItsRegistration`, `TestDevicesListShowsPushAndIsland`.
- [ ] **Step 2:** Run `go test ./internal/setup/ ./cmd/matchblox/ -run 'Phone|Gate|Register|Devices'`. Expected: FAIL.
- [ ] **Step 3:** Implement. The upgrade runs once per service start and logs `upgraded N phone lines` to serve.log.
- [ ] **Step 4:** Run. Expected: PASS. Then `go test -race ./...`.
- [ ] **Step 5:** Commit `feat: a phone registers for the buzz and the island through the gate`.

### Task 7: The open path

**Files:**
- Create: `internal/service/focus.go`, `internal/service/focus_test.go`
- Modify: `cmd/matchblox/gate.go` (`gateDecision` maps `matchblox console --open <n>` for phone mode to `[]string{}`, as `matchblox console`; new `func openTarget(orig string) (n int, ok bool)`; `gateMain`, before `run(argv)`, sends the act `focus:<n>` to the service socket and ignores a failure), `internal/service/actions.go` (act `focus:<n>` → resolve with `Numbers` → `state.Doc.Focus`), `internal/state/state.go` (`Focus *Focus` with `{SessionID string; Pane string; Gone bool; At time.Time}`), `internal/app/model.go` (on a new `Focus.At`: select the session, open its answer peek when it waits, else its pane; `Gone` → flash `That session ended.`)
- Test: `cmd/matchblox/gate_test.go`, `internal/app/focus_test.go`

**Interfaces:**
- Consumes: `Numbers.SessionOf` (Task 2) through `Service.PushNumbers *push.Numbers` (Task 9 sets it).
- Produces: the URL the phone opens: `matchblox://open?host=<8 hex>&session=<n>`; the ssh command it runs: `matchblox console --open <n>`.

- [ ] **Step 1: Write the failing tests:** gate rows `{"phone", "<fp>", "matchblox console --open 12", {}}`, `TestOpenTargetReadsTheNumber` and refusals (`--open x`, `--open 12 13`, `--open -1`); `TestFocusOpensThePeekOfAWait`, `TestFocusOpensThePaneOfABurn`, `TestFocusOfAGoneSessionSaysSo` (flash `That session ended.`).
- [ ] **Step 2:** Run. Expected: FAIL.
- [ ] **Step 3:** Implement. The focus is a field of the doc, so every console that is attached gets it with the next broadcast; the console acts once per `At`.
- [ ] **Step 4:** Run. Expected: PASS.
- [ ] **Step 5:** Commit `feat: a tap on the island or a buzz opens that session`.

### Task 8: Keep matchblox running (the user unit door)

**Files:**
- Create: `internal/setup/unit.go`, `internal/setup/unit_test.go`
- Modify: `internal/doors/doors.go` (ID `KeepRunning`), `internal/setup/setup.go` (`Doors` :47 after the way back; `known` :86), `docs/setup.md` or the doors list in the docs

**Interfaces:**
- Produces: `func keepRunningDoor(e Env) doors.Door` (Title `Keep matchblox running`; Why `the phone buzzes with no console open`); macOS: `~/Library/LaunchAgents/com.blox.matchblox.plist` with `ProgramArguments [<exe>, "serve"]`, `RunAtLoad true`, `KeepAlive true`, `StandardErrorPath <state dir>/serve.log`, then `launchctl bootstrap gui/<uid> <plist>`; Linux: `~/.config/systemd/user/matchblox.service` (`ExecStart=<exe> serve`, `Restart=on-failure`, `WantedBy=default.target`), then `systemctl --user enable --now matchblox.service` and the advice `loginctl enable-linger` in `Term`. Done when the file holds the same content.

- [ ] **Step 1: Write the failing tests:** `TestTheKeepRunningDoorWritesAPlistOnMacOS`, `...AUnitOnLinux`, `TestTheDoorIsDoneWhenTheUnitIsThere`, `TestTheDoorShowsItsCommand` (`Term` has the exact `launchctl` / `systemctl` argv).
- [ ] **Step 2:** Run. Expected: FAIL.
- [ ] **Step 3:** Implement, with the GOOS from `Env` so both are tested on any OS. The console's own detached `serviceCmd` (main.go:618) stays: it finds the running service on the socket and starts none.
- [ ] **Step 4:** Run. Expected: PASS.
- [ ] **Step 5:** Commit `feat(setup): a door keeps matchblox running as a user unit`.

### Task 9: Wire the sender into the service

**Files:**
- Modify: `internal/config/config.go` (section `Push{ Relay string "relay" }`, default `https://matchblox.sh`; `Off bool "off"`), `config.example.toml` (the section with its defaults), `internal/service/service.go` (fields `Push *push.Sender`, `PushNumbers *push.Numbers`; `publish` :329 calls `s.Push.OnDoc(doc)` after `rec(doc)`; `state.Doc.Push string` = `s.Push.Status()`), `cmd/matchblox/main.go` (`newService` builds them), `internal/app/panels.go` (`machine` :71 shows `Doc.Push` once, in `warn`, under the alerts)
- Test: `internal/service/push_test.go`, `internal/app/panels_test.go`, `internal/config/config_test.go`

**Interfaces:**
- Consumes: Tasks 2, 3, 4, 6.

- [ ] **Step 1: Write the failing tests:** `TestPublishHandsTheDocToTheSender`, `TestPushOffSendsNothing`, `TestTheMachineTabShowsAPushFailureOnce`, `TestExampleLoads` (existing) passes with the new section.
- [ ] **Step 2:** Run. Expected: FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run `go test -race ./...`. Expected: PASS. The replay test passes with no change to its frames (the line shows only on a failure).
- [ ] **Step 5:** Commit `feat: the service sends the buzz and the island`.

### Task 10: The relay

**Files:**
- Create: `worker/push.js`, `worker/push.test.js`, `worker/attest.js`, `worker/attest.test.js`
- Modify: `worker/index.js` (route `/push/*` on matchblox.sh to `push.js` before the install script), `.github/workflows/ci.yml` (job `Worker`: `node --test worker/`, Node 22)
- Secrets (documented in RELEASING.md, set with `wrangler secret put`): `APNS_KEY` (.p8), `APNS_KEY_ID`, `APNS_TEAM_ID`, `APNS_BUNDLE_ID`, `GRANT_SECRET`.

**Interfaces:**
- Produces:
  - `POST /push/grant` body `{attestation?, assertion?, attest_grant?, key_id, client_data: {items: [{kind, value}]}}` → `{attest_grant, grants: {"<kind>:<value>": "<hex>"}}`. The first call verifies an App Attest attestation (CBOR, x5c chain to Apple's App Attestation Root CA, nonce = SHA-256(authData ‖ SHA-256(client_data)), rpIdHash = SHA-256(`<team>.<bundle>`)) and returns `attest_grant` = HMAC of the key's public key, with the public key, so later calls verify an assertion against it with no storage.
  - `POST /push/channel` (assertion as above) → `{channel}` from Apple's channel management API (`POST /1/apps/<bundle>/channels`, `message-storage-policy: 0`, `push-type: LiveActivity`).
  - `POST /push/send` body `Event` (Task 4) → `{status}`: checks `grant == HMAC(GRANT_SECRET, kind ‖ ":" ‖ target)`, signs an ES256 JWT (cached 50 minutes), sends to `api.push.apple.com` (`/3/device/<token>`, or `/4/broadcasts/apps/<bundle>` with `apns-channel-id`), and returns Apple's status. Never logs the body.
  - `function grantFor(secret, kind, value)`, `function checkGrant(secret, event)`, `function apnsRequest(env, event)` exported for tests.
- Check every Apple URL and header against Apple's current docs before coding; the test fixtures name them once.

- [ ] **Step 1: Write the failing tests** (`node --test`, a fake `fetch`): `grant for one item does not open another`, `a send without a grant is refused 403`, `a send with a good grant reaches APNs with the right path and headers`, `a broadcast send uses apns-channel-id`, `no payload is logged` (a console spy), `an attestation with a wrong rpIdHash is refused`, `an assertion verifies against the attest grant`.
- [ ] **Step 2:** Run `node --test worker/`. Expected: FAIL.
- [ ] **Step 3:** Implement with WebCrypto only (no npm dependencies). The CBOR decoder in attest.js covers maps, byte strings, text strings, arrays and unsigned ints only.
- [ ] **Step 4:** Run. Expected: PASS. `wrangler deploy --dry-run` succeeds.
- [ ] **Step 5:** Commit `feat(worker): the relay grants, makes channels and sends pushes`.

### Task 11: iOS targets, capabilities and tokens

**Files:**
- Create: `ios/MatchbloxIsland/` (Widget extension: `Info.plist` via XcodeGen, `MatchbloxIsland.swift` stub bundle), `ios/MatchbloxBuzz/` (Notification Service Extension stub), `ios/Matchblox/Tokens.swift`, `internal/app/tokens_swift_test.go`
- Modify: `ios/project.yml` (targets `MatchbloxIsland` app-extension `com.apple.widgetkit-extension`, `MatchbloxBuzz` `com.apple.usernotifications.service`; App Group `group.com.blox.matchblox`; Keychain group `$(AppIdentifierPrefix)com.blox.matchblox.shared`; push, App Attest (`com.apple.developer.devicecheck.appattest-environment`), `NSSupportsLiveActivities: true`; minimum 18.0), `ios/Matchblox/Matchblox.entitlements`, `ios/MatchbloxKit/Package.swift` (`.iOS("18.0")`), `ios/Matchblox/Theme.swift` (reads `Tokens`), `ios/README.md` (team and capabilities)

**Interfaces:**
- Produces: `enum Tokens { static let bg, fg, muted, faint, hair, accent, wash, warn, neg, flame, flameAmber, flameCore, matchHead, matchBreath1, matchBreath2, matchBreath3, matchEmber, markTop, markLeft, markRight, markTray, markStriker, markStick: Color }` (dark set); `Theme` keeps its names and reads from `Tokens`.

- [ ] **Step 1: Write the failing Go test** `TestTokensSwiftMatchesTheConsole`: parse `ios/Matchblox/Tokens.swift` for `0xRRGGBB` per name, compare with `Tokens(true)` from internal/app/theme.go.
- [ ] **Step 2:** Run. Expected: FAIL.
- [ ] **Step 3:** Write Tokens.swift and the project changes.
- [ ] **Step 4:** Run the Go test. Then `cd ios && xcodegen generate && xcodebuild -scheme Matchblox -destination 'generic/platform=iOS' -derivedDataPath build -skipPackagePluginValidation CODE_SIGNING_ALLOWED=NO build`. Expected: PASS and `BUILD SUCCEEDED`.
- [ ] **Step 5:** Commit `build(ios): the island and buzz extensions, iOS 18, shared tokens`.

### Task 12: MatchbloxKit: payloads, faces, names, the open link, the seal

**Files:**
- Create: `ios/MatchbloxKit/Sources/MatchbloxKit/IslandState.swift`, `Face.swift`, `IslandNames.swift`, `OpenLink.swift`, `Buzz.swift`; tests `IslandStateTests.swift`, `FaceTests.swift`, `IslandNamesTests.swift`, `OpenLinkTests.swift`, `BuzzTests.swift`
- Modify: `ios/MatchbloxKit/Tests/MatchbloxKitTests/Fixture.swift` (a loader for `testdata/push/*.json`)

**Interfaces:**
- Produces:
  - `public struct IslandState: Codable, Hashable, Sendable` mirroring `ContentState` (Task 1), with `Top`, `AccountUse`, `WindowUse`; `RegisterRequest`, `RegisterReply`.
  - `public enum Show: String, Codable { case sessions, usage, auto }`; `public enum Face: Equatable { case nothing, sessions, usage }`
  - `public func face(for s: IslandState, show: Show, followed: String?, breakThroughUntil: Date?, now: Date) -> Face` (`nothing` when `s.host != followed`; `usage` for Auto with `stoker`; `sessions` while a wait broke through, for 60 s)
  - `public func tightest(_ s: IslandState) -> WindowUse?`; `public func barColorLevel(_ pct: Int) -> Level` (`.normal`, `.warn` ≥ 60, `.neg` ≥ 90)
  - `public struct IslandNames: Codable { sessions: [Int: String]; accounts: [Int: String]; func session(_ n: Int) -> String /* "session 12" */; func account(_ n: Int) -> String }`, stored in the App Group as `island-names-<host8>.json`
  - `public func host8(_ fingerprint: String) -> String` (same as Go `Host8`, tested on testdata/push/host8.json)
  - `public struct OpenLink { host: String; session: Int; init?(url: URL); var url: URL; var command: String /* "matchblox console --open 12" */ }`
  - `public func openBuzz(_ sealed: Data, key: Curve25519.KeyAgreement.PrivateKey) throws -> String` (HPKE `Curve25519_SHA256_ChachaPoly`, info `matchblox buzz v1`)

- [ ] **Step 1: Write the failing tests:** `testTheStateReadsTheFixture`, `testAStateOfAHostNotFollowedShowsNothing`, `testAutoShowsUsageWhileTheStokerRuns`, `testAWaitBreaksThroughFor60s`, `testTheTightestWindowWins`, `testBarsWarnAt60AndNegAt90`, `testAnUnknownNumberIsNamedByItsNumber`, `testTheOpenLinkRoundTrips`, `testAnOpenLinkWithABadNumberIsRefused`, `testTheGoVectorOpens` (testdata/push/seal-vector.json), `testHost8MatchesGo` (testdata/push/host8.json).
- [ ] **Step 2:** Run `swift test --package-path ios/MatchbloxKit`. Expected: FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run. Expected: PASS.
- [ ] **Step 5:** Commit `feat(ios): the island's state, faces, names and the open link`.

### Task 13: The app: registration, the Island screen, the open link

**Files:**
- Create: `ios/Matchblox/IslandView.swift`, `ios/Matchblox/Registration.swift`, `ios/Matchblox/Attest.swift`, `ios/Matchblox/IslandSettings.swift`
- Modify: `ios/Matchblox/MatchbloxApp.swift` (`open(_:)` :73 handles `matchblox://open`; at launch end activities whose host is not followed), `ios/Matchblox/HostsView.swift` (the `Island` entry and a long press), `ios/Matchblox/ConsoleView.swift` (after `unlockKey()` succeeds: `Registration.run(host:key:)` once per open; `open(command:)` takes `OpenLink.command` when the route came from a link), `ios/Matchblox/Opening.swift` (`Route.console(Host, open: Int?)`), `ios/README.md` (the "Check on a device" steps of design 0010 §7)

**Interfaces:**
- Consumes: Task 12 types; the relay routes (Task 10); `push register` and `console --open` (Tasks 6, 7); `SSHSession.open(command:pty:)`, `write`, `Pairing.readReply(from:)`.
- Produces:
  - `IslandSettings` (App Group `UserDefaults`): `follow: String?` (host fingerprint), `show: Show`, `buzz: [String: Bool]`; defaults per Global Constraints.
  - `enum Registration { static func run(host: Host, key: NIOSSHPrivateKey) async throws }`: ask the relay for grants (App Attest via `DCAppAttestService`), for the followed host also a channel and the push-to-start token from `Activity<IslandAttributes>.pushToStartTokenUpdates`; send `RegisterRequest` over `matchblox push register`; store the reply's names as `IslandNames`.
  - The Island screen with the exact labels; a change of Follow or Buzz runs `Registration.run` for the hosts concerned with Face ID; an unreachable host shows `work-1 is not reachable: it changes at the next connect`.

- [ ] **Step 1:** Unit-test what can run without a device in MatchbloxKit (Task 12 holds the logic). Here, write the README device checks first: buzz with the app closed; push-to-start; both faces; Show changes the face at once; a second host buzzes and does not take the island; a tap opens the session; stray activities end at launch.
- [ ] **Step 2:** Implement.
- [ ] **Step 3:** Build: `xcodebuild … build` as in Task 11. Expected: `BUILD SUCCEEDED`. Install on the owner's iPad (see the build notes in ios/README) and run the README checks against a private test host. Write down each result.
- [ ] **Step 4:** Commit `feat(ios): the phone registers, follows one host and opens a tapped session`.

### Task 14: The Live Activity and the buzz extension

**Files:**
- Create: `ios/MatchbloxIsland/IslandAttributes.swift` (shared with the app target), `ios/MatchbloxIsland/SessionsFace.swift`, `ios/MatchbloxIsland/UsageFace.swift`, `ios/MatchbloxIsland/IslandWidget.swift`, `ios/MatchbloxBuzz/NotificationService.swift`

**Interfaces:**
- Consumes: Task 0 (approved Usage mockup), Task 12 (`face`, `tightest`, `barColorLevel`, `IslandNames`, `openBuzz`), Tokens (Task 11).
- Produces: `struct IslandAttributes: ActivityAttributes { typealias ContentState = IslandState; let host: String }`; the widget renders lock screen, compact, minimal and expanded for both faces as 0006 §6.6 and Task 0 draw them; `widgetURL` and `Answer`/`Open` use `OpenLink.url`. Only fire and smoke move (DESIGN.md §1 rule 1). The service extension opens `Buzz.sealed` with the key from the shared Keychain group, sets the body, and sets `userInfo["open"]` to the open link.

- [ ] **Step 1:** SwiftUI previews of each size and face from the fixture (`IslandState` of testdata/push/content-state.json), checked against the mockups.
- [ ] **Step 2:** Implement.
- [ ] **Step 3:** Build (Task 11's command). Expected: `BUILD SUCCEEDED`. On the iPad: start an activity from the app with the fixture, then let the test host update it through the relay (a staging Worker), and check each README line of Task 13.
- [ ] **Step 4:** Commit `feat(ios): the island's two faces and the sealed buzz`.

### Task 15: DESIGN.md, docs and the changelog

**Files:**
- Modify: `DESIGN.md` (new `## 9. The island`: both faces, the sizes, the wait that breaks through, the tokens; the match glyphs stay §4), `docs/phone.md` (the buzz, the island, the setting, and the exact list of what leaves the host in clear from design 0010 §3.7), `docs/setup.md` (the door), `CHANGELOG.md` (`### Added`: **The phone buzzes when an agent waits, with the app closed.** **The island shows the session that matters most, or your usage.** **A tap opens that session.**), `config.example.toml` (done in Task 9), `RELEASING.md` (the Worker secrets)

- [ ] **Step 1:** Run `go test ./cmd/matchblox/ ./internal/app/ -run Docs` (docs name only real commands and keys). Expected: PASS.
- [ ] **Step 2:** Write the docs.
- [ ] **Step 3:** Run the docs tests again and `.github/scripts/changelog-guard.sh`. Expected: PASS.
- [ ] **Step 4:** Commit `docs: the island, the buzz and what leaves the host`.
