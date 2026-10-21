# The box opens Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Pair a device in one gesture. The console gets a pair page (a door, `DEVICES` on the Machine tab), the link becomes a compact v2, and the app pairs on scan or paste with no form. The host and the app both play the opening of the front-on box.

**Architecture:**
- The link codec lives in `internal/pair` (Go) and `MatchbloxKit` (Swift), with shared vectors in `testdata/pair/`.
- The host keeps the facts in files under `~/.ssh`, all in `internal/setup`, so `gate`, the console and `matchblox pair` need no service or proto change:
  - the claim notes it has today
  - a `.seen` note per code
  - a device note per device
- The pair page is a clock-driven renderer in `internal/app` (`pairbox.go`) with a console layer around it (`pairpage.go`). Standalone `matchblox pair` runs the same layer in its own Bubble Tea program.

**Tech Stack:** Go 1.26, Bubble Tea v2 (`charm.land/bubbletea/v2`), lipgloss v2, `rsc.io/qr`; Swift 6 / SwiftUI, iOS 17.2, VisionKit DataScanner, XCTest.

**Spec:** `design/0009-the-box-opens.md`. It is binding, and DESIGN.md is the canon over it.

## Global Constraints

- Every console colour is a token of `app.Tokens(dark)`. The replay rule (`replay.Parse`) must accept every pair-page frame in both themes.
- The QR label in a dark terminal: light modules `#EBE6DC` (`fg`), dark `#0F0D0A` (`bg`). In a light terminal: light `#F5F1E7` (`bg`), dark `#1C1813` (`fg`). A 2-module quiet zone is part of the label.
- QR error correction is L. The v2 link for the fixture payload is at most 57 columns × 29 rows in half blocks.
- Motion (ms):
  - open 260, out 280, strike 150 (ease-in), stand 320, with the drawer shutting in 260 during stand
  - knock 240 (one cell left and back, twice)
  - lock 180 (app)
- The pair page fits 100 × 40. Under that, the QR shows alone. Under 61 × 33, the page shows the line `Make this terminal 61 × 33 or larger to show the code, or press c to copy the link.`
- No spinners, no new glyph, no new colour. `✦ ✧ ╿ │` never show on the pair page.
- `--no-motion`, `NO_MOTION=1` and iOS Reduce Motion show the last frame of each step. Haptics still play.
- The poll runs every 100 ms while the page shows.
- Copy, exactly:
  - `Scan with the matchblox app, or paste the link.`
  - `c copies the link · esc stops`
  - `link copied`
  - `A device is here…` and `<name> is here…`
  - `Paired <name> (<fp>). Not yours? x on it in DEVICES.`
  - `<name> is in. Answer an agent from it, or esc to go back.`
  - `<name> <verb> <session>`
  - `The code expired. Enter pairs again.` and `The code expired. Run matchblox pair again.`
  - `Stopped. The code no longer works.`
  - `Pair from the host's own terminal.`
  - `Remove devices from the host's own terminal.`
  - Door: title `Pair a device`, why `answer your agents from your phone`.
  - App: `Run matchblox pair on your host, or open Pair a device in its console.` and `That is not a matchblox pair link. Copy it again with c on the pair page.`
- Conventional commits. Each commit ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. The work runs on branch `worktree-feat-box-opens` in `.claude/worktrees/feat-box-opens`.
- `go test ./...`, `make -C ios test` and `make -C ios build` pass after every task that touches them.

## Review Focus

1. **A one-time key must never stay in `authorized_keys`.** The console quits (ctrl+c, `q`, the service drops, the terminal closes) while the page shows, and the key line must still be removed. Test in Task 5.
2. **Two devices claim codes with the same model name at once.** The names must stay unique (`iPad`, `iPad 2`) under the keys lock. Test in Task 2.
3. **`authorized_keys` holds old phone lines (`gate phone` with no id), new ones, laptop lines and pair lines.** `Devices`, `RemoveDevice` and `gate` must handle all of them. Tests in Tasks 2 and 3.
4. **The terminal is resized under 61 × 33 mid-opening, then back.** The page must keep its state and draw the right fallback each time, and `c` must still copy. Test in Task 5.
5. **A device presses `Enter` on the door in the shared console.** It must get the refusal, and the builder pressing next must get the page. Test in Task 8.

