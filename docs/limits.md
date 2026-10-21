# Limits

What does not work yet, and what to do until it does.

| Limit | What to do |
|---|---|
| The way back needs the console's own session | Run `matchblox` outside tmux. The tap on `◂ matchblox` also needs the tmux mouse: press `m` in the way back's preview. |
| The stoker compacts Claude Code only | Compact Codex, OpenCode and Cursor sessions yourself: Sessions marks them `! compact`. |
| The merge guard watches Claude Code only | A Codex, OpenCode or Cursor session has no hook for it. The STOKER tab says `no merge guard watches <session>`, with each go the run does not give it (`its merges, releases and deploys go unchecked`): give such a session none of them, or do them yourself. |
| The guard does not see what you type straight into a pane | Send from the console (`a`) or stop the stoker. |
| A rest trigger fires for Claude Code only | `matchblox stoker when --rest` waits for the end of a turn, which only Claude Code's transcript tells. For a Codex or OpenCode session, the stoker sets a ship trigger or waits for its wake. |
| The merge guard is a seatbelt, not a sandbox | It keeps an agent that acts in good faith on the stoker's messages to the order: the ways an honest agent writes `gh pr merge`, `gh release` and `gh workflow run` are caught, and so is any line in which `gh` comes before a guarded word (`merge`, `release`, `workflow`, `api`, `graphql`, `mergePullRequest`). An agent that hides the command on purpose goes past it: a variable, `eval`, an alias of the shell or of `gh` (`gh alias set`), a function, a script, brace or glob expansion (`{gh,} pr merge`, `/usr/bin/g[h]`), a `GH` that a case-insensitive file system runs as `gh`, or a here-doc or `$((…))` that the guard reads apart from the shell. Protect your branches on GitHub, and give the order only the goes you mean. |
| Who acts is the pane a process runs in | The service finds the pane of each `matchblox` command and each hook from its pid's parents, and the guard trusts the `TMUX_PANE` of the agent's own process. A process that leaves its pane's process tree (`setsid`, a daemon, a console client left running in the background) is in no pane, and its acts count as yours. An agent the stoker starts with `TMUX_PANE` unset or changed is guarded as the pane it names, or not at all. A hook that reaches the spool, not the service, is taken without that check. |
| A new pane of the stoker's session is guarded by tmux's answer | The guard asks tmux for the session of a pane the run does not list yet. When tmux is too slow to answer, the call goes through, so a busy machine never denies in your own panes. The next sample lists the new pane, about 2 seconds later. |
| A here-document piped on in some spellings reads as text | The guard reads a here-document fed to `cat` or `tee` as text, and one fed to a shell as commands. When the command the pipe feeds comes on the line after the body (`cat <<EOF \|`, the body, `EOF`, then `bash`), when the here-document sits in a group (`(cat <<EOF …) \| bash`, `{ cat <<EOF …; } \| bash`), or when its lines go to `while read l; do eval "$l"; done`, the guard reads them as text, and a merge in them goes through. Feed a shell its script directly (`bash <<'EOF'`), and protect your branches on GitHub. |
| The go for a bare merge goes by the branch the shell is on | A `gh pr merge` with no number merges the pull request of the branch checked out where it runs. After a `cd` or a checkout of another PR (`gh pr checkout 8 && gh pr merge`), that is another pull request, and the go lets it through. Tell a session to merge its pull request by number. A number after a quoted flag (`--subject "feat: a b" 7`) reads as a branch and is denied: put the number first. |
| A merge go names a number, not a repository | A go you gave with `y` for `gh pr merge 7` also covers `gh pr merge 7 -R other/repo` and a pull request URL of another repository. Give a merge's go only where the session works on one repository, and protect your branches on GitHub. |
| Two calls at once can share one go | Each guarded command uses up one go. Two tool calls that reach the guard together, before the service records the first, can both pass on it. The card shows each guarded command that ran. |
| A go you gave with `y` lasts until it is used | A go that nothing used yet stays in its pane until the run ends. Give the go when the session is about to run the command, and stop the stoker to take back one you no longer want. |
| Triggers need the stoker agent running | A trigger's hand runs as an act from the stoker's own pane. With no stoker agent in it, each trigger that fires logs `failed` and does nothing. Keep the stoker pane's agent running while triggers wait. |
| Codex context use needs Linux | On macOS, Codex shows "not measured". |
| Context use for Claude Code, Codex, OpenCode and Cursor only | Other agents show "not measured", and still show in Sessions. Cursor's needs the door "Show Cursor's context". |
| A Claude model that matchblox does not know has no window | Without the status line, a new model shows "not measured" until matchblox knows it. Open the door "Show your limits" in Sessions: the status line then gives each session its window. Or set the model in `[sessions.windows]`. |
| Waits of agents other than Claude Code and Cursor come from their pane | They wait for you in Sessions once their pane is idle and its last lines ask. Cursor's approval menu is read from its pane too. |
| Plan limits for Claude Code, Codex and Cursor only | OpenCode's usage shows "not measured". Cursor's needs `[limits] cursor_usage = true`, and macOS: it is read with the token in the Keychain. |
| Ships reads GitHub only | A pull request on another forge does not show. |
| No week of Opus or Sonnet in usage | Claude Code's status line gives the 5-hour window and the week only. |
| tmux only | Run your agents in tmux. |
| New session starts Claude Code, Codex, OpenCode and Cursor only | Start another agent in tmux yourself: Sessions shows it all the same. |
| New session finds an agent on the service's `PATH` | An agent that is a shell alias, or on a `PATH` only your shell sets, is not offered. Put it on the `PATH` the service runs with, or start it yourself. |
| Rename names a Codex, OpenCode or Cursor agent in tmux only | They keep no name of their own: Sessions shows them by their tmux window. Claude Code takes `/rename` at rest, so a busy session is renamed when it rests. |
| No native Windows | Use Linux or macOS. |
| One host on a screen | `esc` goes back to Hosts. |

## Soon

- **Start a session in a container**
  ([#10](https://github.com/blox-eng/matchblox/issues/10)), and **on the
  account with room** ([#55](https://github.com/blox-eng/matchblox/issues/55)).
