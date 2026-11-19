<!-- ape {{VERSION}}: written by `ape init`, which rewrites it. Don't edit; project rules go in PROJECT.md. -->
# APE: Agentic Plaintext Engineering

`ape/` = this project's private docs + task board. Own git repo; the code repo ignores it. Present only; the past is
`git -C ape log` (a task's, done or dropped: `git -C ape log -p -- 'tasks/T12-*'`).

```
ape/
  _FRAMEWORK.md        this file (read-only)
  PROJECT.md           what the project is + how we work on it; read every session
  DECISIONS.md         numbered decisions (Dn): what, why, rejected. grep the ones you need, not front to back
  tasks/Tn-slug.md     one file per open task; deleted when done
  ref/                 optional: private reference not versioned with code: research, reports, outside specs
```

## Task

```yaml
---
kind: bug | feat | spike | chore | owner    # owner = only the owner can do it (a device, an account, an answer)
pri: 1 | 2 | 3                              # 1 now, 2 next, 3 later
status: new | triage | ready | blocked      # doing = its worktree exists (ape start); never written
area: <paths it may touch>                  # `.` = whole repo
excl: yes                                   # must run alone in its area (a scripted split, a rename, a format)
needs: owner | Tn [Tn…]                     # only while blocked; `ape done Tn` takes Tn out
from: <who>@<sha> | <tracker ref>          # where it came from, if outside ape (`web@3f2a9c1`, `gh#91`)
---
# <title>
<goal / what happened + why>. do: <steps, if not obvious>. done: <checks>.
## Ask        one question + `rec:`, one line ≤ ~110 chars (the board shows it); plain sentences: the owner reads it
## Progress   handoff: done, half-built + where, not started, measurements, failing; notes for other tasks
```
- ID `Tn` (T12): its file `tasks/T12-slug.md`, branch `ape/T12-slug`; cite as T12, like Dn. Numbers never reused.
  To the owner (reports, Asks, chat): ID + what it is, never bare: "T31 (`needs` can't hold owner + Tn)".
- A report (bug, request) = a `status: new` task: what happened, repro, workaround; triage makes it ready. Before
  `ape new`, grep ape/tasks for a duplicate.
- Labels via `ape new`, `ape set`; the `#` comments are docs, not allowed in files.

## CLI

```
ape                          board (below); READY in pick order: pri, then oldest
ape new <slug> [--title "…"] [k=v…] <<'EOF'   next ID; labels as set, status new unless given; body from stdin
                             (heredoc); refuses what CHECK would flag; one commit; prints the path
ape set <id> k=v...          labels: kind=bug pri=1 status=ready area="a/ b/" needs=owner|"T3 T5"; k= deletes.
                             needs: open tasks only, not itself, no loop
ape start <id>               branch ape/Tn-slug + worktree ../<repo>-wt/Tn-slug + ape/ linked in = the claim;
                             resumes a paused branch. Refuses: taken, not ready, needs set, kind owner
ape pause <id>               remove the worktree (refuses uncommitted changes); branch kept
ape done <id> "<summary>"    delete task, worktree, branch (must be merged); commit `done(Tn): <summary> (code
                             <sha>)`; summary = what, measured, left. Releases what needed it: Tn out of their
                             needs; none left + blocked → ready; prints `released: T7 T8`
ape drop <id> "<reason>"     delete task, worktree, branch; commit `drop(Tn): <reason>`; unmerged work → --force.
                             What needed it stays blocked; CHECK `T7: needs Tn, dropped` → point it elsewhere
ape commit -m "<msg>" <paths…>   commit exactly these ape/ files (new, edited, deleted); prints the sha. A path not
                             inside ape/ from the cwd is read as ape/-relative (`tasks/…`, `PROJECT.md`)
ape init                     in the main worktree root: creates ape/ + T1 (set it up from what the repo has),
                             ignores it, points AGENTS.md (+ CLAUDE.md) at it. Rerun: rewrites this file
```
- `<id>` = T12 or 12. Task bodies, docs: plain tools, then `ape commit`, never `git commit` (other agents commit in
  ape/ concurrently); set, done, drop first commit their task's own edits.
- ape/ commit messages: `type(scope): what`, scope = a task ID or a file: `progress(T4): auth half-built`,
  `docs(PROJECT): checks need Docker`. The CLI writes `new|set|edit|done|drop(Tn): …` itself.
- READY: `[clashes Tn]` = area overlaps a task with a worktree, one of the two `excl`; `[conflicts Tn: files]` = its
  paused branch and Tn's really conflict (git merge-tree); `[paused]` = branch kept by pause. DOING `[conflicts main:
  files]` = merging it into main conflicts now; `in regenerated files only` = only files the code repo's
  `.gitattributes` marks `linguist-generated` (regenerate them after merging). `[needs owner]` = no `## Ask` written
  yet. CHECK lines name their fix; the rest → Orchestrator 1.