---

### Task 1: The v2 pair link

**Files:**
- Modify: `internal/pair/pair.go`, `internal/pair/qr.go`
- Create: `testdata/pair/v2.json` (the fixture payload of `v1.json` and its v2 `url`)
- Test: `internal/pair/pair_test.go`

**Interfaces:**
- Produces:
  - `func (p Payload) URL() string`, now v2. `V` is set to 2 by `URL`.
  - `func Parse(raw string) (Payload, error)`, which reads v1 (JSON) and v2 (binary).
  - `func QR(text string) ([]string, error)`, now at level L.
  - `func Modules(text string) (size int, light func(x, y int) bool, err error)`: the module grid with the 2-module quiet zone included, for the label renderer.

- [ ] **Step 1: Write the failing tests**
  - `TestV2URLMatchesTheFixture`: `load2(t).Payload.URL() == load2(t).URL`.
  - `TestParseReadsV1AndV2`: both fixture URLs parse to the same `Payload` except `V`.
  - `TestParseRefusesV2`, a table:
    - a short blob
    - version byte 3
    - a missing `0x00` separator
    - an empty host, user or name
    - port 0
    - a bad base64url
  - `TestQRSizeV2`: for the payload with host `martin-lexroom-macbook.tail1a2b3c.ts.net`, user `martinyankov`, name `Martin-Lexroom-Macbook`, `len(lines) <= 29` and the width is `<= 57` runes.
- [ ] **Step 2:** Run `go test ./internal/pair/`. Expected: FAIL (undefined `load2`, v1 URL).
- [ ] **Step 3: Implement**
  - Byte layout: `0x02`, port as 2 bytes big-endian, `sha256(hostkey)` 32 bytes, seed 32 bytes, then `host 0x00 user 0x00 name`.
  - The host key fingerprint round-trips as `"SHA256:" + base64.RawStdEncoding(sum)`.
  - Keep the v1 validation rules for both versions.
  - Change `qr.M` to `qr.L` in `qr.go`. `QR` uses `Modules`.
- [ ] **Step 4:** Run `go test ./internal/pair/ ./cmd/matchblox/`. Expected: PASS. Update `cmd/matchblox/pair_test.go` only where it pins the v1 URL text.
- [ ] **Step 5:** Commit `feat(pair): a compact v2 link halves the QR code`.

### Task 2: Device ids, unique names and the host's notes

**Files:**
- Modify: `internal/setup/devices.go`
- Test: `internal/setup/devices_test.go`

**Interfaces:**
- Produces:
  - `type Device struct{ Kind, Name, Fingerprint, ID string }`. `ID` is `""` for an old phone line and a laptop line.
  - `withPhone(old []byte, exe, pubKey, name, id string)` writes `command="'<exe>' gate phone <id>"`.
  - The `gateLine` regexp accepts `gate`, `gate phone` and `gate phone <12 hex>`.
  - `ClaimPairKey(home, exe, id, pubKey, name string) (Device, error)` picks a unique name. If `name` is taken, it uses `name 2`, `name 3` and so on, under the keys lock, and returns the name it wrote.
  - `func MarkPairSeen(home, id, name string) error` (`~/.ssh/.matchblox-pair-<id>.seen`, 0600, JSON `{"at":…,"name":…}`; `name` may be "").
  - `func PairSeen(home, id string) (seen bool, name string)`.
  - `func RemovePairSeen(home, id string)`.
  - `func MarkDeviceIn(home, id, tty string, at time.Time) error` (`~/.ssh/.matchblox-device-<id>`, 0600, JSON `{"at":…,"tty":…}`).
  - `func DeviceIn(home, id string) (at time.Time, tty string, ok bool)`.
  - `func DeviceTTYs(home string) map[string]string` (tty → id, for every device note).

- [ ] **Step 1: Write the failing tests**
  - `TestClaimWritesTheDeviceID`: the new line contains `gate phone <id>`, and `Devices` returns `ID == id`.
  - `TestClaimMakesNamesUnique`: claiming three codes with name `iPad` yields `iPad`, `iPad 2`, `iPad 3`. `RemoveDevice(home, "iPad 2")` removes only that one.
  - `TestDevicesReadsEveryLineKind`: a file with an old `gate phone` line, a new `gate phone <id>` line, a laptop `gate` line, a pair line and a plain user key gives the three devices, with the right kinds and ids.
  - `TestSeenAndDeviceNotes`: a round-trip of both notes, and `DeviceTTYs` maps `/dev/ttys004` to its id.
