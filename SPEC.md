# ape: implementation spec

The user-facing contract is [framework/_FRAMEWORK.md](framework/_FRAMEWORK.md). This file pins what it leaves open. Go 1.26, stdlib only, Linux + macOS (flock).

## Terms

- **main root**: the code repo's main worktree: `dirname(git rev-parse --path-format=absolute --git-common-dir)` when that ends in `/.git`.
- **ape dir**: `<main root>/ape`, its own git repo.
- **task**: `ape/tasks/Tn-slug.md`. ID `Tn` = `T` + the number in decimal, no padding (`T12`, `T12345`); slug = `[a-z0-9]+(-[a-z0-9]+)*`. Branch `ape/Tn-slug`; worktree `<dirname(main root)>/<basename(main root)>-wt/Tn-slug`.
- **ID as typed** (command arguments, `needs` tokens): `T12`, `t12` or `12`, leading zeros allowed. ape always writes `Tn` (set rewrites needs tokens to it).

## Finding the ape dir (every command but init)

1. `$APE_DIR` if set (must contain `_FRAMEWORK.md`).
2. Walk up from cwd; at each dir D: if `D/_FRAMEWORK.md` and `D/tasks` or `D/.git` exist and D's basename is `ape`, it's D; else if `D/ape/_FRAMEWORK.md` exists, it's `D/ape`. Resolve symlinks (worktrees link `ape` to the main root's).
3. Else exit 3: `ape: no ape/ found here or above; run 'ape init' in the repo root`.

main root (from git, as `ape` may link outside the repo): found as `D/ape`, D's main root; found as D itself (inside ape/), the main root of D's parent; both as given, unresolved, falling back to that dir when it isn't in a repo. With `$APE_DIR`: cwd's main root, or outside a repo the ape dir's parent.

## Task file

```
---
key: value
...
---
# Title
body...
```

