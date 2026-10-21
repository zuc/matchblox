# The box opens: pair a device in one gesture

Status: draft (2026-10-21), for the owner's approval. Language: ASD-STE100.
Code names stay as they are. Where this file and DESIGN.md differ, DESIGN.md
applies until the DESIGN.md changes in §8 land in the same pull request.

## 1. Who, and the moment

The builder runs agents on a laptop or a server and wants to answer them from
an iPhone or an iPad. Today pairing works, but it feels like a form:

- `matchblox pair` prints a 77 × 39 QR code, a 335-character link and one
  line. Nothing moves. Nothing tells the builder that the phone reached the
  host until the code is gone.
- The app scans, then shows a form (`Pair with X`, `Address`, `iPhone name`,
  `Pair`). The builder taps `Pair`, sees `Pairing…`, and the console opens.
- The console, where the builder looks all day, does not tell the builder
  that a phone can be paired at all.

The moment to design: **the builder holds the phone up to the screen, and the
box opens on both screens at the same time.** One gesture, two screens, one
strike.

## 2. Goals, non-goals, anti-goals

Goals:

1. Pairing lives in the console: a door finds the builder on the first run,
   and a `DEVICES` list on the Machine tab keeps the devices after that.
2. Scan is the consent. There is no form on the phone.
3. The host and the phone show the same box, the same parts and the same
   motion (DESIGN.md §3), each from its own side.
4. The QR code is about half its area today, so the box fits a normal
   terminal.
5. A device that cannot scan (the simulator, an iPad that shows the host's
   terminal itself) can paste the link.
6. Every state that stops has its fix on the screen (DESIGN.md §1 rule 3).

Non-goals:

- The pair failures of #102 and #120 (Tailscale SSH, an other
  `AuthorizedKeysFile`). They get their own pull request. This design gives
  them a place: a failed check shows the shut box and one line with the fix,
  as an expired code does.
- Renaming a device. The device name comes from the phone (§6.4); a rename is
  a later issue.
- Pairing from a console of another host (remote mode). §4.6 says what that
  console shows.
- The site. The site's theme-switch box does not change.

Anti-goals (a failure even if it works):

- A spinner, a progress bar or a burning fuse. DESIGN.md §5 has no spinners.
- A match that stands for the device or carries its name. The match in the
  mark is the logo (§3), never an agent match (§4 rule 2).
- A new glyph or colour. The motions this design adds are listed in §8, each
  with one meaning.
- A QR code that does not scan: skewed, too small, or with too little
  contrast in a light terminal.

## 3. The compact pair link (v2)

The QR code is large because the link is large: JSON in base64 with text
fields. The v2 link packs the same facts in binary.

- Form: `matchblox://pair?p=<base64url, no padding>`, the same prefix as v1.
- Bytes, in order:
  - `0x02` (the version)
  - the port, 2 bytes, big-endian
  - the SHA-256 of the host key, 32 bytes. The app shows and compares it as
    today's `SHA256:<base64, no padding>` string.
  - the one-time seed, 32 bytes
  - then UTF-8 text fields separated by `0x00`: host, user, name
- The name stays. It labels the host on the phone before the reply comes,
  and in error messages.
- QR error correction: L, not M. A screen is clean and flat.
- Measured with `rsc.io/qr` for a 40-character Tailscale name: **210
  characters, 57 × 29 cells** (v1 today: 335 characters, 77 × 39). Without
  the name it would be 53 × 27, and that small gain is not worth losing the
  name.
- `matchblox pair` and the console make v2 only.
- The app reads v1 and v2, so a new app pairs with an older host.
- An older app cannot read v2. It decodes JSON first, so it says "This code
  is damaged…", not the version message. The app is sideloaded and has one
  builder today (`docs/phone.md`), so this is accepted. There is no v1 flag.
  The CHANGELOG entry and `docs/phone.md` say to build the app again with
  this release.
- `internal/pair` gets `Payload.URL` (v2) and `Parse` (v1 and v2), each with
  tests. `MatchbloxKit/PairPayload` does the same, with the same test vectors.

## 4. The host

### 4.1 Where pairing lives

