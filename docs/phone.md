# The phone

On a phone, the console is the control pane: you connect to it, not to
tmux. You see who waits, the context of each session and the load. You go
into a pane and back without tmux keys.

## The matchblox app

The matchblox app for iPhone and iPad is not in the App Store or on
TestFlight yet. To get it now, build it on a Mac with Xcode: see
[ios/README.md](https://github.com/blox-eng/matchblox/blob/main/ios/README.md).
Without a Mac, use [Without the app](#without-the-app).

The app pairs with one scan. It needs no account and no password. Build
the app again with each release that changes the pair link: an older app
cannot read the new code.

1. On the host, open the console and choose the door "Pair a device", or
   `+ pair a device` under `DEVICES` on the Machine tab. Or run:

   ```sh
   matchblox pair
   ```

   matchblox shows a box with a QR code on its label. The code works once.
   It expires after 5 minutes. `c` copies the link. `esc` stops, and the
   code no longer works.
2. In the app, point the camera at the code. The scan is the consent: there
   is no form and no Pair button. The box on the phone catches the code and
   opens, at the same time as the box on the host. The app names the device
   after the phone: `iPhone` or `iPad`. A second one is `iPad 2`.
3. The console of the host slides in. On the host, the page says that the
   device is in, then what it did first.

If the camera cannot read the code, copy the link with `c` on the host, and
tap `Paste` in the app. The Camera app also opens the link.

A host can have a name that the phone cannot reach, such as its name on
your home network. Run `matchblox pair --address <host>`. The console pairs
on port 22. If sshd listens on another port, run `matchblox pair --port <n>`.
If the app says `Can't reach <host>.`,
type the address that the device can reach, then tap `Try again`.

If the app says "This code was used or expired", open Pair a device on the
host again, or run `matchblox pair` again, and scan the new code.

When a device pairs, the host shows its name and fingerprint. If it is not
yours, remove it at once: press `x` on it in `DEVICES`, or run
`matchblox devices rm <fingerprint>`.

A device sees `DEVICES` in its console but cannot pair or remove: it says
`Pair from the host's own terminal.` or `Remove devices from the host's own
terminal.`

### Use the app

- Each time the app opens, Face ID or the passcode unlocks the key. The key
  stays in the Secure Enclave of the device. Each host has its own key.
- With one host, the app opens straight into its console. With two or
  more, it lists the hosts. Tap a host to open its console. Tap
  `+ Pair a host` to add one.
- If the app says `Can't reach <host>.` and `New address?`, type the
  address that the device can reach, then tap `Try again`. The pairing
  stays.
- If the app says that the host has a new host key, the app does not
  connect. Check that the change is yours, then run `matchblox pair` on the
  host and scan the new code. A host key that does not match at pairing
  gets the same message, and the app shows the Address field: the address
  reaches another machine.
- When you quit the console, the app says that the console ended. Tap
  `Open again` to open it. With two or more hosts, tap `Hosts` to go to
  the list.
- A swipe on a host in the list removes the host and its key from the
  device. It does not remove the key on the host. Run
  `matchblox devices rm <name>` on the host for that.

### Remove a device

To remove a device, press `x` on it in `DEVICES` on the Machine tab, and
type `y`. Or run `matchblox devices` on the host. It lists each phone and
laptop that opens the host. Then run:

```sh
matchblox devices rm <name>
```

You can give the fingerprint instead of the name. See
[Remote hosts](remote.md#see-and-remove-the-devices-of-a-host).

## The buzz and the island

With the app closed, the phone buzzes when an agent waits, and a Live
Activity (the island) shows the session that matters most, or your usage.
It needs iOS 18, and the host must run `matchblox serve`: the door "Keep
matchblox running" in `matchblox setup` keeps it running with no console
open. The app registers with the host each time it opens, after Face ID or
the passcode. Nothing connects to a host from the background.

- **The buzz:** a new wait, a session done and a session out. At most one
  buzz each 30 s for each host. `Answer` opens the session.
- **The island:** the match of the session that waits, and its state.
  Progress moves in steps of 5 %, at most each 3 minutes. The Usage face
  moves when a window changes by 2 % or more, or resets, at most each 5
  minutes. A wait breaks through the Usage face for 60 s.
- **A tap opens the session:** the app asks for Face ID, connects and
  opens the console with the number of that session. The console opens on it. If it
  has ended, it says `That session ended.`
- **The setting:** the `Island` entry in the app's console, next to `Hosts`.
  `Follow` is the one host that has the island (the host opened last). `Show`
  is `Sessions`, `Usage` or `Auto (Usage while the stoker runs)`. `Buzz` is
  on or off for each host. A host out of reach keeps its old setting until
  the next connect.
- **If a push fails,** the Machine tab says `push: <why>`. If the island
  needs the app, it says `island: open matchblox on the phone to renew`. To
  stop all of it on a host, set `[push] off = true` in the config: there is
  then no buzz, no island and no connection to the relay.
- **`matchblox devices`** shows `push` next to a phone that registered, and
  `island` next to the one that follows this host.

### What leaves the host in clear

An update to a Live Activity cannot be encrypted, so the same rule holds
for the buzz and both faces. The host sends to the relay, and the relay to
Apple, only:

- numbers: counts, session and account numbers, and percents;
- states and progress;
- times, such as the reset of a window and `out Fri ~20:00`;
- window short names: `5h`, `wk`;
- `host`: 8 characters of the fingerprint of the host key.

No session name, account name, provider, path or text leaves the host. The
app turns a number into a name on the phone, from what it learned in the
console (`session 12` and `account 3` where it knows none). The relay stores
nothing and logs no payload. A state is under 2 KB.

## Without the app

matchblox must be on the host, and the setup step "Add the way back" must
be done.

**Termius**: edit the host, and set its startup snippet to:

```sh
matchblox
```

**Any SSH app or terminal**:

```sh
ssh -t ws-1 matchblox
```

If ssh does not find matchblox, use its full path: `~/.local/bin/matchblox`,
or `~/go/bin/matchblox` after `go install`.

Outside tmux, `matchblox` attaches to the tmux session `matchblox`, or
starts it. A second phone or laptop attaches to the same session.

## Go to an agent and back

1. Tap an agent in Sessions. The key line says `tap again: <command>`.
2. Tap it again. You are in the agent's pane.
3. To come back, tap `◂ matchblox` at the left of the tmux status line, or
   press `prefix m` (the tmux prefix, then m).

The tap on `◂ matchblox` needs the tmux mouse on. Press `m` in the preview
of "Add the way back" to turn it on with it, or add `set -g mouse on` to your
tmux config. Without it, use `prefix m`.

The console then selects the next agent that waits.

## Answer without leaving the console

Tap an agent, then tap it again: on a one-column phone the peek rises from
the bottom, over the dimmed list, with what the agent said last; held
upright with a small font, the drawer on the right becomes the
conversation and the list stays beside it. Tap an option, or type a line
and press `Enter`; it goes to that agent's pane at once. An agent that
works gets it as a message, which Claude Code queues. After an answer the
peek moves to the next agent that waits. A swipe left or right, or a tap
on `← prev` or `next →`, moves it along the list; `Esc` or a tap outside
the peek closes it. See [the answer
peek](console.md#the-answer-peek).

## The phone layout

A phone held upright is read by its shape, so the keyboard coming and
going does not change the layout. With a small font that makes it 90
columns or more, the list sits on the left and the selected agent's detail
in a drawer on the right, and the dock shows two panels side by side. On its side it
goes by its width. Under 60 columns, the console has one column. Each agent is one line,
grouped by what it needs from you: who waits first. The selected one opens
in place: what it said last, its ship, its plan and its keys. Only
the selected setup step shows its detail.

Under the list, when there is room, the dock shows usage, the machine or
the stoker: `u`, or a tap on the word at its right, goes to the next. While
the stoker runs, its panel shows by itself until you switch; a tap on it
opens the Stoker tab. With the keyboard up the dock
folds away; the Machine tab has it.

The key line is at the bottom: an SSH app that shrinks the terminal for
its keyboard keeps it above the keys. A tap on a key presses it.

Turn the phone on its side, at 60 columns or more, and the list and the
detail of the selected agent sit side by side.

Every key is on the key bar of an SSH app: letters, digits, arrows, `Enter`
and `Esc`. See [Keys](keys.md).