- Lines may end `\r\n` (the `\r` isn't part of a value; a line ape adds to such a header ends `\r\n` too).
- A well-named file that doesn't parse is still Tn (it counts, holds its number, commands find it): title `(unreadable)`, no labels (so NEW, or DOING with a worktree), the parse error its only CHECK problem; ape never rewrites it.
- Header: lines `key: value` (one line each, value trimmed, may be empty or contain spaces). Comments (`#` after the value) are not supported in files; the YAML in `_FRAMEWORK.md` is documentation.
- Known keys, in this order when ape writes them: `kind pri status area excl needs from`. Unknown keys are kept, in place, and reported by CHECK.
- Title = first line starting `#` after the header. Ask = first non-empty line after a `## Ask` line.
- `ape` rewrites only header lines; the body is preserved byte for byte.
- Values: kind ∈ {bug, feat, spike, chore, owner}; pri ∈ {1,2,3}; status ∈ {new, triage, ready, blocked}; excl ∈ {yes}; needs = `owner` alone, or IDs as typed, space-separated; area = space-separated tokens.

Validation problems (shown in CHECK, never fatal for the board): missing kind (only on status ready or blocked: triage sets it); invalid kind; invalid pri; invalid excl; missing status; status ∉ set; ready (or blocked on a task) without pri, or without area unless kind owner; blocked without needs; needs on a non-blocked task; needs with its own number; unknown key; no title; bad filename; duplicate number.

## Task's branch and worktree

Found by number, whatever the task's slug is now (it may have been renamed since start), the same way by the board and every command (`board.Resolve`):

- its **branches**: local `ape/Tn-*` with its number. More than one: CHECK `Tn: several branches: a, b; delete the stale one`; start, pause, done, drop exit 2 `Tn has several branches: a, b; delete the stale one`.
- its **worktrees**: those on one of its branches (by number, as above), and those in `<main root>-wt/` named `Tn-*` with its number that aren't on an `ape/Tn-*` branch (detached mid-rebase, say).
- A worktree whose directory was deleted by hand (git: prunable) doesn't count as there: CHECK `Tn: worktree <path> missing; ape pause Tn prunes it`; start runs `git worktree prune` first; pause, done, drop prune it.
- A worktree can go (pause, done, drop) only if it isn't the main worktree (2: `<branch> is checked out in the main worktree <path>; check out another branch there first`), isn't locked (2: `worktree <path> is locked; git worktree unlock it`, even with --force), and, unless drop --force: a detached HEAD is merged (as in done) into the branch (pause) or the main root's HEAD (done, drop) (2: `worktree <path> is detached at commits not in <it>; finish the rebase (or check out its branch), then <command>`), and `git status --porcelain --untracked-files=normal --ignore-submodules=none` lists nothing but the `ape` link (2: `worktree <path> has uncommitted changes; commit or stash, then <command>`), whatever the repo's config says. Everything is checked before anything is removed.

## State, per task (exactly one column)

1. **DOING**: it has a worktree (above), unless `needs: owner` and kind ≠ owner (its agent waits on the owner).
2. **OWNER**: `needs: owner` or `kind: owner`.
3. **NEW**: status new or triage (triage shown as `[triage]`).
4. **BLOCKED**: status blocked and `needs: Tn [Tm…]`, the tasks existing or not (gone: marked and in CHECK).
5. **READY**: everything else with status ready.

Area overlap: tokens a, b (trailing `/` stripped) overlap if a == b, one starts with the other + `/`, or either is `.` (the whole repo).

A READY task against each task with a worktree (DOING, or OWNER marked `doing`), W:

- **clash**: their areas overlap and either has `excl: yes`.
- else **conflict**: the READY task has a branch (paused) and `git merge-tree --write-tree <its branch> <W's worktree HEAD>` conflicts, in at least one file not `linguist-generated` (below).
- else nothing: overlapping areas alone predict few real conflicts.

A DOING task with commits of its own (beyond the main root's HEAD) is checked against main: `git merge-tree --write-tree <main root HEAD> <its worktree HEAD>`. Regenerated files: those `git check-attr linguist-generated` (from the main root's working tree) reports `set` or `true`. Merges run in parallel (≤ 8 at once); nothing is written. Clashes and conflicts are advice: start never refuses them.

## Board output (`ape`)

Exactly the format in `_FRAMEWORK.md`'s example:

- Line 1: `ape: N tasks · O owner · D doing · W new · B blocked · R ready · K done 24h` (counts in section order).
- Then sections in order OWNER, DOING, NEW, BLOCKED, READY, DONE 24h, CHECK; an empty section is omitted.
- Task line: two spaces, ID left-aligned width 4, space, kind width 5, space, `pN` (or two spaces if no pri), two spaces, title, then marks `[..]` joined by `;` inside one pair of brackets. Ask line: 7 spaces, `?`, ask.
- Ask and DONE lines longer than 120 columns (runes, leading spaces included) are cut to 120: the first 119, trailing spaces dropped, then `…`. Task and CHECK lines are never cut.
- Marks: OWNER: `needs owner` when it has no Ask line, and none when it does, then `doing` with a worktree; DOING: `Xm|Xh|Xd since commit` (from the last commit of its worktree's HEAD; `no commits` if none beyond the main root's HEAD), then `conflicts <main root's branch, or HEAD>: <files>` or, all regenerated, `conflicts <branch> in regenerated files only: <files>`; NEW: `triage`, `from <from>`; BLOCKED: `needs Tn Tm` listing the tasks that still exist, or, none left, all of them: `needs Tn Tm (gone)`; READY: `clashes Tn, Tm`, then `conflicts Tn: <files>` per task (by number), then `paused`. `<files>`: sorted, `,`-joined, the first 3 then `, +N more`.
- Order within every section (OWNER included): pri ascending (none last), then number ascending (oldest first).
- DONE 24h: commits in the ape repo from the last 24 h whose subject is `done(<ID>): <text>` or `drop(<ID>): <text>` (ID read as typed): two spaces, ID width 4, space, `done`/`drop` width 5, space, text. Newest first.
- CHECK lines (two spaces each):
  - `Tn: <problem>`
  - `ape/Tn-slug: worktree without a task`
  - `ape/Tn-slug: branch without a task`
  - for a task with a worktree (OWNER included), `Tn: quiet 4h (worktree, no commit)` 3 h after its last own commit, or with none `Tn: quiet 4h (no commits since start)` 3 h after the worktree's creation (the mtime of `commondir` in its git dir)
  - the branch and worktree lines of "Task's branch and worktree"
  - per task a blocked task needs that's gone and whose newest `done(Tm)`/`drop(Tm)` commit in the ape repo (ID as typed; one `git log --grep` for all of them) is a drop: `Tn: needs Tm, dropped; ape set Tn needs=… (what replaces it), or drop Tn`; every task it needs gone, none dropped: `Tn: needs Tm Tk: all done; ape set Tn status=ready needs=`
  - `_FRAMEWORK.md: outdated; run ape init`
- Exit 0 even with CHECK lines. No colour.

## Writes

- Every command except the board takes an exclusive `flock` on `<ape dir>/.git/ape.lock` for its whole run (blocking, 30 s timeout → exit 2 `ape: lock held >30s: <path>`).
- Commit messages ape writes: `<verb>(Tn): <text>` (Conventional Commits, the task's ID as scope; each command below gives its own), and `init: …`, `framework: …`.
- Each write command ends with one commit in the ape repo of exactly its paths, never through the shared index (a concurrent `git commit -- <path>` writes back a stale copy of it): in a private index (`GIT_INDEX_FILE`), `read-tree HEAD` (none on an unborn branch), `update-index --add --remove -- paths`, `write-tree`; same tree as HEAD's → no commit; else `commit-tree -p HEAD` (hooks don't run) and `update-ref HEAD <new> <old>` (old empty if unborn). HEAD moved or a ref locked (`cannot lock ref`) → start over, up to ~15 s, 50–250 ms apart with jitter. Then `update-index --add --remove -- paths` in the shared index, best effort (retried on `index.lock`), so `git status` is clean.
- Files ape rewrites (tasks, `.next`) are written atomically: temp file in the same dir, fsync, rename.
- Arguments: one is a flag only if it names one of the command's flags (`-k`, `--k`, `--k=v`) or is `-h`/`--help`; anything else, `-20% p95` included, is a positional. `--` ends flags.
- Errors: one line on stderr `ape: <what>; <what to do>` (unknown command: `ape: unknown command "x"; run 'ape help'`); an unforeseen one (git's, the file system's) as `ape: <its lines joined by "; ">`, exit 1. git runs without `GIT_DIR`, `GIT_WORK_TREE`, `GIT_INDEX_FILE`, `GIT_COMMON_DIR`, `GIT_OBJECT_DIRECTORY` from the environment (a git hook may run ape).
- Exit codes: 1 usage/validation, 2 conflict (taken, dirty, unmerged, lock), 3 not found.
- done, drop and set commit the task's own uncommitted edits (tracked or untracked; that file only: other dirty files in ape/ stay so) on their own, as above, as `edit(Tn): hand edits` (nothing pending → no commit), once their checks pass, then make their own commit. A refusal by those checks commits nothing.
- Success: commands print a few lines, terse, starting with the task's path or the worktree path.

## Commands

### new

`ape new <slug> [--kind k] [--from who@sha] [--title "…"]`

Validate slug. Under the lock: number = `.next` (a file in the ape dir holding one integer; if missing, 1 + the max number in tasks/ and in every `tasks/*` path in the ape repo's history, a name's leading ID read as typed). Write `tasks/Tn-slug.md` with header `kind` (if given), `status: new`, `from` (if given), then `# <title or slug with dashes as spaces>`, blank line. Write `.next` = number+1. Commit `new(Tn): <title>`. Print the task path.

### set

`ape set <id> k=v…`

Keys must be known (else exit 1). Task unreadable: exit 1 `Tn is unreadable: <err>; fix the file by hand`. `k=` deletes k; a value's whitespace collapses to single spaces (`needs="3  5"` → `3 5`). `status=triage` on a task already in triage: exit 2 `Tn is already in triage`, nothing written (the claim others skip). Apply, then validate the task's status, kind, pri, excl, needs values (exit 1 on invalid, nothing written). Setting needs to task IDs also checks each against tasks/ (nothing written on refusal): the task itself (1 `Tn can't need itself`); not in tasks/: its newest `done(Tm)`/`drop(Tm)` commit says which (1 `Tm is done; nothing to wait for`, 1 `Tm was dropped; need what replaces it`), none (3 `no Tm in <tasks dir>`); a loop through other tasks' needs (1 `needs would loop: Tn → Tm → … → Tn; drop one of them`). Commit the task's pending edits (`edit(Tn): hand edits`; a hand edit landing between the read and this commit goes into it). Right before writing, re-read the file: changed since read (a hand edit) → exit 2 `Tn changed while ape set ran; run it again`. Commit `set(Tn): k=v …`. Print the task's new header line by line.

### start

`ape start <id>`

Task must exist (3), not be DOING (2: `taken: worktree <path>`; its branch checked out anywhere, the main worktree included, is DOING), have no `needs` (1: `Tn needs X; once that's resolved: ape set Tn status=ready needs=`), have status ready (1: `Tn is <status>; triage it`), kind ≠ owner (1). If it has a branch (any slug): `git worktree add <path> <branch>`, path named after the branch; else `git worktree add -b ape/Tn-slug <path> <base>`, base = the main root's `HEAD` (none: 1 `code repo has no commits; commit first`, nothing created). Then symlink `<path>/ape` → the ape dir (absolute). No commit (nothing in ape/ changes). Print `<path>` and `branch <branch>`; if git doesn't ignore the link there, also `warning: ape/ not ignored by the code repo; run ape init in the main worktree`.

### pause

`ape pause <id>`

Task must have a worktree, missing ones included (else 1 `Tn has no worktree; nothing to pause`). Its worktrees must be able to go (see "Task's branch and worktree"); then `git worktree remove <path>` each (prune a missing one). Branch kept. No commit. Print `paused Tn; branch <branch> kept`.

### done

`ape done <id> "<summary>"`

Task must exist. If it has a branch: the branch's commit must be merged into the main root's HEAD: an ancestor of it, or merging it in changes nothing (`git merge-tree --write-tree HEAD <commit>` clean and = HEAD's tree: squash and forge merges; a conflict isn't merged) (else 2 `<branch> not merged into <branch of main root>; merge first`); its worktrees must be able to go. Then remove them, and delete the branch at the commit checked (`update-ref -d`, not `git branch -d`, which also compares with the upstream). Commit the task's pending edits (`edit(Tn): hand edits`), then delete the task file. Release: each other readable task whose needs names Tn gets its own pending edits committed (as above), Tn taken out of needs (none left: needs deleted, and status blocked → ready); unreadable ones are left alone. Commit `done(Tn): <summary> (code <short main HEAD>)` (without a branch, omit `(code …)`) of the task's deletion and those edits, one commit. Print, after the usual lines, `released: Tn Tm` naming the tasks made ready, if any.

### drop

`ape drop <id> "<reason>" [--force]`

Task must exist. If its branch isn't merged into the main root's HEAD (as in done): exit 2 unless --force; with --force remove its worktrees (`--force`; a locked one still refuses). Else as done does. The branch goes at the commit checked. Commit the task's pending edits, then delete the task. Commit `drop(Tn): <reason>`. No release: tasks that need it stay as they are (dropped work never landed; the board's CHECK names them); print `still needed by Tn Tm: ape set their needs to what replaces it, or drop them` if any.

