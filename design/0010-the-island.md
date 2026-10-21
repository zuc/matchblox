# 0010 — The island: the match and the usage, with the app closed

Status: approved by the owner (2026-10-21). Language: ASD-STE100.
Code names stay as they are. This design builds #89 (the buzz) and #90 (the
match on the lock screen) from design/0006 §6.4 and §6.6, adds a usage face
and a setting, and builds the open path of 0006 §6.3.5 so that a tap opens
the session. Where this file and 0006 differ, this file applies. Where this
file and DESIGN.md differ, DESIGN.md applies.

## 1. Who, and the moment

The builder has two hosts paired: a personal host and a work host. The
stoker drives the sessions of the personal host tonight. The builder does not
want to see each session. The builder wants to know one thing: will the
limits hold? A glance at the Dynamic Island says `5h 61%`. Later an agent on
the work host waits. The phone buzzes `work-1 · api waits: "Run the
migration?"`, and the island stays on the personal host. One tap on the buzz
and one Face ID open the `api` session on the work host.

Another day the builder drives the sessions by hand. The island shows the
match of the session that matters most, as 0006 §6.6 says.

The app is closed in both cases.

## 2. Goals, non-goals, anti-goals

Goals:

1. The phone buzzes when an agent waits, with the app closed (#89, 0006
   §6.4).
