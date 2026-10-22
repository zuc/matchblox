# Ships

A ship is a pull request one of your sessions drives. Ships follows each
one from open to live, so you see on one screen which PR runs, which waits
for you and which is stuck, and why.

## Which pull requests

A session drives a pull request when its own transcript shows it, the
strongest first:

1. it created it (`gh pr create` and the PR's URL);
2. it pushed its branch, and an open PR has that branch as its head;
3. it acted on it with `gh pr merge`, `checks`, `view`, `edit`, `ready` or
   `comment`, naming it by number or by URL.

A PR named only in what an agent wrote does not count, and neither does a
PR's URL anywhere else in a command: in `curl`, `echo`, `gh api`, a
commit message or a PR's body it is a mention. One PR belongs to
the session with the strongest, then the newest evidence. The open PR of a
session's worktree counts too. Each transcript is read whole once, then
only what it adds.

## The track

```
 #4551 Rate limits on the public API     4m
   ●━◉┄○┄○┄○  CI · 6 of 9 jobs
```

The five stages are `open → ci → green → merged → live`. A done stage is
`●`, the current one `◉`: `warn` while it runs on its own, the accent when
it waits for you, and `✕` in `neg` when it is stuck. The note says the
state in words:

| Note | It means |
|---|---|
| `CI · 6 of 9 jobs · 12m` | The checks run, for 12 minutes so far. |
| `CI failed · <job>` | A check failed. |
| `conflicts with main`, `behind main` | It cannot merge as it is. |
| `draft` | It is a draft. |
| `waits for review`, `changes requested` | A review holds it. |
| `waits for a required check` | Its checks passed, but a branch rule still blocks the merge. |
| `checking mergeability` | GitHub has not worked out yet whether it merges. |
| `green · waits for your merge` | Every check passed: `x` merges it. |
| `auto-merge on` | It merges by itself. |
| `merged · deploying · 2h` | Merged, and a run on the merge commit has been going for 2 hours. |
| `merged · deploy queued · 1h` | Merged, and the runs on the merge commit have waited an hour to start. |
| `live` | Every run on the merge commit finished, and none failed. |
| `state unknown · <why>` | GitHub could not be read, or not lately. Nothing is guessed. |

A failed, cancelled or skipped run counts when a newer run of the same
workflow on the default branch passed: the default branch is healthy, and a
newer deploy carried the change. A ship that
went live leaves the list at the end of that day.

To count only the workflows that deploy a repository, name them in the
[config](config.md):

```toml
[ships.deploy]
"owner/name" = ["Release"]
```

## Keys

| Key | What it does |
|---|---|
| `Enter` | Selects the session that drives the ship, in Sessions. |
| `x` | Merges a ship that waits for your merge: a box shows `gh pr merge <n> --squash --match-head-commit <sha>` (the repository's merge method, pinned to the commit that was read green), and a typed `y` runs it. It is offered only on a read from the last two minutes. |
| `a` | Opens [the answer peek](console.md#the-answer-peek) on the session of a stuck ship, to tell it what to do. |

On a phone the selected ship adds who drives it and its keys. Wider, the
detail beside the list shows the five stages with their times, and the
PR's URL. A session's row and detail show its ship too.

## Reading GitHub

The service reads GitHub with `gh`, one query per repository a minute, and
only while a ship is in flight. It shares GitHub's rate limit with every
agent on the machine: when less than a tenth is left it stops until the
limit resets, and the tab says until when. Without `gh`, or with `gh` not
signed in, the ships still list, with `state unknown · gh not signed in`.
A repository that cannot be read is asked again after a minute, then less
and less often, up to every 30 minutes; a PR that was never read leaves
the list after a day.

What the service found, and how far it read each transcript, is kept in
`~/.local/state/matchblox/ships.json`, so a restart reads only what is new.
