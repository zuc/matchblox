# The console

From the top: the header, the tabs, the open tab, then usage and the
machine, and at the bottom the key line.

The header shows the host, how many agents wait for you (`● 3 wait for
you`; a tap selects the first) and [the stoker](stoker.md): `stoker off`,
`stoker`, or `☾ night → 07:00`. For a moment after an action it says what
ran instead.

The tabs are `sessions`, `ships`, `machine`, `git`, `history`, `panes` and `stoker`,
opened by the digits `1-7`. The open one is uppercase. A narrow screen shortens the
tabs far from the open one to their digit. `machine !` has a problem.

The key line at the bottom shows the keys of the open tab, and how old the
sample is. A tap on a key presses it. It is also where you confirm a step
or type a search. See [Keys](keys.md).

## Usage, the machine and the stoker

Under the open tab, when there is room, the dock shows how much of each
plan your agents have used, the machine, and [the stoker](stoker.md):

```
 USAGE                                    │ MACHINE        ws-1 · 16 cores
 ✻ Max    5h   ▰▰▰▰▰▱▱▱ 62%  · 16:00      │ cpu  ▃▄▅▄▃▅▆▅ 38%   load 4.2
          wk   ▰▰▰▱▱▱▱▱ 41%  · Thu 09:00  │ mem  ▰▰▰▰▰▱▱▱ 41 of 64 GB
 ❋ Plus   5h   ▰▱▱▱▱▱▱▱ 12%  · 18:40      │ disk ▰▰▰▰▰▰▱▱ 71% of 1 TB
          wk   ▰▰▱▱▱▱▱▱ 30%  · Sat 11:00  │ runs 13 claude · 1 codex
```

- Each account shows its windows as `/usage` does: `5h` is this 5-hour
  window, `wk` this week, each with the percent used and when it resets.
  A bar turns `warn` from 60% and `neg` from 90%.
- The week ends with whether it lasts at your pace so far. Hours you do not
  usually work count as a tenth: set them with `quiet_hours` and
  `quiet_days` in the [config](config.md).
- `out Fri ~20:00`: the recommendations name the session of that account
  that burns the most. `Enter` jumps to it, and `x` compacts it while it is
  idle. The row then says what happened, or what failed and that the key
  tries again.
- On a phone the dock shows one panel at a time. `u`, or a tap on the
  word at its right (`machine ›`), goes to the next: usage, the machine,
  the stoker. While a run is in charge, or has steps you have not seen, the
  stoker takes the dock until you switch.
- Wider, two panels side by side: usage and the machine, or usage and the
  stoker while it claims the dock. A wide desk shows all three.
- When the body leaves rows free, the dock grows into them: each account's
  forecast and resets, the top processes, the stoker's recent steps. The
  detail beside the list shows more of what the agent said.
- `c`, or a tap on the stoker's panel, opens the STOKER tab: your words, its plan with each step's outcome, its standing rules and today's steps.
- `disk` is the file system that holds your home directory, and `/` beside
  it when that is another one.
- Claude Code's status line gives the 5-hour window and the week only, so
  there is no `opus` or `sonnet` line.
- When the screen is short (a phone with its keyboard up), the dock folds
  away. The Machine tab has all of it.

Where the figures come from:

| Agent | What to do |
|---|---|
| Claude Code | Open the door "Show your limits" in Sessions. It adds matchblox to your status line; a status line you have keeps showing what it showed. |
| Codex | Nothing: Codex reports its limits after each turn. A plan with a monthly limit shows `mo`. |
| OpenCode | It shows "not measured": OpenCode keeps no limits. |

A line that says `as of 14:02` has not changed for an hour: the account's
agents have not worked since. Each reading is kept for five weeks in
`~/.local/state/matchblox/limits.jsonl`, on this machine only.
`matchblox status` has every figure under `limits`.

## Read it from a script or an agent

`matchblox sessions`, `matchblox ships` and `matchblox usage` print one line
a row; with `--json` they print JSON. `matchblox ships --json` and
`matchblox usage --json` print the ships and the accounts' limits. They need
`matchblox serve` running (or a console), and else say so. Each row of
`matchblox sessions --json` has:

| Field | It is |
|---|---|
| `name` | The session's name (Claude Code's, or the agent's). |
| `pane`, `target` | Its tmux pane (`%3`) and target (`app:1.1`). Any of `name`, `pane` and `target` names it to a hand command. |
| `word` | What it does, in one word: `asks` (it waits for an answer or an approval), `busy` (a turn runs), `idle` (at rest), `background` (at rest, and a shell it started in the background still runs), or `agents` (its subagents still work). Read this one. |
| `status` | The status word the agent itself reports, as it reports it. Claude Code says `busy`, `idle` or `shell`; `shell` is a session at rest with a background shell, not a pane without an agent. |
| `reason` | Why a session at rest is idle (`#4556 waits your merge`, `CI red · …`, `stuck: …`, `out of work`, `idle`); empty while it works or asks. |
| `held` | Nothing types into it: you parked it, or the stoker held it. |
| `stoker` | It is the stoker agent, on matchblox's own tmux server. Sessions leaves it out. |
| `context_pct` | How full its context is, in percent. |
| `plan_pct` | The progress bar it drew in its last reply, or null. |
| `account` | The account its plan runs on. |
| `ship` | The pull request it drives (`repo`, `number`, `stage`, `kind`, `note`), or null. |

`matchblox peek api` prints what the session said last and what it asks,
if anything: the question with its numbered options, or an approval that
waits in its pane. It is what the console's peek shows, and it acts on
nothing. `--lines 40` keeps the last 40 lines of what it said (20 by
default, 0 for all); `--json` prints `name`, `pane`, `target`, `said`,
`asks` (the first line of the question), `questions` (each with its
`options`) and `approval`.

## Act on a session from a script or an agent

`<who>` is a pane (`%3`), a target (`app:1.1`) or a name; a name two sessions
share is refused, never guessed. Each command asks the running service, which
checks what the console checks, and prints the exact keys it sent; `send`
starts with `send api: typed`. A refusal is printed on stderr, with exit
code 1, and names the command and why (`send api: refused: …`). A send
whose text went in and whose `Enter` a guard or tmux stopped (a permission
prompt came up between the two) says `send api: typed, not sent (Enter not
pressed): …`: the text waits in the prompt, so do not send it again. When the
service did not answer, the command says whether the act went: `send api:
not typed: …` when it never reached the service, and `send api: not known
whether it was typed: peek api before you send again` when the service
took it and the answer never came.