- **The door "Pair a device"** under `SET UP`, with the why in `muted`:
  `answer your agents from your phone`. `Enter` opens the pair page (§4.2).
  The door does not show a diff or an argv: the page is its preview.
  - Done: one or more devices of kind `phone` in `authorized_keys`, so the
    door folds.
  - `x` and a typed `y` close it, as for every door.
  - `matchblox setup` shows it with the other doors.
- **`DEVICES · N`** on the Machine tab, after the detached busy loops and
  before the top processes:
  - One row for each key that opens this host, phones first (today's
    `matchblox devices` order): kind, name, fingerprint.
  - The last row is `+ pair a device`, and `Enter` on it opens the pair page.
  - `x` on a device and a typed `y` remove it (`setup.RemoveDevice`, with its
    backup), as `matchblox devices rm` does. The flash names the backup.
  - With no devices, only the `+` row shows.

### 4.2 The pair page

The pair page is a layer over the console, as the peek is. It takes the
whole screen while it shows.

```
            ┌──── the sleeve, front-on ─────────┐
            │▚▞▚▞▚▞▚▞ striker ▚▞▚▞▚▞▚▞▚▞▚▞▚▞▚▞│
            │   ┌──────────────────────────┐   │╶─ drawer (shut)
            │   │                          │   │
            │   │   QR, 57 × 29 cells,     │   │
            │   │   paper label            │   │
            │   │                          │   │
            │   └──────────────────────────┘   │
            └───────────────────────────────────┘
        Scan with the matchblox app, or paste the link.   4:12
        c copies the link · esc stops
```

- **The box** is the site's theme-switch box (`www/index.html` `#match`)
  seen front-on: the sleeve, the striker along its top, the drawer that
  slides out to the right, and the match. It is taller than the site's box,
  because it holds a square label. The parts, the poses and the timings are
  the site's.
- **The label** is the QR code. It is paper on the sleeve, with explicit
  colours from the terminal's own token set, so every cell is a token (the
  replay rule) and the code scans in both themes:
  - In a dark terminal, light modules are `fg` `#EBE6DC` and dark modules
    are `bg` `#0F0D0A`.
  - In a light terminal, light modules are `bg` `#F5F1E7` and dark modules
    are `fg` `#1C1813`.
  - The label draws its own 2-module quiet zone, so the sleeve's colour
    never touches the code.
- **The faces** of the sleeve and the drawer use `mark-*` and the striker
  uses `mark-striker`, in the terminal's theme.
- **Under the box**, one line in `muted`: `Scan with the matchblox app, or
  paste the link.`, then the countdown `4:12` in `muted`. The countdown is
  plain text. It never turns into a colour or a bar. In the last 30 s it
  stays `muted`: running out is not an exception, because the fix
  (`matchblox pair` again, or `Enter` on the door) is always one key away.
- **The key line**: `c copies the link · esc stops`.
  - `c` copies the v2 link with OSC 52, so the copy works over SSH. The flash
    says `link copied`.
  - The link itself does not show on the page. It is the same secret as the
    QR code and only makes the page longer.
- **Fits** in 100 columns × 40 rows: the box, the drawer when open, the match
  standing beside it, and the two lines.
  - In a smaller terminal, the QR code shows without the box, with the same
    lines.
  - Under 61 × 33, the QR code does not fit. The page says `Make this
    terminal 61 × 33 or larger to show the code, or press c to copy the
    link.`
  - A resize draws again at once.

### 4.3 What happens on the page

