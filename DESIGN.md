# matchblox design

Status: binding (2026-10-07). Language: ASD-STE100. Code names stay as they
are.

This file is the one source for how matchblox looks, moves and speaks: in the
console, on matchblox.com and in the README. Where a design document
(`design/0001` §10, `design/0003` §4) gives other values, this file applies.
A change to a token, a glyph or a motion changes this file in the same pull
request.

## 1. Idea

matchblox keeps the fire burning. Each coding agent is a **match**. A match
burns while its agent works, rests while it waits for the person, and is
spent when its context is full. The builder's work is to keep the matches
burning: answer what waits, compact what is spent.

Four rules follow from the idea. Every screen is judged against them.

1. **The work of the system is silent. The act of the person has weight.**
   Only fire and a session that needs the person move. A confirmed action
   has a clear end.
2. **One meaning for each sign.** A glyph, a colour or a motion means one
   thing everywhere: in the console, on the site, in the README.
3. **No dead ends.** Each state that needs the person shows the one action
   that resolves it, in the same place. An error tells the fix.
4. **Calm by default.** A normal row is quiet. Colour is an event: only an
   exception gets a colour wash, and the accent appears once in each view.

## 2. Colour

Dark first. The light set applies when the terminal background is light (the
console asks the terminal) or the visitor picks light (the site).

### 2.1 Console tokens

The code is `internal/app/theme.go`. `app.Tokens(dark)` names each colour;
the replay maps every colour of a frame to its token, so a colour that is
not a token fails the replay test.

| Token | Dark | Light | Use |
|---|---|---|---|
| `bg` | `#0F0D0A` | `#F5F1E7` | ground |
| `fg` | `#EBE6DC` | `#1C1813` | text |
| `muted` | `#A69D8D` | `#5F5A4C` | secondary text |
| `faint` | `#776F60` | `#8A8170` | labels, units, a spent match |
| `hair` | `#2E2921` | `#DDD5C3` | hairlines |
| `accent` | `#C3A56A` | `#9A7B3F` | the active tab, the selection, the one primary action, a session that asks |
| `wash` | `#221D14` | `#ECE3CF` | the background of the selected row |
| `warn` | `#B98F44` | `#8A6420` | a warning |
| `neg` | `#B0563F` | `#9E4A34` | a failure, a destructive action, context at the compact limit |