| Command | It does |
|---|---|
| `matchblox send api "run the tests"` | Types the line and Enter into the session. It queues if the session works, and is refused at a question or a permission prompt. |
| `matchblox send api --pick 2` | Answers the question the session asks with its second option. |
| `matchblox compact api` | Types `/compact` into a Claude Code session at rest that waits for nothing and was not compacted in the last 30 minutes. |
| `matchblox hold api --why "not the priority"` | Queues "finish this step, then stop and wait" and holds the session: nothing else types into it. |
| `matchblox release api` | Lets a session the stoker held go, and tells it "carry on". A session you parked stays parked: only `z` at a console lets it go (`parked by you: only z lets it go`). |
| `matchblox new "fix the flaky test"` | Starts Claude Code here, in a tmux session of its own named after the directory, with the words as its first message. `--where <path>` starts it elsewhere, `--agent codex` (or `opencode`, `cursor-agent`) starts another agent, `--branch <b>` first adds a worktree for a new branch beside the repository (`<repo>-<b>`, from origin's default branch, tracking nothing), and `--name <name>` names it. The tmux session, its window and Claude Code's own name are that one name. A first message that starts with a dash is refused: the agent would read it as a flag. |
| `matchblox rename api ships-flake` | Names the session, its tmux window, its tmux session when it is that session's one window, and Claude Code (`/rename`), all alike. A Claude Code session that works takes its name when it rests (`api renames when it rests`). A name is one word of letters, digits, dashes and underscores, and no other tmux session may have it. |
| `matchblox kill api` | Ends the session: `tmux kill-pane`, then a `TERM` to its agent if it still runs. It is refused while the session works or waits for you: `--anyway` kills it all the same. Its worktree and its transcript stay. |

An act typed in a pane of the stoker's tmux session is the stoker's, and so
is an act from a session the stoker messaged in the run: what that session
sends counts toward the run's messages, and the session it sends to is the
stoker's from then on. Such an act is refused when no run is on (`the
stoker is off: no run`), while the stoker rests (`the stoker rests: a person
works`), on its own pane, and after `max_steps` messages in one run (60 by
default).

The launch sheet's and the STOKER tab's verbs, and `z`, are yours at a
console only. A command or an agent that sends them is refused (`only you
do this, at the console`).

The stoker agent keeps its plan, logs its steps and sets its triggers
with commands of its own. matchblox takes them only from the stoker's
pane.

| Command | It does |
|---|---|
| `matchblox stoker plan` | Reads the plan on stdin (a here-document, `matchblox stoker plan <<'EOF'`), one step a line (`1. api: merge #4556 on green`), and puts it on the card in place of the plan before it. Nobody approves it. An `asks:` or `grant:` line, which older briefs asked for, is read and ignored: the gos are your chips. |
| `matchblox stoker step 1 --state doing "told api to rebase"` | Logs a step of the plan to the card: `doing`, `done`, `failed` or `waits`, and prints `step 1 logged: doing`. `--needs merge:#4556` asks you for a go the step needs: the stoker only asks, and only your `y` gives it; a guarded command the guard denies lands on the card by itself. |
| `matchblox stoker step 0 --rule "compact on PR" --state done "compacted api"` | Logs what a standing rule of your ways did. |
| `matchblox stoker when --ship 4573 --stage merged send api "rebase on main"` | Sets a trigger matchblox fires itself, once: at a pull request's stage (`ci`, `green`, `merged`, `live`, `red`) or, with `--rest <who>`, at the end of a session's turn. The hand is `send`, `compact`, `release` or `hold <who> --why "…"`. `--list` prints the run's triggers, `--drop <id>` drops one. |

## The match

Each agent row starts with a match:

| Mark | Word | It means |
|---|---|---|
| `✦` | busy | The agent works. The match burns. |
| `✦` | agents | Its turn ended and its subagents still work: their results start its next turn. It does not wait for you. |
| `╿` | asks, waits, done, paused | The agent waits for you. The match is at rest. |
| `│` | its state | The context is full (at `compact_tokens`, 350k by default, or `compact_at`, 85% of its window). The match is spent, and Sessions says `! compact`. |
| `╿` or `│` | out | The plan's limit stops an idle session (`│` when it is also full). The next column says when it can work again: `19:50`, or the weekday when that is more than a day away. A session left a day or more keeps its word: `cold`, `stale`. |

## 1 Sessions

The console opens here. On top, while there are some: the setup steps
(`SET UP`). Then every agent, by what it needs from
you, and last the recommendations, each with its evidence and its exact
command. `d` dismisses the selected recommendation until its evidence
changes; `h` folds them all to one line, and `h` again shows them. The
console remembers both.

- **WAITS FOR YOU**, the oldest wait first:
  - **asks**: the agent asks for a permission, or asks with its question
    tool.
  - **waits**: the agent asked a question, or waits for input.
  - **paused**: the turn ended with its progress bar under 100%.
  - **done**: the turn ended.
- **WORKING**: `busy`, `agents` (its subagents work), `tests` or `shell`
  (a shell it left running).
- **QUIET**: `idle`, then `cold` after one day, then `stale` after seven
  days, the newest first. On a stale session, `x` and a typed `y` end the
  agent.
- **HELD**, last: sessions you parked with `z` (`⏸ parked by you`) or the
  stoker holds (`⏸ held by stoker · why`). Nothing types into them.

A session at rest that does not ask you says why where its state word
would be, in a few words: `#4556 waits your merge`, `CI red · lint`,
`stuck: …`, `back 14:20`. It replaces `done`, `paused` and `idle`.

A row shows the match, the name, the state, how long it waits and its
context use (`warn` from 70%). A wide screen adds the plan and the ship the
session drives with its track (`#4551 ●━◉┄○┄○`, see [Ships](ships.md)); with
no ships read, the open pull request of its worktree (`#N`) and its tmux
place.

On a phone the selected row opens: what the agent said last, its ship, its
plan and its keys. The console reads the screen's shape (rows × 2 ≥
columns) before its width: a phone held upright under 90 columns is a
phone, and from 90 columns (a small font) its detail is a drawer beside
the list. Wider, the same shows in a detail beside the list, with
the account, the tokens and the background work.