| When | The host shows |
|---|---|
| The page opens | The box is shut, the label is the QR code, the countdown runs. |
| The phone connects (`gate pair` starts) | **The knock** (§8): the box moves one cell left and back, twice, in 240 ms. The countdown line says `Martin's iPad is here…` (the name comes later; until the request is read, the line says `A device is here…`). |
| The device claims the code | **The opening** (§8): the label turns to plain paper with the device's name in `bg` ink. The drawer opens, the match comes out, strikes along the striker and stands beside the box. In a dark terminal it burns; in a light one it stays unlit (DESIGN.md §3). |
| The opening ends | The door folds and the device shows in `DEVICES`. The line says `Paired Martin's iPad (SHA256:…). Not yours? x on it in DEVICES.` In the console, the page waits for beats 3 and 4 (§4.4). Standalone `matchblox pair` waits 1.2 s, leaves the alternate screen and prints today's `Paired …` line. |
| The code expires | The label goes blank paper and the box stays shut. The line says `The code expired. Enter pairs again.` (console) or `The code expired. Run matchblox pair again.` (standalone). |
| `esc` or Ctrl-C | The page closes and the code is removed at once. Standalone prints `Stopped. The code no longer works.` |
| The phone took the code at the last moment | As today: the claim wins and the opening plays. |

- **The knock** needs a sign that the phone reached `gate pair`. `pairMain`
  writes the note `~/.ssh/.matchblox-pair-<id>.seen` when it starts, before it
  reads the request. After the request is read and is valid, it adds the
  device name to the note. The page reads the note each poll.
- **Speed.** The page polls `authorized_keys` and the notes each 100 ms
  while it shows (today: 1 s). Both screens then strike within about 0.2 s of
  each other: the host strikes when the key line is gone, the phone strikes
  when the `ok` reply comes.

### 4.4 After the strike: the device proves itself

The pair page does not close at the strike. It has two more beats, so the
builder sees the new device work on the host's own screen (owner,
2026-10-21):

| Beat | When | The host shows |
|---|---|---|
| 3. In | The new device's own key starts the console (`gate phone`), not the one-time key | The label keeps the device's name. The line says `iPad is in. Answer an agent from it, or esc to go back.` |
| 4. First act | That console sends its first act to the service: an answer, a key, a compact | The line says once `iPad answered api` (the act and the session), then the page folds back to the console. |

- If beat 3 does not come within 60 s, the page folds anyway, and the flash
  says the device is paired. A device that pairs but opens later is not an
  error.
- `esc` folds the page at any beat. The device stays paired.
- A device does not get a console of its own. `gate phone` attaches it to
  the tmux session `matchblox`, so the builder and every device see one
  console process (`internal/panes/home.go`, `new-session -A`). So:
  - New phone lines carry the device's id in the forced command: `gate
    phone <id>`, with the same 12-hex id as `gate pair <id>`. `gateMain`
    and `parseDevice` accept both forms, so old lines keep working, without
    beats 3 and 4.
  - `gate phone <id>` writes the note `~/.ssh/.matchblox-device-<id>`
    (the time and the ssh tty) before it attaches. Beat 3 is a note for the
    new device's id with a time after the claim.
  - Beat 4 is the first act the console sends while the client that tmux
    saw last (`tmux list-clients -t =matchblox -F '#{client_activity}
    #{client_tty}'`) has the new device's tty.
- Standalone `matchblox pair` stops at beat 2, as before: it has no service
  to watch.

### 4.5 What a device's console may do

A device sees `DEVICES`, but it cannot change them (owner, 2026-10-21):

- `Enter` on the door or on `+ pair a device` from a device says `Pair from
  the host's own terminal.`
- `x` on a device from a device says `Remove devices from the host's own
  terminal.`
- The console is shared (§4.4), so it cannot know from its environment who
  pressed a key. Before it opens the pair page or removes a device, it asks
  tmux for the client that was active last (as in §4.4). If that client's
  tty is in a device note, it refuses with the line above.
- The door and the `+` row show for everyone, because the screen is shared.
  The refusal comes on `Enter`: `Pair from the host's own terminal.`
- This is a guard against a mistake, not a security wall. A device can
  already tell an agent to run any command, so a device holder who wants to
  add a key can do it that way. That is true today as well (owner,
  2026-10-21).

### 4.6 A console of another host

The pair key must be written on the host that the phone will reach. In a
console of another host (remote mode):

- The door does not show. `DEVICES` shows one row, `run matchblox pair on
  <host>`, and nothing else.
- The console does not read the laptop's own `authorized_keys` under a host's
  Machine tab.
- There is no `proto` change in this pull request.

### 4.7 `matchblox pair`, the fallback