2. A Live Activity shows one host on the lock screen and in the Dynamic
   Island, with the app closed, and stays current (#90, 0006 §6.6).
3. The activity has two faces, **Sessions** and **Usage**. A setting in the
   app chooses `Sessions`, `Usage` or `Auto`. Auto shows Usage while the
   stoker of that host runs, else Sessions. The default is `Sessions`.
4. With two or more hosts, the island follows **one** host, which the
   builder chooses. Every host can still buzz.
5. A tap on the activity, on its `Answer`, or on a buzz opens **that
   session** on its host (owner, 2026-10-21).
6. All of it ships together (owner, 2026-10-21): the relay, the host side,
   the app side, both faces and the open path.

Non-goals:

- The one-line reason of each wait made on the phone by Apple's model
  (#127). It comes after this design.
- Importance levels (#40). The order of 0006 §6.6.1 stays.
- Android, the browser window and a watch.
- Usage history by hour and weekday (#41).

Anti-goals (a failure even if it works):

- **Security goes down.** No key opens a host without Face ID or the
  passcode (owner, 2026-10-21). Nothing connects to a host from the
  background.
- A server of ours sees pane text, a prompt, an answer, a session name or an
  account name (0006 anti-goal 2).
- The island shows two hosts at the same time. The island is calm
  (DESIGN.md §1 rule 4).
- The Usage face hides an agent that waits for the builder.
- The setting is tied to the stoker: the builder must be able to see Usage
  with the stoker off, and Sessions with it on.
- A new glyph, colour or motion that DESIGN.md does not have. A new one is a
  change to DESIGN.md first.

## 3. The parts

```
 host (each paired host)                 relay                    phone
 ┌──────────────────────────┐   HTTPS   ┌──────────────────┐ APNs ┌──────────────────────┐
 │ matchblox serve          │──────────▶│ matchblox.sh     │─────▶│ Notification Service │
 │  (user unit: launchd,    │  events   │ Worker route     │      │ Extension (buzz)     │
 │   systemd --user)        │           │ /push/*          │      │ Widget extension     │
 │  push.Sender             │           │ no storage       │      │ (Live Activity)      │
 └──────────────────────────┘           └──────────────────┘      │ the app (settings)   │
          ▲  registration over ssh, in the foreground, with Face ID └──────────────────────┘
          └──────────────────────────────────────────────────────────────────┘
```

### 3.1 Broadcast channels (iOS 18)

The host never needs a token that the phone makes later. So nothing
connects to a host from the background, and no new key is needed:

- The island of each host uses one **APNs broadcast channel** (iOS 18).
  The phone asks the relay to make the channel when the builder chooses the
  host to follow, in the foreground (§3.2). The relay makes it with Apple's
  channel management API and returns its id. The relay keeps nothing.
- Every activity of that host subscribes to that channel id. The host
  pushes each update to the channel. An activity that starts later (push to
  start, the next 8 h) subscribes to the same id, so the host needs no new
  token.
- The host starts activities with the **push-to-start token** that the
  phone registered in the foreground. iOS rotates this token seldom. When it
  rotates, the app sends the new one at its next open. Until then the
  running activity keeps updating through the channel; only a new start can
  fail, and the host shows that once on the Machine tab: `island: open
  matchblox on the phone to renew`.
- The app's minimum iOS goes from 17.2 to **18.0**.

### 3.2 Registration

The phone registers with each paired host over ssh with its Secure Enclave
key, so with Face ID or the passcode, and only while the app is open:

- A new phone-only gate command `matchblox push register`, beside
  `matchblox console` (cmd/matchblox/gate.go). It reads one JSON request on
  stdin and writes one reply. It does nothing else.
- The request: the device token, the grants (§3.3), the notification public
  key (§3.5), the buzz switch of this host. Only for the host that the island
  follows: the push-to-start token and the channel id.
- The reply: the **names** of the sessions and the accounts, by number
  (§3.6). The app keeps them in the app group, where the widget and the
  notification extension read them.
- The app registers at each open of the app and at each change of the
  setting (§4.3). Registration is not part of pairing, so design 0009 does
  not change.
- The host keeps one registration per phone key, next to its
  `authorized_keys` line (internal/setup), and removes it with `matchblox
  devices rm`. A follow change tells the old host to drop the channel and
  the push-to-start token, at the same moment, in the foreground.

### 3.3 The relay

A route on the existing Worker (`worker/index.js`, `wrangler.jsonc`), as
0006 §6.4.2 says. It stores nothing.

- `POST /push/grant`: the app sends an App Attest attestation (the first
  time) or an assertion (later), and the tokens and the channel id that it
  wants grants for. The relay checks it with Apple and answers one grant per
  item: `HMAC-SHA256(relay secret, kind ‖ item)`. A grant opens only its own
  token or channel.
- `POST /push/channel`: with a valid App Attest assertion, the relay makes
  one broadcast channel and answers its id.
- `POST /push/send`: the host sends the target (device token, push-to-start
  token, or channel id), its grant, the push type and the payload. The relay
  checks the grant, signs a JWT with the matchblox APNs key, sends the push
  and answers Apple's status.
- Secrets in the Worker: the APNs key (`.p8`), its key id, the team id, the
  HMAC secret. Never in the repo.
- Cloudflare rate rules limit each target. The relay logs no payload.

### 3.4 The host

- **The service runs at all times.** A door `Keep matchblox running` in
  `matchblox setup` and the console installs `matchblox serve` as a user
  unit: a launchd agent on macOS (`~/Library/LaunchAgents`), a systemd user
  unit on Linux (start at boot with lingering). It is the one service; the
  console keeps connecting to it.
- **`push.Sender`** (new package `internal/push`) watches the service's
  state and sends:
  - a **buzz** to each registered phone with the buzz on, when the set of
    sessions that wait changes: at most one event each 30 s for each host
    (0006 §6.4.1);
  - **activity updates** to the channel of this host, when a phone follows
    it (§3.6).
- The host sends to the relay with a timeout and drops an event that fails;
  it never queues. A failure shows once on the Machine tab (`push: <why>`)
  with the fix.

### 3.5 The buzz

As 0006 §6.4.3: the alert body is encrypted on the host to the phone's
notification key, and a Notification Service Extension opens it:
`work-1 · api waits: "Run the migration?"`. The notification key is a
Curve25519 key that the app makes at its first registration and keeps in the
Keychain group of the app and the extension. The alert carries the host
fingerprint and the session number in clear, so a tap can open the session
(§4.4).

### 3.6 The content state of the activity

One activity, for one host, as 0006 §6.6 says, with both faces in every
update:

- `host`: the first 8 characters of the host key fingerprint. The widget
  shows nothing for a host it does not follow, so an old host that still
  pushes never makes a second island.
- Sessions: the counts (burn, wait), and the session that matters most: its
  number, state, progress in 5 % steps, and `out` time (0006 §6.6.1–4).
- Usage: for each account, up to 4: the account number and its windows:
  short name (`5h`, `wk`, `mo`), whole percent, reset time; and **its own
  forecast** (`lasts`, or the hour it runs out), as `limits.Account`
  forecasts each account.
- `stoker`: on or off, for Auto.
- The host label (`ws-1`) comes from the phone's own list.

Session numbers: the host gives each session a number when it first sees
it and keeps it for the life of the session; a number is never used again
on that host. Accounts get numbers the same way. A number that the phone
does not know yet shows as `session 12` or `account 3` until the next
registration.

When updates go out: a new wait, done or out at once, high priority.
Progress in 5 % steps, at most each 3 minutes. Usage when a window's whole
percent changes by 2 or more, or a window resets, at most each 5 minutes,
low priority. The stoker on or off at once. The content state stays under
2 KB (Apple's limit is 4 KB).

### 3.7 What leaves the host in clear

A Live Activity update cannot be encrypted (Apple shows it with no
extension), so 0006 §6.4.4 applies to both faces: numbers, states,
progress, counts, times, percents, window short names, and 8 characters of
the host fingerprint. No session name, account name, provider, path or
text. `docs/phone.md` lists exactly this.

## 4. The app

### 4.1 Faces

Both faces use DESIGN.md tokens through `Tokens.swift` (0006 §6.6.9) and
the glyphs of DESIGN.md. Mockups come before code (§8).

**Sessions** (0006 §6.6, unchanged): the horizontal match; top line
`▰ ws-1  ✦ 5 ╿ 2`; bottom line the session name and its state word. Island
compact: a small match and the state word; minimal: the front of the match;
expanded: the match, the percent and `Answer` (or `Open`).

**Usage**:

- Lock screen: top line `▰ ws-1` and, on the right, the stoker word while
  it runs (`stoker`, in its token). Then one row for each account: its name,
  and each window as in the console's dock: `5h`, the bar `▰▰▰▱▱▱▱▱`, the
  percent, `· 14:10`. The bar is `warn` from 60 % and `neg` from 90 %
  (DESIGN.md §6, Dock). Then the account's forecast: `lasts the week`
  (`muted`) or `out Fri ~20:00` (`warn`). No match on this face.
- Island compact: the tightest window of all accounts: its short name and
  percent (`5h 61%`), coloured as its bar. Minimal: a short bar of that
  window. Expanded: the rows of the first two accounts, and `Open`.
- **A wait breaks through.** On the Usage face, a new wait shows the
  Sessions face for 60 s (the match breathes, `╿ waits`), then Usage comes
  back. While any session waits, the compact island shows `╿` before the
  percent, in the accent. An agent that waits is never hidden.

### 4.2 Auto

Auto shows Usage while `stoker` is on in the content state, else Sessions.

### 4.3 The setting

A screen **Island** in the app (from the hosts list, and from a long press
on a host):

- **Follow**: one paired host, or `None`. Default: the host opened last.
- **Show**: `Sessions` · `Usage` · `Auto (Usage while the stoker runs)`.
  Default: `Sessions`.
- **Buzz**: on or off for each host. Default: on.

Show lives on the phone only, in the app group that the widget reads, so a
change shows at once and no host is asked (each update has both faces).
Follow and Buzz change what a host sends, so a change registers again with
the hosts concerned (§3.2), at once, with Face ID. A host that cannot be
reached keeps its old registration; the screen says `work-1 is not
reachable: it changes at the next connect`, and the app ends any activity
of a host that it does not follow whenever it runs.

### 4.4 A tap opens the session

- The activity, its `Answer`, and a buzz carry a link
  `matchblox://open?host=<fingerprint 8>&session=<number>`.
- The app opens it after Face ID: it connects to that host and starts the
  console with the target, through a new phone-only gate command
  `matchblox console --open <number>` (the open path of 0006 §6.3.5).
- The console opens on that session: on its answer peek when it waits,
  else on its pane. A number that is gone opens the console on Sessions and
  says so in the flash: `That session ended.`

## 5. Pairing and devices

- Registration (§3.2) needs no change to pairing (design 0009).
- `matchblox devices` shows `push` next to a phone that registered, and
  `island` next to the phone that follows this host.
- `matchblox devices rm` removes the registration with the key.

## 6. Apple and accounts

- The owner has a paid Apple Developer account (2026-10-21). The bundle
  id, the App Group, the Keychain group, the push and the App Attest
  capabilities go on that team.
- An APNs key (`.p8`) for the relay, in the Worker's secrets.
- Targets: the app, a Widget extension (the Live Activity), a Notification
  Service Extension, in `ios/project.yml`.
- Minimum iOS 18.0 (§3.1).

## 7. Tests

- One JSON fixture for each payload (buzz, content state, the register
  request and reply) in `testdata/push/`, read by a Go test and a Swift
  test, as `testdata/pair` and `testdata/home` are now.
- Go: the sender's rules (30 s per host, 5 % steps, the usage thresholds, a
  wait at once), with a fake clock and a fake relay; session numbers that
  stay and never come back; a test that reads every string of the content
  state fixture and finds no name; the gate allows `push register` and
  `console --open <n>` only for a phone key.
- Worker: the grant check (good, bad, missing, a grant for another item),
  the rate rule, that no payload is logged.
- Swift: the face choice (Sessions, Usage, Auto with the stoker on and off,
  the wait that breaks through, a host that is not followed), the number to
  name map with an unknown number, the open link.
- On the device (ios/README "Check on a device"): the buzz with the app
  closed; push-to-start; the faces; Show changes the face at once; a second
  host buzzes and does not take the island; a tap opens the session.

## 8. Order of work, in one project

1. Mockups of the Usage face (lock screen, compact, minimal, expanded) next
   to the approved Sessions mockup (`design/0006-live-match.html`), for the
   owner's approval.
2. The fixtures (§7). Go and Swift read them.
3. The relay routes, the user unit door, `push register`, `push.Sender`,
   session numbers, `console --open`.
4. The extensions, the faces, the setting, the open link.
5. DESIGN.md: a section for the island (both faces), in the same pull
   request. CHANGELOG and `docs/phone.md`.

## 9. Calls made in this design (check these)

1. Broadcast channels (iOS 18) keep the island live with no new key and no
   background connection; the minimum iOS goes to 18.0.
2. Registration is a gate command after pairing, so design 0009 does not
   change.
3. Each update carries both faces, so Show stays on the phone; Follow and
   Buzz register again with the hosts concerned.
4. The `host` tag in the content state stops a second island from an old
   host; the app also ends stray activities when it runs.
5. A grant opens one token or channel only.
6. Session and account numbers stay for their life and are never used
   again on that host.
7. One forecast per account.
8. The Usage face shows up to 4 accounts; the expanded island shows 2.
9. A wait breaks through the Usage face for 60 s; the compact island keeps
   `╿` while any session waits.
10. Usage updates at a change of 2 whole percent or a reset, at most each 5
    minutes.
11. The service runs as a user unit through a door (0006 §6.4.7).
12. The default face is `Sessions`; the default followed host is the one
    opened last.
13. A failed push is dropped, not queued, and shows once on the Machine tab.

## 10. Left to the builder

- The encryption scheme of the buzz (§3.5), if the relay cannot open it.
- The names of the targets, the App Group and the Keychain group.
- The exact JSON field names of the fixtures.
- How the Worker checks App Attest (Apple's server API or a library).