`Enter` jumps to the pane. Come back from a jump and the next agent that
waits is already selected.

### The answer peek

`a` on an agent opens the conversation with it. Where the detail sits
beside the list, the detail pane becomes the conversation: the list stays
lit on the left and follows the peek, and `Esc` gives the pane back. On a
one-column phone it is a panel along the bottom, over the dimmed list. On an agent that waits it answers; on
any other it sends a message, titled `To <name>`, which Claude Code queues
while it works.

```
╭─ flint-auth asks · 1 of 2 ─────────────────────────────────╮
│ The refresh endpoint is in and its tests pass. One choice  │
│ is yours before I wire the rotation: what should a refresh │
│ do with the token it was given?                            │
│                                                            │
│ Should a refresh also rotate the token?                    │
│ › 1 Rotate it  the old one stops at once                   │
│   2 Rotate, keep the old one a minute                      │
│   3 Keep the token                                         │
│   4 Type an answer…                                        │
╰─ ⏎ question 2 ─ ← prev  3/7  next → ─ esc ─────────────────╯
```

It shows what the agent said in this turn, its end up to nine lines (a
longer reply starts with `…`), its Markdown as plain text. When Claude
Code asks with its question tool, its options are numbered and the last
one types an answer; several questions come one at a time, and a question
of several choices ticks with `Space`. In auto mode the options come from
the queue hooks (see [Agents](agents.md#how-an-agent-tells-that-it-waits)),
so add them again when the setup step asks. Otherwise you type one line.

`Enter` sends it to the pane named in the title: the peek is the review,
so there is no second confirm. The options go to Claude Code's own menu
with the keys it takes, never as typed text, and only while the pane shows
the page they answer: if you answered in the pane yourself, nothing more
is sent. The header says `sending to flint-auth…`, then `sent to
flint-auth`; an answer that did not go says `not sent to flint-auth` and
why on the key line. The peek moves to the next agent that waits, or
closes. There no option is picked, and for a moment it takes no key, so a
second `Enter` never answers a question you have not read. `→` and `←`
move the peek to the next and the previous agent, `Esc` closes. Every
letter is text. A permission prompt is answered in its
pane: its peek shows the prompt, and `Enter` goes there.

- Context use is read from the agent's own files, as a share of the
  session's window. Without a window the row shows `—`. See
  [Agents](agents.md).
- A new session shows in Sessions at the next sample, about 2 seconds
  after it starts. A session from `N` or `matchblox new` shows at once.
- The account is the provider's mark, then the email and plan, or
  "API key": `✻` Anthropic, `❋` OpenAI, `▣` OpenCode.
- An agent can draw a progress bar in its last reply:
  `Progress [████░░░░] <step>`. The console shows it as `▰▰▰▱▱ 75%`.
- `s` sorts by another column (`newest` is the one with the least idle
  time first) inside each group; `S` turns it around.
- `g` groups the list by need (the groups above), by state word, by repo or
  not at all (one flat list). `z` parks the selected session, or lets it go.
- Agents that exited and wait for their parent are one faint line under the
  sessions. `e` lists them with the command that clears them.

## 2 Ships

The pull requests your sessions drive, each on its track from open to
live. `p` opens it. See [Ships](ships.md).

## 3 Machine

Usage first, then the machine: CPU per core, load, pressure, memory and
swap, temperature, CPU power limits, GPUs, and the CPU share of the
container groups you name in the [config](config.md).

Then the detached busy loops: shells that keep burning CPU after their pane
is gone, named to the pane that started them. `x` and a typed `y` kill
one. While one burns, the tab says `machine !`.

## 4 Git

Every git checkout that your sessions use: dirty paths, the default branch
behind its remote, and worktrees that are merged and safe to remove.
`Space` marks one, `X` marks every safe one, and `x` with a typed `y`
removes the marked ones.

## 5 History

24 hours of CPU, load, pressure, temperature, memory and swap.

## 6 Panes

Every tmux pane, with its command. `Enter` goes to it.