- **With a terminal:** the same pair page, as a program of its own: the same
  renderer, the same keys, the alternate screen.
- **Without a terminal** (a pipe, a script, a test): today's plain output with
  the v2 QR code and the v2 link, and today's lines.
- The flags stay: `--address`, `--port` and `--name`.
- With `--no-motion` or `NO_MOTION=1`, every motion shows its last frame only.

## 5. The motion (host and app share it)

The site's timings, from `www/app.js`:

| Step | Time | What moves |
|---|---|---|
| Open | 260 ms | the drawer slides out |
| Out | 280 ms | the match comes out of the drawer to the start of the striker |
| Strike | 150 ms, ease-in | the match head runs along the striker; at the end, a flash in `flame-core` |
| Stand | 320 ms | the match stands up beside the box and the flame grows; the drawer shuts at the same time (260 ms) |

The total is about 1 s. The motion runs on the clock, not on a frame count
(DESIGN.md §4 rule 4), so the replay and the tests can draw any moment.
`--no-motion`, `NO_MOTION=1` and iOS Reduce Motion show the last frame of
each step at once. Haptics still play with Reduce Motion.

## 6. The app

### 6.1 The scan screen

- **The camera fills the screen.**
- **The box outline** (the sleeve, front-on, with the striker strip on top)
  is the viewfinder in the middle. It is drawn in `hair` and `muted`, not
  filled.
- At the top: `Pair a host`, and `Cancel` when there is a host to go back to.
- At the bottom: one line in `muted`, `Run matchblox pair on your host, or
  open Pair a device in its console.`, and a **Paste the link** button
  (§6.3).
- **No camera** (unsupported, or denied): the box is drawn solid and shut,
  with today's text and fix (`Open Settings`) and Paste the link.

### 6.2 The box catches the code

1. **Lock.** DataScanner reports the code with its corner points. The outline
   moves to the code's corners in 180 ms, with a light haptic (`.impact
   light`).
2. **Close.** The outline goes back to the sleeve shape and fills: the
   camera dims behind it, and the label turns to paper with the host's name
   (from the link). While the app connects, the name pulses (DESIGN.md §5
   Pulse, 1.6 s: a value that is not measured yet). There is no spinner.
3. **Pair.** `PairFlow.run` as today, with the device name from §6.4 and the
   address from the link.
4. **Strike.** On `ok`: the opening of §5. A rigid haptic plays at the
   strike, and a success haptic plays when the match stands.
5. **The console** opens from the box: it slides in from the drawer side
   (from the right), 300 ms.

### 6.3 Paste the link

- SwiftUI `PasteButton`, so iOS does not ask to allow the paste.
- It takes the first `matchblox://pair` URL of the pasted text and goes to
  step 2 of §6.2 (there is no lock to show).
- Text that is not a pair link: one line in `neg` under the button, `That is
  not a matchblox pair link. Copy it again with c on the pair page.`
- A deep link (the Camera app opens `matchblox://pair?…`) goes to step 2 the
  same way.

### 6.4 No form

- `PairView`'s form goes away.
- The device name is `UIDevice.current.name`, cleaned by
  `Pairing.cleanName`. On iOS 16 and later this is the model, `iPhone` or
  `iPad`, unless Apple grants the user-assigned device name entitlement.
  Asking Apple for it is out of scope.
- So two devices can both be `iPad`. `gate pair` makes the name unique on
  the host when it claims the code: `iPad`, then `iPad 2`, `iPad 3`. So
  `matchblox devices rm iPad 2` works, and the `DEVICES` rows differ by name.
- The examples in this file say `Martin's iPad`. With today's iOS, the host
  shows `iPad`.
- The address is the one in the link.

### 6.5 When it fails

- The box stays shut and the label shows the host's name in `faint`.
- Under the box, today's message from `Pairing.message` in `neg`.
- When the failure is a connection failure (the host did not answer, or the
  name does not resolve), an `Address` field shows, filled with the link's
  address, with `Try again`. The one-time code is not used by a failed
  connection, so a retry works until the code expires.
- `Scan again` always shows.
- An error haptic (`.notification error`) plays once.

## 7. Parity