- [ ] **Step 2:** Run `go test ./internal/setup/`. Expected: FAIL.
- [ ] **Step 3:** Implement. Unique names count existing phone names only. `ValidDeviceName` must still hold for `name 40-chars + " 2"`, so trim the base so the result is at most 40 runes.
- [ ] **Step 4:** Run `go test ./internal/setup/ ./cmd/matchblox/`. Expected: PASS.
- [ ] **Step 5:** Commit `feat(setup): a device line carries its id, and two iPads get two names`.

### Task 3: gate knows the device and the knock

**Files:**
- Modify: `cmd/matchblox/gate.go`, `cmd/matchblox/pair.go` (`pairMain`)
- Test: `cmd/matchblox/gate_test.go`, `cmd/matchblox/pair_test.go`

**Interfaces:**
- Consumes: `MarkPairSeen`, `MarkDeviceIn`, `ClaimPairKey` (Task 2).
- Produces:
  - `gateMain` accepts `["phone"]` and `["phone", <12 hex>]`.
  - With an id, before `run`, it calls `setup.MarkDeviceIn(home, id, os.Getenv("SSH_TTY"), time.Now())`. A note write failure never blocks the console.
  - `pairMain` calls `MarkPairSeen(home, id, "")` before it reads, and `MarkPairSeen(home, id, req.Name)` once the request is valid.

- [ ] **Step 1: Write the failing tests**
  - `TestGatePhoneWithIDMarksTheDevice`
  - `TestGatePhoneRefusesABadID`: `phone xyz` exits 126.
  - `TestGatePhoneWithoutIDStillOpens`
  - `TestPairMainMarksSeenBeforeItReads`: with a reader that blocks, `PairSeen` is true and the name is "".
  - `TestPairMainNamesTheSeenNote`
- [ ] **Step 2:** Run `go test ./cmd/matchblox/ -run 'Gate|PairMain'`. Expected: FAIL.
- [ ] **Step 3:** Implement. `gateDecision` is unchanged. Validate the id with `setup.ValidPairID`.
- [ ] **Step 4:** Run the same command. Expected: PASS.
- [ ] **Step 5:** Commit `feat(gate): a device's connection leaves a note the pair page reads`.

### Task 4: The pair box renderer

**Files:**
- Create: `internal/app/pairbox.go`
- Test: `internal/app/pairbox_test.go`

**Interfaces:**
- Consumes: `pair.Modules` (Task 1), `Tokens`, `markColours` (`darkMark`/`lightMark`).
- Produces:
  ```go
  type boxPhase int // boxShut, boxKnock, boxOpen, boxExpired
  type boxState struct {
  	phase   boxPhase
  	since   time.Duration // time since the phase began
  	link    string        // the v2 URL; the label's QR
  	name    string        // the device's name on the label after the claim; "" before
  }
  // pairBox draws the box at a moment, centred in w × h, or nil when the QR does not fit.
  func pairBox(s boxState, dark, noMotion bool, w, h int) (lines []string, fits fitKind)
  type fitKind int // fitBox, fitQROnly, fitNone
  const (knockFor = 240*time.Millisecond; openFor = 260*time.Millisecond; outFor = 280*time.Millisecond; strikeFor = 150*time.Millisecond; standFor = 320*time.Millisecond)
  ```
- The geometry:
  - the sleeve in `mark-right`, its outline in `mark-left`
  - the striker row of `▚▞` in `mark-striker` on `mark-right`, on the sleeve's first inner row
  - the label centred under the striker
  - the drawer (`mark-tray`, edges `mark-left`) behind the sleeve's lower half, sliding up to 16 columns right
  - the match: stick `━`/`┃` in `mark-stick`, head `●` in `match-head` (`accent` once struck), flame in half blocks of `flame`/`flame-amber`/`flame-core` in a dark terminal only
  - the strike flash: one cell `▸` in `flame-core` for the last 40 ms of the strike