The night (#45, owner 2026-10-11): while the stoker is in charge the whole
console wears the night. Every cell is painted on `night-bg` (a terminal has
no transparency: this is the ambient light), the text takes the night inks,
and only the fires keep their warmth: `accent`, `warn`, `neg`, the flames
and the match heads stay as they are. It is one palette in both themes, and
when the stoker rests the day comes back.

| Token | Night | Use |
|---|---|---|
| `night-bg` | `#0C1019` | the ground under every cell |
| `night-fg` `night-muted` `night-faint` | `#D9DCE3` `#8E96A8` `#636B7D` | text, as `fg` `muted` `faint` |
| `night-hair` | `#1F2636` | hairlines |
| `night-wash` | `#18202F` | the background of the selected row |
| `night` | `#7C8BB0` | the stoker word in the header and the rule under the tabs |

The match and the mark (§3, §4):

| Token | Dark | Light | Use |
|---|---|---|---|
| `flame` | `#E8822E` | `#E8822E` | the flame |
| `flame-amber` | `#F5A54A` | `#DF8E2C` | the flame, between its orange and its core |
| `flame-core` | `#FFD27A` | `#D69A2A` | the core of the flame, the flash of a strike |
| `match-head` | `#C2452D` | `#B23A24` | the head of a match at rest |
| `match-breath-1..3` | `#9E3A26` `#7A2F1F` `#572318` | `#BF5F4B` `#CD8372` `#DAA899` | the head as it breathes, toward the ground |
| `match-ember` | `#9C5A46` | `#9E5E4A` | a flame that dies, a head that burns away |
| `mark-top` `mark-left` `mark-right` | `#3A3328` `#1C1914` `#2A251D` | `#EFE9DC` `#CFC6AF` `#DED6C3` | the faces of the box |
| `mark-tray` `mark-striker` `mark-stick` | `#15120E` `#5A4128` `#C9A877` | `#C4BAA2` `#8C6A43` `#B8925B` | the drawer, the striker, the wood |

### 2.2 Site tokens

The site (`www/styles.css`) has its own text tokens for a page (`--bg`,
`--fg`, `--muted`, `--faint`, `--hair*`, `--primary`) and the console tokens
as `--t-<token>` for the replay. A replay span gets the class `fg-<token>` or
`bg-<token>`. To add a token: add it to `Tokens()`, to both `--t-` sets
(dark and light) and the class list in `styles.css`, and to the tables here.

## 3. The mark

The mark is an isometric matchbox: three faces, a drawer, a striker on the
side, and a match that stands on the drawer.

- **Site:** `www/assets/matchblox-mark*.svg`, and the strike canvas in the
  hero (`app.js`): blocks fall into the box, the drawer opens, the match
  strikes and burns. In light mode it is daylight: the match lights only
  while the pointer is on it.
- **Console start screen:** the same mark, drawn with half-blocks (two pixels
  in each cell), 40 × 24 pixels. Fall to 300 ms, open to 380 ms, strike to
  480 ms, stand to 560 ms. In a dark terminal the match burns; in a light
  terminal it stays unlit. Any key stops it. `--no-motion` or `NO_MOTION=1`
  shows the last frame only. The start screen stays while the console waits
  for the service.
- **Console header:** `▰ matchblox · <host>`. The `▰` is the accent.
- **The front-on box** (design/0009): the pair page and the app's pair
  screens draw the site's theme-switch box seen front-on: the sleeve, the
  striker along its top, the drawer that slides out to the right, and the
  match. It has the site's parts, poses and timings (§5). It is taller than
  the site's box, because it holds a square label. The label is paper, with
  the code on it: in a dark terminal light modules are `fg` and dark modules
  are `bg`; in a light terminal light modules are `bg` and dark modules are
  `fg`. After a claim the label turns to plain paper with the device's name
  in ink. The faces use `mark-*` and the striker uses `mark-striker`. In a
  dark terminal the match burns after the strike; in a light one it stays
  unlit. In the app the match head stays `match-head` after the strike, as
  on the site. This match is the logo, not an agent (§4 rule 2).

## 4. The match

The match is the element of matchblox. One cell, and the character is the
state. The code is `internal/app/match.go`.

| State | Glyph | Colour | Motion | Means |
|---|---|---|---|---|
| burning | `✦` | `flame`, `flame-amber`, `flame-core` | flickers: a new colour each 200 ms; `✧` for one step in six | the agent works |
| at rest, asks | `╿` | `match-head` → `match-breath-1..3` | breathes: one slow breath each 2.4 s | the agent waits for an answer or a permission |
| at rest | `╿` | `match-head` | still | the agent finished its turn and waits |
| spent | `│` | `faint` | still | the context is at or above the compact limit (`sessions.compact_tokens`, default 350k, or `sessions.compact_at`, default 85 %, whichever comes first) |

Order: spent comes first. A session that works with a full context is spent.
A session the plan's limit stops (`out`) keeps the at-rest glyph, or spent
when full; its word is `out` and the idle column holds its reset.

A change of state plays once:

| Change | Time | Frames |
|---|---|---|
| **The strike**: at rest → burning | 800 ms | `╿` `flame-core` (the head flashes, 200 ms) → `·` `flame-core` (a spark, 300 ms) → `✧` `flame-amber` (it grows, 300 ms) → burning |
| **Goes out**: burning → at rest | 400 ms | `✧` `flame-amber` → `✧` `match-ember` → at rest |
| **Burns away**: → spent | 600 ms | `╿` `match-ember` → `│` |

Rules:

1. The glyph never stands alone: the state word stays next to it
   (`✦ busy`, `╿ asks`, `│ burnt`).
2. A match means one agent session. Do not use it for anything else.
3. The console wakes only while something moves: a burning match, a match
   that asks, or a change that still plays. Then it draws 10 times a second;
   else it draws only on a new state or a key.
4. Motion comes from the clock, not from a frame count, so the same moment
   always draws the same frame (the replay depends on it).
5. With `--no-motion` or `NO_MOTION=1`: `✦` in `flame`, `╿` in
   `match-head`, `│` in `faint`, no change plays.
6. Every place that shows an agent uses this match: the queue, the sessions,
   the replay, the site, STOKED (the stoker's run), the docs, and later the
   tmux status line. A new place uses the same glyphs and motions.
   A new state is a change to this section first.

## 5. Motion

| Motion | Time | Use |
|---|---|---|
| Flicker | 200 ms a step | a burning match (§4) |
| Breath | 2.4 s | a match that asks (§4) |
| Strike | 800 ms | a session starts to work (§4) |
| Out | 400 ms | a session stops (§4) |
| Burn | 600 ms | a session reaches the compact limit (§4) |
| Fade | 140 ms | new values, menus |
| Rise | 180 ms | the confirm bar, toasts |
| Fall | 560 ms | the start screen (§3) |
| Stamp | 240 ms | a confirmed destructive action |
| Heal | 240 ms | a corrected figure shows "was X" |
| Pulse | 1.6 s | a value that is not measured yet |
| Knock | 240 ms | a device reached the pair page's code: the box moves one cell left and back, twice |
| Open | about 1 s | a device claimed the code: the drawer slides out (260 ms), the match comes out (280 ms), strikes (150 ms, ease-in) and stands (320 ms) while the drawer shuts (260 ms) |
| Lock | 180 ms | the app found a pair code: its outline moves to the corners of the code |

There are no spinners. `--no-motion` and `NO_MOTION=1` make every motion an
instant change; on the site, `prefers-reduced-motion` does the same. In the
app, iOS Reduce Motion does the same. The app holds the stood frame for
240 ms before the console slides in from the right (300 ms).

**Haptics** (the app): lock is a light impact, the strike is a rigid
impact, a stand is success, and a failure is error. Haptics play with
Reduce Motion.

## 6. Console layout

- **Screen** (design/0006, owner 2026-10-12): top to bottom, the header,
  the tabs, a hairline, the body of the open tab, the dock when it fits,
  a hairline and the key line.
- **Header:** one line. The mark, the name and the host on the left (a
  phone drops the host when it does not fit); on the right `● N wait for
  you` in the accent (a tap selects the first one) and the stoker word:
  `stoker off` in `faint`, `stoker` and `☾ night → 07:00` in the stoker's
  token, `stoker rests` in `muted`. For 2.5 s after an action a toast
  takes the right side (`ran: kill 4242`, `sent to flint-auth`).
- **Tabs:** one row, every tab named: `SESSIONS 14  ships  machine  git
  history  panes`. The open tab is uppercase in the accent with its rule,
  the others lowercase in `faint`; Sessions carries its count, Ships the
  ships in flight, and Machine `!` in `neg` while a detached busy loop
  burns. When the names do not fit,
  the tabs far from the open one shorten to their digit, the nearest last.
  The digits follow the order: `1` Sessions to `6` Panes; `p` opens Ships.
  The console opens on Sessions.
- **Sessions** holds what the Queue tab held: the doors on top, then the sessions in three groups,
  each under a faint label and its count: `WAITS FOR YOU` (asks, waits,
  done, paused; the oldest wait first), `WORKING` (busy, agents, a shell:
  `tests` when it runs a test runner) and `QUIET` (idle, cold, stale, out;
  the newest first). A sort other than the default drops the groups for
  one flat list named by its sort (`14 SESSIONS · by context ▾`). The
  recommendations come last (owner 2026-10-12), under
  `RECOMMENDATIONS · N · h hides`: `d` or a tap on `d dismiss` hides the
  selected one until its evidence changes, `h` folds the section to
  `RECOMMENDATIONS · N hidden · h shows`; both are kept in the state
  directory.
- **Rows:** the selection mark `▌`, the match, the name, then the state
  word (accent for who waits), the wait and the context (`warn` from 70%,
  `neg` past the line). A desk row adds the plan (or `! compact`, `▲
  clear` when one is due) and the ship with its track (`#4551 ●━◉┄○┄○`);
  with no ships read, the worktree's pull request and the tmux place. On a
  phone the selected row opens in place, on the wash: what the agent said
  last in quotes (two lines at most), its ship with its note, then its plan
  and its keys.
  The list scrolls so the opened row is on the screen.
- **Detail:** on an opened phone (60–99 columns, split at the middle) and
  a desk (100 and over, the list in ≈78 columns) the selected row's detail
  sits right of the list: the name, the state in words with the wait and
  the place, `CLAUDE SAID` and the last message (six lines at most), the
  context and plan bars, the ship, the subagents and the background
  shells, the figures (model, tokens, account, pid) and the keys. A door's
  diff that waits for its `y` takes the full width.
- **Dock** (design/0006 §7): usage, the machine and the stoker, between
  the body and the key line, when the body, the dock and one blank row fit;
  else it folds away and the Machine tab has it. With rows to spare it
  grows into them (forecasts and resets, the top processes, the stoker's
  recent steps): no run of three blank rows while a panel has more to say. Usage is each account's windows
  as `/usage` says them: `5h`, `wk` (and any other window), a bar (`warn`
  from 60%, `neg` from 90%), the percent and the reset, then the forecast
  (`lasts the week` in `muted`, `out Fri ~20:00` in `warn`) and `as of
  14:02` in `warn` for a reading over an hour old; no reading says `not
  measured · <the fix>`. The machine is cpu with its last minute and the
  load, memory, the disk (`▰▰▰▰▰▰▱▱ 71% of 1 TB`: the file system of the
  home, and `/ 40%` when it is another), and the agents that run. Claude
  Code's status line has no week per model, so there is no `opus` line
  (checked 2026-10-12, Claude Code 2.1.296). A phone shows one panel;
  `u` or a tap on `machine ›` switches. Matches and sparks (#38) are gone
  (owner, 2026-10-12).
- **Machine tab:** usage first, then the machine, then the detached busy
  loops (their rows, `⏎` and `x`) and the top processes.
- **Regions:** a small uppercase label in `faint`, a hairline and space.
  Panels have no boxes.
- **Pull requests** (owner, 2026-10-08): the open pull request of an
  agent's worktree shows as `#N` on its desk row, under its opened row on
  a phone, and with its title and URL in the detail, while no ship of the
  session is read (a ship wins). The git scan asks `gh` for the open ones
  of each repository; a fork's pull request never marks a branch of ours.
- **Ships** (design/0006 §8): tab `2`, or `p`. A strip on top says plainly
  why ships may be behind: `paused · GitHub's rate limit is low, ships read
  again at 14:33`, `GitHub cannot be read: <why>`, `not read: <repo>:
  <why>`, in `warn`. Under the faint label `open → ci → green → merged →
  live`, each ship is two lines and a blank one: `▌#4551 <title>` with its
  age on the right, then the track `●━●━◉┄○┄○` and the note. Done stages
  `●` and `━` in `muted`; the current one `◉` in `warn` while it runs, in
  the accent when it waits for you, `✕` in `neg` when stuck, and `●` in
  the accent once live; the rest `○` and `┄` in `faint`. The note takes
  the colour of the current stage. On a phone the selected ship adds `by
  <session>` and its keys; wider, the detail shows `#N by <session> ·
  <repo>`, the title, `TRACK` with the five stages down (their times, the
  note on the current one, `┃` between done stages, `┆` after), the URL and
  the keys. `⏎` selects the driving session in Sessions; `x` on a ship
  that waits for your merge shows `RUN gh pr merge <n> --<method> -R
  <repo>` and runs it after a typed `y`; `a` on a stuck ship opens the
  answer peek on its session.
- **Peek** (design/0006 §6, owner 2026-10-12): `a` on any session with a
  pane, or a tap on the selected one, opens the conversation: an answer to
  a session that waits, a message to any other. Where there is a detail
  pane (an opened phone, an upright drawer, a desk) it is that pane: the
  list stays lit beside it, its selection following the peek, the title
  says the state, the typing line and the footer sit at the pane's foot,
  and `esc` gives the pane back to the detail. On a one-column phone it is
  a panel over the screen, everything else redrawn in `faint`, full width
  up to 72 columns (centred past that), its foot on the line above the key
  line's hairline; from 60 columns it is a
  box in the middle, 62 columns at most. A rounded `hair` frame; the title
  in it, `<name> asks` (the question tool, a question, a permission),
  `<name> said` (it waits) or `To <name>` (it does not), in the accent and
  `muted`, with `· 1 of 2` in `faint` for
  several questions. Inside: the last message whole up to 8 lines on a
  phone and 9 wider, its end kept and a leading `…`, light Markdown as
  plain text; then the question tool's question in
  `text` and its options, `› 1 <label>  <description>` (the cursor's on the
  wash, `[✔]` ticks for several choices), the last always `Type an
  answer…`; then, when typing, a `├─┤` rule and `› <text>▏`. The foot is
  the keys: `⏎ send` (`⏎ question 2` before the last question, `⏎ open` at
  a permission prompt) `─ ␣ tick ─ ← prev  3/7  next → ─ esc`, each a
  tap target, the place in the list when it fits. `←`/`→` (a phone's
  horizontal swipe) and `⇥`/Shift+Tab move the peek along Sessions (who
  waits first, wrapping); every letter is text, `n` and `p` too. `⏎` sends with no
  second confirm (the peek is the review); options go to Claude Code's
  menu as its keys (digits, `Down`, `Enter`, `1` on the review page),
  never as typed text. After a send the toast says `sending to <name>…`,
  then `sent to <name>` when the keys went, or the key line says `not sent
  to <name>: <why>`; the peek moves to the next that waits, with nothing
  picked and no key taken for 400 ms, or closes.
- **Key line** (owner, 2026-10-12, reversing 2026-10-08): the last line,
  under a hairline. The keys of the open tab and the selected row, each a
  tap target (`⏎ open  a answer  p ships  f stoker`, `a message` on a
  session that does not wait); a desk adds `↑↓
  move`, `/ find` and `q quit`, and "sampled N ago" on the right. It is
  also where the person acts: a tap shows `tap again: <command>`, a pending
  action shows `RUN <command>` and its confirm, `/` the search, and the
  stoker asks to take over here. A phone app that
  shrinks the terminal for its keyboard keeps the bottom row above it.
- **Widths:** the shape picks first. A cell is about twice as tall as it
  is wide, so a screen is portrait when rows × 2 ≥ columns. Portrait under
  90 columns is a phone: one column, the selected row opens in place; from
  70 columns its rows add the plan and the ship. Portrait from 90 columns
  (a phone held upright with a small font: 107×60, 107×101 with the
  keyboard down) splits the list and the detail at the middle, the detail
  a drawer on the right, no row opening in place. Every portrait screen
  has one dock panel at a time (`u` switches, the fold rule unchanged) and
  a peek at the bottom, 72 columns at most, centred; its layout holds when
  the keyboard comes and goes. A landscape screen goes by its width: under
  60 columns (phone) as above; 60 to 99, the list and the detail split at
  the middle, usage and the machine side by side; 100 and over, the list
  with every column and the detail beside it.
- **Rows:** each list row spans its pane, the selection wash too. Its
  columns are fixed tracks computed once per render from the pane's width
  (the word, the wait, the context, then the plan and the ship or the
  place when the row is 70 columns or more), so each starts at the same x
  on every row; the name takes what is left, cut with `…`. A ship's number
  is padded to the widest one read, so titles and tracks line up.
- **Selection:** the selected row is on the `wash` across its whole pane,
  every cell of it, with `▌` and its name in the accent; every other row's
  name is `muted`, so the cursor is the brightest thing in a list. On a
  one-column phone the opened block (the row and what it grows by) stands
  between two hairlines. At night the wash is `night-wash` and the accent
  stays.
- **Keys:** each key is on a phone SSH key bar: digits, arrows, `Enter`,
  `Esc` and letters. No `Ctrl` combinations. `Enter` never runs a
  destructive step; that needs `x` and then a typed `y`.
- **Progress** (owner, 2026-10-08): the bar an agent draws in its last reply
  with text (`Progress [████░░░░] <step>`) shows as `▰▰▰▱▱ 75%`: on a desk
  row when no action is due, under the opened row on a phone (`plan 75%`),
  and with its step in the detail. The
  SessionStart hook asks each new session for that line
  (`progress_prompt`, on by default). The console never types it into a pane.
- **Age** (owner, 2026-10-08): an idle session is `idle`, then `cold` from
  one day, then `stale` from seven days. Under a day, a session that waits
  shows the word of its queue row: `asks`, `waits`, `done`, or `paused`
  when its turn ended with its progress bar under 100% and no question
  (owner, 2026-10-08). A turn that ends on a question waits; it is never
  `done`. The oldest sink to the bottom of `QUIET`. On a stale session `x` and a typed `y` end the agent;
  on another session `x` says when it works. Agents that exited and wait
  for their parent (zombies) are one faint line under the sessions
  ("137 exited agents · e shows them"); `e` lists them with the parent that
  keeps them and the command that clears them. They never count as sessions.
- **Layers** (#23, as in k9s): Hosts sits above the console of one host.
  Hosts is `HOSTS · N`, one row each: `this machine`, each added host,
  `+ add a host`; the header says `▰ matchblox · hosts`. `Enter` opens a row.
  In a console, `esc` with nothing to cancel or clear goes back to Hosts.
  `+ add a host` lists the hosts of `~/.ssh/config` and `type a host…`; the
  pick shows `matchblox connect <host>` and runs it after a typed `y`. The
  first run asks "Where do your agents run?" (`this machine`, `another
  machine`) once. A console of a host shows the host in the header, `stale`
  in `warn` while the connection is lost, and before its first state only
  the name, never zero figures. A host the console cannot reach says why and
  shows the connect (design/0005-remote-mode.md §3.5).
- **Doors** (#25): the setup steps sit above the sessions under `SET UP · N`,
  one row each: the title, then why in `muted` (a door that cannot open shows
  the fix in `warn`). On a phone only the selected door shows why. `Enter`
  shows the exact diff (`+` in the accent, `-` in `neg`) or the exact argv
  under the doors, and the key line names the door. A diff longer than
  the screen scrolls with `↑↓`, and `y` works only after its last line was
  on the screen. A door with a choice shows it above the diff, off:
  `[ ] m <label>`; `m` turns it on and the diff shows what it adds (the
  way back's tmux mouse, owner 2026-10-10). A typed `y` opens it,
  `x` and `y` close it. A door that is done folds away at once. `matchblox
  setup` shows every door, done and closed ones too. An agent that waits is
  selected before a door; with nobody waiting, the first door is.
- **The stoker** (#24, owner 2026-10-09): `f` turns it on or off, `n`
  starts a night that ends at `night_ends`. The header shows `stoker` or
  `night → 07:00` in `accent` after the host. The stoker lives in the
  dock (design/0006 §7, owner 2026-10-12): its panel says its state, the
  score and the last step, and takes the dock (or the machine's place
  beside usage) while a run is in charge or has steps the builder has not
  seen. `c` or a tap on it opens the card `STOKED <since> → <end> · N` in
  the right pane, or on a phone's Machine tab: one line for each step
  (time, the session's match, name, pane, then `before → after` or the
  skip reason), the `RESUME:` line under each compact, two lines for each
  step in a pane under 60 columns. `Enter` marks it seen and folds it. Its
  events show as the header toast. The night door ("Try a
  night") sits with the doors and folds after the first run.
  While a run is in charge the whole console wears the night (§2.1) and the
  word takes the `night` token. The first key or second tap that acts on a
  session (an act's `Enter`, `x`, `a`) asks on the key line `The stoker
  is on. Take over?  y rests it while you work · n keeps it on`, once a run
  (#45, owner 2026-10-11). What was pressed is held and runs after the
  answer; any other key drops both. `y` rests the stoker: the day look,
  `stoker rests` in `muted`, `f` wakes it; it wakes itself 15 minutes after
  the builder's last key in tmux (any client, any pane) or a console, or 2
  minutes after the last console closed (a dropped link or a restart of the
  service is not leaving). Every run that ends leaves
  a card, 0 steps included: `NIGHT|STOKED <since> → <end> · ready N/M`, then
  `N watched · N were full · N compacted · N waited · Nk tokens given back`
  (two lines on a phone), then each session of the start that is not ready
  with why in `warn`, then each one it chose not to compact with why in
  `muted` (`69% · finishes ~79% · left alone`), then the steps, each with
  the verdict that called for it under it in `muted`. Ready: alive, not
  stopped by the plan's limit, not waiting, not past the line. The stoker
  decides each session by its pace (#59, owner 2026-10-12): tokens per point
  of its progress bar against compact_at; one that finishes under it is left
  alone, one whose next 10 points would cross it is compacted at the end of
  a step (a turn that moved its bar), one past it at once; without a pace
  compact_at or compact_tokens, the first. A rest is the transcript's turn
  end, not Claude Code's busy flag (background work keeps that busy); a
  session resting while its subagents work reads `agents`, not idle. A
  limit that stopped sessions during
  the run is the card's first line, in `warn`: `limit out <from> → <reset>`;
  a session it stops is named `out until <reset>`. Every card is kept in
  `stoker-runs.jsonl`.
- **Lists** (owner, 2026-10-08): `/` searches the open tab (every key is
  text until `⏎` keeps it or `esc` clears it; a tab change ends it); an empty
  result says "nothing matches <text>". `s` sorts Sessions and Panes by the
  next column and `S` turns it around; on Panes a tap on a column header
  does both. Sessions start grouped (above); another sort names itself in
  the list's label (`by context ▾`, `▴` when turned around). On Git, `space` marks a worktree that is
  safe to remove (or says why not), `X` marks every safe one, and `x` with a
  typed `y` removes the marked ones: one guarded act each, then one summary
  ("removed 11, skipped 1: it has changes").
- **The door "Pair a device"** (design/0009): under `SET UP`, with the why
  `answer your agents from your phone`. `Enter` opens the pair page. It
  shows no diff and no argv: the page is its preview. It folds when one or
  more devices of kind `phone` open this host. `x` and a typed `y` close it,
  as every door. `matchblox setup` shows it with the other doors.
- **DEVICES · N** (design/0009): on the Machine tab, after the detached busy
  loops and before the top processes. One row for each key that opens this
  host, phones first: kind, name, fingerprint. The last row is `+ pair a
  device`, and `Enter` on it opens the pair page. `x` on a device and a
  typed `y` remove it, with its backup (the flash names it), as `matchblox
  devices rm` does. With no devices only the `+` row shows. In a console of
  another host, `DEVICES` has one row, `run matchblox pair on <host>`.
- **The pair page** (design/0009): a layer over the console, as the peek is,
  and it takes the whole screen. It shows the front-on box (§3) with the QR
  code on its label, then `Scan with the matchblox app, or paste the link.`
  and the countdown in `muted`, then the key line `c copies the link · esc
  stops`. It fits 100 × 40. In a smaller terminal the code shows without the
  box. Under 61 × 33 the page says `Make this terminal 61 × 33 or larger to
  show the code, or press c to copy the link.` It polls each 100 ms. The
  steps: the phone connects (the knock, `A device is here…` or `<name> is
  here…`), the device claims the code (the opening, then `Paired <name>
  (<fp>). Not yours? x on it in DEVICES.`), and in the console two more
  beats. At beat 3, `<name> is in.`: the page folds to a one-line strip on
  the key line and the keys go back to the shared console, so the device can
  answer. `esc` with nothing else to cancel dismisses the strip. At beat 4
  the strip says once `<name> <verb> <session>` for 1.5 s. Without beat 3 the
  page folds after 60 s. `matchblox pair` in a terminal is the same page as a
  program of its own; without a terminal it prints the plain QR and link.
- **The device guard** (design/0009, owner 2026-10-21): a device sees
  `DEVICES` but cannot pair or remove. The console asks tmux for the client
  active last; when its tty is a device's, `Enter` on the door or on `+ pair
  a device` says `Pair from the host's own terminal.` and `x` on a device
  says `Remove devices from the host's own terminal.` It guards against a
  mistake, not an attacker.
- **After a jump** (owner, 2026-10-08): when `Enter` takes the person to an
  agent, the console selects the next session that waits, else the next
  idle one, so the way back lands on it.

## 7. Copy

- Plans, specs, docs and on-screen text use ASD-STE100: short sentences,
  one instruction each, active voice.
- Docs, the README and the site lead with what the reader gets and what to
  do. They never explain why matchblox is built the way it is, defend a
  choice ("never a guess", "so that…") or praise matchblox. The why lives in
  design/ and the ledger.
- A status is a glyph and a word: `✦ busy`, `╿ asks`, `! compact`,
  `▲ 92 °C`.
- An empty state is one sentence (and the action, when there is one):
  "nothing waits for you".
- An error tells the fix: "the service (pid 4242) does not answer: kill
  4242, then start matchblox again".
- Every command the console runs is shown before it runs, as the exact argv.
- The pair page and the app's pair screens (design/0009), exactly:
  - Page: `Scan with the matchblox app, or paste the link.`, `c copies the
    link · esc stops`, `link copied`, `A device is here…`, `<name> is
    here…`, `Paired <name> (<fp>). Not yours? x on it in DEVICES.`, `<name>
    is in. Answer an agent from it, or esc to go back.`, `<name> <verb>
    <session>`, `The code expired. Enter pairs again.` and `The code
    expired. Run matchblox pair again.`, `Stopped. The code no longer
    works.`, and under an offer that failed, `Enter tries again · esc goes
    back`. In the console, under the key line when a row is free: `Port 22.
    For another port, run matchblox pair --port <n>.`
  - Guard: `Pair from the host's own terminal.` and `Remove devices from the
    host's own terminal.`
  - Door: `Pair a device`, `answer your agents from your phone`.
  - App: `Run matchblox pair on your host, or open Pair a device in its
    console.`, `That is not a matchblox pair link. Copy it again with c on
    the pair page.`, and the system's `Paste` button.
  - App failures have no Pair button, because the scan is the consent: a
    refusal that needs a new code ends `Scan the code again.`; a host key
    that does not match says `<address> is not <host>. Check the address,
    then tap Try again.`, with the Address field. The messages say `this
    device`, not `iPhone`, because the app runs on an iPad too.
- Public text names no real machine, person or company. The demo host is
  `ws-1`. Its sessions build an invented app, Hearth: `hearth-api`,
  `flint-auth`, `spark-docs`, `kindle-web`, `ember-billing`, Codex in
  `cinder-ci` and OpenCode in `wick-mobile`, on `example.com` accounts
  (#51, owner 2026-10-11).

## 8. The site

- **The story:** once, everyone carried a matchbox to light a fire wherever
  they went. matchblox is the one you carry to every terminal: the fire is
  an idea, and keeping it burning is to keep building it. **One fire for
  each view:** only the hero's match burns; other motifs are the box, the
  drawer and the striker.
- **The first screen:** the header; the hero, with the news line ("New ·
  <what is new> · Changelog →", to `CHANGELOG.md`), the line ("Light an
  idea. Keep it burning."), one sentence and the install block on the left
  and the strike canvas on the right; and at the bottom centre the invite
  to scroll: a closed matchbox whose drawer slides out, over "Open the box".
  On a phone the canvas comes first and the invite follows the install.
- **Tags:** a feature that is not released yet has the tag "Soon".
- **Then:** the replay of the real console, which starts when it scrolls
  into view; three short blocks; the motto "Light the match. Keep it
  burning."; the links; the footer (From Blox Engineering · Docs · GitHub ·
  Discord · Changelog · bloxng.com). The docs (docs.matchblox.com) use the
  same tokens and type, and load nothing from another site.
- **Then:** the replay plays on an iPhone Duo in the middle of the stage, a
  CSS body with a notch. Folded it plays the phone (44×31); open it plays
  the duo (104×38, landscape, the console's desk layout) at about the same
  height. From 600px, where the open Duo is half as wide again, it unfolds
  sideways once, partway through; narrower, it stays folded and readable
  unless the visitor opens it. A tap, Enter or Space folds and unfolds it,
  and the run carries on from the same moment; the stage keeps its tallest
  height, so the page below never jumps. Each line keeps its columns: a
  glyph Plex Mono lacks gets a one-column cell (in the still frame too),
  and the font shrinks to the largest size that fits the glass. Without
  script the open Duo's still frame stays; with reduced motion the phone's
  last frame stands, folded, and a tap shows the duo's.
- **README:** the same recordings, as `www/demo/replay-desk.svg` and
  `www/demo/replay-phone.svg` side by side; the docs show the desk's.
- **Type:** IBM Plex Sans (variable) for text, IBM Plex Mono 400 and 500 for
  commands, labels and the replay. Both are served from the site.
- **Install block:** one framed block with tabs (Script, Go), a `$` prompt,
  the command, and Copy joined to it; under it the platform and "read the
  script first ↗". On a phone the command wraps; it is never cut.
- **Replay:** recorded frames of the real console (`TestReplay`), drawn in
  the site's tokens, so it follows light and dark. It walks one evening
  into the morning: who waits, an answer typed with `a`, the sessions, the
  usage and a compact from the week's recommendation, a merged worktree
  removed, a busy loop killed, then `n`, "Take over?", the stoker's compact
  at 23:40 and the night's card at 07:00 (#51). The demo service runs on
  the demo clock, so the night and its card are real. A still frame is in the
  HTML for no script and for reduced motion.
- **Rules:** everything loads from the site itself: `script-src 'self'`,
  `connect-src 'self'`, no inline script or style, no third-party resource.
  No horizontal scroll at 390 px. Check light and dark at 390 and 1280 px.