| | Host | App |
|---|---|---|
| The box | front-on sleeve, striker on top, drawer to the right | the same, in SwiftUI `Path`s with the site's geometry, made taller as on the host |
| The label | the QR code, then the device's name | the camera, then the host's name |
| Before the claim | the knock when the phone connects | the lock, then the pulse while it connects |
| The claim | the opening (§5) | the opening (§5) and haptics |
| After | "iPad is in", then its first act; then the page folds | the console opens from the drawer; its first answer shows on the host |

Each screen shows the other side's name on its label: the host shows the
device, the device shows the host.

## 8. DESIGN.md changes (in the same pull request)

- **§3 The mark:** add the front-on box, as the pair page and the app's pair
  screens use it. It has the site's parts and timings, and it is taller so it
  holds a square label. The label is paper in the dark `fg` and `bg`, in
  both terminal themes.
- **§5 Motion:** add three rows:
  - Knock, 240 ms: a device reached the pair page's code.
  - Open, 1 s (§5 of this file): a device claimed the code.
  - Lock, 180 ms: the app found a pair code.
- **§5, a haptics note (new):** lock is a light impact, the strike is a rigid
  impact, a stand is success, and a failure is error. Haptics play with
  Reduce Motion.
- **§6:**
  - The door "Pair a device".
  - `DEVICES · N` on the Machine tab, with `+ pair a device` and remove.
  - The pair page as a layer.
- **§7 Copy:** the new lines of §4 and §6, in the same words.

## 9. Not in this

- #102 and #120 (§2).
- Device rename, and a "last used" column in `DEVICES` (matchblox does not
  record it today).
- Pairing a device to another host from a remote console (§4.6).
- QR on the isometric lid: a skewed code of this size does not scan.

## 10. Done when

- `go test ./...` and the iOS tests pass, with new tests for:
  - v2 encode and parse, v1 still parses, and the same vectors in Go and
    Swift
  - the `.seen` note from `gate pair`
  - the pair page's frames at fixed moments: every colour is a token (the
    replay rule)
  - the door's done check
  - `DEVICES` remove, with its typed `y`
  - `matchblox pair` without a terminal prints the plain output
  - a key from a device's tmux client (the last active client has a device
    tty) gets the refusals of §4.5 for the pair page and for `x`
  - beats 3 and 4: a console start and a first act from the new device's
    key move the page, another key's don't, and an old phone line still
    opens the console
- On the iPad (`Martin's iPad`) with this Mac:
  - The door opens the page. A scan pairs with no form, and both screens
    strike within about 0.2 s of each other.
  - Paste the link pairs the same way.
  - A wrong address shows the fix, and `Try again` pairs.
  - An expired code and `esc` each end as §4.3 says.
  - Reduce Motion and `NO_MOTION=1` show the last frames.
  - A light terminal scans.
- The page fits 100 × 40 and falls back as §4.2 says.
- CHANGELOG Unreleased, `docs/phone.md` and DESIGN.md are updated.
- For the site's "Continue on your phone" section (matchbloxapp session):
  - Replay stills of the pair page, made by the same renderer as the
    `<pre class="replay">` spans of `www/index.html`. Three states: shut
    with the QR, the knock line, and opened with the name label and the
    match standing.
  - The app's final frames, as iPad screenshots.

## 12. Calls made in this file (the builder check's questions)

- **The label's colours** are the terminal's own `fg` and `bg`, swapped by
  theme (§4.2). There is no new token.
- **An older app** says "damaged" for a v2 code. This is accepted because
  the app is sideloaded (§3).
- **Remote mode** shows no door and one `DEVICES` row with the command
  (§4.6). There is no `proto` change.
- **Device names** are the model, made unique on the host (`iPad 2`) (§6.4).
  The entitlement is out of scope.

## 11. Left to the builder

- The exact proportions of the taller box, within 100 × 40 with the drawer
  open and the match standing.
- How the pair page shares code between the console layer and the standalone
  program.
- The knock's exact frames, within 240 ms and one cell.
- The SwiftUI structure of the box and the scan screen. Port the site's
  geometry and poses, not its code.
