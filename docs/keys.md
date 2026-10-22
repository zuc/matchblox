# Keys

Every key works on the key bar of a phone SSH app: letters, digits, arrows,
`Enter` and `Esc`. No key needs `Ctrl`. The key line at the bottom shows
the keys of the open tab, and a tap on one presses it.

## The console

| Key | What it does |
|---|---|
| `↑` or `k` | Selects the row above. |
| `↓` or `j` | Selects the row below. |
| `Enter` | Shows the safe action of the row: go to the agent's pane (Sessions, Machine, Panes), open a shell in the worktree (Git), do a recommendation, show a setup step. On Ships, selects the session that drives the ship in Sessions. |
| `x` | Shows the other action of the row: the second action of a recommendation, close a setup step or kill the session (Sessions), kill a detached busy loop (Machine), remove the marked worktrees (Git), merge a ship that waits for your merge (Ships). Kill shows its commands, `tmux kill-pane -t <pane>`, then `kill -TERM <pid>` for an agent still running, and runs them on a typed `y`. A session that works or waits for you asks for a second `y`. Its worktree and its transcript stay. |
| `N` | Opens **New session** (Sessions, Git): where it starts (a path, or `←` `→` through the Git tab's repositories and worktrees), a branch for a new worktree beside the repository (blank: none), its name, the agent (`←` `→`, from the installed Claude Code, Codex and OpenCode) and a first message (blank: it waits for you). `Tab` goes to the next line, `⏎` starts it in a tmux session of its own, `Esc` sends nothing. The row shows in Sessions within seconds. |
| `r` | Renames the session (Sessions): its tmux window, its tmux session when it is that session's one window, and Claude Code's own name (`/rename`), all alike. A Claude Code session that works takes its new name when it rests. On Git, `r` scans the git checkouts again. |
| `A` | Switches the agent of the session (Sessions): another installed agent (`←` `→`) starts in its pane and directory with a handoff line you may edit or clear. The plan shows, `y` runs it, and a session that works or waits asks for a second `y`. The console jumps to the pane after. |
| `a` | Opens the peek on the selected agent: what it said, and your answer to an agent that waits or a message to any other (Claude Code queues a message typed while it works). The key line says `a answer` or `a message`. On Ships, opens it on the session of a stuck ship. |
| `1-7` | Opens a tab: Sessions, Ships, Machine, Git, History, Panes, Stoker. |
| `p` | Opens Ships. |
| `Tab` or `Shift+Tab` | Opens the next or the previous tab. |
| `u` | Cycles the dock of a phone: usage, the machine, the stoker. While a run is in charge, or has steps you have not seen, the stoker's panel takes the dock until you press `u`. |
| `c` | Opens the STOKER tab: your words and the gos, the plan with each step's outcome, the goes that wait for you, the triggers, the standing rules, today's compacts with their RESUME lines. Opening it marks the steps seen. |
| `/` | Searches the open tab. |
| `s` | Sorts Sessions and Panes by the next column. |
| `S` | Turns the sort around. |
| `g` | Groups Sessions by need, by state, by repo, or not at all. The sort works inside each group. |
| `z` | Parks the selected session (Sessions): nothing types into it, and the stoker leaves it alone. `z` again lets it go. |
| `Space` | Marks a worktree that is safe to remove (Git). |
| `X` | Marks every worktree that is safe to remove (Git). |
| `f` | Turns [the stoker](stoker.md) off while it is on. While it rests, turns it back on. Otherwise opens the launch sheet for a day run. |
| `n` | Opens the launch sheet for a night: the stoker runs until the morning, then turns itself off. While a night runs, `n` ends it. |
| `o` | Gives a new order while a day run is on: the launch sheet opens with the run's gos, and the order goes to the stoker as one line. The run goes on, and done steps stay done. A night is sealed: `n` stops it first. |
| `e` | Shows or hides the agents that exited and wait for their parent (Sessions). |
| `d` | Dismisses the selected recommendation until its evidence changes (Sessions). |
| `h` | Folds the recommendations to one line, or shows them again (Sessions). |
| `Esc` | Clears the search. With nothing to clear, goes back to Hosts. |
| `q` or `Ctrl+C` | Quits the console. The service keeps running. |

## The STOKER tab

`c` opens it, and so does its digit. The first row is the stoker agent,
which matchblox runs on a tmux server of its own. Every other row is a
step the stoker took or planned, a command that waits for your go, or a
trigger that fired. The key line shows the keys of the selected row.

| Key | What it does |
|---|---|
| `Enter` | On the stoker agent's row, opens the stoker (`⏎ open the stoker`): inside tmux in a popup, `tmux display-popup -E -w 90% -h 90% 'tmux -L matchblox attach -t stoker'`, which detaching (prefix `d`) closes; outside tmux the console names the command to run. This is where you answer the stoker's own prompts. On a step, shows the exact command it ran, or the one it waits to run, or hides it again. |
| `Q` | Quits the stoker agent and its tmux session, after a typed `y`: `tmux -L matchblox kill-session -t =stoker`. `f` only turns the stoker off and leaves the agent running, so its context carries to the next order. |
| `y` | Gives the go a step waits for (`⏸ waits for you: api-gw wants to run gh pr merge 4556`). The go is good for one command of that kind in that session, and the stoker hears of it at its next wake. |
| `a` | On a failed step (`✕`), opens the peek on its session. |

## An action before it runs

The key line shows `RUN` and the exact command.

| Key | What it does |
|---|---|
| `Enter` | Runs a safe action. |
| `y` | Runs a destructive action. `Enter` never does. |
| `y`, `n` | Answer "Take over?" while the stoker runs: `y` rests it while you work, `n` keeps it on. |
| `↓` or `j` | Scrolls a long change. `y` works only after its last line was on the screen. |
| `m` | Turns the choice of a setup step on or off: the tmux mouse, in "Add the way back". |
| `Esc` | Cancels. Any other key cancels too. |

## New session, Rename and Switch agent

A tap on a line puts the cursor there, and a tap on an agent picks it.

| Key | What it does |
|---|---|
| `Tab` or `↓` | Goes to the next line. `Shift+Tab` or `↑` goes back. |
| `←` or `→` | On where, steps through the Git tab's repositories and worktrees. On agent, picks the agent before or after; Switch agent leaves out the one the session runs. |
| `Enter` | Starts the session, renames it, or shows the switch's plan. A refusal stays in the form, with why. |
| `Esc` | Closes the form. Nothing is sent. |

## The answer peek

The peek is the review: `Enter` sends what it shows to the pane named in
its title, with no second confirm. Every other printable key, `q` too, is
text on the typing line.

| Key | What it does |
|---|---|
| `Enter` | Sends the typed line, or the option under the cursor. With several questions, goes to the next one first. At a permission prompt, goes to its pane instead. |
| A digit | Picks an option: the question goes on at once. With several choices, ticks it. The digit after the options opens the typing line. |
| `↑` or `↓` | Moves between the options and the typing line. |
| `Space` | Ticks the option under the cursor, when the question takes several. |
| `→` or `←` | Moves the peek to the next or the previous agent, in the order of Sessions (who waits first), wrapping. A phone's horizontal swipe sends them. `Tab` and `Shift+Tab` do the same. |
| `Backspace` | Deletes the last character. |
| `Esc` | Closes the peek. Nothing is sent. |
| `Ctrl+O` | Closes the peek and goes to the agent's pane, as `Enter` on Sessions does. Nothing is sent. |
| `Ctrl+C` | Quits the console. |

## The launch sheet

`n` and `f` open it, and `o` by day. With no stoker agent running,
matchblox starts it as the sheet opens, and the sheet says `starting the
stoker…` until the agent rests. It asks for the order and the gos you
give, and `Enter` starts the run at once. On the order line every
printable key is text.

| Key | What it does |
|---|---|
| `Enter` | Sends the order, and the run starts. While the stoker agent starts, or is busy mid-turn, the order waits and goes once it rests. By day, after `o`, it goes to the stoker as one line and the run goes on. |
| `Tab` | Takes the cursor to the chips: the gos (merges, releases, deploys), then the recipes of your ways file. |
| `Backspace` | Deletes the last character. |
| `Esc` | Closes the sheet. Nothing is sent. |
| `Ctrl+C` | Quits the console. |

On the chips:

| Key | What it does |
|---|---|
| `→` or `←` | Moves to the next or the previous chip. |
| `Space` | Turns a go chip on or off, or puts a recipe into the order. A go on lets a session the stoker drives merge, release or deploy; off, that command waits for your `y`. A tap on a go chip turns it on or off too. |
| `Tab` or `Esc` | Goes back to the order line. Any other printable key goes back too, and is typed there. |
| `Enter` | Sends the order, as on the order line. |

## Typing a search

| Key | What it does |
|---|---|
| `Enter` | Keeps the search. |
| `Esc` | Cancels. |
| `Backspace` | Deletes the last character. |

## Hosts

| Key | What it does |
|---|---|
| `↑` or `k`, `↓` or `j` | Selects a host. |
| `Enter` | Opens the console of the host, or adds a host. |
| `x` | Removes a host from the list. matchblox stays on that host. |
| `y` | Confirms the remove or the connect. |
| `Esc` | Leaves the list of hosts to add. |
| `q` or `Ctrl+C` | Quits. |

## A phone

A tap selects a row. A tap on an agent, once it is selected, opens the
peek; a tap on an option of the peek picks it, a tap on a key of its foot
(`⏎ send`, `← prev`, `next →`, `esc`) presses it, and a tap outside it closes
it. On any other row the key line says `tap again: <command>`, and a
second tap on the same row runs it. On Ships, a tap on the selected ship
selects its session in Sessions. Any other action (a
recommendation, a setup step, a destructive step) then shows `RUN` and
waits for `Enter` or a typed `y`, as from the keys. A tap on a tab opens
it, a tap on a key of the key line presses it, and a tap on `● N wait for
you` selects the first agent that waits. A tap on the header's stoker word
(`stoker waits for you` among them) opens the stoker agent, as `Enter` on
its row does. A tap on `machine ›` or `usage ›`
switches the dock. On Panes, a tap on a column header sorts by it.