- Poses follow `www/app.js` `POSE`: in the drawer, then the start of the striker, then the end of the striker, then upright right of the drawer.
- The opened label is plain paper with `name` centred in label ink. The expired label is blank paper.

- [ ] **Step 1: Write the failing tests**
  - `TestPairBoxEveryCellIsAToken`: for `dark` in {true, false} and each phase at `since` in {0, 100 ms, 500 ms, 1 s, 2 s}, `replay.Parse(join(lines), Tokens(dark))` returns no error.
  - `TestPairBoxLabelScans`: decoding the label cells back to modules (light = the label's light ink) equals `pair.Modules(link)`, in both themes.
  - `TestPairBoxFits`:
    - 100 × 40 gives `fitBox`, and no line is wider than 100
    - 70 × 36 gives `fitQROnly`
    - 60 × 32 gives `fitNone`
  - `TestPairBoxOpenEndsStill`: the frame at `since` = 1.1 s equals the frame at 10 s.
  - `TestPairBoxNoMotionIsTheLastFrame`: noMotion at `since=0` equals the motion frame at 10 s, for every phase.
  - `TestPairBoxLightIsUnlit`: no `flame*` colour in a light terminal at 10 s of `boxOpen`.
  - `TestPairBoxNoAgentGlyph`: no frame contains `✦ ✧ ╿ │`.
- [ ] **Step 2:** Run `go test ./internal/app/ -run PairBox`. Expected: FAIL.
- [ ] **Step 3:** Implement. Copy the clock-driven approach of `markFrame` (mark.go:100): pure on `(s, dark, noMotion, w, h)`.
- [ ] **Step 4:** Run the same command. Expected: PASS.
- [ ] **Step 5:** Commit `feat(console): the pair box, a front-on matchbox with the code as its label`.

### Task 5: The pair page layer

**Files:**
- Create: `internal/app/pairpage.go`
- Modify:
  - `internal/app/model.go`: fields; the key switch at :596-622 and the touch check at :605; the `Options` field `Home string`
  - `internal/app/view.go` (`render`: the page takes the screen like the splash, view.go:70)
  - `internal/app/mouse.go` (route taps to the page first)
  - `cmd/matchblox/main.go:420-456` (set `Options.Home` from `os.UserHomeDir()`)
- Test: `internal/app/pairpage_test.go`

**Interfaces:**
- Consumes:
  - Task 4 (`pairBox`)
  - Task 2 notes
  - `setup.AddPairKey`, `PairKeyPresent`, `RemovePairKey`, `TakeClaim`, `pair.NewKey`
  - `hostKeyFingerprint`, moved from cmd to `setup.HostKeyFingerprint(path string) (string, error)` so the console can use it. Keep the cmd name as a thin call.
- Produces:
  ```go
  type pairPage struct {
  	id, link   string
  	until      time.Time
  	phase      boxPhase
  	phaseAt    time.Time
  	seenName   string
  	device     setup.Device // after the claim
  	standalone bool         // matchblox pair: stops after the opening
  	beat       int          // 1 shut, 2 opened, 3 in, 4 first act (Task 9)
  	line       string       // the line under the box
  }
  func (m Model) openPairPage() (Model, tea.Cmd) // makes the key, writes it, starts the 100 ms poll
  func (m Model) closePairPage(why string) Model  // removes the key line and the .seen note; why goes to the flash
  type pairPollMsg struct{}
  func pairPoll() tea.Cmd                       // tea.Tick(100*time.Millisecond)
  func osc52(text string) string                // "\x1b]52;c;" + base64(text) + "\x07"
  ```
- Keys while the page shows: `c` copies (sets the flash to `link copied`), `esc` closes with `Stopped. The code no longer works.`, and `ctrl+c` closes the page, then quits. Everything else is ignored.
- `m.quit` and every path that ends the program call `closePairPage` first.
- Each poll:
  1. Check expiry.
  2. Check `PairSeen`; the first one sets `boxKnock` and the line `A device is here…` / `<name> is here…`.
  3. Check `PairKeyPresent`. When the key is gone, `TakeClaim` sets `device`, `boxOpen`, `name` on the label, and the line `Paired …`.
- On expiry: `boxExpired` and the line `The code expired. Enter pairs again.`. The page stays until `esc` or `Enter`, and `Enter` makes a new code.

- [ ] **Step 1: Write the failing tests** (`TestPairPageCopiesTheLink` checks the text `Options.Clip` gets; use `loadedWith`, a temp `Options.Home` with `.ssh/authorized_keys`, and `Options.Now` for the clock):
  - `TestPairPageWritesAndRemovesTheKey`: opening it adds one pair line, and `esc` removes it and the `.seen` note.
  - `TestPairPageRemovesTheKeyOnQuit`: `ctrl+c` with the page open leaves no pair line (Review Focus 1).
  - `TestPairPageKnocksOnSeen`: `MarkPairSeen` then a poll gives the line `A device is here…`. With a name, it is `iPad is here…`.
  - `TestPairPageOpensOnClaim`: `ClaimPairKey` then a poll gives `boxOpen`, the label name, and the line `Paired iPad (SHA256:…). Not yours? x on it in DEVICES.`
  - `TestPairPageExpires`
  - `TestPairPageCopiesTheLink`: `Clip` gets the link, the flash is `link copied`, and `osc52("x") == "\x1b]52;c;eA==\x07"`.
  - `TestPairPageSurvivesResize`: `WindowSizeMsg` 60×30 shows the `Make this terminal…` line, 100×40 shows the box again in the same phase, and `c` works at 60×30 (Review Focus 4).
  - `TestPairPageViewIsTokens`: every rendered view of the page passes `replay.Parse(view, Tokens(dark))`.
- [ ] **Step 2:** Run `go test ./internal/app/ -run PairPage`. Expected: FAIL.
- [ ] **Step 3:** Implement. Copy through a new `Options.Clip func(text string)`, which tests replace. The default writes `osc52(text)` to `os.Stdout`; the sequence does not move the cursor, so Bubble Tea's frame stays right. `tea.Printf` is wrong: it prints above the program.
- [ ] **Step 4:** Run `go test ./internal/app/`. Expected: PASS.
- [ ] **Step 5:** Commit `feat(console): the pair page, a layer that pairs a device`.

### Task 6: The door "Pair a device"

**Files:**
- Modify:
  - `internal/doors/doors.go` (const `Pair = "pair"`)
  - `internal/setup/setup.go` (`Doors`: add `pairDoor(e)` after `wayback`, before `guide`; add `"pair"` to `known` at :86)
  - `cmd/matchblox/setup.go:119` (a special case like Night: `press Enter on it in the console, or run matchblox pair`)
  - `internal/app/doors.go` (`doorAction`)
  - `internal/app/model.go:765-776` and `mouse.go:150-162` (Enter and a tap on the pair door open the page and never set `pending`)
- Test: `internal/setup/setup_test.go`, `internal/app/doors_test.go`, `cmd/matchblox/setup_test.go`

**Interfaces:**
- Consumes: `openPairPage` (Task 5), `setup.Devices`.
- Produces: `func pairDoor(e Env) doors.Door`. It is `Done` when `Devices(e.Home)` has a `phone`; it has no `Sum` and no `Term`. Title and Why are as in the Global Constraints.

- [ ] **Step 1: Write the failing tests**
  - `TestPairDoorIsDoneWithAPhone`
  - `TestPairDoorCloses`: `Close(e, "pair")` works.
  - `TestEnterOnThePairDoorOpensThePage`: `m.pending == nil` and the page is open.
  - `TestTapOnThePairDoorOpensThePage`
  - `TestPairDoorHiddenInRemoteMode`: with `Options.Host="ws-1"`, the console filters the door out.
  - `TestSetupSaysHowToPair`
- [ ] **Step 2:** Run `go test ./internal/setup/ ./internal/app/ ./cmd/matchblox/ -run 'PairDoor|SetupSays'`. Expected: FAIL.
- [ ] **Step 3:** Implement. Filter in `openDoors` callers when `m.opt.Host != ""`.
- [ ] **Step 4:** Run `go test ./...`. Expected: PASS.
- [ ] **Step 5:** Commit `feat(console): a door pairs a device`.

### Task 7: `DEVICES` on the Machine tab

**Files:**
- Create: `internal/app/devices.go`
- Modify:
  - `internal/app/panels.go:160-162` (the region between the busy loops and the top processes)
  - `internal/app/tabs.go:90-115` (`count`/`index`/`pick`/`primary`/`secondary` with an offset after the orphans)
  - `internal/app/model.go` (`action` gets `local func(Model) (Model, string)`; `confirm` runs `local` instead of sending, and its string goes to the flash)
- Test: `internal/app/devices_test.go`

**Interfaces:**
- Consumes: `setup.Devices`, `setup.RemoveDevice`, `openPairPage`.
- Produces:
  - `func (m Model) devicesBody(w int) body`
  - `m.devices []setup.Device`, read when the Machine tab opens and after each change, never per frame
  - Rows: `kind  name  fingerprint`, phones first, then `+ pair a device`.
  - In remote mode, the one row `run matchblox pair on <host>`.

- [ ] **Step 1: Write the failing tests**
  - `TestDevicesListsPhonesFirst`
  - `TestDevicesPlusRowOpensThePage`
  - `TestDevicesRemoveNeedsATypedY`: `x` then `n` keeps the device. `x` then `y` removes it, and the flash names the backup.
  - `TestDevicesRemoteShowsTheCommand`
  - `TestDevicesKeepsOrphanRows`: with two orphans and two devices, `x` on orphan 1 still builds the orphan kill.
- [ ] **Step 2:** Run `go test ./internal/app/ -run Devices`. Expected: FAIL.
- [ ] **Step 3:** Implement. The region label is `devices`, so `region` uppercases it and the count follows: `DEVICES · N`.
- [ ] **Step 4:** Run `go test ./internal/app/`. Expected: PASS.
- [ ] **Step 5:** Commit `feat(console): DEVICES lists and removes the keys that open this host`.

### Task 8: A device can only see

**Files:**
- Create: `internal/app/lastclient.go`
- Modify: `internal/app/pairpage.go` (`openPairPage`), `internal/app/devices.go` (the remove action)
- Test: `internal/app/lastclient_test.go`

**Interfaces:**
- Consumes: `setup.DeviceTTYs` (Task 2).
- Produces:
  - `Options.LastClient func() (tty string, err error)`. The default runs `tmux list-clients -t =matchblox -F '#{client_activity} #{client_tty}'` and returns the tty with the largest activity. It returns "" outside the home session or when tmux is missing.
  - `func (m Model) fromDevice() bool`: the last client's tty is in `DeviceTTYs(m.opt.Home)`.

- [ ] **Step 1: Write the failing tests**
  - `TestLastClientPicksTheLatest`: parse a fixed `list-clients` output.
  - `TestDeviceGetsTheRefusals`: with `LastClient` returning a device tty, `Enter` on the door gives the flash `Pair from the host's own terminal.` and no page, and `x` then `y` on a device gives `Remove devices from the host's own terminal.` with the device kept.
  - `TestBuilderAfterDevice`: the same model, `LastClient` now the builder's tty, `Enter` opens the page (Review Focus 5).
- [ ] **Step 2:** Run `go test ./internal/app/ -run 'LastClient|DeviceGets|BuilderAfter'`. Expected: FAIL.
- [ ] **Step 3:** Implement. On a tmux error, treat the key as the builder's: the guard is not a wall (spec §4.5).
- [ ] **Step 4:** Run `go test ./internal/app/`. Expected: PASS.
- [ ] **Step 5:** Commit `feat(console): a device can see DEVICES but not change them`.

### Task 9: Beats 3 and 4

**Files:**
- Modify: `internal/app/pairpage.go`, `internal/app/model.go` (`confirm`: tell the page about each act it sends)
- Test: `internal/app/pairpage_test.go`

**Interfaces:**
- Consumes: `setup.DeviceIn` (Task 2), `LastClient` (Task 8).
- Produces: `func (m Model) pairActed(a action) Model`, called by `confirm` after it sends a service act.
- Beat 3: a poll finds `DeviceIn(home, device.ID)` with `at` after the claim. The line becomes `<name> is in. Answer an agent from it, or esc to go back.`
- Beat 4: an act arrives while the last client's tty is that device's tty. The line becomes `<name> <verb> <session>` once, and the page folds after 1.5 s.
  - Verbs by `rec` prefix: `answer:` answered, `pick:` picked in, `message:` wrote to, `compact:` compacted, otherwise acted in.
  - `<session>` is the session's name for the pane.
- If no beat 3 comes within 60 s of the opening, the page folds with the `Paired …` flash. `esc` folds the page at any beat.
- Standalone mode never waits for beats 3 and 4.

- [ ] **Step 1: Write the failing tests**
  - `TestBeatThreeOnTheDevicesNote`: a note for another id does nothing.
  - `TestBeatFourNamesTheAct`: an answer to pane `%2` (session `api`) from the device's tty gives `iPad answered api`, then the page folds.
  - `TestBeatFourIgnoresTheBuilder`: the same act from the builder's tty keeps beat 3.
  - `TestFoldsWithoutBeatThree`: 60 s pass and the page folds.
- [ ] **Step 2:** Run `go test ./internal/app/ -run 'Beat|FoldsWithout'`. Expected: FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run `go test ./internal/app/`. Expected: PASS.
- [ ] **Step 5:** Commit `feat(console): the pair page shows the new device come in and act`.

### Task 10: `matchblox pair` runs the page

**Files:**
- Modify: `cmd/matchblox/pair.go` (`pairCmd`)
- Create: `internal/app/pairalone.go` (`func RunPairPage(o PairOptions) (result string, err error)`, a Bubble Tea program with only the page, `standalone: true`, the alternate screen)
- Test: `cmd/matchblox/pair_test.go`

**Interfaces:**
- Consumes: Tasks 1, 4 and 5.
- Produces:
  - `pairCmd` keeps its flags and its signature.
  - With a terminal (`term.IsTerminal(os.Stdout.Fd())`), it calls `app.RunPairPage` and prints the result line after the alternate screen closes.
  - Without one, it keeps today's plain output, with the v2 QR, the v2 URL and today's lines.
  - `pairPollInterval` becomes 100 ms.

- [ ] **Step 1: Write the failing tests**
  - `TestPairCmdPlainOutputIsV2`: the output's URL parses as `V == 2`, and the QR has at most 29 lines.
  - The existing `TestPairCmd*` tests pass unchanged except for the v2 text.
- [ ] **Step 2:** Run `go test ./cmd/matchblox/ -run PairCmd`. Expected: FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run `go test ./...`. Expected: PASS. Then by hand: `go build -o /tmp/mb ./cmd/matchblox && /tmp/mb pair --address Martin-Lexroom-Macbook.local` shows the page. `esc` prints `Stopped. The code no longer works.`
- [ ] **Step 5:** Commit `feat(pair): matchblox pair shows the pair page`.

### Task 11: MatchbloxKit reads v2 and finds a pasted link

**Files:**
- Modify: `ios/MatchbloxKit/Sources/MatchbloxKit/PairPayload.swift`, `Pairing.swift`
- Test: `ios/MatchbloxKit/Tests/MatchbloxKitTests/PairPayloadTests.swift`, `PairingTests.swift`, `Fixture.swift` (load `v2.json` too)

**Interfaces:**
- Produces:
  - `PairPayload(url:)` reads v1 and v2, and keeps `hostkey` as `"SHA256:<base64, no padding>"`.
  - `public static func firstLink(in text: String) -> URL?` on `Pairing` returns the first `matchblox://pair?p=…` token.

- [ ] **Step 1: Write the failing tests**
  - `testV2FixtureParses`: equal to the v1 payload except `v`.
  - `testV2Refusals`: the same cases as Go Task 1.
  - `testFirstLinkInText`: the link inside prose, inside a line with a trailing `.`, none, and two links (the first wins).
- [ ] **Step 2:** Run `make -C ios test`. Expected: FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run `make -C ios test`. Expected: PASS.
- [ ] **Step 5:** Commit `feat(app): the app reads the v2 link and finds one in pasted text`.

### Task 12: The box in SwiftUI, and haptics

**Files:**
- Create: `ios/Matchblox/MatchBox.swift`, `ios/Matchblox/Haptics.swift`
- Modify: `ios/Matchblox/Theme.swift` (add the `mark-*`, `flame`, `flame-amber`, `flame-core`, `match-head` dark tokens of DESIGN.md §2.1)

**Interfaces:**
- Produces:
  ```swift
  enum BoxPose: Equatable { case outline, shut, open(Date), failed }  // open carries the start of the opening
  struct MatchBox: View { let pose: BoxPose; let label: AnyView? }  // label sits in the sleeve's window
  enum Haptics { static func lock(); static func strike(); static func stood(); static func failed() }
  ```
- Geometry: `www/index.html` `#match` (viewBox 0 0 100 66), made taller so the label window is square. Draw it with `Path`s, not an image.
- Timeline: a `TimelineView(.animation)` from the `open` date, with the Global Constraints timings. Reduce Motion draws the last frame.
- Haptics: `UIImpactFeedbackGenerator(.light)`, `(.rigid)`, `UINotificationFeedbackGenerator` `.success` and `.error`.

- [ ] **Step 1:** Add `#Preview`s for each pose. There is no unit test: MatchbloxKit has no SwiftUI.
- [ ] **Step 2:** Run `make -C ios build`. Expected: BUILD SUCCEEDED.
- [ ] **Step 3:** Commit `feat(app): the matchbox and its haptics`.

### Task 13: Scan, catch, strike: the app's pairing flow

**Files:**
- Modify: `ios/Matchblox/ScanView.swift`, `ios/Matchblox/MatchbloxApp.swift`
- Replace: `ios/Matchblox/PairView.swift` with a `PairingView` in the same file (`PairFlow` stays as is)

**Interfaces:**
- Consumes: Tasks 11 and 12, `PairFlow.run`.
- Produces:
  - `ScanView(problem:notice:onCode:onCancel:)` keeps its signature:
    - the camera fills the screen, with `MatchBox(pose: .outline)` over it
    - `QRScanner` reports the code's `bounds` too, and the outline moves to them over 180 ms with `Haptics.lock()`
    - `PasteButton(payloadType: String.self)` takes `Pairing.firstLink(in:)`, and otherwise shows the not-a-link line
    - no camera: `MatchBox(pose: .shut)` with today's text
  - `PairingView(draft:store:onPaired:onScanAgain:)` starts `PairFlow.run` in `.task`, with no form:
    - while it runs, the host name on the label pulses at 1.6 s
    - `ok` gives `.open(now)`, `Haptics.strike()` at the strike, `Haptics.stood()` at the stand, then `onPaired` after the opening
    - a failure gives `.failed`, today's message in `neg`, `Haptics.failed()`, an Address field and `Try again` for a connection failure, and `Scan again`
  - `MatchbloxApp`: `.pair` uses `PairingView`. The transition to `.console` is a move from the trailing edge, 300 ms.

- [ ] **Step 1:** Run `make -C ios build`. Expected: BUILD SUCCEEDED.
- [ ] **Step 2:** Install on the iPad with the commands in the spec's handoff. Test:
  - a scan pairs with no form, and both screens strike within about 0.2 s
  - Paste the link pairs
  - a wrong address shows the field, and Try again pairs
  - Reduce Motion
- [ ] **Step 3:** Commit `feat(app): scan is the consent: the box catches the code and opens`.

### Task 14: Canon, docs and the site's stills

**Files:**
- Modify:
  - `DESIGN.md` (§3 the front-on box; §5 rows Knock 240 ms, Open about 1 s, Lock 180 ms, and the haptics note; §6 the door, `DEVICES`, the pair page; §7 the copy of the Global Constraints)
  - `CHANGELOG.md` (Unreleased > Added, and a line that the app must be built again for the v2 link)
  - `docs/phone.md` (`### Use the app`, `### Remove a phone` becomes `### Remove a device`)
- Create: `internal/app/pairstills_test.go` (`TestPairStills`, which with `-stills` writes `testdata/pair/stills/{shut,knock,open}.ansi` from `pairBox` at 100 × 40 in the dark theme)

- [ ] **Step 1:** Run `go test ./internal/app/ -run PairStills -stills`. Check that the three files exist and that `replay.Parse` accepts each.
- [ ] **Step 2:** Run `go test ./...` and `make -C ios test`. Expected: PASS.
- [ ] **Step 3:** Take iPad screenshots of the scan, the opened box and a failure into the scratchpad for matchbloxapp.
- [ ] **Step 4:** Commit `docs: the box opens in DESIGN.md, the changelog and the phone guide`.
- [ ] **Step 5:** Message matchbloxapp with the stills' paths and the screenshots.