### commit

`ape commit -m "<msg>" <paths…>`

Paths are absolute or relative to the cwd; a relative one that from the cwd isn't an existing path inside the ape dir (missing, or outside it) is relative to the ape dir if `<ape dir>/<path>` exists or is a file in the ape repo's HEAD (agents in a worktree name `tasks/…`; from the repo root `DECISIONS.md` is ape/'s even when the code repo has one too); else it stays the cwd's. Each must resolve (through symlinks, e.g. a worktree's `ape`) to a file, symlink or deleted path inside the ape dir, not in `.git` (else exit 1, nothing committed). Under the lock, commit exactly those paths with msg as above. Print the short sha, or `nothing to commit` (exit 0) if they match HEAD. Agents commit hand edits in ape/ this way, never with `git commit`.

### init

`ape init`

Run in a code repo's main worktree root (`git rev-parse --show-toplevel` = main root; else exit 1).

- No `ape/`: create it, `git init`, write `_FRAMEWORK.md` (embedded, `{{VERSION}}` filled, mode 0444), `PROJECT.md` and `DECISIONS.md` (embedded templates), `tasks/.keep`, T1 `tasks/T1-setup-ape.md` (templates/setup-task.md: set up ape/ from what the repo has), `.next` = 2; commit `init: set up ape/`.
- `ape/` exists: rewrite `_FRAMEWORK.md` if it differs (chmod u+w, write, 0444); commit `framework: <version>`.
- Ignore: add a line `/ape` to `<common dir>/info/exclude` if absent (create `info/` and the file if missing; print it). It covers every worktree and needs no commit, so a worktree started before anything is committed ignores its `ape` link. `.gitignore` is never touched (an existing `/ape` line there stays).
- Code repo (not committed; print what changed, then a reminder to commit them while they're uncommitted): ensure `AGENTS.md` starts with the pointer block (templates/agents-pointer.md); prepend it if absent, create the file if missing. Same for `CLAUDE.md` and `.claude/CLAUDE.md` if present and not already bringing in `AGENTS.md` (a symlink to it, or a line `@<relative path to AGENTS.md>`), never created (Claude Code reads them instead of `AGENTS.md`). The reminder names the uncommitted ones: `commit a, b and c in the code repo: worktrees branch from HEAD`.
- After an init that created ape/'s first commit, last line: `next: T1 (ape/tasks/T1-setup-ape.md): move what's only yours into ape/, shared rules stay; read it before starting`.

## Tests

- Unit tests per package; golden tests for board rendering (`testdata/*.golden`, `-update` flag).
- Integration tests build the binary once (`TestMain`) and run it in temp git repos (`t.TempDir()`).
- Race test: 20 concurrent `ape new` processes → 20 distinct numbers, 20 commits, `.next` = 21; 5 concurrent `ape start` of one task → exactly one exit 0, four exit 2; 20 concurrent `set` on 20 tasks → 20 commits, all labels applied; 10 `set` (5 on hand-edited tasks) racing 10 `ape commit` of hand edits (tracked and new files) → 25 commits, each `edit` before its `set`.