```
$ ape
ape: 7 tasks · 2 owner · 1 doing · 1 new · 0 blocked · 3 ready · 1 done 24h
OWNER
  T3   owner p1  Approve the pricing page copy
       ? Ship the shorter copy? rec: yes; the long one tested worse.
  T7   feat  p2  Export to CSV   [needs owner]
DOING
  T4   feat  p1  OAuth login   [12m since commit; conflicts main: auth/session.go]
NEW
  T11  bug       Dark mode flickers on load   [from web@3f2a9c1]
READY
  T5   bug   p1  Session expires early
  T9   chore p2  Upgrade the test runner   [conflicts T4: go.mod; paused]
  T12  chore p3  Split auth/ into packages   [clashes T4]
DONE 24h
  T2   done  signup form + validation; e2e green (code 8c1d2e4)
CHECK
  ape/T31-foo: branch without a task
```

## Orchestrator

1. `ape`. CHECK: `quiet` → `ape pause Tn` only if no process runs in its worktree and nothing there changed in 3 h;
   a worktree or branch without a task, anything odd → owner.
2. Triage NEW: `ape set Tn status=triage` (the claim: a second one fails); rewrite the report as goal + `done:`, keep
   the repro; then rank (`ape set Tn kind=… pri=… area=… status=ready`), split (`ape new` per part, drop the
   original), merge (`ape drop Tn "dup of Tm"`), drop (reason), or ask (Owner).
3. Start the top READY tasks without clashes or conflicts (advice: start doesn't refuse them), in parallel if you
   can, one per agent: `ape start Tn` (ape/-only tasks too: start is the claim); brief "Tn" + its worktree. The task
   file is the brief.
4. Done reported → review; leftovers → `ape new`; `[conflicts main…]` → its agent rebases (regenerated only: you
   regenerate after merging); squash-merge into main, one commit per task: `type(scope): what`
   (Conventional Commits; scope = the domain: `feat(compiler): …`), why in the body, no `ape/` branch name
   (PROJECT.md may choose another merge or format); checks green; push if PROJECT.md says; `ape done Tn "<what,
   measured, left>"` (releases what needed it).
5. Hand-off reported → `ape pause Tn`.

Owner's "do Tn": you're orchestrator and agent; `ape start Tn`. Owner asks for work with no task: done this
session, and no task with a worktree (DOING, or OWNER marked doing) touches what it changes (judge by `area` and
what its agent is doing) → do it on main, no task; else `ape new`, then as "do Tn".

## Agent (on a task)

Read the task, PROJECT.md, what they cite, and DECISIONS.md's lines on its subject (grep). Work in its worktree
(PROJECT.md's per-worktree setup first). Edit only `area`, its own task file, new tasks; anything else, ape/
included → `ape new` (a report), don't fix. Commit on its branch; merging and `ape done` are the orchestrator's.
Hand-off: commit WIP on the branch, write `## Progress`, `ape commit` it. Report: done, checks run, left,
needs-owner.

## Owner

Ask (err toward asking): a lasting decision (one DECISIONS.md would hold) PROJECT.md doesn't delegate, or one it
reserves; their devices, accounts, money; work against DECISIONS.md or a public ADR, or bigger than the task's goal;
main red with no clear culprit; anything blocking ≥ 2 tasks. Not: choices that end with the task; bugs with a clear
fix (file them).
- Blocks the task → in its file: `## Ask`, `ape set Tn status=blocked needs=owner`.
- Else, or a second question → `ape new <slug> kind=owner pri=… status=ready`, body `## Ask` (Tm); a decision's
  `rec` holds meanwhile as a `prov:` Dn. Work goes on.
- Answered → where it takes effect (DECISIONS, tasks, ref/, what cites the task; a code-repo doc → a task); `## Ask`
  removed; `ape commit -m "owner(Tn or Tm): <what was decided>" <paths>`; then `ape set Tn status=ready needs=`, or
  `ape done Tm "<answer>"` (also when the owner did the work; releases the tasks that need Tm).

## Writing

- Present tense. Rewrite a fact where it lives; never dated entries, changelogs, "previously"; why it changed →
  commit message.
- One home per fact; link elsewhere.
- Docs about the code (spec, design, architecture) and rules for anyone working on it (build, checks, style:
  AGENTS.md) → the code repo, edited on the task's branch with the code it describes; ape/ isn't branched (one
  copy, live in every worktree). Decisions they record with their why stay there; DECISIONS.md links them and holds
  the rest. PROJECT.md (Architecture) says where they are.
- Terse, like this file: fragments, lists, `→ = ·`; no filler, hedges, recaps, emphasis, or what code, git or this
  file already say. Names, paths, numbers, commands, conditions exact; why only where not obvious (decisions:
  always); no abbreviations beyond this file's.
- Over ~60 lines (task) or ~300 (other file): delete history, or split by subject (a task: `ape new` per part),
  never by date.
- Private terms (PROJECT.md's list) and ape/ content (the pointer block aside) never reach the code repo, its commit
  messages or pushed branch names. Code may cite `Dn`, `Tn`; a public repo's code only public IDs (its issues,
  ADRs).

## Audit (when the owner asks)

Per file: apply Writing (tighten; history → deleted or present tense; duplicates → one home + links; oversized →
split by subject); spot-check DECISIONS and tasks against the code (wrong doc → fix; wrong code → `ape new`). Never
change a decision's substance or answer an Ask: ask instead. Report sizes before/after.
